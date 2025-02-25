package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/user"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"connectrpc.com/connect"
	connectcors "connectrpc.com/cors"
	"github.com/blang/semver"
	"github.com/humanlogio/api/go/svc/auth/v1/authv1connect"
	cliupdatepb "github.com/humanlogio/api/go/svc/cliupdate/v1"
	"github.com/humanlogio/api/go/svc/cliupdate/v1/cliupdatev1connect"
	"github.com/humanlogio/api/go/svc/feature/v1/featurev1connect"
	"github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	"github.com/humanlogio/api/go/svc/localhost/v1/localhostv1connect"
	"github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	"github.com/humanlogio/api/go/svc/user/v1/userv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/errutil"
	"github.com/humanlogio/humanlog/internal/localsvc"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/selfupdate"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/humanlogio/humanlog/pkg/retry"
	ksvc "github.com/kardianos/service"
	"github.com/rs/cors"
	"github.com/urfave/cli"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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

	expcfg := config.GetRuntime().GetExperimentalFeatures()
	if expcfg != nil {
		// check for updates more often if you use
		// experimental features
		updateCheckFrequency = 10 * time.Minute
		if expcfg.ReleaseChannel != nil {
			// and even more frequently if using a non-default channel
			updateCheckFrequency = time.Minute
		}
	}

	publicClOpts := connect.WithInterceptors(auth.NewRefreshedUserAuthInterceptor(ll, tokenSource))
	authedClOpts := connect.WithInterceptors(auth.Interceptors(ll, tokenSource)...)

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
}

type serviceClient interface {
	DoLogout(ctx context.Context, returnToURL string) error
	DoLogin(ctx context.Context, returnToURL string) error
	DoUpdate(ctx context.Context) error
	DoRestart(ctx context.Context) error
	CheckUpdate(ctx context.Context) error
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
	onClose   []func() error
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
	}

	return hdl, nil
}

func (hdl *serviceHandler) run(ctx context.Context, cancel context.CancelFunc) error {
	cfg := hdl.config.GetRuntime()
	hdl.cancel = cancel

	eg, ctx := errgroup.WithContext(ctx)

	if cfg != nil && cfg.ExperimentalFeatures != nil && cfg.ExperimentalFeatures.ServeLocalhost != nil {
		localhostCfg := cfg.ExperimentalFeatures.ServeLocalhost
		ll := hdl.ll.WithGroup("localhost")
		app := &localstorage.AppCtx{
			EnsureLoggedIn: func(ctx context.Context) error {
				return fmt.Errorf("please sign in with the systray button, or via `humanlog auth login`")
			},
			Features: hdl.featureSvc,
		}
		registerOnCloseServer := func(srv *http.Server) {
			hdl.onCloseMu.Lock()
			defer hdl.onCloseMu.Unlock()
			hdl.onClose = append(hdl.onClose, func() error {
				ll.InfoContext(ctx, "requesting to close server")
				return srv.Close()
			})
		}
		eg.Go(func() error {
			if err := hdl.runLocalhost(ctx, ll, localhostCfg, version, app, registerOnCloseServer); err != nil {
				ll.ErrorContext(ctx, "unable to run localhost", slog.Any("err", err))
				return err
			}
			ll.InfoContext(ctx, "stopped running localhost")
			cancel()
			return nil
		})
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
	for _, onClose := range hdl.onClose {
		if err := onClose(); err != nil {
			return err
		}
	}
	ll.InfoContext(ctx, "service done")
	return nil
}

func (hdl *serviceHandler) runLocalhost(
	ctx context.Context,
	ll *slog.Logger,
	localhostCfg *typesv1.ServeLocalhostConfig,
	ownVersion *typesv1.Version,
	app *localstorage.AppCtx,
	registerOnCloseServer func(srv *http.Server),
) error {
	port := int(localhostCfg.Port)

	// obtaining the listener is our way of also getting an exclusive lock on the storage engine
	// although if someone was independently using the DB before we started, we'll be holding the listener
	// lock while failing to open the storage... this will cause the service to exit
	localhostAddr := net.JoinHostPort("localhost", strconv.Itoa(port))
	var (
		l   net.Listener
		err error
	)
	err = retry.Do(ctx, func(ctx context.Context) (bool, error) {
		ll.InfoContext(ctx, "requesting listener for address", slog.String("addr", localhostAddr))
		l, err = net.Listen("tcp", localhostAddr)
		if err != nil && !errutil.IsEADDRINUSE(err) {
			return false, fmt.Errorf("listening on host/port: %v", err)
		}
		if errutil.IsEADDRINUSE(err) {
			// try again
			ll.InfoContext(ctx, "address in use, retrying later")
			return true, nil
		}
		return false, nil
	}, retry.UseBaseSleep(20*time.Millisecond), retry.UseCapSleep(time.Second))
	if err != nil {
		return fmt.Errorf("unable to obtain localhost listener: %v", err)
	}
	if l == nil {
		return fmt.Errorf("never obtained listener, giving up")
	}
	defer l.Close()
	ll.InfoContext(ctx, "obtained listener")

	ll.InfoContext(ctx, "opening storage engine")
	storage, err := localstorage.Open(
		ctx,
		localhostCfg.Engine,
		ll.WithGroup("storage"),
		localhostCfg.EngineConfig.AsMap(),
		app,
	)
	if err != nil {
		return fmt.Errorf("opening localstorage %q: %v", localhostCfg.Engine, err)
	}
	defer func() {
		ll.InfoContext(ctx, "closing storage engine")
		if err := storage.Close(); err != nil {
			ll.ErrorContext(ctx, "unable to cleanly close storage engine", slog.Any("err", err))
		} else {
			ll.InfoContext(ctx, "storage engine closed cleanly")
		}
	}()

	ll.InfoContext(ctx, "preparing localhost services")

	mux := http.NewServeMux()

	localhostsvc := localsvc.New(ll, hdl.state, ownVersion, storage,
		hdl.DoLogin,
		hdl.DoLogout,
		hdl.DoUpdate,
		hdl.DoRestart,
		hdl.GetConfig,
		hdl.SetConfig,
		hdl.whoami,
	)
	mux.Handle(localhostv1connect.NewLocalhostServiceHandler(localhostsvc))
	mux.Handle(ingestv1connect.NewIngestServiceHandler(localhostsvc))
	mux.Handle(queryv1connect.NewQueryServiceHandler(localhostsvc))

	httphdl := h2c.NewHandler(mux, &http2.Server{})
	httphdl = withCORS(httphdl)

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "oh noes the sky is falling\n\n%s", string(debug.Stack()))
				panic(r)
			}
		}()
		httphdl.ServeHTTP(w, r)
	})}

	registerOnCloseServer(srv)

	ll.InfoContext(ctx, "serving localhost services")
	if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	ll.InfoContext(ctx, "stopped serving localhost services")

	return nil
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

// withCORS adds CORS support to a Connect HTTP handler.
func withCORS(connectHandler http.Handler) http.Handler {
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
		AllowedHeaders: connectcors.AllowedHeaders(),
		ExposedHeaders: connectcors.ExposedHeaders(),
		MaxAge:         7200, // 2 hours in seconds
	})
	return c.Handler(connectHandler)
}
