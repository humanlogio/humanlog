package localproject

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	dashboardv1 "github.com/humanlogio/api/go/svc/dashboard/v1"
	projectv1 "github.com/humanlogio/api/go/svc/project/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/localstate"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/zeebo/blake3"
	"google.golang.org/protobuf/proto"
)

type ProjectSource interface {
	GetProject()
}

type CreateProjectFn func() *typesv1.Project

type GetProjectFn func(
	*typesv1.Project,
) error

type GetProjectHydratedFn func(
	*typesv1.Project,
	[]*typesv1.Dashboard,
	[]*typesv1.AlertGroup,
) error
type GetDashboardFn func(*typesv1.Dashboard) error
type CreateDashboardFn func(*typesv1.Dashboard) error
type UpdateDashboardFn func(*typesv1.Dashboard) error
type DeleteDashboardFn func() error
type GetAlertGroupFn func(*typesv1.AlertGroup) error
type CreateAlertGroupFn func(*typesv1.AlertGroup) error
type UpdateAlertGroupFn func(*typesv1.AlertGroup) error
type DeleteAlertGroupFn func() error
type GetAlertRuleFn func(*typesv1.AlertRule) error

type projectStorage interface {
	getOrCreateProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onCreate CreateProjectFn, onGetProject GetProjectFn) error
	getProjectHydrated(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onGetProject GetProjectHydratedFn) error
	getProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onGetProject GetProjectFn) error
	syncProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onGetProject GetProjectFn) error
	getDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, id string, onDashboard GetDashboardFn) error
	createDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, dashboard *typesv1.Dashboard, onCreated CreateDashboardFn) error
	updateDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, id string, dashboard *typesv1.Dashboard, onUpdated UpdateDashboardFn) error
	deleteDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, id string, onDeleted DeleteDashboardFn) error
	getAlertGroup(ctx context.Context, alertState localstorage.Alertable, projectName string, ptr *typesv1.ProjectPointer, groupName string, onAlertGroup GetAlertGroupFn) error
	createAlertGroup(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, alertGroup *typesv1.AlertGroup, onCreated CreateAlertGroupFn) error
	updateAlertGroup(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, groupName string, alertGroup *typesv1.AlertGroup, onUpdated UpdateAlertGroupFn) error
	deleteAlertGroup(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, groupName string, onDeleted DeleteAlertGroupFn) error
	getAlertRule(ctx context.Context, alertState localstorage.Alertable, projectName string, ptr *typesv1.ProjectPointer, groupName, ruleName string, onAlertRule GetAlertRuleFn) error
	validateProjectPointer(ctx context.Context, ptr *typesv1.ProjectPointer) error
}

func errInvalid(msg string, args ...any) error {
	return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf(msg, args...))
}

func errInternal(msg string, args ...any) error {
	return connect.NewError(connect.CodeInternal, fmt.Errorf(msg, args...))
}

func errNotFound(msg string, args ...any) error {
	return connect.NewError(connect.CodeNotFound, fmt.Errorf(msg, args...))
}

type watch struct {
	db     *dbStorage
	remote *remoteGitStorage
	local  *localGitStorage

	mu          sync.Mutex
	cfg         *config.Config
	alertState  localstorage.Alertable
	logQlParser func(string) (*typesv1.Query, error)

	timeNow func() time.Time
}

func Watch(
	ctx context.Context,
	fs billy.Filesystem,
	cfg *config.Config,
	alertState localstorage.Alertable,
	logQlParser func(string) (*typesv1.Query, error),
) (localstate.DB, error) {
	return internalWatch(ctx, fs, cfg, alertState, logQlParser, time.Now)
}

func internalWatch(
	ctx context.Context,
	fs billy.Filesystem,
	cfg *config.Config,
	alertState localstorage.Alertable,
	logQlParser func(string) (*typesv1.Query, error),
	timeNow func() time.Time,
) (localstate.DB, error) {
	gitfs := memfs.New()
	remote, err := newRemoteGitStorage(nil, gitfs, logQlParser, timeNow)
	if err != nil {
		return nil, err
	}
	return &watch{
		db:          newDBStorage(nil, nil, logQlParser, timeNow),
		remote:      remote,
		local:       newLocalGitStorage(nil, fs, logQlParser, timeNow),
		cfg:         cfg,
		alertState:  alertState,
		logQlParser: logQlParser,
		timeNow:     timeNow,
	}, nil
}

