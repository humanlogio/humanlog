package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	connectcors "connectrpc.com/cors"
	otelconnect "connectrpc.com/otelconnect"
	"github.com/blang/semver"
	"github.com/humanlogio/api/go/svc/auth/v1/authv1connect"
	cliupdatepb "github.com/humanlogio/api/go/svc/cliupdate/v1"
	"github.com/humanlogio/api/go/svc/cliupdate/v1/cliupdatev1connect"
	"github.com/humanlogio/api/go/svc/feature/v1/featurev1connect"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	"github.com/humanlogio/api/go/svc/user/v1/userv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/localproject"
	"github.com/humanlogio/humanlog/internal/localserver"
	"github.com/humanlogio/humanlog/internal/localstate"
	"github.com/humanlogio/humanlog/internal/pkg/absfs"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/selfupdate"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	ksvc "github.com/kardianos/service"
	"github.com/rs/cors"
	"github.com/urfave/cli"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/contrib/propagators/ot"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"golang.org/x/sync/errgroup"

	// imported for side-effect of `init()` registration
	_ "github.com/humanlogio/humanlog/internal/diskstorage"
)

const (
	serviceCmdName = "service"
)

func serviceCmd(
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getBaseSiteURL func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
	getConnectOpts func(*cli.Context) []connect.ClientOption,
) cli.Command {
	var svcHandler *serviceHandler
	return cli.Command{
		Hidden:    hideUnreleasedFeatures == "true",
		Name:      serviceCmdName,
		ShortName: "svc",
		Usage:     "Run humanlog as a background service, with a systray and all.",
		Before: func(cctx *cli.Context) error {
			var err error
			svcHandler, err = prepareServiceCmd(cctx,
				getCtx,
				getLogger,
				getCfg,
				getState,
				getTokenSource,
				getAPIUrl,
				getBaseSiteURL,
				getHTTPClient,
			)
			return err
		},
		After: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()

			if svcHandler == nil {
				return nil // service was never started
			}
			return svcHandler.close(ctx)
		},
		Subcommands: []cli.Command{
			{
				Name: "install",
				Action: func(cctx *cli.Context) error {
					return svcHandler.Install()
				},
			},
			{
				Name: "uninstall",
				Action: func(cctx *cli.Context) error {
					return svcHandler.Uninstall()
				},
			},
			{
				Name: "reinstall",
				Action: func(cctx *cli.Context) error {
					if err := svcHandler.Uninstall(); err != nil {
						logerror("will install, but couldn't uninstall first: %v", err)
					}
					return svcHandler.Install()
				},
			},
			{
				Name: "start",
				Action: func(cctx *cli.Context) error {
					return svcHandler.Start(svcHandler.ctx)
				},
			},
			{
				Name: "stop",
				Action: func(cctx *cli.Context) error {
					return svcHandler.Stop(svcHandler.ctx)
				},
			},
			{
				Name: "restart",
				Action: func(cctx *cli.Context) error {
					if err := svcHandler.Stop(svcHandler.ctx); err != nil {
						logwarn("failed to stop: %v", err)
					}
					return svcHandler.Start(svcHandler.ctx)
				},
			},
			{
				Name: "run",
				Action: func(cctx *cli.Context) error {
					// don't use `svc.Run` because it doesn't do anything useful
					// and it prevents control over running systray running on the
					// main thread, which it requires.
					ctx := getCtx(cctx)
					cfg := getCfg(cctx)
					ll := getLogger(cctx)
					baseSiteURL := getBaseSiteURL(cctx)
					ll.InfoContext(ctx, "service preparing to start")
					ctx, cancel := context.WithCancel(ctx)
					defer cancel()

					eg, ctx := errgroup.WithContext(ctx)

					eg.Go(func() error {
						err := svcHandler.run(ctx, cancel)
						if err != nil {
							ll.ErrorContext(ctx, "service stopped running with an error", slog.Any("err", err))
							return err
						}
						ll.InfoContext(ctx, "service stopped running without problems")
						return err
					})

					go func() {
						defer cancel()
						ll.InfoContext(ctx, "service started, all command groups are on")
						if err := eg.Wait(); err != nil {
							ll.ErrorContext(ctx, "service command group had an error", slog.Any("err", err))

						} else {
							ll.InfoContext(ctx, "service command group is done")
						}
						// schedule a hard kill in 1s if something is blocking
						go time.AfterFunc(time.Second, func() {
							ll.ErrorContext(ctx, "service shutdown stuck, resorting to hard exit. sorry fam")
							os.Exit(1)
						})
					}()

					expcfg := cfg.GetRuntime().GetExperimentalFeatures()
					if expcfg != nil && expcfg.ServeLocalhost != nil && expcfg.ServeLocalhost.ShowInSystray != nil && *expcfg.ServeLocalhost.ShowInSystray {
						trayll := ll.WithGroup("systray")
						if err := runSystray(ctx, trayll, svcHandler, version, baseSiteURL); err != nil {
							trayll.ErrorContext(ctx, "systray stopped in error", slog.Any("err", err))
							cancel()
						}
					} else {
						// wait for cancellation
						<-ctx.Done()
					}

					return nil
				},
			},
		},
	}
}

