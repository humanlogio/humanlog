package localstate

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"connectrpc.com/connect"
	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	dashboardv1 "github.com/humanlogio/api/go/svc/dashboard/v1"
	projectv1 "github.com/humanlogio/api/go/svc/project/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/oklog/ulid/v2"
	persesv1 "github.com/perses/perses/pkg/model/api/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ DB = (*Mem)(nil)

type Mem struct {
	mu sync.Mutex

	projectlist []string
	projects    map[string]*project
}

type project struct {
	project *typesv1.Project

	dashboardlist []string
	dashboards    map[string]*typesv1.Dashboard

	alertGroupList []string
	alertGroups    map[string]*alertGroup
}

type alertGroup struct {
	group *typesv1.AlertGroup

	// Full alert rules (Meta+Spec+Status), keyed by name
	rules map[string]*typesv1.AlertRule
}

func NewMemory() *Mem {
	db := &Mem{
		projects: make(map[string]*project),
	}
	projectRes, err := db.CreateProject(context.Background(), &projectv1.CreateProjectRequest{
		Spec: &typesv1.ProjectSpec{
			Name: "test-ephemeral-project",
			Pointer: &typesv1.ProjectPointer{Scheme: &typesv1.ProjectPointer_Db{
				Db: &typesv1.ProjectPointer_Virtual{
					Uri: "db://localhost/test-ephemeral-project",
				},
			}},
		},
	})
	if err != nil {
		panic(err)
	}
	project := projectRes.Project
	_, err = db.CreateDashboard(context.Background(), &dashboardv1.CreateDashboardRequest{
		ProjectName: project.Spec.Name,
		Spec:        defaultDashboard.Spec,
	})
	if err != nil {
		panic(err)
	}
	return db
}

func (db *Mem) withProject(name string, fn func(st *project) error) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	for _, project := range db.projects {
		if project.project.Spec.Name == name {
			return fn(project)
		}
	}
	return connect.NewError(connect.CodeNotFound, fmt.Errorf("no project with name %q exists", name))
}

func (db *Mem) withAlertGroup(projectName, alertGroupName string, fn func(st *project, group *alertGroup) error) error {
	return db.withProject(projectName, func(st *project) error {
		group, ok := st.alertGroups[alertGroupName]
		if !ok {
			return connect.NewError(connect.CodeNotFound, fmt.Errorf("no alert group with name %q exists", alertGroupName))
		}
		return fn(st, group)
	})
}

func (db *Mem) CreateProject(ctx context.Context, req *projectv1.CreateProjectRequest) (*projectv1.CreateProjectResponse, error) {
	out := &typesv1.Project{
		Meta: &typesv1.ProjectMeta{
			Id: ulid.Make().String(),
		},
		Spec: req.Spec,
		Status: &typesv1.ProjectStatus{
			CreatedAt: timestamppb.Now(),
			UpdatedAt: timestamppb.Now(),
		},
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	for _, project := range db.projects {
		if project.project.Spec.Name == req.Spec.Name {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("project %q already uses the name %q", project.project.Meta.Id, req.Spec.Name))
		}
	}
	db.projectlist = append(db.projectlist, out.Meta.Id)
	db.projects[out.Meta.Id] = &project{
		project:     out,
		dashboards:  make(map[string]*typesv1.Dashboard),
		alertGroups: make(map[string]*alertGroup),
	}
	return &projectv1.CreateProjectResponse{Project: out}, nil
}

func (db *Mem) ValidateProject(ctx context.Context, req *projectv1.ValidateProjectRequest) (*projectv1.ValidateProjectResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Run the same validation as CreateProject
	for _, project := range db.projects {
		if project.project.Spec.Name == req.Spec.Name {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("project %q already uses the name %q", project.project.Meta.Id, req.Spec.Name))
		}
	}

	// In-memory projects don't have directory conflicts, so return empty status
	return &projectv1.ValidateProjectResponse{
		Status: &typesv1.ProjectStatus{},
	}, nil
}