func (wt *watch) storageForPointer(ptr *typesv1.ProjectPointer) (projectStorage, error) {
	switch ptr.Scheme.(type) {
	case *typesv1.ProjectPointer_Remote:
		return wt.remote, nil
	case *typesv1.ProjectPointer_Localhost:
		return wt.local, nil
	case *typesv1.ProjectPointer_Db:
		return wt.db, nil
	}
	return nil, errInvalid("unknown project pointer type: %T", ptr.Scheme)
}

func (wt *watch) CreateProject(ctx context.Context, req *projectv1.CreateProjectRequest) (*projectv1.CreateProjectResponse, error) {
	name := req.Spec.Name

	err := func() error {
		wt.mu.Lock()
		defer wt.mu.Unlock()
		cfg, err := wt.cfg.Reload()
		if err != nil {
			return fmt.Errorf("reading config file: %v", err)
		}
		wt.cfg = cfg
		if cfg.Runtime == nil {
			cfg.Runtime = &typesv1.RuntimeConfig{}
		}
		if cfg.Runtime.ExperimentalFeatures == nil {
			cfg.Runtime.ExperimentalFeatures = &typesv1.RuntimeConfig_ExperimentalFeatures{}
		}
		if cfg.Runtime.ExperimentalFeatures.Projects == nil {
			cfg.Runtime.ExperimentalFeatures.Projects = &typesv1.ProjectsConfig{}
		}
		projects := cfg.Runtime.ExperimentalFeatures.Projects

		sp := &typesv1.ProjectsConfig_Project{
			Name:    name,
			Pointer: req.Spec.Pointer,
		}

		onCreate := func() *typesv1.Project { panic("todo") }

		var toStoreInConfig *typesv1.ProjectsConfig_Project
		onGet := func(p *typesv1.Project) error {
			toStoreInConfig = &typesv1.ProjectsConfig_Project{
				Name:    p.Spec.Name,
				Pointer: p.Spec.Pointer,
			}
			return nil
		}

		storage, err := wt.storageForPointer(req.Spec.Pointer)
		if err != nil {
			return err
		}
		if err := wt.validateProjectPointer(ctx, projects.Projects, name, sp, storage); err != nil {
			return err
		}
		if err := storage.getOrCreateProject(ctx, name, req.Spec.Pointer, onCreate, onGet); err != nil {
			return err
		}

		projects.Projects = append(projects.Projects, toStoreInConfig)
		cfg.Runtime.ExperimentalFeatures.Projects = projects
		if err := cfg.WriteBack(); err != nil {
			return connect.NewError(connect.CodeInternal, fmt.Errorf("writing back configuration: %v", err))
		}
		return nil
	}()
	if err != nil {
		return nil, err
	}
	var out *typesv1.Project
	err = wt.lockedWithProjectByName(ctx, req.Spec.Name, func(ptr *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		return storage.getProject(ctx, ptr.Name, ptr.Pointer, func(p *typesv1.Project) error {
			out = p
			// Enrich project with all warnings
			wt.enrichProjectWithWarnings(p.Status, ptr)
			return nil
		})
	})
	return &projectv1.CreateProjectResponse{Project: out}, err
}

