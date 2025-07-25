//go:build !darwin

package main

import (
	"context"
	"log/slog"

	typesv1 "github.com/humanlogio/api/go/types/v1"
)

func runSystray(ctx context.Context, ll *slog.Logger, svcHandler *serviceHandler, version *typesv1.Version, baseSiteURL string) error {
	<-ctx.Done()
	return nil
}

var _ systrayClient = (*systrayController)(nil)

type systrayController struct{ ll *slog.Logger }

func newSystrayController(ctx context.Context, ll *slog.Logger, client serviceClient, currentVersion *typesv1.Version, baseSiteURL string) (*systrayController, error) {
	return &systrayController{ll: ll}, nil
}

func (ctrl *systrayController) NotifyAlert(ctx context.Context, ar *typesv1.AlertRule, as *typesv1.AlertState, o *typesv1.Obj) error {
	ctrl.ll.WarnContext(ctx, "systray: not implemented on this platform: NotifyError")
	return nil
}
func (ctrl *systrayController) NotifyError(ctx context.Context, err error) error {
	ctrl.ll.WarnContext(ctx, "systray: not implemented on this platform: NotifyError")
	return nil
}
func (ctrl *systrayController) NotifyUnauthenticated(ctx context.Context) error {
	ctrl.ll.WarnContext(ctx, "systray: not implemented on this platform: NotifyUnauthenticated")
	return nil
}
func (ctrl *systrayController) NotifyAuthenticated(ctx context.Context, user *typesv1.User, defaultOrg, currentOrg *typesv1.Organization) error {
	ctrl.ll.WarnContext(ctx, "systray: not implemented on this platform: NotifyAuthenticated")
	return nil
}
func (ctrl *systrayController) NotifyUpdateAvailable(ctx context.Context, oldV, newV *typesv1.Version) error {
	ctrl.ll.WarnContext(ctx, "systray: not implemented on this platform: NotifyUpdateAvailable")
	return nil
}