func (db *Mem) SyncProject(ctx context.Context, req *projectv1.SyncProjectRequest) (*projectv1.SyncProjectResponse, error) {
	var (
		out         *typesv1.Project
		dashboards  []*typesv1.Dashboard
		alertGroups []*typesv1.AlertGroup
	)
	err := db.withProject(req.Name, func(st *project) error {
		out = st.project
		for _, key := range st.dashboardlist {
			dashboards = append(dashboards, st.dashboards[key])
		}
		for _, key := range st.alertGroupList {
			alertGroups = append(alertGroups, st.alertGroups[key].group)
		}
		return nil
	})
	return &projectv1.SyncProjectResponse{Project: out}, err
}

func (db *Mem) GetProject(ctx context.Context, req *projectv1.GetProjectRequest) (*projectv1.GetProjectResponse, error) {
	var (
		out         *typesv1.Project
		dashboards  []*typesv1.Dashboard
		alertGroups []*typesv1.AlertGroup
	)
	err := db.withProject(req.Name, func(st *project) error {
		out = st.project
		for _, key := range st.dashboardlist {
			dashboards = append(dashboards, st.dashboards[key])
		}
		for _, key := range st.alertGroupList {
			alertGroups = append(alertGroups, st.alertGroups[key].group)
		}
		return nil
	})
	return &projectv1.GetProjectResponse{Project: out, Dashboards: dashboards, AlertGroups: alertGroups}, err
}

func (db *Mem) UpdateProject(ctx context.Context, req *projectv1.UpdateProjectRequest) (*projectv1.UpdateProjectResponse, error) {
	var out *typesv1.Project
	err := db.withProject(req.Name, func(st *project) error {
		out = st.project
		out.Spec = req.Spec
		out.Status.UpdatedAt = timestamppb.Now()
		return nil
	})
	return &projectv1.UpdateProjectResponse{Project: out}, err
}

func (db *Mem) DeleteProject(ctx context.Context, req *projectv1.DeleteProjectRequest) (*projectv1.DeleteProjectResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	for _, project := range db.projects {
		if project.project.Spec.Name == req.Name {
			delete(db.projects, project.project.Meta.Id)
			db.projectlist = slices.DeleteFunc(db.projectlist, func(e string) bool { return e == project.project.Meta.Id })

			return nil, nil
		}
	}
	return nil, nil
}

func (db *Mem) ListProject(ctx context.Context, req *projectv1.ListProjectRequest) (*projectv1.ListProjectResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	cursor := req.Cursor
	limit := req.Limit
	limit = max(limit, 100)
	limit = min(limit, 10)

	var (
		out  []*projectv1.ListProjectResponse_ListItem
		next *typesv1.Cursor
		err  error
	)

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
		i = slices.IndexFunc(db.projectlist, func(e string) bool { return e == fromID }) + 1
	}
	if i > len(db.projectlist) {
		return &projectv1.ListProjectResponse{}, nil
	}
	from := i
	to := min(i+int(limit), len(db.projectlist))

	for _, id := range db.projectlist[from:to] {
		st := db.projects[id]
		out = append(out, &projectv1.ListProjectResponse_ListItem{Project: st.project})
	}
	if len(out) == int(limit) && limit != 0 {
		next = new(typesv1.Cursor)
		p := stringPage{LastID: out[len(out)-1].Project.Meta.Id}
		next.Opaque, err = json.Marshal(p)
		if err != nil {
			return nil, err
		}
	}
	return &projectv1.ListProjectResponse{}, nil
}

func (db *Mem) CreateDashboard(ctx context.Context, req *dashboardv1.CreateDashboardRequest) (*dashboardv1.CreateDashboardResponse, error) {
	var out *typesv1.Dashboard
	err := db.withProject(req.ProjectName, func(st *project) error {
		id := ulid.Make()
		out = &typesv1.Dashboard{
			Meta: &typesv1.DashboardMeta{
				Id: id.String(),
			},
			Spec: req.Spec,
			Status: &typesv1.DashboardStatus{
				CreatedAt: timestamppb.Now(),
				UpdatedAt: timestamppb.Now(),
			},
		}

		st.dashboards[out.Meta.Id] = out
		st.dashboardlist = append(st.dashboardlist, out.Meta.Id)
		return nil
	})
	return &dashboardv1.CreateDashboardResponse{Dashboard: out}, err
}

