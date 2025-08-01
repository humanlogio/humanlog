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

	alertState map[string]*typesv1.AlertState
}

func NewMemory() *Mem {
	db := &Mem{
		projects: make(map[string]*project),
	}
	projectRes, err := db.CreateProject(context.Background(), &projectv1.CreateProjectRequest{
		Name: "test-ephemeral-project",
		Pointer: &typesv1.ProjectPointer{Scheme: &typesv1.ProjectPointer_Db{
			Db: &typesv1.ProjectPointer_Virtual{
				Uri: "db://localhost/test-ephemeral-project",
			},
		}},
	})
	if err != nil {
		panic(err)
	}
	project := projectRes.Project
	_, err = db.CreateDashboard(context.Background(), &dashboardv1.CreateDashboardRequest{
		ProjectName: project.Name,
		Name:        defaultDashboard.Name,
		Description: defaultDashboard.Description,
		IsReadonly:  defaultDashboard.IsReadonly,
		PersesJson:  defaultDashboard.PersesJson,
	})
	if err != nil {
		panic(err)
	}
	return db
}

func (db *Mem) withProject(name string, fn func(st *project) error) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	project, ok := db.projects[name]
	if !ok {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("no project with name %q exists", name))
	}
	return fn(project)
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
		Name:      req.Name,
		Pointer:   req.Pointer,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	for _, project := range db.projects {
		if project.project.Name == req.Name {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("project %q already uses the name %q", project.project.Name, req.Name))
		}
	}
	db.projectlist = append(db.projectlist, out.Name)
	db.projects[out.Name] = &project{
		project:     out,
		dashboards:  make(map[string]*typesv1.Dashboard),
		alertGroups: make(map[string]*alertGroup),
	}
	return &projectv1.CreateProjectResponse{Project: out}, nil
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
		for _, mutation := range req.Mutations {
			switch do := mutation.Do.(type) {
			case *projectv1.UpdateProjectRequest_Mutation_SetName:
				out.Name = do.SetName
			case *projectv1.UpdateProjectRequest_Mutation_SetPointer:
				out.Pointer = do.SetPointer
			}
		}
		out.UpdatedAt = timestamppb.Now()
		return nil
	})
	return &projectv1.UpdateProjectResponse{Project: out}, err
}

func (db *Mem) DeleteProject(ctx context.Context, req *projectv1.DeleteProjectRequest) (*projectv1.DeleteProjectResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.projects, req.Name)
	db.projectlist = slices.DeleteFunc(db.projectlist, func(e string) bool { return e == req.Name })
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

	for _, name := range db.projectlist[from:to] {
		st := db.projects[name]
		out = append(out, &projectv1.ListProjectResponse_ListItem{Project: st.project})
	}
	if len(out) == int(limit) && limit != 0 {
		next = new(typesv1.Cursor)
		p := stringPage{LastID: out[len(out)-1].Project.Name}
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
			Id:          id.String(),
			Name:        req.Name,
			Description: req.Description,
			IsReadonly:  req.IsReadonly,
			PersesJson:  req.PersesJson,
			CreatedAt:   timestamppb.Now(),
			UpdatedAt:   timestamppb.Now(),
		}

		st.dashboards[out.Id] = out
		st.dashboardlist = append(st.dashboardlist, out.Id)
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
		for _, mutation := range req.Mutations {
			switch do := mutation.Do.(type) {
			case *dashboardv1.UpdateDashboardRequest_Mutation_SetName:
				out.Name = do.SetName
			case *dashboardv1.UpdateDashboardRequest_Mutation_SetDescription:
				out.Description = do.SetDescription
			case *dashboardv1.UpdateDashboardRequest_Mutation_SetReadonly:
				out.IsReadonly = do.SetReadonly
			case *dashboardv1.UpdateDashboardRequest_Mutation_SetSourceFile:
				out.Source = &typesv1.Dashboard_File{File: do.SetSourceFile}
			case *dashboardv1.UpdateDashboardRequest_Mutation_SetPersesJson:
				out.PersesJson = do.SetPersesJson
			}
		}
		out.UpdatedAt = timestamppb.Now()
		st.dashboards[out.Id] = out
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
			p := stringPage{LastID: out[len(out)-1].Dashboard.Id}
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
			Name:        req.Name,
			Interval:    req.Interval,
			QueryOffset: req.QueryOffset,
			Limit:       req.Limit,
			Rules:       req.Rules,
			Labels:      req.Labels,
		}
		for _, el := range st.alertGroups {
			if el.group.Name == req.Name {
				return connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("an alert group with name %q already exists", req.Name))
			}
		}
		st.alertGroupList = append(st.alertGroupList, out.Name)
		st.alertGroups[out.Name] = &alertGroup{
			group:      out,
			alertState: make(map[string]*typesv1.AlertState),
		}
		return nil
	})
	return &alertv1.CreateAlertGroupResponse{AlertGroup: out}, err
}