func prepareServiceCmd(
	cctx *cli.Context,
	getCtx func(cctx *cli.Context) context.Context,
	getLogger func(cctx *cli.Context) *slog.Logger,
	getCfg func(cctx *cli.Context) *config.Config,
	getState func(cctx *cli.Context) *state.State,
	getTokenSource func(cctx *cli.Context) *auth.UserRefreshableTokenSource,
	getAPIUrl func(cctx *cli.Context) string,
	getBaseSiteURL func(cctx *cli.Context) string,
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
) (
	svcHandler *serviceHandler,
	err error,
) {
	u, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("looking up current user: %v", err)
	}
	ctx := getCtx(cctx)
	ll := getLogger(cctx)
	config := getCfg(cctx)
	state := getState(cctx)
	tokenSource := getTokenSource(cctx)
	apiURL := getAPIUrl(cctx)
	baseSiteURL := getBaseSiteURL(cctx)
	httpClient := getHTTPClient(cctx, apiURL)

	authCheckFrequency := time.Minute
	updateCheckFrequency := time.Hour

	doneOtel := func(ctx context.Context) error { return nil }
	expcfg := config.GetRuntime().GetExperimentalFeatures()
	if expcfg != nil {
		// check for updates more often if you use
		// experimental features
		updateCheckFrequency = 10 * time.Minute
		if expcfg.ReleaseChannel != nil {
			// and even more frequently if using a non-default channel
			updateCheckFrequency = time.Minute
		}

		shouldEmitOtel := expcfg.GetServeLocalhost().GetOtlp() != nil
		isDevMode := expcfg.GetReleaseChannel() == "dev"
		if shouldEmitOtel && isDevMode {
			ll.DebugContext(ctx, "setting up self-monitoring with otel")
			doneOtel, err = setupOtel(ctx, ll)
			if err != nil {
				ll.ErrorContext(ctx, "can't setup self-monitoring with otel", slog.Any("err", err))
			}
		}
	}

	otelIctpr, err := otelconnect.NewInterceptor()
	if err != nil {
		doneOtel(ctx)
		return nil, fmt.Errorf("can't create otel interceptors for clients: %v", err)
	}
	baseIcptrs := []connect.Interceptor{otelIctpr}

	publicIcptrs := append(baseIcptrs, auth.NewRefreshedUserAuthInterceptor(ll, tokenSource))
	authedIcptrs := append(baseIcptrs, auth.Interceptors(ll, tokenSource)...)

	publicClOpts := connect.WithInterceptors(publicIcptrs...)
	authedClOpts := connect.WithInterceptors(authedIcptrs...)

	svcCfg := &ksvc.Config{
		Name:        "io.humanlog.humanlogd",
		DisplayName: "humanlog.io",
		Description: "humanlog runs a service on your machine so that you can send it data and then query it back",
		UserName:    u.Name,
		Arguments:   []string{serviceCmdName, "run"},
		Option: ksvc.KeyValue{
			// darwin stuff
			"KeepAlive":     true,
			"RunAtLoad":     true,
			"UserService":   true,
			"SessionCreate": true,
		},
	}
	svcHandler, err = newServiceHandler(
		ctx,
		ll,
		config,
		state,
		svcCfg,
		baseSiteURL,
		tokenSource,
		authCheckFrequency,
		updateCheckFrequency,
		cliupdatev1connect.NewUpdateServiceClient(httpClient, apiURL, publicClOpts),
		authv1connect.NewAuthServiceClient(httpClient, apiURL, publicClOpts),
		userv1connect.NewUserServiceClient(httpClient, apiURL, authedClOpts),
		featurev1connect.NewFeatureServiceClient(httpClient, apiURL, authedClOpts),
		doneOtel,
	)
	if err != nil {
		return nil, fmt.Errorf("preparing service: %v", err)
	}
	return svcHandler, nil
}

