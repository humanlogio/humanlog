package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"connectrpc.com/connect"
	"github.com/blang/semver"
	"github.com/getlantern/systray"
	cliupdatepb "github.com/humanlogio/api/go/svc/cliupdate/v1"
	"github.com/humanlogio/api/go/svc/cliupdate/v1/cliupdatev1connect"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	"github.com/humanlogio/api/go/svc/user/v1/userv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/auth"
	"github.com/pkg/browser"
	"github.com/urfave/cli"
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
		updateClient cliupdatev1connect.UpdateServiceClient
		userClient   userv1connect.UserServiceClient
	)

	return cli.Command{
		Hidden:    hideUnreleasedFeatures == "true",
		Name:      serviceCmdName,
		ShortName: "svc",
		Usage:     "Run humanlog as a background service, with a systray and all.",
		Before: func(cctx *cli.Context) error {
			ll := getLogger(cctx)
			tokenSource := getTokenSource(cctx)
			apiURL := getAPIUrl(cctx)
			httpClient := getHTTPClient(cctx, apiURL)

			publicClOpts := connect.WithInterceptors(auth.NewRefreshedUserAuthInterceptor(ll, tokenSource))
			updateClient = cliupdatev1connect.NewUpdateServiceClient(httpClient, apiURL, publicClOpts)

			authedClOpts := connect.WithInterceptors(auth.Interceptors(ll, tokenSource)...)
			userClient = userv1connect.NewUserServiceClient(httpClient, apiURL, authedClOpts)
			return nil
		},
		Action: func(cctx *cli.Context) error {
			ctx := getCtx(cctx)
			ll := getLogger(cctx)
			cfg := getCfg(cctx)
			state := getState(cctx)

			var channelName *string
			if cfg.ExperimentalFeatures != nil && cfg.ExperimentalFeatures.ReleaseChannel != nil {
				channelName = cfg.ExperimentalFeatures.ReleaseChannel
			}
			currentSV, err := version.AsSemver()
			if err != nil {
				return fmt.Errorf("parsing current version: %v", err)
			}

			onReady := func() {
				// systray.SetIcon(hlembed.IconDarkPNG)
				systray.SetTitle("humanlog")
				systray.SetTooltip("logs for humans to eat. miam miam")

				mUserMenuItem := systray.AddMenuItem("Login...", "log into humanlog.io")
				mUserMenuItem_Sub_Settings := mUserMenuItem.AddSubMenuItem("Settings...", "edit your account settings")
				mUserMenuItem_Sub_Logout := mUserMenuItem.AddSubMenuItem("Logout", "log out of humanlog")

				mQuery := systray.AddMenuItem("Query", "Query your logs")

				systray.AddSeparator()

				mSettings := systray.AddMenuItem("Settings...", "Configure humanlog on your machine")
				mUpdate := systray.AddMenuItem(
					fmt.Sprintf("%s", currentSV.String()),
					fmt.Sprintf("Currently running humanlog version %s", currentSV.String()),
				)
				mUpdate.Disable()

				mQuit := systray.AddMenuItem("Quit", "Quit the whole app")
				onClick(mQuit, func() { systray.Quit() })
				// Sets the icon of a menu item. Only available on Mac and Windows.
				// mQuit.SetIcon(hlembed.IconDarkPNG)

				err := runServiceHandler(ctx, ll, state,
					func(err error) {},
					updateClient,
					channelName,
					userClient,
					mUserMenuItem, mUserMenuItem_Sub_Settings, mUserMenuItem_Sub_Logout,
					mQuery, mUpdate, mSettings,
				)
				if err != nil {
					ll.ErrorContext(ctx, "running humanlog service handler", slog.Any("err", err))
				}
			}

			onExit := func() {
				ll.WarnContext(ctx, "exiting...")
			}

			ll.InfoContext(ctx, "starting service")

			go func() {
				<-ctx.Done()
				log.Printf("signal received, sending quit to systray...")
				systray.Quit()
			}()

			systray.Run(onReady, onExit)

			return nil
		},
	}
}

func onClick(mi *systray.MenuItem, do func()) {
	go func() {
		for range mi.ClickedCh {
			do()
		}
	}()
}

type serviceHandler struct {
	ll          *slog.Logger
	state       *state.State
	notifyError func(err error)

	updateSvc         cliupdatev1connect.UpdateServiceClient
	updateChannelName *string

	userSvc                    userv1connect.UserServiceClient
	user                       *typesv1.User
	userOrg                    *typesv1.Organization
	curOrg                     *typesv1.Organization
	mUserMenuItem              *systray.MenuItem
	mUserMenuItem_Sub_Settings *systray.MenuItem
	mUserMenuItem_Sub_Logout   *systray.MenuItem
	mUpdate                    *systray.MenuItem
}

func runServiceHandler(
	ctx context.Context,
	ll *slog.Logger,
	state *state.State,
	notifyError func(err error),

	updateSvc cliupdatev1connect.UpdateServiceClient,
	updateChannelName *string,

	userSvc userv1connect.UserServiceClient,
	mUserMenuItem *systray.MenuItem,
	mUserMenuItem_Sub_Settings *systray.MenuItem,
	mUserMenuItem_Sub_Logout *systray.MenuItem,

	mQuery, mUpdate, mSettings *systray.MenuItem,

) error {
	svc := &serviceHandler{
		ll:                         ll,
		state:                      state,
		notifyError:                notifyError,
		updateSvc:                  updateSvc,
		updateChannelName:          updateChannelName,
		userSvc:                    userSvc,
		mUserMenuItem:              mUserMenuItem,
		mUserMenuItem_Sub_Settings: mUserMenuItem_Sub_Settings,
		mUserMenuItem_Sub_Logout:   mUserMenuItem_Sub_Logout,
		mUpdate:                    mUpdate,
	}

	svc.registerClickUserLogin(ctx, svc.mUserMenuItem)
	svc.registerClickUserSettings(ctx, svc.mUserMenuItem_Sub_Settings)
	svc.registerClickUserLogout(ctx, svc.mUserMenuItem_Sub_Logout)

	svc.registerClickQueryLogs(ctx, mQuery)
	svc.registerClickUpdate(ctx, mUpdate)
	svc.registerClickLocalhostSettings(ctx, mSettings)

	return svc.maintainState(ctx)
}

