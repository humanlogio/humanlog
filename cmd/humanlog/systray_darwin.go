//go:build darwin

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"

	"github.com/blang/semver"
	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/pkg/browser"
)

func runSystray(ctx context.Context, ll *slog.Logger, svcHandler *serviceHandler, version *typesv1.Version, baseSiteURL string) error {
	onReady := func() {
		sysctrl, err := newSystrayController(ctx, ll, svcHandler, version, baseSiteURL)
		if err != nil {
			ll.ErrorContext(ctx, "running humanlog systray controller", slog.Any("err", err))
		} else {
			ll.InfoContext(ctx, "humanlog systray controller started")
			svcHandler.registerClient(sysctrl)
		}
	}
	onExit := func() {
		ll.WarnContext(ctx, "exiting...")
	}
	ll.InfoContext(ctx, "enabling systray menu")
	systray.Run(onReady, onExit) // systray must run on `main` goroutine
	go func() {
		<-ctx.Done()
		ll.Warn("signal received, sending quit to systray...")
		systray.Quit()
	}()
	return nil
}

var _ systrayClient = (*systrayController)(nil)

type systrayController struct {
	ll *slog.Logger

	client      serviceClient
	baseSiteURL *url.URL

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
	currentVersion       *typesv1.Version
	currentVersionSV     semver.Version
	nextVersion          *typesv1.Version
	nextVersionSV        semver.Version
	hasUpdate            bool
	lastNotifiedVersion  semver.Version
	requestedUpdateCheck bool

	user    *typesv1.User
	userOrg *typesv1.Organization
	curOrg  *typesv1.Organization
}

func newSystrayController(ctx context.Context, ll *slog.Logger, client serviceClient, currentVersion *typesv1.Version, baseSiteURL string) (*systrayController, error) {

	baseSiteU, err := url.Parse(baseSiteURL)
	if err != nil {
		return nil, fmt.Errorf("parsing base site URL: %v", err)
	}

	mdl := &systrayModel{currentVersion: currentVersion}

	currentSV, err := mdl.currentVersion.AsSemver()
	if err != nil {
		return nil, fmt.Errorf("parsing current version: %v", err)
	}
	mdl.lastNotifiedVersion = currentSV

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
		fmt.Sprintf("%s (latest, click to check)", currentSV.String()),
		fmt.Sprintf("Currently running humanlog version %s", currentSV.String()),
	)

	mQuit := systray.AddMenuItem("Quit", "Quit the whole app")
	_ = onClick(ctx, mQuit, func(ctx context.Context) {
		ll.InfoContext(ctx, "quitting the app")
		systray.Quit()
	})

	ll.InfoContext(ctx, "registering systray clickers and stuff")
	ctrl := &systrayController{
		ll:                         ll,
		client:                     client,
		baseSiteURL:                baseSiteU,
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
	if err := beeep.Alert("humanlog has problems!", err.Error(), ""); err != nil {
		return err
	}
	return nil
}

func (ctrl *systrayController) NotifyUnauthenticated(ctx context.Context) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	wasSignedIn := ctrl.model.user != nil
	ctrl.model.user = nil
	ctrl.model.userOrg = nil
	ctrl.model.curOrg = nil

	if wasSignedIn {
		err := beeep.Notify(
			"humanlog: signed out",
			"successfully signed out of humanlog",
			"",
		)
		if err != nil {
			ctrl.ll.ErrorContext(ctx, "can't notify desktop", slog.Any("err", err))
		}
	}
	return ctrl.renderLoginMenuItem(ctx)
}

func (ctrl *systrayController) NotifyAuthenticated(ctx context.Context, user *typesv1.User, defaultOrg, currentOrg *typesv1.Organization) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	wasSignedOff := ctrl.model.user == nil
	ctrl.model.user = user
	ctrl.model.userOrg = defaultOrg
	ctrl.model.curOrg = currentOrg
	if wasSignedOff {
		err := beeep.Notify(
			"humanlog: signed in",
			fmt.Sprintf("humanlog is signed in as %s (%s)", user.FirstName, user.Email),
			"",
		)
		if err != nil {
			ctrl.ll.ErrorContext(ctx, "can't notify desktop", slog.Any("err", err))
		}
	}
	return ctrl.renderLoginMenuItem(ctx)
}