type systrayClient interface {
	NotifyError(ctx context.Context, err error) error
	NotifyUnauthenticated(ctx context.Context) error
	NotifyAuthenticated(ctx context.Context, user *typesv1.User, defaultOrg, currentOrg *typesv1.Organization) error
	NotifyUpdateAvailable(ctx context.Context, oldV, newV *typesv1.Version) error
	NotifyAlert(ctx context.Context, ar *typesv1.AlertRule, as *typesv1.AlertState, o *typesv1.Obj) error
}

type serviceClient interface {
	DoLogout(ctx context.Context, returnToURL string) error
	DoLogin(ctx context.Context, returnToURL string) error
	DoUpdate(ctx context.Context) error
	DoRestart(ctx context.Context) error
	CheckUpdate(ctx context.Context) error

	LastUpdateCheck(ctx context.Context) (*timestamppb.Timestamp, error)
	CurrentConfig(ctx context.Context) (*config.CurrentConfig, error)
}

var _ serviceClient = (*serviceHandler)(nil)

type serviceHandler struct {
	ctx                  context.Context
	ll                   *slog.Logger
	config               *config.Config
	localhostCfg         *typesv1.ServeLocalhostConfig
	state                *state.State
	svcCfg               *ksvc.Config
	baseSiteURL          string
	tokenSource          *auth.UserRefreshableTokenSource
	authCheckFrequency   time.Duration
	updateCheckFrequency time.Duration

	updateSvc  cliupdatev1connect.UpdateServiceClient
	authSvc    authv1connect.AuthServiceClient
	userSvc    userv1connect.UserServiceClient
	featureSvc featurev1connect.FeatureServiceClient

	clientMu sync.Mutex
	client   systrayClient

	cancel    context.CancelFunc
	onCloseMu sync.Mutex
	onClose   []func(context.Context) error
}

func newServiceHandler(
	ctx context.Context,
	ll *slog.Logger,
	cfg *config.Config,
	state *state.State,
	svcCfg *ksvc.Config,
	baseSiteURL string,
	tokenSource *auth.UserRefreshableTokenSource,
	authCheckFrequency time.Duration,
	updateCheckFrequency time.Duration,
	updateSvc cliupdatev1connect.UpdateServiceClient,
	authSvc authv1connect.AuthServiceClient,
	userSvc userv1connect.UserServiceClient,
	featureSvc featurev1connect.FeatureServiceClient,
	doneOtel func(context.Context) error,
) (*serviceHandler, error) {
	if authCheckFrequency < time.Minute {
		authCheckFrequency = time.Minute
	}
	if updateCheckFrequency < time.Minute {
		updateCheckFrequency = time.Minute
	}
	expcfg := cfg.GetRuntime().GetExperimentalFeatures()
	if expcfg == nil || expcfg.ServeLocalhost == nil {
		return nil, fmt.Errorf("experimental localhost features is not enabled")
	}

	hdl := &serviceHandler{
		ctx:                  ctx,
		ll:                   ll,
		config:               cfg,
		localhostCfg:         expcfg.ServeLocalhost,
		state:                state,
		svcCfg:               svcCfg,
		baseSiteURL:          baseSiteURL,
		tokenSource:          tokenSource,
		authCheckFrequency:   time.Minute,
		updateCheckFrequency: time.Hour,
		updateSvc:            updateSvc,
		authSvc:              authSvc,
		userSvc:              userSvc,
		featureSvc:           featureSvc,
		onClose:              []func(context.Context) error{doneOtel},
	}

	return hdl, nil
}