func (wt *watch) ValidateProject(ctx context.Context, req *projectv1.ValidateProjectRequest) (*projectv1.ValidateProjectResponse, error) {
	name := req.Spec.Name

	var status *typesv1.ProjectStatus
	err := func() error {
		wt.mu.Lock()
		defer wt.mu.Unlock()
		cfg, err := wt.cfg.Reload()
		if err != nil {
			return fmt.Errorf("reading config file: %v", err)
		}
		wt.cfg = cfg
		if cfg.Runtime == nil {
			cfg.Runtime = &typesv1.RuntimeConfig{}
		}
		if cfg.Runtime.ExperimentalFeatures == nil {
			cfg.Runtime.ExperimentalFeatures = &typesv1.RuntimeConfig_ExperimentalFeatures{}
		}
		if cfg.Runtime.ExperimentalFeatures.Projects == nil {
			cfg.Runtime.ExperimentalFeatures.Projects = &typesv1.ProjectsConfig{}
		}
		projects := cfg.Runtime.ExperimentalFeatures.Projects

		sp := &typesv1.ProjectsConfig_Project{
			Name:    name,
			Pointer: req.Spec.Pointer,
		}

		storage, err := wt.storageForPointer(req.Spec.Pointer)
		if err != nil {
			return err
		}
		// Run the same validation as CreateProject
		if err := wt.validateProjectPointer(ctx, projects.Projects, name, sp, storage); err != nil {
			return err
		}

		// Create a status object to compute warnings using the same code path
		status = &typesv1.ProjectStatus{}
		wt.enrichProjectWithWarnings(status, sp)
		return nil
	}()
	if err != nil {
		return nil, err
	}

	return &projectv1.ValidateProjectResponse{Status: status}, nil
}

func (wt *watch) validateProjectPointer(ctx context.Context, projects []*typesv1.ProjectsConfig_Project, name string, sp *typesv1.ProjectsConfig_Project, storage projectStorage) error {
	if name == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("project name cannot be empty"))
	}
	for _, project := range projects {
		if project.Name == name {
			return errInvalid("a project with this name already exists")
		}
	}
	return storage.validateProjectPointer(ctx, sp.Pointer)
}

