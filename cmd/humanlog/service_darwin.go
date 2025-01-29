//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/user"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"connectrpc.com/connect"
	connectcors "connectrpc.com/cors"
	"github.com/blang/semver"
	"github.com/getlantern/systray"
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
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/humanlogio/humanlog/pkg/retry"
	ksvc "github.com/kardianos/service"
	"github.com/pkg/browser"
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
	getHTTPClient func(cctx *cli.Context, apiURL string) *http.Client,
) cli.Command {
	var (
		svcHandler *serviceHandler
		svcCfg     *ksvc.Config
		svc        ksvc.Service
	)
	return cli.Command{
		Hidden:    hideUnreleasedFeatures == "true",
		Name:      serviceCmdName,
		ShortName: "svc",
		Usage:     "Run humanlog as a background service, with a systray and all.",
		Before: func(cctx *cli.Context) error {
			u, err := user.Current()
			if err != nil {
				return fmt.Errorf("looking up current user: %v", err)
			}
			ctx := getCtx(cctx)
			ll := getLogger(cctx)
			config := getCfg(cctx)
			state := getState(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx, apiURL)

			publicClOpts := connect.WithInterceptors(auth.NewRefreshedUserAuthInterceptor(ll, tokenSource))
			authedClOpts := connect.WithInterceptors(auth.Interceptors(ll, tokenSource)...)

			svcCfg = &ksvc.Config{
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
				tokenSource,
				cliupdatev1connect.NewUpdateServiceClient(httpClient, apiURL, publicClOpts),
				authv1connect.NewAuthServiceClient(httpClient, apiURL, publicClOpts),
				userv1connect.NewUserServiceClient(httpClient, apiURL, authedClOpts),
				featurev1connect.NewFeatureServiceClient(httpClient, apiURL, authedClOpts),
			)
			if err != nil {
				return fmt.Errorf("preparing service: %v", err)
			}

			svc, err = ksvc.New(svcHandler, svcCfg)
			if err != nil {
				return fmt.Errorf("preparing service: %v", err)
			}
			return nil
		},
		Subcommands: []cli.Command{
			{
				Name: "install",
				Action: func(cctx *cli.Context) error {
					return svc.Install()
				},
			},
			{
				Name: "uninstall",
				Action: func(cctx *cli.Context) error {
					return svc.Uninstall()
				},
			},
			{
				Name: "reinstall",
				Action: func(cctx *cli.Context) error {
					if err := svc.Uninstall(); err != nil {
						logerror("will install, but couldn't uninstall first: %v", err)
					}
					return svc.Install()
				},
			},
			{
				Name: "start",
				Action: func(cctx *cli.Context) error {
					return svc.Start()
				},
			},
			{
				Name: "stop",
				Action: func(cctx *cli.Context) error {
					return svc.Stop()
				},
			},
			{
				Name: "restart",
				Action: func(cctx *cli.Context) error {
					return svc.Restart()
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

					eg, ctx := errgroup.WithContext(ctx)

					eg.Go(func() error { return svcHandler.run(ctx) })

					if cfg.ExperimentalFeatures != nil && cfg.ExperimentalFeatures.ShowInSystray != nil && *cfg.ExperimentalFeatures.ShowInSystray {
						trayll := ll.WithGroup("systray")
						mdl := &systrayModel{currentVersion: version}
						onReady := func() {
							sysctrl, err := newSystrayController(ctx, trayll, svcHandler, mdl)
							if err != nil {
								trayll.ErrorContext(ctx, "running humanlog systray controller", slog.Any("err", err))
							} else {
								trayll.InfoContext(ctx, "humanlog systray controller started")
								svcHandler.registerClient(sysctrl)
							}
						}
						onExit := func() {
							trayll.WarnContext(ctx, "exiting...")
						}
						trayll.InfoContext(ctx, "enabling systray menu")
						systray.Run(onReady, onExit) // systray must run on `main` goroutine
						go func() {
							<-ctx.Done()
							trayll.Warn("signal received, sending quit to systray...")
							systray.Quit()
						}()
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

var _ ksvc.Interface = (*serviceHandler)(nil)

type systrayClient interface {
	NotifyError(ctx context.Context, err error) error
	NotifyUnauthenticated(ctx context.Context) error
	NotifyAuthenticated(ctx context.Context, user *typesv1.User, defaultOrg, currentOrg *typesv1.Organization) error
	NotifyUpdateAvailable(ctx context.Context, oldV, newV *typesv1.Version) error
}

type serviceClient interface {
	DoLogout(ctx context.Context) error
	DoLogin(ctx context.Context) error
	DoUpdate(ctx context.Context) error
}

var _ serviceClient = (*serviceHandler)(nil)

type serviceHandler struct {
	ctx         context.Context
	ll          *slog.Logger
	config      *config.Config
	state       *state.State
	tokenSource *auth.UserRefreshableTokenSource

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
	tokenSource *auth.UserRefreshableTokenSource,
	updateSvc cliupdatev1connect.UpdateServiceClient,
	authSvc authv1connect.AuthServiceClient,
	userSvc userv1connect.UserServiceClient,
	featureSvc featurev1connect.FeatureServiceClient,
) (*serviceHandler, error) {

	hdl := &serviceHandler{
		ctx:         ctx,
		ll:          ll,
		config:      cfg,
		state:       state,
		tokenSource: tokenSource,
		updateSvc:   updateSvc,
		authSvc:     authSvc,
		userSvc:     userSvc,
		featureSvc:  featureSvc,
	}

	return hdl, nil
}

// Start provides a place to initiate the service. The service doesn't
// signal a completed start until after this function returns, so the
// Start function must not take more then a few seconds at most.
func (hdl *serviceHandler) Start(s ksvc.Service) error {
	ll := hdl.ll
	ctx, cancel := context.WithCancel(hdl.ctx)
	ll.InfoContext(ctx, "starting service")
	hdl.cancel = cancel

	go hdl.run(ctx)

	return nil
}

func (hdl *serviceHandler) run(ctx context.Context) error {
	cfg := hdl.config

	eg, ctx := errgroup.WithContext(ctx)

	if cfg != nil && cfg.ExperimentalFeatures != nil && cfg.ExperimentalFeatures.ServeLocalhost != nil {
		localhostCfg := *cfg.ExperimentalFeatures.ServeLocalhost
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
				return srv.Close()
			})
		}
		eg.Go(func() error {
			return hdl.runLocalhost(ctx, ll, localhostCfg, version, app, registerOnCloseServer)
		})
	}

	eg.Go(func() error { return hdl.maintainState(ctx) })

	return eg.Wait()
}

// Stop provides a place to clean up program execution before it is terminated.
// It should not take more then a few seconds to execute.
// Stop should not call os.Exit directly in the function.
func (hdl *serviceHandler) Stop(s ksvc.Service) error {
	ctx := hdl.ctx
	ll := hdl.ll
	ll.InfoContext(ctx, "stopping service")
	tr := time.AfterFunc(10*time.Second, hdl.cancel) // give a stronger hint to quit after 10s
	defer tr.Stop()
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
	localhostCfg config.ServeLocalhost,
	ownVersion *typesv1.Version,
	app *localstorage.AppCtx,
	registerOnCloseServer func(srv *http.Server),
) error {
	port := localhostCfg.Port

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
	defer l.Close()
	ll.InfoContext(ctx, "obtained listener")

	ll.InfoContext(ctx, "opening storage engine")
	storage, err := localstorage.Open(
		ctx,
		localhostCfg.Engine,
		ll.WithGroup("storage"),
		localhostCfg.Cfg,
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

	localhostsvc := localsvc.New(ll, hdl.state, ownVersion, storage)
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
	var channelName *string
	if hdl.config != nil && hdl.config.ExperimentalFeatures != nil && hdl.config.ExperimentalFeatures.ReleaseChannel != nil {
		channelName = hdl.config.ExperimentalFeatures.ReleaseChannel
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
	var channelName *string
	if hdl.config != nil && hdl.config.ExperimentalFeatures != nil && hdl.config.ExperimentalFeatures.ReleaseChannel != nil {
		channelName = hdl.config.ExperimentalFeatures.ReleaseChannel
		ll = ll.With(slog.String("channel", *channelName))
	}

	ll.InfoContext(ctx, "priming initial background state")
	hdl.primeState(ctx)
	ll.InfoContext(ctx, "starting to maintain background state")

	checkAuth := time.NewTicker(time.Minute)
	defer checkAuth.Stop()
	checkUpdate := time.NewTicker(1 * time.Hour)
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
	cerr := new(connect.Error)
	res, err := hdl.userSvc.Whoami(ctx, connect.NewRequest(&userv1.WhoamiRequest{}))
	if errors.As(err, &cerr) && cerr.Code() == connect.CodeUnauthenticated {
		return hdl.notifyUnauthenticated(ctx)
	} else if err != nil {
		return fmt.Errorf("looking up user authentication status: %v", err)
	}
	return hdl.notifyAuthenticated(ctx, res.Msg.User, res.Msg.DefaultOrganization, res.Msg.CurrentOrganization)
}

func (hdl *serviceHandler) checkUpdate(ctx context.Context, channel *string) error {
	ll := hdl.ll
	currentSV, err := version.AsSemver()
	if err != nil {
		return fmt.Errorf("parsing current version: %v", err)
	}
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
	if !currentSV.LT(nextSV) {
		return nil
	}
	return hdl.notifyUpdateAvailable(ctx, version, msg.NextVersion)
}

func (hdl *serviceHandler) DoLogout(ctx context.Context) error {
	if err := performLogoutFlow(ctx, hdl.userSvc, hdl.tokenSource); err != nil {
		return err
	}
	return hdl.checkAuth(ctx)
}
func (hdl *serviceHandler) DoLogin(ctx context.Context) error {
	if _, err := performLoginFlow(ctx, hdl.state, hdl.authSvc, hdl.tokenSource); err != nil {
		return err
	}
	return hdl.checkAuth(ctx)

}
func (hdl *serviceHandler) DoUpdate(ctx context.Context) error {
	panic("todo")
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
	hdl.clientMu.Lock()
	defer hdl.clientMu.Unlock()
	if hdl.client == nil {
		return nil
	}
	return hdl.client.NotifyError(ctx, err)
}

func (hdl *serviceHandler) notifyUnauthenticated(ctx context.Context) error {
	hdl.clientMu.Lock()
	defer hdl.clientMu.Unlock()
	if hdl.client == nil {
		return nil
	}
	return hdl.client.NotifyUnauthenticated(ctx)
}

func (hdl *serviceHandler) notifyAuthenticated(ctx context.Context, user *typesv1.User, defaultOrg, currentOrg *typesv1.Organization) error {
	hdl.clientMu.Lock()
	defer hdl.clientMu.Unlock()
	if hdl.client == nil {
		return nil
	}
	return hdl.client.NotifyAuthenticated(ctx, user, defaultOrg, currentOrg)
}

func (hdl *serviceHandler) notifyUpdateAvailable(ctx context.Context, oldV, newV *typesv1.Version) error {
	hdl.clientMu.Lock()
	defer hdl.clientMu.Unlock()
	if hdl.client == nil {
		return nil
	}
	return hdl.client.NotifyUpdateAvailable(ctx, newV, newV)
}

var _ systrayClient = (*systrayController)(nil)

type systrayController struct {
	ll *slog.Logger

	client serviceClient

	mu sync.Mutex

	model *systrayModel

	mQuery                     *systray.MenuItem
	mUserMenuItem              *systray.MenuItem
	mUserMenuItem_Sub_Settings *systray.MenuItem
	mUserMenuItem_Sub_Login    *systray.MenuItem
	mUserMenuItem_Sub_Logout   *systray.MenuItem

	mSettings *systray.MenuItem
	mUpdate   *systray.MenuItem
}

type systrayModel struct {
	currentVersion   *typesv1.Version
	currentVersionSV semver.Version
	nextVersion      *typesv1.Version
	nextVersionSV    semver.Version
	hasUpdate        bool

	user    *typesv1.User
	userOrg *typesv1.Organization
	curOrg  *typesv1.Organization
}

func newSystrayController(ctx context.Context, ll *slog.Logger, client serviceClient, mdl *systrayModel) (*systrayController, error) {
	currentSV, err := mdl.currentVersion.AsSemver()
	if err != nil {
		return nil, fmt.Errorf("parsing current version: %v", err)
	}

	// systray.SetIcon(hlembed.IconDarkPNG)

	ll.InfoContext(ctx, "creating systray menu")
	systray.SetTitle("humanlog")
	systray.SetTooltip("logs for humans to eat. miam miam")

	mUserMenuItem := systray.AddMenuItem("Account", "log into humanlog.io")
	mUserMenuItem_Sub_Settings := mUserMenuItem.AddSubMenuItem("Settings...", "edit your account settings")
	mUserMenuItem_Sub_Login := mUserMenuItem.AddSubMenuItem("Login", "log in with humanlog")
	mUserMenuItem_Sub_Logout := mUserMenuItem.AddSubMenuItem("Logout", "log out of humanlog")

	mQuery := systray.AddMenuItem("Query", "Query your logs")

	systray.AddSeparator()

	mSettings := systray.AddMenuItem("Settings...", "Configure humanlog on your machine")
	mUpdate := systray.AddMenuItem(
		currentSV.String(),
		fmt.Sprintf("Currently running humanlog version %s", currentSV.String()),
	)
	mUpdate.Disable()

	mQuit := systray.AddMenuItem("Quit", "Quit the whole app")
	_ = onClick(ctx, mQuit, func(ctx context.Context) {
		ll.InfoContext(ctx, "quitting the app")
		systray.Quit()
	})

	ll.InfoContext(ctx, "registering systray clickers and stuff")
	ctrl := &systrayController{
		ll:                         ll,
		client:                     client,
		model:                      mdl,
		mUserMenuItem:              mUserMenuItem,
		mUserMenuItem_Sub_Settings: mUserMenuItem_Sub_Settings,
		mUserMenuItem_Sub_Login:    mUserMenuItem_Sub_Login,
		mUserMenuItem_Sub_Logout:   mUserMenuItem_Sub_Logout,
		mQuery:                     mQuery,
		mSettings:                  mSettings,
		mUpdate:                    mUpdate,
	}
	ctrl.registerClickUserSettings(ctx, mUserMenuItem_Sub_Settings)
	ctrl.registerClickUserLogin(ctx, mUserMenuItem_Sub_Login)
	ctrl.registerClickUserLogout(ctx, mUserMenuItem_Sub_Logout)
	ctrl.registerClickQuery(ctx, mQuery)
	ctrl.registerClickUpdate(ctx, mUpdate)
	ctrl.registerClickLocalhostSettings(ctx, mSettings)

	return ctrl, nil
}

func (ctrl *systrayController) NotifyError(ctx context.Context, err error) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	return err // TODO: implement desktop notifications
}

func (ctrl *systrayController) NotifyUnauthenticated(ctx context.Context) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	// changed := false
	// if ctrl.model.user != nil || ctrl.model.userOrg != nil || ctrl.model.curOrg != nil {
	ctrl.model.user = nil
	ctrl.model.userOrg = nil
	ctrl.model.curOrg = nil
	// changed = true
	// }
	// if changed {
	return ctrl.renderLoginMenuItem(ctx)
	// }
	// return nil
}

func (ctrl *systrayController) NotifyAuthenticated(ctx context.Context, user *typesv1.User, defaultOrg, currentOrg *typesv1.Organization) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	// changed := false
	// if ctrl.model.user == nil || ctrl.model.user != user {
	ctrl.model.user = user
	// changed = true
	// }
	// if ctrl.model.userOrg == nil || ctrl.model.userOrg != defaultOrg {
	ctrl.model.userOrg = defaultOrg
	// changed = true
	// }
	// if ctrl.model.curOrg == nil || ctrl.model.curOrg != currentOrg {
	ctrl.model.curOrg = currentOrg
	// changed = true
	// }
	// if changed {
	return ctrl.renderLoginMenuItem(ctx)
	// }
	// return nil
}

func (ctrl *systrayController) NotifyUpdateAvailable(ctx context.Context, currentV, nextV *typesv1.Version) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	// changed := false

	// if ctrl.model.currentVersion == nil || ctrl.model.currentVersion != currentV {
	currentSV, err := currentV.AsSemver()
	if err != nil {
		return fmt.Errorf("converting current version into semver: %v", err)
	}
	ctrl.model.currentVersion = currentV
	ctrl.model.currentVersionSV = currentSV
	// 	changed = true
	// }
	// if ctrl.model.nextVersion == nil || ctrl.model.nextVersion != nextV {
	nextSV, err := nextV.AsSemver()
	if err != nil {
		return fmt.Errorf("converting next version into semver: %v", err)
	}
	ctrl.model.nextVersion = nextV
	ctrl.model.nextVersionSV = nextSV
	// changed = true
	// }
	hasUpdate := ctrl.model.currentVersionSV.LT(ctrl.model.nextVersionSV)
	// if ctrl.model.hasUpdate != hasUpdate {
	ctrl.model.hasUpdate = hasUpdate
	// changed = true
	// }
	// if changed {
	return ctrl.renderUpdateMenuItem(ctx)
	// }
	// return nil
}

func (ctrl *systrayController) renderLoginMenuItem(ctx context.Context) error {
	mdl := ctrl.model
	mi := ctrl.mUserMenuItem
	if mdl.user != nil {
		ctrl.ll.InfoContext(ctx, "rendering as authenticated")
		mi.SetTitle(fmt.Sprintf("%s (%s)", mdl.user.FirstName, mdl.user.Email))
		ctrl.mUserMenuItem_Sub_Settings.Show()
		ctrl.mUserMenuItem_Sub_Login.Hide()
		ctrl.mUserMenuItem_Sub_Logout.Show()
	} else {
		ctrl.ll.InfoContext(ctx, "rendering as unauthenticated")
		mi.SetTitle("Click to login")
		ctrl.mUserMenuItem_Sub_Settings.Hide()
		ctrl.mUserMenuItem_Sub_Login.Show()
		ctrl.mUserMenuItem_Sub_Logout.Hide()
	}
	return nil
}

func (ctrl *systrayController) renderUpdateMenuItem(ctx context.Context) error {
	hasUpdate := ctrl.model.hasUpdate
	current := ctrl.model.currentVersionSV
	mi := ctrl.mUpdate
	if !hasUpdate {
		mi.SetTitle(fmt.Sprintf("%s (latest)", current.String()))
		mi.Disable()
	} else {
		nextVersion := ctrl.model.nextVersionSV
		mi.SetTitle(fmt.Sprintf("Update available! (%s)", nextVersion.String()))
		mi.SetTooltip("Click to update")
		mi.Enable()
	}
	return nil
}

func (ctrl *systrayController) registerClickUserSettings(ctx context.Context, mi *systray.MenuItem) context.CancelFunc {
	return onClick(ctx, mi, func(ctx context.Context) {
		if mi.Disabled() {
			ctrl.ll.DebugContext(ctx, "clicked user settings, but button disabled")
			return
		}
		ctrl.ll.DebugContext(ctx, "clicked user settings")
		browser.OpenURL("https://humanlog.dev/user/edit")
	})
}

func (ctrl *systrayController) registerClickUserLogin(ctx context.Context, mi *systray.MenuItem) context.CancelFunc {
	return onClick(ctx, mi, func(ctx context.Context) {
		if mi.Disabled() {
			ctrl.ll.DebugContext(ctx, "clicked user login, but button disabled")
			return
		}
		ctrl.ll.DebugContext(ctx, "clicked user login")
		if err := ctrl.client.DoLogin(ctx); err != nil {
			ctrl.NotifyError(ctx, err)
		}
	})
}

func (ctrl *systrayController) registerClickUserLogout(ctx context.Context, mi *systray.MenuItem) context.CancelFunc {
	return onClick(ctx, mi, func(ctx context.Context) {
		if mi.Disabled() {
			ctrl.ll.DebugContext(ctx, "clicked user logout, but button disabled")
			return
		}
		ctrl.ll.DebugContext(ctx, "clicked user logout")
		if err := ctrl.client.DoLogout(ctx); err != nil {
			ctrl.NotifyError(ctx, err)
		}
	})
}

func (ctrl *systrayController) registerClickUpdate(ctx context.Context, mi *systray.MenuItem) context.CancelFunc {
	return onClick(ctx, mi, func(ctx context.Context) {
		if mi.Disabled() {
			ctrl.ll.DebugContext(ctx, "clicked updated, but button disabled")
			return
		}
		ctrl.ll.DebugContext(ctx, "clicked updated")
		if err := ctrl.client.DoUpdate(ctx); err != nil {
			ctrl.NotifyError(ctx, err)
		}
	})
}

func (ctrl *systrayController) registerClickQuery(ctx context.Context, mi *systray.MenuItem) context.CancelFunc {
	return onClick(ctx, mi, func(ctx context.Context) {
		if mi.Disabled() {
			ctrl.ll.DebugContext(ctx, "clicked query, but button disabled")
			return
		}
		ctrl.ll.DebugContext(ctx, "clicked query")
		browser.OpenURL("https://humanlog.dev/localhost")
	})
}

func (ctrl *systrayController) registerClickLocalhostSettings(ctx context.Context, mi *systray.MenuItem) context.CancelFunc {
	return onClick(ctx, mi, func(ctx context.Context) {
		if mi.Disabled() {
			ctrl.ll.DebugContext(ctx, "clicked settings, but button disabled")
			return
		}
		ctrl.ll.DebugContext(ctx, "clicked settings")
		browser.OpenURL("https://humanlog.dev/localhost/edit")
	})
}

func onClick(ctx context.Context, mi *systray.MenuItem, do func(ctx context.Context)) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-mi.ClickedCh:
				do(ctx)
			}
		}
	}()
	return cancel
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
