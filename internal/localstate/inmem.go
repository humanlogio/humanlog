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
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/oklog/ulid/v2"
	persesv1 "github.com/perses/perses/pkg/model/api/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ DB = (*Mem)(nil)

type Mem struct {
	mu            sync.Mutex
	dashboardlist []string
	dashboards    map[string]*typesv1.Dashboard

	alertrulelist []int64
	alertrules    map[int64]*typesv1.AlertRule
}

func NewMemory() *Mem {
	db := &Mem{
		dashboards: make(map[string]*typesv1.Dashboard),
		alertrules: make(map[int64]*typesv1.AlertRule),
	}
	_, err := db.CreateDashboard(context.Background(), &dashboardv1.CreateDashboardRequest{
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

func (db *Mem) CreateDashboard(ctx context.Context, req *dashboardv1.CreateDashboardRequest) (*typesv1.Dashboard, error) {
	id := ulid.Make()
	d := &typesv1.Dashboard{
		Id:          id.String(),
		Name:        req.Name,
		Description: req.Description,
		IsReadonly:  req.IsReadonly,
		PersesJson:  req.PersesJson,
		CreatedAt:   timestamppb.Now(),
		UpdatedAt:   timestamppb.Now(),
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	db.dashboards[d.Id] = d
	db.dashboardlist = append(db.dashboardlist, d.Id)
	return d, nil
}

func (db *Mem) GetDashboard(ctx context.Context, id string) (*typesv1.Dashboard, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	dd, ok := db.dashboards[id]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no such dashboard"))
	}

	return dd, nil
}

func (db *Mem) UpdateDashboard(ctx context.Context, id string, mutations []*dashboardv1.UpdateDashboardRequest_Mutation) (*typesv1.Dashboard, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	dd, ok := db.dashboards[id]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no such dashboard"))
	}
	for _, mutation := range mutations {
		switch do := mutation.Do.(type) {
		case *dashboardv1.UpdateDashboardRequest_Mutation_SetName:
			dd.Name = do.SetName
		case *dashboardv1.UpdateDashboardRequest_Mutation_SetDescription:
			dd.Description = do.SetDescription
		case *dashboardv1.UpdateDashboardRequest_Mutation_SetReadonly:
			dd.IsReadonly = do.SetReadonly
		case *dashboardv1.UpdateDashboardRequest_Mutation_SetSourceFile:
			dd.Source = &typesv1.Dashboard_File{File: do.SetSourceFile}
		case *dashboardv1.UpdateDashboardRequest_Mutation_SetPersesJson:
			dd.PersesJson = do.SetPersesJson
		}
	}
	db.dashboards[dd.Id] = dd
	return dd, nil
}

func (db *Mem) DeleteDashboard(ctx context.Context, id string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.dashboards, id)
	db.dashboardlist = slices.DeleteFunc(db.dashboardlist, func(e string) bool { return e == id })
	return nil
}

type stringPage struct {
	LastID string `json:"lastID"`
}

func (db *Mem) ListDashboard(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*typesv1.Dashboard, *typesv1.Cursor, error) {
	limit = max(limit, 100)
	limit = min(limit, 10)

	db.mu.Lock()
	defer db.mu.Unlock()

	var fromID string
	if cursor != nil {
		var p stringPage
		if err := json.Unmarshal(cursor.Opaque, &p); err != nil {
			return nil, nil, err
		}
		fromID = p.LastID
	}
	var i int
	if fromID != "" {
		i = slices.IndexFunc(db.dashboardlist, func(e string) bool { return e == fromID }) + 1
	}
	if i > len(db.dashboardlist) {
		return nil, nil, nil
	}
	from := i
	to := min(i+int(limit), len(db.dashboardlist))

	var out []*typesv1.Dashboard
	for _, id := range db.dashboardlist[from:to] {
		d := db.dashboards[id]
		out = append(out, d)
	}
	var (
		next *typesv1.Cursor
		err  error
	)
	if len(out) == int(limit) && limit != 0 {
		next = new(typesv1.Cursor)
		p := stringPage{LastID: out[len(out)-1].Id}
		next.Opaque, err = json.Marshal(p)
		if err != nil {
			return out, next, err
		}
	}

	return out, next, nil
}

func (db *Mem) CreateAlertRule(ctx context.Context, req *alertv1.CreateAlertRuleRequest) (*alertv1.CreateAlertRuleResponse, error) {
	item := &typesv1.AlertRule{
		Id:            int64(len(db.alertrulelist) + 1),
		Name:          req.Name,
		Expr:          req.Expr,
		Labels:        req.Labels,
		Annotations:   req.Annotations,
		For:           req.For,
		KeepFiringFor: req.KeepFiringFor,
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	db.alertrules[item.Id] = item
	db.alertrulelist = append(db.alertrulelist, item.Id)
	return &alertv1.CreateAlertRuleResponse{AlertRule: item}, nil
}

func (db *Mem) GetAlertRule(ctx context.Context, req *alertv1.GetAlertRuleRequest) (*alertv1.GetAlertRuleResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	item, ok := db.alertrules[req.Id]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no such alert rule"))
	}

	return &alertv1.GetAlertRuleResponse{AlertRule: item}, nil
}

func (db *Mem) UpdateAlertRule(ctx context.Context, req *alertv1.UpdateAlertRuleRequest) (*alertv1.UpdateAlertRuleResponse, error) {
	var (
		id        = req.Id
		mutations = req.Mutations
	)
	db.mu.Lock()
	defer db.mu.Unlock()
	item, ok := db.alertrules[id]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no such dashboard"))
	}
	for _, mutation := range mutations {
		switch do := mutation.Do.(type) {
		case *alertv1.UpdateAlertRuleRequest_Mutation_SetName:
			item.Name = do.SetName
		case *alertv1.UpdateAlertRuleRequest_Mutation_SetExpr:
			item.Expr = do.SetExpr
		case *alertv1.UpdateAlertRuleRequest_Mutation_SetLabels:
			item.Labels = do.SetLabels
		case *alertv1.UpdateAlertRuleRequest_Mutation_SetAnnotations:
			item.Annotations = do.SetAnnotations
		case *alertv1.UpdateAlertRuleRequest_Mutation_SetFor:
			item.For = do.SetFor
		case *alertv1.UpdateAlertRuleRequest_Mutation_SetKeepFiringFor:
			item.KeepFiringFor = do.SetKeepFiringFor
		}
	}
	db.alertrules[item.Id] = item
	return &alertv1.UpdateAlertRuleResponse{AlertRule: item}, nil
}