func (wt *watch) UpdateProject(ctx context.Context, req *projectv1.UpdateProjectRequest) (*projectv1.UpdateProjectResponse, error) {
	err := wt.lockedWithProjectByName(ctx, req.Name, func(sc *typesv1.ProjectsConfig_Project) error {
		candidate := proto.Clone(sc).(*typesv1.ProjectsConfig_Project)
		candidate.Name = req.Spec.Name
		candidate.Pointer = req.Spec.Pointer
		storage, err := wt.storageForPointer(candidate.Pointer)
		if err != nil {
			return err
		}
		projects := wt.cfg.GetRuntime().GetExperimentalFeatures().GetProjects()
		// Filter out the project being updated to avoid false conflicts when keeping the same name
		otherProjects := make([]*typesv1.ProjectsConfig_Project, 0, len(projects.Projects)-1)
		for _, p := range projects.Projects {
			if p.Name != req.Name { // req.Name is the OLD name
				otherProjects = append(otherProjects, p)
			}
		}
		if err := wt.validateProjectPointer(ctx, otherProjects, candidate.Name, candidate, storage); err != nil {
			return err
		}
		wt.cfg.GetRuntime().GetExperimentalFeatures().Projects = projects
		if err := wt.cfg.WriteBack(); err != nil {
			return connect.NewError(connect.CodeInternal, fmt.Errorf("updating project on disk: %v", err))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// return an updated version of the project
	var out *typesv1.Project

	err = wt.lockedWithProjectByName(ctx, req.Name, func(ptr *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		return storage.getProject(ctx, ptr.Name, ptr.Pointer, func(p *typesv1.Project) error {
			out = p
			// Enrich project with all warnings
			wt.enrichProjectWithWarnings(p.Status, ptr)
			return nil
		})
	})
	return &projectv1.UpdateProjectResponse{Project: out}, err
}

func (wt *watch) DeleteProject(ctx context.Context, req *projectv1.DeleteProjectRequest) (*projectv1.DeleteProjectResponse, error) {
	// return an updated version of the project
	err := wt.lockedWithProjectConfig(ctx, func(s *typesv1.ProjectsConfig) error {
		found := false
		s.Projects = slices.DeleteFunc(s.Projects, func(e *typesv1.ProjectsConfig_Project) bool {
			found = true
			return e.Name == req.Name
		})
		if !found {
			return nil
		}
		wt.cfg.GetRuntime().GetExperimentalFeatures().Projects = s
		if err := wt.cfg.WriteBack(); err != nil {
			return connect.NewError(connect.CodeInternal, fmt.Errorf("deleting project on disk: %v", err))
		}
		return nil
	})
	return &projectv1.DeleteProjectResponse{}, err
}

func (wt *watch) GetProject(ctx context.Context, req *projectv1.GetProjectRequest) (*projectv1.GetProjectResponse, error) {
	var (
		project     *typesv1.Project
		dashboards  []*typesv1.Dashboard
		alertGroups []*typesv1.AlertGroup
		err         error
	)
	err = wt.lockedWithProjectByName(ctx, req.Name, func(ptr *typesv1.ProjectsConfig_Project) error {

		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		return storage.getProjectHydrated(ctx, ptr.Name, ptr.Pointer, func(p *typesv1.Project, d []*typesv1.Dashboard, ag []*typesv1.AlertGroup) error {
			project = p
			dashboards = d
			alertGroups = ag

			// Enrich project with all warnings
			wt.enrichProjectWithWarnings(p.Status, ptr)

			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return &projectv1.GetProjectResponse{Project: project, Dashboards: dashboards, AlertGroups: alertGroups}, nil
}

func (wt *watch) SyncProject(ctx context.Context, req *projectv1.SyncProjectRequest) (*projectv1.SyncProjectResponse, error) {
	var (
		project *typesv1.Project
		err     error
	)
	err = wt.lockedWithProjectByName(ctx, req.Name, func(ptr *typesv1.ProjectsConfig_Project) error {

		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		return storage.getProjectHydrated(ctx, ptr.Name, ptr.Pointer, func(p *typesv1.Project, d []*typesv1.Dashboard, ag []*typesv1.AlertGroup) error {
			project = p
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return &projectv1.SyncProjectResponse{Project: project}, nil
}

func (wt *watch) ListProject(ctx context.Context, req *projectv1.ListProjectRequest) (*projectv1.ListProjectResponse, error) {
	var (
		out  []*projectv1.ListProjectResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = wt.lockedWithProjectConfig(ctx, func(sc *typesv1.ProjectsConfig) error {
		next, err = cursorForSlice(sc.Projects, req.Cursor, req.Limit, 10, 100,
			func(sp *typesv1.ProjectsConfig_Project) string { return sp.Name },
			func(sp *typesv1.ProjectsConfig_Project) error {
				storage, err := wt.storageForPointer(sp.Pointer)
				if err != nil {
					return err
				}
				return storage.getProject(ctx, sp.Name, sp.Pointer, func(p *typesv1.Project) error {
					// Enrich project with all warnings
					wt.enrichProjectWithWarnings(p.Status, sp)
					out = append(out, &projectv1.ListProjectResponse_ListItem{Project: p})
					return nil
				})
			},
		)
		return err
	})
	return &projectv1.ListProjectResponse{Items: out, Next: next}, err
}

func (wt *watch) CreateDashboard(ctx context.Context, req *dashboardv1.CreateDashboardRequest) (*dashboardv1.CreateDashboardResponse, error) {
	var out *typesv1.Dashboard
	err := wt.lockedWithProjectByName(ctx, req.ProjectName, func(ptr *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		// ID will be extracted from Perses dashboard metadata.name (slug) in storage layer
		dashboard := &typesv1.Dashboard{
			Meta:   &typesv1.DashboardMeta{},  // ID set by storage layer
			Spec:   req.Spec,
			Status: &typesv1.DashboardStatus{},
		}
		return storage.createDashboard(ctx, ptr.Name, ptr.Pointer, dashboard, func(dashboard *typesv1.Dashboard) error {
			out = dashboard
			return nil
		})
	})
	return &dashboardv1.CreateDashboardResponse{Dashboard: out}, err
}

func (wt *watch) UpdateDashboard(ctx context.Context, req *dashboardv1.UpdateDashboardRequest) (*dashboardv1.UpdateDashboardResponse, error) {
	var out *typesv1.Dashboard
	err := wt.lockedWithProjectByName(ctx, req.ProjectName, func(ptr *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		dashboard := &typesv1.Dashboard{
			Meta:   &typesv1.DashboardMeta{Id: req.Id},
			Spec:   req.Spec,
			Status: &typesv1.DashboardStatus{},
		}
		return storage.updateDashboard(ctx, ptr.Name, ptr.Pointer, req.Id, dashboard, func(dashboard *typesv1.Dashboard) error {
			out = dashboard
			return nil
		})
	})
	return &dashboardv1.UpdateDashboardResponse{Dashboard: out}, err
}

func (wt *watch) DeleteDashboard(ctx context.Context, req *dashboardv1.DeleteDashboardRequest) (*dashboardv1.DeleteDashboardResponse, error) {
	err := wt.lockedWithProjectByName(ctx, req.ProjectName, func(ptr *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		return storage.deleteDashboard(ctx, ptr.Name, ptr.Pointer, req.Id, func() error {
			return nil
		})
	})
	return &dashboardv1.DeleteDashboardResponse{}, err
}
func (wt *watch) GetDashboard(ctx context.Context, req *dashboardv1.GetDashboardRequest) (*dashboardv1.GetDashboardResponse, error) {
	var (
		out *typesv1.Dashboard
		err error
	)
	err = wt.lockedWithDashboardByID(ctx, req.ProjectName, req.Id, func(s *typesv1.Dashboard) error {
		out = s
		return nil
	})
	return &dashboardv1.GetDashboardResponse{Dashboard: out}, err
}
func (wt *watch) ListDashboard(ctx context.Context, req *dashboardv1.ListDashboardRequest) (*dashboardv1.ListDashboardResponse, error) {
	var (
		out  []*dashboardv1.ListDashboardResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = wt.lockedWithProjectByName(ctx, req.ProjectName, func(ptr *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		return storage.getProjectHydrated(ctx, ptr.Name, ptr.Pointer, func(p *typesv1.Project, items []*typesv1.Dashboard, ag []*typesv1.AlertGroup) error {
			next, err = cursorForSlice(items, req.Cursor, req.Limit, 10, 100,
				func(sp *typesv1.Dashboard) string { return sp.Meta.Id },
				func(sp *typesv1.Dashboard) error {
					out = append(out, &dashboardv1.ListDashboardResponse_ListItem{Dashboard: sp})
					return nil
				},
			)
			return nil
		})
	})
	return &dashboardv1.ListDashboardResponse{Items: out, Next: next}, err
}

func (wt *watch) CreateAlertGroup(ctx context.Context, req *alertv1.CreateAlertGroupRequest) (*alertv1.CreateAlertGroupResponse, error) {
	var out *typesv1.AlertGroup
	err := wt.lockedWithProjectByName(ctx, req.ProjectName, func(ptr *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		alertGroup := &typesv1.AlertGroup{
			Meta:   &typesv1.AlertGroupMeta{Id: req.Spec.Name}, // ID from group name
			Spec:   req.Spec,
			Status: &typesv1.AlertGroupStatus{},
		}
		return storage.createAlertGroup(ctx, ptr.Name, ptr.Pointer, alertGroup, func(alertGroup *typesv1.AlertGroup) error {
			out = alertGroup
			return nil
		})
	})
	return &alertv1.CreateAlertGroupResponse{AlertGroup: out}, err
}

func (wt *watch) UpdateAlertGroup(ctx context.Context, req *alertv1.UpdateAlertGroupRequest) (*alertv1.UpdateAlertGroupResponse, error) {
	var out *typesv1.AlertGroup
	err := wt.lockedWithProjectByName(ctx, req.ProjectName, func(ptr *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		alertGroup := &typesv1.AlertGroup{
			Meta:   &typesv1.AlertGroupMeta{Id: req.Name},
			Spec:   req.Spec,
			Status: &typesv1.AlertGroupStatus{},
		}
		return storage.updateAlertGroup(ctx, ptr.Name, ptr.Pointer, req.Name, alertGroup, func(alertGroup *typesv1.AlertGroup) error {
			out = alertGroup
			return nil
		})
	})
	return &alertv1.UpdateAlertGroupResponse{AlertGroup: out}, err
}
func (wt *watch) DeleteAlertGroup(ctx context.Context, req *alertv1.DeleteAlertGroupRequest) (*alertv1.DeleteAlertGroupResponse, error) {
	err := wt.lockedWithProjectByName(ctx, req.ProjectName, func(ptr *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		return storage.deleteAlertGroup(ctx, ptr.Name, ptr.Pointer, req.Name, func() error {
			return nil
		})
	})
	return &alertv1.DeleteAlertGroupResponse{}, err
}
func (wt *watch) GetAlertGroup(ctx context.Context, req *alertv1.GetAlertGroupRequest) (*alertv1.GetAlertGroupResponse, error) {
	var out *typesv1.AlertGroup
	err := wt.lockedWithAlertGroupByName(ctx, req.ProjectName, req.Name, func(_ *typesv1.ProjectsConfig_Project, s *typesv1.AlertGroup) error {
		out = s
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Populate status from alertState storage
	if out.Status == nil {
		out.Status = &typesv1.AlertGroupStatus{}
	}
	if out.Status.Rules == nil {
		out.Status.Rules = make([]*typesv1.AlertGroupStatus_NamedAlertRuleStatus, 0, len(out.Spec.Rules))
	}

	// Query status for each rule from storage
	for _, namedRule := range out.Spec.Rules {
		status, err := wt.alertState.AlertGetOrCreate(ctx, req.ProjectName, req.Name, namedRule.Id, func() *typesv1.AlertRuleStatus {
			return &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{}}
		})
		if err != nil {
			// If project doesn't exist in alert state yet, use default unknown status
			status = &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{}}
		}

		// Check if status already exists in the array
		found := false
		for _, namedStatus := range out.Status.Rules {
			if namedStatus.Id == namedRule.Id {
				namedStatus.Status = status
				found = true
				break
			}
		}
		if !found {
			out.Status.Rules = append(out.Status.Rules, &typesv1.AlertGroupStatus_NamedAlertRuleStatus{
				Id:     namedRule.Id,
				Status: status,
			})
		}
	}

	return &alertv1.GetAlertGroupResponse{AlertGroup: out}, nil
}

func (wt *watch) ListAlertGroup(ctx context.Context, req *alertv1.ListAlertGroupRequest) (*alertv1.ListAlertGroupResponse, error) {
	var (
		out  []*alertv1.ListAlertGroupResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = wt.lockedWithProjectByName(ctx, req.ProjectName, func(ptr *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(ptr.Pointer)
		if err != nil {
			return err
		}
		return storage.getProjectHydrated(ctx, ptr.Name, ptr.Pointer, func(p *typesv1.Project, d []*typesv1.Dashboard, items []*typesv1.AlertGroup) error {
			next, err = cursorForSlice(items, req.Cursor, req.Limit, 10, 100,
				func(sp *typesv1.AlertGroup) string { return sp.Spec.Name },
				func(sp *typesv1.AlertGroup) error {
					out = append(out, &alertv1.ListAlertGroupResponse_ListItem{AlertGroup: sp})
					return nil
				},
			)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	return &alertv1.ListAlertGroupResponse{Items: out, Next: next}, nil
}

func (wt *watch) CreateAlertRule(ctx context.Context, req *alertv1.CreateAlertRuleRequest) (*alertv1.CreateAlertRuleResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost alerts are created on the filesystem"))
}
func (wt *watch) UpdateAlertRule(ctx context.Context, req *alertv1.UpdateAlertRuleRequest) (*alertv1.UpdateAlertRuleResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost alerts are updated on the filesystem"))
}
func (wt *watch) DeleteAlertRule(ctx context.Context, req *alertv1.DeleteAlertRuleRequest) (*alertv1.DeleteAlertRuleResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost alerts are deleted on the filesystem"))
}
func (wt *watch) GetAlertRule(ctx context.Context, req *alertv1.GetAlertRuleRequest) (*alertv1.GetAlertRuleResponse, error) {
	var (
		out *typesv1.AlertRule
		err error
	)
	err = wt.lockedWithAlertByName(ctx, req.ProjectName, req.GroupName, req.Name, func(ar *typesv1.AlertRule) error {
		out = ar
		return nil
	})
	return &alertv1.GetAlertRuleResponse{AlertRule: out}, err
}
func (wt *watch) ListAlertRule(ctx context.Context, req *alertv1.ListAlertRuleRequest) (*alertv1.ListAlertRuleResponse, error) {
	var (
		out  []*alertv1.ListAlertRuleResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = wt.lockedWithAlertGroupByName(ctx, req.ProjectName, req.GroupName, func(_ *typesv1.ProjectsConfig_Project, ag *typesv1.AlertGroup) error {
		// Note: ag.Status.Rules is already hydrated by lockedWithAlertGroupByName
		// Create a map for quick lookup by id
		stateById := make(map[string]*typesv1.AlertRuleStatus)
		for _, namedStatus := range ag.Status.Rules {
			stateById[namedStatus.Id] = namedStatus.Status
		}

		next, err = cursorForSlice(ag.Spec.Rules, req.Cursor, req.Limit, 10, 100,
			func(named *typesv1.AlertGroupSpec_NamedAlertRuleSpec) string { return named.Id },
			func(named *typesv1.AlertGroupSpec_NamedAlertRuleSpec) error {
				// Get the actual runtime state for this rule
				state, ok := stateById[named.Id]
				if !ok {
					state = &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{Unknown: &typesv1.AlertUnknown{}}}
				}

				// Construct full AlertRule from spec + state
				rule := &typesv1.AlertRule{
					Meta: &typesv1.AlertRuleMeta{
						Id: named.Id,
					},
					Spec:   named.Spec,
					Status: state,
				}
				out = append(out, &alertv1.ListAlertRuleResponse_ListItem{AlertRule: rule})
				return nil
			},
		)
		return err
	})
	return &alertv1.ListAlertRuleResponse{Items: out, Next: next}, err
}

func (wt *watch) lockedWithProjectConfig(ctx context.Context, fn func(*typesv1.ProjectsConfig) error) error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	// make sure we load the latest data
	cfg, err := wt.cfg.Reload()
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("reloading config file: %v", err))
	}
	wt.cfg = cfg

	projects := cfg.GetRuntime().GetExperimentalFeatures().GetProjects()
	if projects == nil {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("localhost projects are not enabled, set them up in your config file"))
	}
	return fn(projects)
}

func (wt *watch) lockedWithProjectByName(ctx context.Context, name string, fn func(*typesv1.ProjectsConfig_Project) error) error {
	return wt.lockedWithProjectConfig(ctx, func(sc *typesv1.ProjectsConfig) error {

		for _, pointer := range sc.Projects {
			if pointer.Name == name {
				return fn(pointer)
			}
		}
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("localhost doesn't have a project named %q in its config file", name))
	})
}

func (wt *watch) lockedWithDashboardByID(ctx context.Context, projectName, id string, fn func(*typesv1.Dashboard) error) error {
	return wt.lockedWithProjectByName(ctx, projectName, func(sc *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(sc.Pointer)
		if err != nil {
			return err
		}
		return storage.getDashboard(ctx, projectName, sc.Pointer, id, fn)
	})
}

func (wt *watch) lockedWithAlertGroupByName(ctx context.Context, projectName, groupName string, fn func(*typesv1.ProjectsConfig_Project, *typesv1.AlertGroup) error) error {
	return wt.lockedWithProjectByName(ctx, projectName, func(sc *typesv1.ProjectsConfig_Project) error {
		storage, err := wt.storageForPointer(sc.Pointer)
		if err != nil {
			return err
		}
		return storage.getAlertGroup(ctx, wt.alertState, projectName, sc.Pointer, groupName, func(ag *typesv1.AlertGroup) error {
			return fn(sc, ag)
		})
	})
}

func (wt *watch) lockedWithAlertByName(ctx context.Context, projectName, groupName, name string, fn func(*typesv1.AlertRule) error) error {
	return wt.lockedWithAlertGroupByName(ctx, projectName, groupName, func(sc *typesv1.ProjectsConfig_Project, ag *typesv1.AlertGroup) error {
		storage, err := wt.storageForPointer(sc.Pointer)
		if err != nil {
			return err
		}

		return storage.getAlertRule(ctx, wt.alertState, projectName, sc.Pointer, groupName, name, fn)
	})
}

// sharedDashboardDirWarning returns a warning message for projects sharing the same dashboard directory
func sharedDashboardDirWarning(otherProjectName, dirPath string) string {
	return fmt.Sprintf("Project %q shares the same dashboard directory (%s). Changes in one project will affect the other.", otherProjectName, dirPath)
}

// sharedAlertDirWarning returns a warning message for projects sharing the same alert directory
func sharedAlertDirWarning(otherProjectName, dirPath string) string {
	return fmt.Sprintf("Project %q shares the same alert directory (%s). Changes in one project will affect the other.", otherProjectName, dirPath)
}

// enrichProjectWithWarnings populates all warnings for a project status
// This should be called whenever returning a project to ensure warnings are up-to-date
func (wt *watch) enrichProjectWithWarnings(status *typesv1.ProjectStatus, ptr *typesv1.ProjectsConfig_Project) {
	wt.addDirectoryConflictWarnings(status, ptr)
	// Future warning checks can be added here
}

// computeProjectWarnings computes warnings for a project without modifying it
// Used by ValidateProject to preview warnings before creating the project
func (wt *watch) computeProjectWarnings(currentPtr *typesv1.ProjectsConfig_Project, allProjects []*typesv1.ProjectsConfig_Project) []string {
	// Only check for localhost projects
	localhost := currentPtr.Pointer.GetLocalhost()
	if localhost == nil {
		return nil
	}

	var conflicts []string
	for _, otherProj := range allProjects {
		if otherProj.Name == currentPtr.Name {
			continue // Skip self
		}

		otherLocalhost := otherProj.Pointer.GetLocalhost()
		if otherLocalhost == nil {
			continue // Only check localhost projects
		}

		// Check if dashboard directories match
		currentDashDir := filepath.Join(localhost.Path, localhost.DashboardDir)
		otherDashDir := filepath.Join(otherLocalhost.Path, otherLocalhost.DashboardDir)
		if currentDashDir == otherDashDir {
			conflicts = append(conflicts, sharedDashboardDirWarning(otherProj.Name, currentDashDir))
		}

		// Check if alert directories match
		currentAlertDir := filepath.Join(localhost.Path, localhost.AlertDir)
		otherAlertDir := filepath.Join(otherLocalhost.Path, otherLocalhost.AlertDir)
		if currentAlertDir == otherAlertDir {
			conflicts = append(conflicts, sharedAlertDirWarning(otherProj.Name, currentAlertDir))
		}
	}

	return conflicts
}

// addDirectoryConflictWarnings checks if this project shares directories with other projects
// and adds warnings to the project status if conflicts are found
func (wt *watch) addDirectoryConflictWarnings(status *typesv1.ProjectStatus, currentPtr *typesv1.ProjectsConfig_Project) {
	cfg, err := wt.cfg.Reload()
	if err != nil {
		return // Can't check, skip
	}

	if cfg.Runtime == nil || cfg.Runtime.ExperimentalFeatures == nil || cfg.Runtime.ExperimentalFeatures.Projects == nil {
		return
	}

	conflicts := wt.computeProjectWarnings(currentPtr, cfg.Runtime.ExperimentalFeatures.Projects.Projects)
	if len(conflicts) > 0 {
		status.Warnings = append(status.Warnings, conflicts...)
	}
}

func dashboardID(projectName, persesProjectName, dashboardName string) string {
	h := blake3.New()
	h.WriteString(projectName)
	h.WriteString(persesProjectName)
	h.WriteString(dashboardName)
	return "hdash_" + strings.ToLower(base32.HexEncoding.WithPadding(base32.NoPadding).EncodeToString(h.Sum(nil)))
}

type stringPage struct {
	LastID string `json:"lastID"`
}

func cursorForSlice[E any](sl []E, cursor *typesv1.Cursor, limit, minLimit, maxLimit int32, toStringID func(E) string, forEach func(E) error) (next *typesv1.Cursor, err error) {
	limit = max(limit, maxLimit)
	limit = min(limit, minLimit)
	var fromID string
	if cursor != nil {
		var p stringPage
		if err := json.Unmarshal(cursor.Opaque, &p); err != nil {
			return nil, err
		}
		fromID = p.LastID
	}
	var i int
	if fromID != "" {
		i = slices.IndexFunc(sl, func(e E) bool { return toStringID(e) == fromID }) + 1
	}
	from := i
	to := min(i+int(limit), len(sl))
	out := sl[from:to]
	for _, el := range out {
		if err := forEach(el); err != nil {
			return nil, err
		}
	}
	if len(out) == int(limit) && limit != 0 {
		next = new(typesv1.Cursor)
		p := stringPage{LastID: toStringID(out[len(out)-1])}
		next.Opaque, err = json.Marshal(p)
		if err != nil {
			return nil, err
		}
	}
	return next, nil
}