func (db *Mem) GetAlertGroup(ctx context.Context, req *alertv1.GetAlertGroupRequest) (*alertv1.GetAlertGroupResponse, error) {
	var out *typesv1.AlertGroup
	err := db.withProject(req.ProjectName, func(st *project) error {
		group, ok := st.alertGroups[req.Name]
		if !ok {
			return connect.NewError(connect.CodeNotFound, fmt.Errorf("no such alert group: %q", req.Name))
		}
		out = group.group
		return nil
	})
	return &alertv1.GetAlertGroupResponse{AlertGroup: out}, err
}

func (db *Mem) UpdateAlertGroup(ctx context.Context, req *alertv1.UpdateAlertGroupRequest) (*alertv1.UpdateAlertGroupResponse, error) {
	var out *typesv1.AlertGroup
	err := db.withProject(req.ProjectName, func(st *project) error {
		group, ok := st.alertGroups[req.Name]
		if !ok {
			return connect.NewError(connect.CodeNotFound, fmt.Errorf("no such alert group"))
		}
		out := group.group
		for _, mutation := range req.Mutations {
			switch do := mutation.Do.(type) {
			case *alertv1.UpdateAlertGroupRequest_Mutation_SetName:
				out.Name = do.SetName
			case *alertv1.UpdateAlertGroupRequest_Mutation_SetInterval:
				out.Interval = do.SetInterval
			case *alertv1.UpdateAlertGroupRequest_Mutation_SetQueryOffset:
				out.QueryOffset = do.SetQueryOffset
			case *alertv1.UpdateAlertGroupRequest_Mutation_SetLimit:
				out.Limit = do.SetLimit
			case *alertv1.UpdateAlertGroupRequest_Mutation_SetLabels:
				out.Labels = do.SetLabels
			}
		}
		group.group = out
		st.alertGroups[out.Name] = group
		return nil
	})
	return &alertv1.UpdateAlertGroupResponse{AlertGroup: out}, err
}

func (db *Mem) DeleteAlertGroup(ctx context.Context, req *alertv1.DeleteAlertGroupRequest) (*alertv1.DeleteAlertGroupResponse, error) {
	err := db.withProject(req.ProjectName, func(st *project) error {
		delete(st.alertGroups, req.Name)
		st.alertGroupList = slices.DeleteFunc(st.alertGroupList, func(e string) bool { return e == req.Name })
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
			p := stringPage{LastID: out[len(out)-1].AlertGroup.Name}
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
		item := &typesv1.AlertRule{
			Name:          req.Name,
			Expr:          req.Expr,
			Labels:        req.Labels,
			Annotations:   req.Annotations,
			For:           req.For,
			KeepFiringFor: req.KeepFiringFor,
		}
		for _, el := range group.group.Rules {
			if el.Name == req.Name {
				return connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("an alert with name %q already exists in this group", req.Name))
			}
		}
		group.group.Rules = append(group.group.Rules, item)
		return nil
	})
	return &alertv1.CreateAlertRuleResponse{AlertRule: out}, nil

}