func (hdl *serviceHandler) run(ctx context.Context, cancel context.CancelFunc) error {
	cfg := hdl.config.GetRuntime()
	hdl.cancel = cancel

	hdl.ll.InfoContext(ctx, "service handler starting", slog.Any("runtime_config", cfg))

	eg, ctx := errgroup.WithContext(ctx)

	if cfg != nil && cfg.ExperimentalFeatures != nil && cfg.ExperimentalFeatures.ServeLocalhost != nil {
		localhostCfg := cfg.ExperimentalFeatures.ServeLocalhost
		ll := hdl.ll.WithGroup("localhost")
		app := &localstorage.AppCtx{
			EnsureLoggedIn: func(ctx context.Context) error {
				return fmt.Errorf("please sign in with the systray button, or via `humanlog auth login`")
			},
			Features: hdl.featureSvc,
			Config:   hdl.config,
			State:    hdl.state,
		}
		registerOnCloseServer := func(srv *http.Server) {
			hdl.onCloseMu.Lock()
			defer hdl.onCloseMu.Unlock()
			hdl.onClose = append(hdl.onClose, func(ctx context.Context) error {
				ll.InfoContext(ctx, "requesting to close server")
				return srv.Close()
			})
		}
		eg.Go(func() error {
			if err := hdl.runLocalhost(ctx, ll, hdl.config, localhostCfg, version, app, registerOnCloseServer); err != nil {
				ll.ErrorContext(ctx, "unable to run localhost", slog.Any("err", err))
				return err
			}
			ll.InfoContext(ctx, "stopped running localhost")
			cancel()
			return nil
		})
	} else {
		hdl.ll.InfoContext(ctx, "not running with localhost")
	}

	eg.Go(func() error {
		if err := hdl.maintainState(ctx); err != nil {
			hdl.ll.ErrorContext(ctx, "unable to maintain state", slog.Any("err", err))
			return err
		}
		hdl.ll.InfoContext(ctx, "stopped maintaining state")
		cancel()
		return nil
	})

	if err := eg.Wait(); err != nil {
		hdl.ll.ErrorContext(ctx, "done waiting", slog.Any("err", err))
		return err
	}
	hdl.ll.InfoContext(ctx, "shutting down")

	return nil
}

