package localproject

import (
	"context"
	"io/fs"

	typesv1 "github.com/humanlogio/api/go/types/v1"
)

type remoteGitStorage struct {
	fs          fs.FS
	logQlParser func(string) (*typesv1.Query, error)
}

func newRemoteGitStorage(projectSource ProjectSource, fs fs.FS, logQlParser func(string) (*typesv1.Query, error)) *remoteGitStorage {
	return &remoteGitStorage{fs: fs, logQlParser: logQlParser}
}

func (store *remoteGitStorage) getOrCreateProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onCreate CreateProjectFn, onGetProject GetProjectFn) error {
	panic("todo")
	// sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Remote)
	// if !ok {
	// 	return errInvalid("remote git can only operate with projectpointers for remote git, but got %T", ptr.Scheme)
	// }
	// lh := sch.Remote
	// st, err := parseProjectPointer(ctx, store.fs, name, lh)
	// if err != nil {
	// 	return errInternal("parsing project pointer: %v", err)
	// }
	// dashboards, err := parseProjectDashboards(ctx, store.fs, name, lh.Path, lh.DashboardDir)
	// if err != nil {
	// 	return errInternal("parsing project dashboards: %v", err)
	// }
	// alertGroups, err := parseProjectAlertGroups(ctx, store.fs, name, lh.Path, lh.AlertDir, store.logQlParser)
	// if err != nil {
	// 	return errInternal("parsing project alert groups: %v", err)
	// }
	// return onProject(st, dashboards, alertGroups)
}

func (store *remoteGitStorage) getProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onGetProject GetProjectHydratedFn) error {
	panic("todo")
}

func (store *remoteGitStorage) getDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, id string, onDashboard GetDashboardFn) error {
	panic("todo")
	// sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	// if !ok {
	// 	return errInvalid("remote git can only operate with projectpointers for remote git, but got %T", ptr.Scheme)
	// }
	// lh := sch.Localhost
	// dashboards, err := parseProjectDashboards(ctx, store.fs, projectName, lh.Path, lh.DashboardDir)
	// if err != nil {
	// 	return errInternal("parsing project dashboards: %v", err)
	// }
	// for _, item := range dashboards {
	// 	if item.Id == id {
	// 		return onDashboard(item)
	// 	}
	// }
	// return nil
}

func (store *remoteGitStorage) getAlertGroup(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, groupName string, onAlertGroup GetAlertGroupFn) error {
	panic("todo")
	// sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	// if !ok {
	// 	return errInvalid("remote git can only operate with projectpointers for remote git, but got %T", ptr.Scheme)
	// }
	// lh := sch.Localhost
	// items, err := parseProjectAlertGroups(ctx, store.fs, projectName, lh.Path, lh.AlertDir, store.logQlParser)
	// if err != nil {
	// 	return errInternal("parsing project alert groups: %v", err)
	// }
	// for _, item := range items {
	// 	if item.Name == groupName {
	// 		return onAlertGroup(item)
	// 	}
	// }
	// return nil
}

func (store *remoteGitStorage) getAlertRule(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, groupName, ruleName string, onAlertRule GetAlertRuleFn) error {
	panic("todo")
}

func (store *remoteGitStorage) validateProjectPointer(ctx context.Context, ptr *typesv1.ProjectPointer) error {
	panic("todo")
}