func (ctrl *systrayController) NotifyUpdateAvailable(ctx context.Context, currentV, nextV *typesv1.Version) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	ctrl.ll.InfoContext(ctx, "notified of an update being available")
	currentSV, err := currentV.AsSemver()
	if err != nil {
		return fmt.Errorf("converting current version into semver: %v", err)
	}
	ctrl.model.currentVersion = currentV
	ctrl.model.currentVersionSV = currentSV
	nextSV, err := nextV.AsSemver()
	if err != nil {
		return fmt.Errorf("converting next version into semver: %v", err)
	}
	ctrl.model.nextVersion = nextV
	ctrl.model.nextVersionSV = nextSV
	hasUpdate := ctrl.model.currentVersionSV.LT(ctrl.model.nextVersionSV)
	ctrl.model.hasUpdate = hasUpdate

	if !ctrl.model.lastNotifiedVersion.EQ(nextSV) {
		err = beeep.Notify(
			"humanlog update available",
			fmt.Sprintf("version %s is available, you can update now", nextSV.String()),
			"",
		)
		if err != nil {
			ctrl.ll.ErrorContext(ctx, "can't notify desktop", slog.Any("err", err))
		} else {
			ctrl.model.lastNotifiedVersion = nextSV
		}
	}
	if ctrl.model.requestedUpdateCheck {

		if hasUpdate {
			err = beeep.Notify(
				"humanlog update available",
				fmt.Sprintf("version %s is available, you can update now", nextSV.String()),
				"",
			)
			if err != nil {
				ctrl.ll.ErrorContext(ctx, "can't notify desktop", slog.Any("err", err))
			}
		} else {
			err = beeep.Notify(
				"humanlog is up to date",
				fmt.Sprintf("you're running the latest version (%s)", currentSV.String()),
				"",
			)
			if err != nil {
				ctrl.ll.ErrorContext(ctx, "can't notify desktop", slog.Any("err", err))
			}
		}

		ctrl.model.requestedUpdateCheck = false
	}

	return ctrl.renderUpdateMenuItem(ctx)
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
		mi.SetTitle(fmt.Sprintf("%s (latest, click to check)", current.String()))
	} else {
		nextVersion := ctrl.model.nextVersionSV
		mi.SetTitle(fmt.Sprintf("Update available! (%s)", nextVersion.String()))
		mi.SetTooltip("Click to update")
		mi.Enable()
	}
	return nil
}

func (ctrl *systrayController) registerClickUserSettings(ctx context.Context, mi *systray.MenuItem) context.CancelFunc {
	userSettingsPath := ctrl.baseSiteURL.JoinPath("/user/edit")
	return onClick(ctx, mi, func(ctx context.Context) {
		if mi.Disabled() {
			ctrl.ll.DebugContext(ctx, "clicked user settings, but button disabled")
			return
		}
		ctrl.ll.DebugContext(ctx, "clicked user settings")
		browser.OpenURL(userSettingsPath.String())
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
			ctrl.ll.DebugContext(ctx, "clicked update, but button disabled")
			return
		}
		ctrl.ll.DebugContext(ctx, "clicked update")
		if ctrl.model.hasUpdate {
			if err := ctrl.client.DoUpdate(ctx); err != nil {
				ctrl.NotifyError(ctx, err)
			}
		} else {
			ctrl.ll.InfoContext(ctx, "starting a manually requested update check")
			ctrl.mu.Lock()
			ctrl.model.requestedUpdateCheck = true
			ctrl.mu.Unlock()
			if err := ctrl.client.CheckUpdate(ctx); err != nil {
				ctrl.NotifyError(ctx, err)
			}
		}
	})
}

func (ctrl *systrayController) registerClickQuery(ctx context.Context, mi *systray.MenuItem) context.CancelFunc {
	queryPath := ctrl.baseSiteURL.JoinPath("/localhost")
	return onClick(ctx, mi, func(ctx context.Context) {
		if mi.Disabled() {
			ctrl.ll.DebugContext(ctx, "clicked query, but button disabled")
			return
		}
		ctrl.ll.DebugContext(ctx, "clicked query")
		browser.OpenURL(queryPath.String())
	})
}

func (ctrl *systrayController) registerClickLocalhostSettings(ctx context.Context, mi *systray.MenuItem) context.CancelFunc {
	settingsPath := ctrl.baseSiteURL.JoinPath("/localhost/edit")
	return onClick(ctx, mi, func(ctx context.Context) {
		if mi.Disabled() {
			ctrl.ll.DebugContext(ctx, "clicked settings, but button disabled")
			return
		}
		ctrl.ll.DebugContext(ctx, "clicked settings")
		browser.OpenURL(settingsPath.String())
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