func (hdl *serviceHandler) close(ctx context.Context) error {
	for _, onClose := range hdl.onClose {
		if err := onClose(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (hdl *serviceHandler) shutdown(ctx context.Context) error {
	ll := hdl.ll
	ll.InfoContext(ctx, "stopping service")
	tr := time.AfterFunc(10*time.Second, func() {
		ll.InfoContext(ctx, "trying harder to stop service cleanly...")
		hdl.cancel()
	}) // give a stronger hint to quit after 10s
	defer tr.Stop()

	dirtyExit := time.AfterFunc(15*time.Second, func() {
		ll.InfoContext(ctx, "took too long to exit cleanly, shutting down the hard way")
		os.Exit(1)
	}) // just die violently after 15s
	defer dirtyExit.Stop()
	if err := hdl.close(ctx); err != nil {
		ll.ErrorContext(ctx, "error closing service handler", slog.Any("err", err))
	}
	ll.InfoContext(ctx, "service done")
	return nil
}

func (hdl *serviceHandler) runLocalhost(
	ctx context.Context,
	ll *slog.Logger,
	cfg *config.Config,
	localhostCfg *typesv1.ServeLocalhostConfig,
	ownVersion *typesv1.Version,
	app *localstorage.AppCtx,
	registerOnCloseServer func(srv *http.Server),
) error {
	openState := func(ctx context.Context, db localstorage.Storage) (localstate.DB, error) {
		return localproject.Watch(ctx, absfs.New("/"), cfg, db, func(s string) (*typesv1.Query, error) {
			return db.Parse(ctx, s)
		}), nil
	}
	openStorage := func(ctx context.Context) (localstorage.Storage, error) {
		return localstorage.Open(
			ctx,
			localhostCfg.Engine,
			ll.WithGroup("storage"),
			localhostCfg.EngineConfig.AsMap(),
			app,
		)
	}

	return localserver.ServeLocalhost(ctx, ll, localhostCfg, ownVersion, app, openStorage, openState, registerOnCloseServer,
		hdl.DoLogin,
		hdl.DoLogout,
		hdl.DoUpdate,
		hdl.DoRestart,
		hdl.GetConfig,
		hdl.SetConfig,
		hdl.whoami,
		hdl.notifyAlert,
	)
}

func (hdl *serviceHandler) primeState(ctx context.Context) {
	ll := hdl.ll
	expcfg := hdl.config.GetRuntime().GetExperimentalFeatures()
	var channelName *string
	if hdl.config != nil && expcfg != nil && expcfg.ReleaseChannel != nil {
		channelName = expcfg.ReleaseChannel
		ll = ll.With(slog.String("channel", *channelName))
	}
	ll.InfoContext(ctx, "doing auth check")
	if err := hdl.checkAuth(ctx); err != nil {
		if err := hdl.notifyError(ctx, err); err != nil {
			ll.ErrorContext(ctx, "notifying client of auth check error", slog.Any("err", err))
		}
	}

	ll.InfoContext(ctx, "doing update check")
	if err := hdl.checkUpdate(ctx, channelName); err != nil {
		if err := hdl.notifyError(ctx, err); err != nil {
			ll.ErrorContext(ctx, "notifying client of update check error", slog.Any("err", err))
		}
	}
}

func (hdl *serviceHandler) maintainState(ctx context.Context) error {
	ll := hdl.ll
	expcfg := hdl.config.GetRuntime().GetExperimentalFeatures()
	var channelName *string
	if hdl.config != nil && expcfg != nil && expcfg.ReleaseChannel != nil {
		channelName = expcfg.ReleaseChannel
		ll = ll.With(slog.String("channel", *channelName))
	}

	ll.InfoContext(ctx, "priming initial background state")
	hdl.primeState(ctx)
	ll.InfoContext(ctx, "starting to maintain background state")

	checkAuth := time.NewTicker(hdl.authCheckFrequency)
	defer checkAuth.Stop()
	checkUpdate := time.NewTicker(hdl.updateCheckFrequency)
	defer checkUpdate.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-checkAuth.C:
			ll.InfoContext(ctx, "checking auth status")
			if err := hdl.checkAuth(ctx); err != nil {
				if err := hdl.notifyError(ctx, err); err != nil {
					ll.ErrorContext(ctx, "notifying client of auth check error", slog.Any("err", err))
				}
			}
		case <-checkUpdate.C:
			ll.InfoContext(ctx, "checking update status")
			if err := hdl.checkUpdate(ctx, channelName); err != nil {
				if err := hdl.notifyError(ctx, err); err != nil {
					ll.ErrorContext(ctx, "notifying client of update check error", slog.Any("err", err))
				}
			}
		}
	}
}

func (hdl *serviceHandler) checkAuth(ctx context.Context) error {
	ll := hdl.ll
	ll.InfoContext(ctx, "checking auth")
	whoami, err := hdl.whoami(ctx)
	if err != nil {
		return fmt.Errorf("looking up user authentication status: %v", err)
	}
	if whoami == nil {
		return hdl.notifyUnauthenticated(ctx)
	}
	return hdl.notifyAuthenticated(ctx, whoami.User, whoami.DefaultOrganization, whoami.CurrentOrganization)
}

func (hdl *serviceHandler) whoami(ctx context.Context) (*userv1.WhoamiResponse, error) {
	ll := hdl.ll
	ll.InfoContext(ctx, "checking whoami")
	cerr := new(connect.Error)
	res, err := hdl.userSvc.Whoami(ctx, connect.NewRequest(&userv1.WhoamiRequest{}))
	if errors.As(err, &cerr) && cerr.Code() == connect.CodeUnauthenticated {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("looking up user authentication status: %v", err)
	}
	return res.Msg, nil
}

func (hdl *serviceHandler) checkUpdate(ctx context.Context, channel *string) error {
	ll := hdl.ll
	ll.InfoContext(ctx, "checking for updates")
	res, err := hdl.updateSvc.GetNextUpdate(ctx, connect.NewRequest(&cliupdatepb.GetNextUpdateRequest{
		ProjectName:            "humanlog",
		CurrentVersion:         version,
		MachineArchitecture:    runtime.GOARCH,
		MachineOperatingSystem: runtime.GOOS,
		Meta:                   reqMeta(hdl.state),
		ReleaseChannelName:     channel,
	}))
	if err != nil {
		return fmt.Errorf("looking up next update: %v", err)
	}
	msg := res.Msg

	lastCheckAt := time.Now()
	nextSV, err := msg.NextVersion.AsSemver()
	if err != nil {
		return fmt.Errorf("parsing next version: %v", err)
	}
	if err := updateFromResMeta(hdl.state, msg.Meta, &nextSV, &lastCheckAt); err != nil {
		ll.ErrorContext(ctx, "failed to persist internal state", slog.Any("err", err))
	}
	return hdl.notifyUpdateAvailable(ctx, version, msg.NextVersion)
}

func (hdl *serviceHandler) DoLogout(ctx context.Context, returnToURL string) error {
	hdl.ll.InfoContext(ctx, "DoLogout", slog.String("return_to_url", returnToURL))
	if err := performLogoutFlow(ctx, hdl.userSvc, hdl.tokenSource, returnToURL); err != nil {
		return err
	}
	return hdl.checkAuth(ctx)
}

func (hdl *serviceHandler) DoLogin(ctx context.Context, returnToURL string) error {
	hdl.ll.InfoContext(ctx, "DoLogin", slog.String("return_to_url", returnToURL))
	if _, err := performLoginFlow(ctx, hdl.state, hdl.authSvc, hdl.tokenSource, returnToURL); err != nil {
		return err
	}
	return hdl.checkAuth(ctx)
}

func (hdl *serviceHandler) DoUpdate(ctx context.Context) error {
	ll := hdl.ll
	expcfg := hdl.config.GetRuntime().GetExperimentalFeatures()
	baseSiteURL := hdl.baseSiteURL
	var channelName *string
	if expcfg != nil {
		channelName = expcfg.ReleaseChannel
	}
	ll.InfoContext(ctx, "starting upgrade in place")
	sv, err := version.AsSemver()
	if err != nil {
		ll.ErrorContext(ctx, "getting current version", "error", err)
		sv = semver.Version{}
	}
	if err := selfupdate.UpgradeInPlace(ctx, sv, baseSiteURL, channelName, nil, nil, nil); err != nil {
		return fmt.Errorf("applying self-update: %v", err)
	}
	// triggering self-shutdown
	go func() {
		time.Sleep(100 * time.Millisecond)
		ll.InfoContext(ctx, "triggering self-shutdown, hoping the service manager will restart us")
		if err := hdl.shutdown(ctx); err != nil {
			ll.ErrorContext(ctx, "shutting down serviceHandler", "error", err)
		} else {
			ll.InfoContext(ctx, "serviceHandler shut downed")
		}
	}()
	return nil
}

func (hdl *serviceHandler) DoRestart(ctx context.Context) error {
	ll := hdl.ll
	// triggering self-shutdown
	go func() {
		time.Sleep(100 * time.Millisecond)
		ll.InfoContext(ctx, "triggering self-shutdown, hoping the service manager will restart us")
		if err := hdl.shutdown(ctx); err != nil {
			ll.ErrorContext(ctx, "shutting down serviceHandler", "error", err)
		} else {
			ll.InfoContext(ctx, "serviceHandler shut downed")
		}
	}()
	return nil
}

func (hdl *serviceHandler) GetConfig(ctx context.Context) (*typesv1.LocalhostConfig, error) {
	ll := hdl.ll
	// triggering self-shutdown
	ll.InfoContext(ctx, "serving localhost config")
	return hdl.config.CurrentConfig, nil
}

func (hdl *serviceHandler) SetConfig(ctx context.Context, cfg *typesv1.LocalhostConfig) error {
	ll := hdl.ll
	// triggering self-shutdown
	ll.InfoContext(ctx, "serving localhost config")
	hdl.config.CurrentConfig = cfg
	return hdl.config.WriteBack()
}

func (hdl *serviceHandler) CheckUpdate(ctx context.Context) error {
	ll := hdl.ll
	var channelName *string
	expcfg := hdl.config.GetRuntime().GetExperimentalFeatures()
	if expcfg != nil {
		channelName = expcfg.ReleaseChannel
	}
	ll.InfoContext(ctx, "checking for update", slog.String("release_channel", *channelName))
	return hdl.checkUpdate(ctx, channelName)
}

func (hdl *serviceHandler) LastUpdateCheck(ctx context.Context) (*timestamppb.Timestamp, error) {
	updateCheckedAt := hdl.state.LastestKnownVersionUpdatedAt
	if updateCheckedAt == nil {
		return nil, nil
	}
	return timestamppb.New(*updateCheckedAt), nil
}

func (hdl *serviceHandler) CurrentConfig(ctx context.Context) (*config.CurrentConfig, error) {
	return hdl.config.CurrentConfig, nil
}

func (hdl *serviceHandler) registerClient(client systrayClient) {
	ctx := hdl.ctx
	ll := hdl.ll
	ll.InfoContext(ctx, "systray client received")
	hdl.clientMu.Lock()
	hdl.client = client
	hdl.clientMu.Unlock()
	ll.InfoContext(ctx, "systray client set, priming it")
	hdl.primeState(ctx)
	ll.InfoContext(ctx, "systray client primed")
}

func (hdl *serviceHandler) notifyAlert(ctx context.Context, ar *typesv1.AlertRule, as *typesv1.AlertState, o *typesv1.Obj) error {
	hdl.ll.InfoContext(ctx, "calling notifyAlert")
	hdl.clientMu.Lock()
	defer hdl.clientMu.Unlock()
	if hdl.client == nil {
		return nil
	}
	return hdl.client.NotifyAlert(ctx, ar, as, o)
}

func (hdl *serviceHandler) notifyError(ctx context.Context, err error) error {
	hdl.ll.InfoContext(ctx, "calling notifyError", slog.Any("err", err))
	hdl.clientMu.Lock()
	defer hdl.clientMu.Unlock()
	if hdl.client == nil {
		return nil
	}
	return hdl.client.NotifyError(ctx, err)
}

func (hdl *serviceHandler) notifyUnauthenticated(ctx context.Context) error {
	hdl.ll.InfoContext(ctx, "calling notifyUnauthenticated")
	hdl.clientMu.Lock()
	defer hdl.clientMu.Unlock()
	if hdl.client == nil {
		return nil
	}
	return hdl.client.NotifyUnauthenticated(ctx)
}

func (hdl *serviceHandler) notifyAuthenticated(ctx context.Context, user *typesv1.User, defaultOrg, currentOrg *typesv1.Organization) error {
	hdl.ll.InfoContext(ctx, "calling notifyAuthenticated", slog.Any("user", user), slog.Any("defaultOrg", defaultOrg), slog.Any("currentOrg", currentOrg))
	hdl.clientMu.Lock()
	defer hdl.clientMu.Unlock()
	if hdl.client == nil {
		return nil
	}
	return hdl.client.NotifyAuthenticated(ctx, user, defaultOrg, currentOrg)
}

func (hdl *serviceHandler) notifyUpdateAvailable(ctx context.Context, oldV, newV *typesv1.Version) error {
	hdl.ll.InfoContext(ctx, "calling notifyUpdateAvailable", slog.Any("oldV", oldV), slog.Any("newV", newV))
	hdl.clientMu.Lock()
	defer hdl.clientMu.Unlock()
	if hdl.client == nil {
		return nil
	}
	return hdl.client.NotifyUpdateAvailable(ctx, oldV, newV)
}

func withCORS(hdl http.Handler) http.Handler {
	c := cors.New(cors.Options{
		// Debug: true,
		AllowedOrigins: []string{
			"https://humanlog.io",
			"https://humanlog.dev",
			"https://app.humanlog.dev",
			"https://app.humanlog.dev:3000",
			"https://humanlog.sh",
			"http://localhost:3000",
			"https://humanlog.test:3000",
		},
		AllowedMethods: connectcors.AllowedMethods(),
		AllowedHeaders: slices.Concat(
			connectcors.AllowedHeaders(),
			[]string{"Browser-Authorization", "Request-Id"},
			ot.OT{}.Fields(),
			b3.New(b3.WithInjectEncoding(b3.B3SingleHeader)).Fields(),
			b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader)).Fields(),
		),
		ExposedHeaders: slices.Concat(
			connectcors.ExposedHeaders(),
			[]string{"Browser-Authorization", "Request-Id"},
			ot.OT{}.Fields(),
			b3.New(b3.WithInjectEncoding(b3.B3SingleHeader)).Fields(),
			b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader)).Fields(),
		),
		MaxAge: 7200, // 2 hours in seconds
	})
	return c.Handler(hdl)
}