func (db *Mem) GetDashboard(ctx context.Context, req *dashboardv1.GetDashboardRequest) (*dashboardv1.GetDashboardResponse, error) {
	var out *typesv1.Dashboard
	err := db.withProject(req.ProjectName, func(st *project) error {
		var ok bool
		out, ok = st.dashboards[req.Id]

		if !ok {
			return connect.NewError(connect.CodeNotFound, fmt.Errorf("no such dashboard"))
		}
		return nil
	})
	return &dashboardv1.GetDashboardResponse{Dashboard: out}, err
}

func (db *Mem) UpdateDashboard(ctx context.Context, req *dashboardv1.UpdateDashboardRequest) (*dashboardv1.UpdateDashboardResponse, error) {
	var out *typesv1.Dashboard
	err := db.withProject(req.ProjectName, func(st *project) error {
		var ok bool
		out, ok = st.dashboards[req.Id]
		if !ok {
			return connect.NewError(connect.CodeNotFound, fmt.Errorf("no such dashboard"))
		}
		out.Spec = req.Spec
		out.Status.UpdatedAt = timestamppb.Now()
		st.dashboards[out.Meta.Id] = out
		return nil
	})
	return &dashboardv1.UpdateDashboardResponse{Dashboard: out}, err
}

func (db *Mem) DeleteDashboard(ctx context.Context, req *dashboardv1.DeleteDashboardRequest) (*dashboardv1.DeleteDashboardResponse, error) {
	err := db.withProject(req.ProjectName, func(st *project) error {
		delete(st.dashboards, req.Id)
		st.dashboardlist = slices.DeleteFunc(st.dashboardlist, func(e string) bool { return e == req.Id })
		return nil
	})
	return &dashboardv1.DeleteDashboardResponse{}, err
}

type stringPage struct {
	LastID string `json:"lastID"`
}