func (db *Mem) DeleteAlertRule(ctx context.Context, req *alertv1.DeleteAlertRuleRequest) (*alertv1.DeleteAlertRuleResponse, error) {
	var (
		id = req.Id
	)
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.alertrules, id)
	db.alertrulelist = slices.DeleteFunc(db.alertrulelist, func(e int64) bool { return e == id })
	return &alertv1.DeleteAlertRuleResponse{}, nil
}

type int64Page struct {
	LastID int64 `json:"lastID"`
}

func (db *Mem) ListAlertRule(ctx context.Context, req *alertv1.ListAlertRuleRequest) (*alertv1.ListAlertRuleResponse, error) {
	var (
		cursor = req.Cursor
		limit  = req.Limit
	)
	limit = max(limit, 100)
	limit = min(limit, 10)

	db.mu.Lock()
	defer db.mu.Unlock()

	var fromID int64
	if cursor != nil {
		var p int64Page
		if err := json.Unmarshal(cursor.Opaque, &p); err != nil {
			return nil, err
		}
		fromID = p.LastID
	}
	var i int
	if fromID != 0 {
		i = slices.IndexFunc(db.alertrulelist, func(e int64) bool { return e == fromID }) + 1
	}
	if i > len(db.alertrulelist) {
		return nil, nil
	}
	from := i
	to := min(i+int(limit), len(db.alertrulelist))

	out := new(alertv1.ListAlertRuleResponse)
	for _, id := range db.alertrulelist[from:to] {
		item := db.alertrules[id]
		out.Items = append(out.Items, &alertv1.ListAlertRuleResponse_ListItem{
			AlertRule: item,
		})
	}
	var (
		next *typesv1.Cursor
		err  error
	)
	if len(out.Items) == int(limit) && limit != 0 {
		next = new(typesv1.Cursor)
		p := int64Page{LastID: out.Items[len(out.Items)-1].AlertRule.Id}
		next.Opaque, err = json.Marshal(p)
		if err != nil {
			return nil, err
		}
		out.Next = next
	}

	return out, nil
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