func (db *Mem) GetAlertRule(ctx context.Context, req *alertv1.GetAlertRuleRequest) (*alertv1.GetAlertRuleResponse, error) {
	var out *typesv1.AlertRule
	err := db.withAlertGroup(req.ProjectName, req.GroupName, func(st *project, group *alertGroup) error {
		for _, el := range group.group.Rules {
			if el.Name == req.Name {
				out = el
				return nil
			}
		}
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("no such alert"))

	})
	return &alertv1.GetAlertRuleResponse{AlertRule: out}, err
}

func (db *Mem) UpdateAlertRule(ctx context.Context, req *alertv1.UpdateAlertRuleRequest) (*alertv1.UpdateAlertRuleResponse, error) {
	applyMutations := func(out *typesv1.AlertRule) (*typesv1.AlertRule, error) {
		if out == nil {
			return out, nil
		}
		for _, mutation := range req.Mutations {
			switch do := mutation.Do.(type) {
			case *alertv1.UpdateAlertRuleRequest_Mutation_SetName:
				out.Name = do.SetName
			case *alertv1.UpdateAlertRuleRequest_Mutation_SetExpr:
				out.Expr = do.SetExpr
			case *alertv1.UpdateAlertRuleRequest_Mutation_SetLabels:
				out.Labels = do.SetLabels
			case *alertv1.UpdateAlertRuleRequest_Mutation_SetAnnotations:
				out.Annotations = do.SetAnnotations
			case *alertv1.UpdateAlertRuleRequest_Mutation_SetFor:
				out.For = do.SetFor
			case *alertv1.UpdateAlertRuleRequest_Mutation_SetKeepFiringFor:
				out.KeepFiringFor = do.SetKeepFiringFor
			}
		}
		return out, nil
	}

	var (
		out *typesv1.AlertRule
		err error
	)
	err = db.withAlertGroup(req.ProjectName, req.GroupName, func(st *project, group *alertGroup) error {
		for _, el := range group.group.Rules {
			if el.Name == req.Name {
				out, err = applyMutations(el)
				return err
			}
		}
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("no such alert"))

	})
	return &alertv1.UpdateAlertRuleResponse{AlertRule: out}, err
}

func (db *Mem) DeleteAlertRule(ctx context.Context, req *alertv1.DeleteAlertRuleRequest) (*alertv1.DeleteAlertRuleResponse, error) {
	err := db.withAlertGroup(req.ProjectName, req.GroupName, func(st *project, group *alertGroup) error {
		group.group.Rules = slices.DeleteFunc(group.group.Rules, func(e *typesv1.AlertRule) bool { return e.Name == req.Name })
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
			i = slices.IndexFunc(group.group.Rules, func(e *typesv1.AlertRule) bool { return e.Name == fromName }) + 1
		}
		if i > len(group.group.Rules) {
			return nil
		}
		from := i
		to := min(i+int(limit), len(group.group.Rules))

		for _, item := range group.group.Rules[from:to] {
			out = append(out, &alertv1.ListAlertRuleResponse_ListItem{
				AlertRule: item,
			})
		}
		if len(out) == int(limit) && limit != 0 {
			next = new(typesv1.Cursor)
			p := stringPage{LastID: out[len(out)-1].AlertRule.Name}
			next.Opaque, err = json.Marshal(p)
			if err != nil {
				return err
			}
		}
		return nil
	})

	return &alertv1.ListAlertRuleResponse{Items: out, Next: next}, err
}

func (db *Mem) AlertStateStorage() localstorage.Alertable {
	return &alertStorageMem{db: db}
}

type alertStorageMem struct {
	db *Mem
}