func setupOtel(ctx context.Context, ll *slog.Logger) (done func(context.Context) error, _ error) {
	var toClose []func(context.Context) error
	done = func(context.Context) error {
		var lastErr error
		for _, closer := range toClose {
			if err := closer(ctx); err != nil {
				lastErr = err
			}
		}
		return lastErr
	}

	// trace and monitor yourself with... yourself in dev mode
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("humanlog.localhost"),
		semconv.ServiceVersion(semverVersion.String()),
	))
	if err != nil {
		return done, fmt.Errorf("merging otel resource: %v", err)
	}

	metricExp, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithInsecure())
	if err != nil {
		return done, fmt.Errorf("creating otel metrics exporter: %v", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
	)

	toClose = append(toClose, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		if err := meterProvider.Shutdown(ctx); err != nil {
			ll.DebugContext(ctx, "shutting down otel metrics provider", slog.Any("err", err))
		}
		return nil
	})
	traceClient := otlptracegrpc.NewClient(otlptracegrpc.WithInsecure())
	traceExp, err := otlptrace.New(ctx, traceClient)
	if err != nil {
		return done, fmt.Errorf("creating otel trace exporter: %v", err)
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExp),
		trace.WithResource(res),
	)
	toClose = append(toClose, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		if err := traceProvider.Shutdown(ctx); err != nil {
			ll.DebugContext(ctx, "shutting down otel traces provider", slog.Any("err", err))
		}
		return nil
	})

	otel.SetTracerProvider(traceProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
			b3.New(),
			ot.OT{},
		),
	)

	return done, nil
}

func defaultLogDir(cfg *config.Config, st *state.State) (string, error) {
	logdir := cfg.GetRuntime().GetExperimentalFeatures().GetServeLocalhost().GetLogDir()
	if logdir == "" {
		stateDir, err := state.GetDefaultStateDirpath()
		if err != nil {
			return "", fmt.Errorf("looking up default state dir: %v", err)
		}
		logdir = filepath.Join(stateDir, "logs")
	}
	return logdir, nil
}