func (db *Mem) ListDashboard(ctx context.Context, req *dashboardv1.ListDashboardRequest) (*dashboardv1.ListDashboardResponse, error) {
	cursor := req.Cursor
	limit := req.Limit
	limit = max(limit, 100)
	limit = min(limit, 10)

	var (
		out  []*dashboardv1.ListDashboardResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = db.withProject(req.ProjectName, func(st *project) error {
		var fromID string
		if cursor != nil {
			var p stringPage
			if err := json.Unmarshal(cursor.Opaque, &p); err != nil {
				return err
			}
			fromID = p.LastID
		}
		var i int
		if fromID != "" {
			i = slices.IndexFunc(st.dashboardlist, func(e string) bool { return e == fromID }) + 1
		}
		if i > len(st.dashboardlist) {
			return nil
		}
		from := i
		to := min(i+int(limit), len(st.dashboardlist))

		for _, id := range st.dashboardlist[from:to] {
			d := st.dashboards[id]
			out = append(out, &dashboardv1.ListDashboardResponse_ListItem{Dashboard: d})
		}
		if len(out) == int(limit) && limit != 0 {
			next = new(typesv1.Cursor)
			p := stringPage{LastID: out[len(out)-1].Dashboard.Meta.Id}
			next.Opaque, err = json.Marshal(p)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return &dashboardv1.ListDashboardResponse{Items: out, Next: next}, err
}

func (db *Mem) CreateAlertGroup(ctx context.Context, req *alertv1.CreateAlertGroupRequest) (*alertv1.CreateAlertGroupResponse, error) {
	var out *typesv1.AlertGroup
	err := db.withProject(req.ProjectName, func(st *project) error {
		out = &typesv1.AlertGroup{
			Meta: &typesv1.AlertGroupMeta{
				Id: ulid.Make().String(),
			},
			Spec:   req.Spec,
			Status: &typesv1.AlertGroupStatus{},
		}
		for _, el := range st.alertGroups {
			if el.group.Spec.Name == req.Spec.Name {
				return connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("an alert group with name %q already exists", req.Spec.Name))
			}
		}
		st.alertGroupList = append(st.alertGroupList, out.Meta.Id)
		st.alertGroups[out.Meta.Id] = &alertGroup{
			group: out,
			rules: make(map[string]*typesv1.AlertRule),
		}
		return nil
	})
	return &alertv1.CreateAlertGroupResponse{AlertGroup: out}, err
}

func (db *Mem) GetAlertGroup(ctx context.Context, req *alertv1.GetAlertGroupRequest) (*alertv1.GetAlertGroupResponse, error) {
	var out *typesv1.AlertGroup
	err := db.withAlertGroup(req.ProjectName, req.Name, func(st *project, ag *alertGroup) error {
		out = ag.group

		// Populate status from AlertRuleStatusStorage
		if out.Status == nil {
			out.Status = &typesv1.AlertGroupStatus{}
		}
		if out.Status.Rules == nil {
			out.Status.Rules = make([]*typesv1.AlertGroupStatus_NamedAlertRuleStatus, 0, len(out.Spec.Rules))
		}

		// Query status for each rule from storage
		alertStorage := db.AlertRuleStatusStorage()
		for _, namedRule := range out.Spec.Rules {
			status, err := alertStorage.AlertGetOrCreate(ctx, req.ProjectName, req.Name, namedRule.Id, func() *typesv1.AlertRuleStatus {
				return &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{}}
			})
			if err != nil {
				return fmt.Errorf("getting alert status for rule %q: %w", namedRule.Id, err)
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

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &alertv1.GetAlertGroupResponse{AlertGroup: out}, nil
}

func (db *Mem) UpdateAlertGroup(ctx context.Context, req *alertv1.UpdateAlertGroupRequest) (*alertv1.UpdateAlertGroupResponse, error) {
	var out *typesv1.AlertGroup
	err := db.withProject(req.ProjectName, func(st *project) error {
		for _, ag := range st.alertGroups {
			if ag.group.Spec.Name == req.Name {
				out := ag.group
				out.Spec = req.Spec
				ag.group = out
				st.alertGroups[out.Meta.Id] = ag
				return nil
			}
		}
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("no such alert group"))
	})
	return &alertv1.UpdateAlertGroupResponse{AlertGroup: out}, err
}

func (db *Mem) DeleteAlertGroup(ctx context.Context, req *alertv1.DeleteAlertGroupRequest) (*alertv1.DeleteAlertGroupResponse, error) {
	err := db.withProject(req.ProjectName, func(st *project) error {
		for _, ag := range st.alertGroups {
			if ag.group.Spec.Name == req.Name {
				delete(st.alertGroups, ag.group.Meta.Id)
				st.alertGroupList = slices.DeleteFunc(st.alertGroupList, func(e string) bool { return e == ag.group.Meta.Id })
				return nil
			}
		}
		return nil
	})
	return &alertv1.DeleteAlertGroupResponse{}, err
}

func (db *Mem) ListAlertGroup(ctx context.Context, req *alertv1.ListAlertGroupRequest) (*alertv1.ListAlertGroupResponse, error) {
	cursor := req.Cursor
	limit := req.Limit
	limit = max(limit, 100)
	limit = min(limit, 10)

	var (
		out  []*alertv1.ListAlertGroupResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = db.withProject(req.ProjectName, func(st *project) error {
		var fromID string
		if cursor != nil {
			var p stringPage
			if err := json.Unmarshal(cursor.Opaque, &p); err != nil {
				return err
			}
			fromID = p.LastID
		}
		var i int
		if fromID != "" {
			i = slices.IndexFunc(st.alertGroupList, func(e string) bool { return e == fromID }) + 1
		}
		if i > len(st.alertGroupList) {
			return nil
		}
		from := i
		to := min(i+int(limit), len(st.alertGroupList))

		for _, id := range st.alertGroupList[from:to] {
			el := st.alertGroups[id]
			out = append(out, &alertv1.ListAlertGroupResponse_ListItem{AlertGroup: el.group})
		}
		if len(out) == int(limit) && limit != 0 {
			next = new(typesv1.Cursor)
			p := stringPage{LastID: out[len(out)-1].AlertGroup.Meta.Id}
			next.Opaque, err = json.Marshal(p)
			if err != nil {
				return err
			}
		}
		return nil
	})

	return &alertv1.ListAlertGroupResponse{Items: out, Next: next}, err
}

func (db *Mem) CreateAlertRule(ctx context.Context, req *alertv1.CreateAlertRuleRequest) (*alertv1.CreateAlertRuleResponse, error) {
	var out *typesv1.AlertRule
	db.withAlertGroup(req.ProjectName, req.GroupName, func(st *project, group *alertGroup) error {
		// Check if rule already exists
		if _, exists := group.rules[req.Name]; exists {
			return connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("an alert with name %q already exists in this group", req.Name))
		}

		// Create the full AlertRule
		item := &typesv1.AlertRule{
			Meta: &typesv1.AlertRuleMeta{
				Id: req.Name,
			},
			Spec: &typesv1.AlertRuleSpec{
				Name:          req.Name,
				Expr:          req.Expr,
				Labels:        req.Labels,
				Annotations:   req.Annotations,
				For:           req.For,
				KeepFiringFor: req.KeepFiringFor,
			},
			Status: &typesv1.AlertRuleStatus{
				Status: &typesv1.AlertRuleStatus_Unknown{Unknown: &typesv1.AlertUnknown{}},
			},
		}

		// Store in rules map
		group.rules[req.Name] = item

		// Also add to group.Spec.Rules slice
		group.group.Spec.Rules = append(group.group.Spec.Rules, &typesv1.AlertGroupSpec_NamedAlertRuleSpec{
			Id:   req.Name,
			Spec: item.Spec,
		})

		out = item
		return nil
	})
	return &alertv1.CreateAlertRuleResponse{AlertRule: out}, nil

}

func (db *Mem) GetAlertRule(ctx context.Context, req *alertv1.GetAlertRuleRequest) (*alertv1.GetAlertRuleResponse, error) {
	var out *typesv1.AlertRule
	err := db.withAlertGroup(req.ProjectName, req.GroupName, func(st *project, group *alertGroup) error {
		rule, ok := group.rules[req.Name]
		if !ok {
			return connect.NewError(connect.CodeNotFound, fmt.Errorf("no such alert"))
		}
		out = rule
		return nil
	})
	return &alertv1.GetAlertRuleResponse{AlertRule: out}, err
}

func (db *Mem) UpdateAlertRule(ctx context.Context, req *alertv1.UpdateAlertRuleRequest) (*alertv1.UpdateAlertRuleResponse, error) {
	var out *typesv1.AlertRule
	err := db.withAlertGroup(req.ProjectName, req.GroupName, func(st *project, group *alertGroup) error {
		rule, ok := group.rules[req.Spec.Name]
		if !ok {
			return connect.NewError(connect.CodeNotFound, fmt.Errorf("no such alert"))
		}

		// Update the spec
		rule.Spec = req.Spec

		// Update in rules map
		group.rules[req.Spec.Name] = rule

		// Update in group.Spec.Rules slice
		for i, named := range group.group.Spec.Rules {
			if named.Id == req.Spec.Name {
				group.group.Spec.Rules[i].Spec = rule.Spec
				break
			}
		}

		out = rule
		return nil
	})
	return &alertv1.UpdateAlertRuleResponse{AlertRule: out}, err
}

func (db *Mem) DeleteAlertRule(ctx context.Context, req *alertv1.DeleteAlertRuleRequest) (*alertv1.DeleteAlertRuleResponse, error) {
	err := db.withAlertGroup(req.ProjectName, req.GroupName, func(st *project, group *alertGroup) error {
		// Delete from rules map
		delete(group.rules, req.Name)

		// Delete from group.Spec.Rules slice
		group.group.Spec.Rules = slices.DeleteFunc(group.group.Spec.Rules, func(e *typesv1.AlertGroupSpec_NamedAlertRuleSpec) bool {
			return e.Id == req.Name
		})
		return nil
	})
	return &alertv1.DeleteAlertRuleResponse{}, err
}

func (db *Mem) ListAlertRule(ctx context.Context, req *alertv1.ListAlertRuleRequest) (*alertv1.ListAlertRuleResponse, error) {
	cursor := req.Cursor
	limit := req.Limit
	limit = max(limit, 100)
	limit = min(limit, 10)

	var (
		out  []*alertv1.ListAlertRuleResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = db.withAlertGroup(req.ProjectName, req.GroupName, func(st *project, group *alertGroup) error {
		// Iterate Spec.Rules in order (preserves YAML order)
		var fromName string
		if cursor != nil {
			var p stringPage
			if err := json.Unmarshal(cursor.Opaque, &p); err != nil {
				return err
			}
			fromName = p.LastID
		}
		var i int
		if fromName != "" {
			i = slices.IndexFunc(group.group.Spec.Rules, func(e *typesv1.AlertGroupSpec_NamedAlertRuleSpec) bool {
				return e.Id == fromName
			}) + 1
		}
		if i >= len(group.group.Spec.Rules) {
			return nil
		}
		from := i
		to := min(i+int(limit), len(group.group.Spec.Rules))

		for _, named := range group.group.Spec.Rules[from:to] {
			rule := group.rules[named.Id]
			out = append(out, &alertv1.ListAlertRuleResponse_ListItem{
				AlertRule: rule,
			})
		}
		if len(out) == int(limit) && limit != 0 {
			next = new(typesv1.Cursor)
			p := stringPage{LastID: out[len(out)-1].AlertRule.Spec.Name}
			next.Opaque, err = json.Marshal(p)
			if err != nil {
				return err
			}
		}
		return nil
	})

	return &alertv1.ListAlertRuleResponse{Items: out, Next: next}, err
}

func (db *Mem) AlertRuleStatusStorage() localstorage.Alertable {
	return &alertStorageMem{db: db}
}

type alertStorageMem struct {
	db *Mem
}

func (as *alertStorageMem) AlertGetOrCreate(ctx context.Context, projectName, groupName, alertName string, create func() *typesv1.AlertRuleStatus) (*typesv1.AlertRuleStatus, error) {
	var out *typesv1.AlertRuleStatus
	err := as.db.withAlertGroup(projectName, groupName, func(st *project, group *alertGroup) error {
		rule, ok := group.rules[alertName]
		if !ok {
			return fmt.Errorf("no alert named %q", alertName)
		}

		// If status is nil, create it
		if rule.Status == nil {
			rule.Status = create()
			group.rules[alertName] = rule
		}

		out = rule.Status
		return nil
	})
	return out, err
}

func (as *alertStorageMem) AlertUpdateState(ctx context.Context, projectName, groupName, alertName string, state *typesv1.AlertRuleStatus) error {
	err := as.db.withAlertGroup(projectName, groupName, func(st *project, group *alertGroup) error {
		rule, ok := group.rules[alertName]
		if !ok {
			return fmt.Errorf("no alert named %q", alertName)
		}

		rule.Status = state
		group.rules[alertName] = rule
		return nil
	})
	return err
}

func (as *alertStorageMem) AlertDeleteStateNotInList(ctx context.Context, projectName, groupName string, keeplist []string) error {
	err := as.db.withAlertGroup(projectName, groupName, func(st *project, group *alertGroup) error {
		keepset := make(map[string]struct{})
		for _, keep := range keeplist {
			keepset[keep] = struct{}{}
		}

		var toDelete []string
		for key := range group.rules {
			if _, ok := keepset[key]; !ok {
				toDelete = append(toDelete, key)
			}
		}

		for _, key := range toDelete {
			delete(group.rules, key)
			// Also remove from group.Spec.Rules
			group.group.Spec.Rules = slices.DeleteFunc(group.group.Spec.Rules, func(e *typesv1.AlertGroupSpec_NamedAlertRuleSpec) bool {
				return e.Id == key
			})
		}
		return nil
	})
	return err
}

var defaultDashboard = func() *typesv1.Dashboard {
	var d persesv1.Dashboard
	if err := json.Unmarshal([]byte(testdashboard), &d); err != nil {
		panic(err)
	}
	out := &typesv1.Dashboard{
		Meta: &typesv1.DashboardMeta{
			Id: ulid.Make().String(),
		},
		Spec: &typesv1.DashboardSpec{
			Name:        d.Spec.Display.Name,
			Description: d.Spec.Display.Description,
			IsReadonly:  true,
			PersesJson:  []byte(testdashboard),
		},
		Status: &typesv1.DashboardStatus{
			CreatedAt: timestamppb.New(d.Metadata.CreatedAt),
			UpdatedAt: timestamppb.New(d.Metadata.UpdatedAt),
		},
	}
	return out
}()

const testdashboard = `{"kind":"Dashboard","metadata":{"name":"monitoring-dashboard","project":"default"},"spec":{"display":{"name":"Monitoring Dashboard","description":"System monitoring dashboard with memory, status, and CPU metrics"},"datasources":{"prometheus":{"default":true,"plugin":{"kind":"PrometheusDatasource","spec":{"directUrl":"http://localhost:9090"}}}},"duration":"1h","refreshInterval":"30s","variables":[],"panels":{"memory-panel":{"kind":"Panel","spec":{"display":{"name":"Memory Usage","description":"Go memory allocation in bytes"},"plugin":{"kind":"TimeSeriesChart","spec":{"legend":{"position":"bottom","size":"small"},"yAxis":{"show":true,"label":"Bytes"}}},"queries":[{"kind":"PrometheusTimeSeriesQuery","spec":{"plugin":{"kind":"PrometheusTimeSeriesQuery","spec":{"query":"go_memstats_alloc_bytes","datasource":{"kind":"PrometheusDatasource","name":"prometheus"}}}}}]}},"status-panel":{"kind":"Panel","spec":{"display":{"name":"Service Status","description":"Service availability status"},"plugin":{"kind":"TimeSeriesChart","spec":{"legend":{"position":"bottom","size":"small"},"yAxis":{"show":true,"label":"Status"}}},"queries":[{"kind":"PrometheusTimeSeriesQuery","spec":{"plugin":{"kind":"PrometheusTimeSeriesQuery","spec":{"query":"up","datasource":{"kind":"PrometheusDatasource","name":"prometheus"}}}}}]}},"cpu-panel":{"kind":"Panel","spec":{"display":{"name":"CPU Usage","description":"CPU garbage collection duration rate"},"plugin":{"kind":"TimeSeriesChart","spec":{"legend":{"position":"bottom","size":"small"},"yAxis":{"show":true,"label":"Rate"}}},"queries":[{"kind":"PrometheusTimeSeriesQuery","spec":{"plugin":{"kind":"PrometheusTimeSeriesQuery","spec":{"query":"rate(go_gc_duration_seconds_sum[5m])","datasource":{"kind":"PrometheusDatasource","name":"prometheus"}}}}}]}}},"layouts":[{"kind":"Grid","spec":{"display":{"title":"System Metrics","collapse":{"open":true}},"items":[{"x":0,"y":0,"width":12,"height":8,"content":{"$ref":"#/spec/panels/memory-panel"}},{"x":0,"y":8,"width":12,"height":8,"content":{"$ref":"#/spec/panels/status-panel"}},{"x":0,"y":16,"width":12,"height":8,"content":{"$ref":"#/spec/panels/cpu-panel"}}]}}]}}`