func (as *alertStorageMem) AlertGetOrCreate(ctx context.Context, projectName, groupName, alertName string, create func() *typesv1.AlertState) (*typesv1.AlertState, error) {
	setState := func(group *alertGroup, rule *typesv1.AlertRule, create func() *typesv1.AlertState) *typesv1.AlertState {
		state, ok := group.alertState[rule.Name]
		if !ok {
			state = create()
			group.alertState[rule.Name] = state
		}
		return state
	}

	var out *typesv1.AlertState
	err := as.db.withAlertGroup(projectName, groupName, func(st *project, group *alertGroup) error {
		for _, alertRule := range group.group.Rules {
			if alertRule.Name == alertName {
				out = setState(group, alertRule, create)
				return nil
			}
		}
		return fmt.Errorf("no alert named %q", alertName)
	})
	return out, err
}
func (as *alertStorageMem) AlertUpdateState(ctx context.Context, projectName, groupName, alertName string, state *typesv1.AlertState) error {
	err := as.db.withAlertGroup(projectName, groupName, func(st *project, group *alertGroup) error {
		for _, alertRule := range group.group.Rules {
			if alertRule.Name == alertName {
				group.alertState[alertName] = state
				return nil
			}
		}
		return fmt.Errorf("no alert named %q", alertName)
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
		for key := range group.alertState {
			if _, ok := keepset[key]; !ok {
				toDelete = append(toDelete, key)
			}
		}
		for _, key := range toDelete {
			delete(group.alertState, key)
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
		Id:          ulid.Make().String(),
		Name:        d.Spec.Display.Name,
		Description: d.Spec.Display.Description,
		IsReadonly:  true,
		CreatedAt:   timestamppb.New(d.Metadata.CreatedAt),
		UpdatedAt:   timestamppb.New(d.Metadata.UpdatedAt),
		PersesJson:  []byte(testdashboard),
	}
	return out
}()

const testdashboard = `{"kind":"Dashboard","metadata":{"name":"monitoring-dashboard","project":"default"},"spec":{"display":{"name":"Monitoring Dashboard","description":"System monitoring dashboard with memory, status, and CPU metrics"},"datasources":{"prometheus":{"default":true,"plugin":{"kind":"PrometheusDatasource","spec":{"directUrl":"http://localhost:9090"}}}},"duration":"1h","refreshInterval":"30s","variables":[],"panels":{"memory-panel":{"kind":"Panel","spec":{"display":{"name":"Memory Usage","description":"Go memory allocation in bytes"},"plugin":{"kind":"TimeSeriesChart","spec":{"legend":{"position":"bottom","size":"small"},"yAxis":{"show":true,"label":"Bytes"}}},"queries":[{"kind":"PrometheusTimeSeriesQuery","spec":{"plugin":{"kind":"PrometheusTimeSeriesQuery","spec":{"query":"go_memstats_alloc_bytes","datasource":{"kind":"PrometheusDatasource","name":"prometheus"}}}}}]}},"status-panel":{"kind":"Panel","spec":{"display":{"name":"Service Status","description":"Service availability status"},"plugin":{"kind":"TimeSeriesChart","spec":{"legend":{"position":"bottom","size":"small"},"yAxis":{"show":true,"label":"Status"}}},"queries":[{"kind":"PrometheusTimeSeriesQuery","spec":{"plugin":{"kind":"PrometheusTimeSeriesQuery","spec":{"query":"up","datasource":{"kind":"PrometheusDatasource","name":"prometheus"}}}}}]}},"cpu-panel":{"kind":"Panel","spec":{"display":{"name":"CPU Usage","description":"CPU garbage collection duration rate"},"plugin":{"kind":"TimeSeriesChart","spec":{"legend":{"position":"bottom","size":"small"},"yAxis":{"show":true,"label":"Rate"}}},"queries":[{"kind":"PrometheusTimeSeriesQuery","spec":{"plugin":{"kind":"PrometheusTimeSeriesQuery","spec":{"query":"rate(go_gc_duration_seconds_sum[5m])","datasource":{"kind":"PrometheusDatasource","name":"prometheus"}}}}}]}}},"layouts":[{"kind":"Grid","spec":{"display":{"title":"System Metrics","collapse":{"open":true}},"items":[{"x":0,"y":0,"width":12,"height":8,"content":{"$ref":"#/spec/panels/memory-panel"}},{"x":0,"y":8,"width":12,"height":8,"content":{"$ref":"#/spec/panels/status-panel"}},{"x":0,"y":16,"width":12,"height":8,"content":{"$ref":"#/spec/panels/cpu-panel"}}]}}]}}`