func (svc *serviceHandler) maintainState(ctx context.Context) error {
	if err := svc.checkAuth(ctx); err != nil {
		svc.notifyError(err)
	}
	if err := svc.checkUpdate(ctx); err != nil {
		svc.notifyError(err)
	}

	checkAuth := time.NewTicker(time.Minute)
	defer checkAuth.Stop()
	checkUpdate := time.NewTicker(1 * time.Hour)
	defer checkUpdate.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-checkAuth.C:
			if err := svc.checkAuth(ctx); err != nil {
				svc.notifyError(err)
			}
		case <-checkUpdate.C:
			if err := svc.checkUpdate(ctx); err != nil {
				svc.notifyError(err)
			}
		}
	}
}

func (svc *serviceHandler) checkAuth(ctx context.Context) error {
	cerr := new(connect.Error)
	res, err := svc.userSvc.Whoami(ctx, connect.NewRequest(&userv1.WhoamiRequest{}))
	if errors.As(err, &cerr) && cerr.Code() == connect.CodeUnauthenticated {
		svc.user = nil
		svc.userOrg = nil
		svc.curOrg = nil
	} else if err != nil {
		return fmt.Errorf("looking up user authentication status: %v", err)
	} else {
		svc.user = res.Msg.User
		svc.userOrg = res.Msg.DefaultOrganization
		svc.curOrg = res.Msg.CurrentOrganization
	}
	return svc.renderLoginMenuItem(ctx)
}

func (svc *serviceHandler) renderLoginMenuItem(ctx context.Context) error {
	mi := svc.mUserMenuItem
	if svc.user != nil {
		mi.SetTitle(fmt.Sprintf("%s (%s)", svc.user.FirstName, svc.user.Email))
		svc.mUserMenuItem_Sub_Settings.Show()
		svc.mUserMenuItem_Sub_Logout.Show()
	} else {
		mi.SetTitle("Click here to login")
		svc.mUserMenuItem_Sub_Settings.Hide()
		svc.mUserMenuItem_Sub_Logout.Hide()
	}
	return nil
}

func (svc *serviceHandler) checkUpdate(ctx context.Context) error {
	currentSV, err := version.AsSemver()
	if err != nil {
		return fmt.Errorf("parsing current version: %v", err)
	}
	res, err := svc.updateSvc.GetNextUpdate(ctx, connect.NewRequest(&cliupdatepb.GetNextUpdateRequest{
		ProjectName:            "humanlog",
		CurrentVersion:         version,
		MachineArchitecture:    runtime.GOARCH,
		MachineOperatingSystem: runtime.GOOS,
		Meta:                   reqMeta(svc.state),
		ReleaseChannelName:     svc.updateChannelName,
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
	if err := updateFromResMeta(svc.state, msg.Meta, &nextSV, &lastCheckAt); err != nil {
		svc.ll.ErrorContext(ctx, "failed to persist internal state", slog.Any("err", err))
	}
	return svc.renderUpdateMenuItem(ctx, currentSV.LT(nextSV), currentSV, nextSV)
}

func (svc *serviceHandler) renderUpdateMenuItem(ctx context.Context, hasUpdate bool, current, nextVersion semver.Version) error {
	mi := svc.mUpdate
	if !hasUpdate {
		mi.SetTitle(fmt.Sprintf("%s (latest)", current.String()))
		mi.Disable()
	} else {
		mi.SetTitle(fmt.Sprintf("Update available! (%s)", nextVersion.String()))
		mi.SetTooltip("Click to update")
		mi.Enable()
	}
	return nil
}

func (svc *serviceHandler) registerClickUserLogin(ctx context.Context, mi *systray.MenuItem) {
	onClick(mi, func() {
		if mi.Disabled() {
			return
		}
		browser.OpenURL("https://humanlog.dev/user/edit")
	})
}

func (svc *serviceHandler) registerClickUserSettings(ctx context.Context, mi *systray.MenuItem) {
	onClick(mi, func() {
		if mi.Disabled() {
			return
		}
		browser.OpenURL("https://humanlog.dev/user/edit")
	})
}

func (svc *serviceHandler) registerClickUserLogout(ctx context.Context, mi *systray.MenuItem) {
	onClick(mi, func() {
		if mi.Disabled() {
			return
		}
		panic("TODO")
	})
}

func (svc *serviceHandler) registerClickUpdate(ctx context.Context, mi *systray.MenuItem) {
	onClick(mi, func() {
		if mi.Disabled() {
			return
		}
		panic("TODO")
	})
}

func (svc *serviceHandler) registerClickQueryLogs(ctx context.Context, mi *systray.MenuItem) {
	onClick(mi, func() {
		if mi.Disabled() {
			return
		}
		browser.OpenURL("https://humanlog.dev/localhost")
	})
}

func (svc *serviceHandler) registerClickLocalhostSettings(ctx context.Context, mi *systray.MenuItem) {
	onClick(mi, func() {
		if mi.Disabled() {
			return
		}
		browser.OpenURL("https://humanlog.dev/localhost/edit")
	})
}
