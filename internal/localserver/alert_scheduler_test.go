package localserver

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	projectv1 "github.com/humanlogio/api/go/svc/project/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/humanlogio/humanlog/pkg/sink"
	otlplogssvcpb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	otlpmetricssvcpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	otlptracesvcpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
)

type schedulerState struct {
	projects    []*typesv1.Project
	alertGroups map[string][]*typesv1.AlertGroup
}

type tick struct {
	at           time.Duration
	reconcile    bool
	evals        []string // "project/group"
	addGroups    map[string][]*typesv1.AlertGroup
	removeGroups map[string][]string
	updateGroups map[string][]*typesv1.AlertGroup
}

func TestAlertScheduler(t *testing.T) {
	start := time.Date(2025, 10, 19, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		init  schedulerState
		ticks []tick
		check func(t *testing.T, events []event)
	}{
		{
			name: "single group evaluates at interval",
			init: schedulerState{
				projects:    []*typesv1.Project{mkproject("proj1")},
				alertGroups: map[string][]*typesv1.AlertGroup{"proj1": {mkalertgroup("group1", setInterval(60 * time.Second))}},
			},
			ticks: []tick{
				{at: 60 * time.Second, evals: []string{"proj1/group1"}},
				{at: 120 * time.Second, evals: []string{"proj1/group1"}},
			},
			check: func(t *testing.T, events []event) {
				require.Equal(t, 2, countEvals(events, "proj1", "group1"))
			},
		},
		{
			name: "multiple groups evaluate at different intervals",
			init: schedulerState{
				projects: []*typesv1.Project{mkproject("proj1")},
				alertGroups: map[string][]*typesv1.AlertGroup{"proj1": {
					mkalertgroup("group1", setInterval(60*time.Second)),
					mkalertgroup("group2", setInterval(30*time.Second)),
				}},
			},
			ticks: []tick{
				{at: 30 * time.Second, evals: []string{"proj1/group2"}},
				{at: 60 * time.Second, evals: []string{"proj1/group1", "proj1/group2"}},
				{at: 90 * time.Second, evals: []string{"proj1/group2"}},
			},
			check: func(t *testing.T, events []event) {
				require.Equal(t, 1, countEvals(events, "proj1", "group1"))
				require.Equal(t, 3, countEvals(events, "proj1", "group2"))
			},
		},
		{
			name: "reconcile discovers new group",
			init: schedulerState{
				projects:    []*typesv1.Project{mkproject("proj1")},
				alertGroups: map[string][]*typesv1.AlertGroup{"proj1": {mkalertgroup("group1", setInterval(60 * time.Second))}},
			},
			ticks: []tick{
				{at: 60 * time.Second, evals: []string{"proj1/group1"}},
				{at: 60 * time.Second, reconcile: true, addGroups: map[string][]*typesv1.AlertGroup{
					"proj1": {mkalertgroup("group2", setInterval(30 * time.Second))},
				}},
				{at: 90 * time.Second, evals: []string{"proj1/group2"}},
				{at: 120 * time.Second, evals: []string{"proj1/group1", "proj1/group2"}},
			},
			check: func(t *testing.T, events []event) {
				require.Equal(t, 2, countEvals(events, "proj1", "group1"))
				require.Equal(t, 2, countEvals(events, "proj1", "group2"))
			},
		},
		{
			name: "reconcile removes deleted group",
			init: schedulerState{
				projects: []*typesv1.Project{mkproject("proj1")},
				alertGroups: map[string][]*typesv1.AlertGroup{"proj1": {
					mkalertgroup("group1", setInterval(30*time.Second)),
					mkalertgroup("group2", setInterval(30*time.Second)),
				}},
			},
			ticks: []tick{
				{at: 30 * time.Second, evals: []string{"proj1/group1", "proj1/group2"}},
				{at: 30 * time.Second, reconcile: true, removeGroups: map[string][]string{"proj1": {"group2"}}},
				{at: 60 * time.Second, evals: []string{"proj1/group1"}},
			},
			check: func(t *testing.T, events []event) {
				require.Equal(t, 2, countEvals(events, "proj1", "group1"))
				require.Equal(t, 1, countEvals(events, "proj1", "group2"))
				require.True(t, hasEvent(events, "evaluator-stopped", "proj1", "group2"))
			},
		},
		{
			name: "reconcile detects interval change and restarts evaluator",
			init: schedulerState{
				projects:    []*typesv1.Project{mkproject("proj1")},
				alertGroups: map[string][]*typesv1.AlertGroup{"proj1": {mkalertgroup("group1", setInterval(60 * time.Second))}},
			},
			ticks: []tick{
				{at: 60 * time.Second, evals: []string{"proj1/group1"}},
				{at: 60 * time.Second, reconcile: true, updateGroups: map[string][]*typesv1.AlertGroup{
					"proj1": {mkalertgroup("group1", setInterval(30 * time.Second))},
				}},
				{at: 90 * time.Second, evals: []string{"proj1/group1"}},
				{at: 120 * time.Second, evals: []string{"proj1/group1"}},
			},
			check: func(t *testing.T, events []event) {
				require.Equal(t, 3, countEvals(events, "proj1", "group1"))
				require.True(t, hasEvent(events, "evaluator-stopped", "proj1", "group1"))
			},
		},
		{
			name: "multiple projects with alert groups",
			init: schedulerState{
				projects: []*typesv1.Project{mkproject("proj1"), mkproject("proj2")},
				alertGroups: map[string][]*typesv1.AlertGroup{
					"proj1": {mkalertgroup("group1", setInterval(30 * time.Second))},
					"proj2": {mkalertgroup("group1", setInterval(30 * time.Second))},
				},
			},
			ticks: []tick{
				{at: 30 * time.Second, evals: []string{"proj1/group1", "proj2/group1"}},
				{at: 60 * time.Second, evals: []string{"proj1/group1", "proj2/group1"}},
			},
			check: func(t *testing.T, events []event) {
				require.Equal(t, 2, countEvals(events, "proj1", "group1"))
				require.Equal(t, 2, countEvals(events, "proj2", "group1"))
			},
		},
		{
			name: "default interval used when not specified",
			init: schedulerState{
				projects:    []*typesv1.Project{mkproject("proj1")},
				alertGroups: map[string][]*typesv1.AlertGroup{"proj1": {mkalertgroup("group1")}},
			},
			ticks: []tick{
				{at: 60 * time.Second, evals: []string{"proj1/group1"}},
			},
			check: func(t *testing.T, events []event) {
				require.Equal(t, 1, countEvals(events, "proj1", "group1"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var mu sync.Mutex
			now := start
			timeNow := func() time.Time {
				mu.Lock()
				defer mu.Unlock()
				return now
			}
			setTime := func(t time.Time) {
				mu.Lock()
				defer mu.Unlock()
				now = t
			}

			reconcileCh := make(chan time.Time, 10)
			evalChans := make(map[string]chan time.Time)

			var events []event
			var reconcileDone, evalDone sync.WaitGroup

			recordEvent := func(typ, project, group string, t time.Time) {
				mu.Lock()
				defer mu.Unlock()
				events = append(events, event{typ: typ, project: project, group: group, time: t})
			}

			onReconcileComplete := func() {
				recordEvent("reconcile-complete", "", "", timeNow())
				reconcileDone.Done()
			}

			onEvaluationComplete := func(project, group string) {
				recordEvent("eval-complete", project, group, timeNow())
				evalDone.Done()
			}

			onEvaluatorWillStart := func(project, group string) {
				// Create channel immediately when evaluator will start
				key := alertGroupKey(project, group)
				mu.Lock()
				evalChans[key] = make(chan time.Time, 10)
				mu.Unlock()
			}

			onEvaluatorStarted := func(project, group string) {
				recordEvent("evaluator-started", project, group, timeNow())
			}

			onEvaluatorStopped := func(project, group string) {
				recordEvent("evaluator-stopped", project, group, timeNow())
			}

			getReconcileTrigger := func() <-chan time.Time {
				return reconcileCh
			}

			getEvalTrigger := func(project, group string, interval time.Duration) <-chan time.Time {
				key := alertGroupKey(project, group)
				mu.Lock()
				defer mu.Unlock()
				return evalChans[key]
			}

			state := newTestState(tt.init.projects, tt.init.alertGroups)
			storage := &mockStorage{}

			reconcileDone.Add(1) // initial reconcile

			scheduler := newAlertScheduler(
				slog.Default(),
				state.asProjectService(),
				storage,
				timeNow,
				getReconcileTrigger,
				getEvalTrigger,
				func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error { return nil },
				withOnReconcileComplete(onReconcileComplete),
				withOnEvaluationComplete(onEvaluationComplete),
				withOnEvaluatorWillStart(onEvaluatorWillStart),
				withOnEvaluatorStarted(onEvaluatorStarted),
				withOnEvaluatorStopped(onEvaluatorStopped),
			)

			go scheduler.Start(ctx)

			// Wait for initial reconcile
			t.Log("Waiting for initial reconcile")
			reconcileDone.Wait()
			t.Log("Initial reconcile done")

			// Process ticks
			for _, tick := range tt.ticks {
				setTime(start.Add(tick.at))

				if tick.addGroups != nil {
					state.add(tick.addGroups)
				}
				if tick.removeGroups != nil {
					state.remove(tick.removeGroups)
				}
				if tick.updateGroups != nil {
					state.update(tick.updateGroups)
				}

				if tick.reconcile {
					reconcileDone.Add(1)
					reconcileCh <- timeNow()
					reconcileDone.Wait()
				}

				if len(tick.evals) > 0 {
					evalDone.Add(len(tick.evals))
					for _, key := range tick.evals {
						parts := strings.Split(key, "/")
						mu.Lock()
						ch := evalChans[alertGroupKey(parts[0], parts[1])]
						mu.Unlock()
						ch <- timeNow()
					}
					evalDone.Wait()
				}
			}

			cancel()
			tt.check(t, events)
		})
	}
}

// ============================================================================
// Helpers
// ============================================================================

type event struct {
	typ     string
	project string
	group   string
	time    time.Time
}

func countEvals(events []event, project, group string) int {
	count := 0
	for _, e := range events {
		if e.typ == "eval-complete" && e.project == project && e.group == group {
			count++
		}
	}
	return count
}

func hasEvent(events []event, typ, project, group string) bool {
	for _, e := range events {
		if e.typ == typ && e.project == project && e.group == group {
			return true
		}
	}
	return false
}

func mkproject(name string) *typesv1.Project {
	return &typesv1.Project{Spec: &typesv1.ProjectSpec{Name: name}}
}

func mkalertgroup(name string, opts ...func(*typesv1.AlertGroupSpec)) *typesv1.AlertGroup {
	spec := &typesv1.AlertGroupSpec{Name: name, Rules: []*typesv1.AlertGroupSpec_NamedAlertRuleSpec{}}
	for _, opt := range opts {
		opt(spec)
	}
	return &typesv1.AlertGroup{Spec: spec}
}

func setInterval(d time.Duration) func(*typesv1.AlertGroupSpec) {
	return func(spec *typesv1.AlertGroupSpec) {
		spec.Interval = durationpb.New(d)
	}
}

type mockProjectService struct {
	listProject    func(context.Context, *projectv1.ListProjectRequest) (*projectv1.ListProjectResponse, error)
	getProject     func(context.Context, *projectv1.GetProjectRequest) (*projectv1.GetProjectResponse, error)
	listAlertGroup func(context.Context, *alertv1.ListAlertGroupRequest) (*alertv1.ListAlertGroupResponse, error)
	getAlertGroup  func(context.Context, *alertv1.GetAlertGroupRequest) (*alertv1.GetAlertGroupResponse, error)
}

func (m *mockProjectService) ListProject(ctx context.Context, req *projectv1.ListProjectRequest) (*projectv1.ListProjectResponse, error) {
	return m.listProject(ctx, req)
}
func (m *mockProjectService) GetProject(ctx context.Context, req *projectv1.GetProjectRequest) (*projectv1.GetProjectResponse, error) {
	return m.getProject(ctx, req)
}
func (m *mockProjectService) ListAlertGroup(ctx context.Context, req *alertv1.ListAlertGroupRequest) (*alertv1.ListAlertGroupResponse, error) {
	return m.listAlertGroup(ctx, req)
}
func (m *mockProjectService) GetAlertGroup(ctx context.Context, req *alertv1.GetAlertGroupRequest) (*alertv1.GetAlertGroupResponse, error) {
	return m.getAlertGroup(ctx, req)
}

type mockStorage struct{}

func (m *mockStorage) Query(context.Context, *typesv1.Query, *typesv1.Cursor, int, ...localstorage.QueryOption) (*typesv1.Data, *typesv1.Cursor, *typesv1.QueryMetrics, error) {
	return &typesv1.Data{Shape: &typesv1.Data_FreeForm{FreeForm: &typesv1.Table{Type: &typesv1.TableType{}, Rows: []*typesv1.Arr{}}}}, nil, nil, nil
}
func (m *mockStorage) AlertGetOrCreate(context.Context, string, string, string, func() *typesv1.AlertRuleStatus) (*typesv1.AlertRuleStatus, error) {
	return &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{}}, nil
}
func (m *mockStorage) AlertUpdateState(context.Context, string, string, string, *typesv1.AlertRuleStatus) error { return nil }
func (m *mockStorage) AlertDeleteStateNotInList(context.Context, string, string, []string) error               { return nil }
func (m *mockStorage) ExportLogs(context.Context, *otlplogssvcpb.ExportLogsServiceRequest) (*otlplogssvcpb.ExportLogsServiceResponse, error) {
	return &otlplogssvcpb.ExportLogsServiceResponse{}, nil
}
func (m *mockStorage) ExportTraces(context.Context, *otlptracesvcpb.ExportTraceServiceRequest) (*otlptracesvcpb.ExportTraceServiceResponse, error) {
	return &otlptracesvcpb.ExportTraceServiceResponse{}, nil
}
func (m *mockStorage) ExportMetrics(context.Context, *otlpmetricssvcpb.ExportMetricsServiceRequest) (*otlpmetricssvcpb.ExportMetricsServiceResponse, error) {
	return &otlpmetricssvcpb.ExportMetricsServiceResponse{}, nil
}
func (m *mockStorage) SinkFor(context.Context, *typesv1.Resource, *typesv1.Scope) (sink.Sink, error) { return nil, nil }
func (m *mockStorage) ReportMetrics(context.Context, localstorage.MetricsReporterFunc) error         { return nil }
func (m *mockStorage) Format(context.Context, *typesv1.Query) (string, error)                        { return "", nil }
func (m *mockStorage) Parse(context.Context, string) (*typesv1.Query, error)                         { return nil, nil }
func (m *mockStorage) ResolveQueryType(context.Context, *typesv1.Query) (*typesv1.DataStreamType, error) {
	return nil, nil
}
func (m *mockStorage) ListSymbols(context.Context, *typesv1.Query, *typesv1.Cursor, int) ([]*typesv1.Symbol, *typesv1.Cursor, error) {
	return nil, nil, nil
}
func (m *mockStorage) Stream(context.Context, *typesv1.Query, func(context.Context, *typesv1.Data) (bool, error), *localstorage.StreamOption) error {
	return nil
}
func (m *mockStorage) GetTraceByID(context.Context, *typesv1.TraceID) (*typesv1.Trace, error)   { return nil, nil }
func (m *mockStorage) GetTraceBySpanID(context.Context, *typesv1.SpanID) (*typesv1.Trace, error) { return nil, nil }
func (m *mockStorage) GetSpanByID(context.Context, *typesv1.SpanID) (*typesv1.Span, error)       { return nil, nil }
func (m *mockStorage) Close() error                                                              { return nil }

type testState struct {
	mu          sync.Mutex
	projects    []*typesv1.Project
	alertGroups map[string][]*typesv1.AlertGroup
}

func newTestState(projects []*typesv1.Project, alertGroups map[string][]*typesv1.AlertGroup) *testState {
	return &testState{projects: projects, alertGroups: alertGroups}
}

func (s *testState) add(groups map[string][]*typesv1.AlertGroup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for projectName, newGroups := range groups {
		s.alertGroups[projectName] = append(s.alertGroups[projectName], newGroups...)
	}
}

func (s *testState) remove(groups map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for projectName, groupNames := range groups {
		existing := s.alertGroups[projectName]
		var filtered []*typesv1.AlertGroup
		for _, g := range existing {
			keep := true
			for _, name := range groupNames {
				if g.Spec.Name == name {
					keep = false
					break
				}
			}
			if keep {
				filtered = append(filtered, g)
			}
		}
		s.alertGroups[projectName] = filtered
	}
}

func (s *testState) update(groups map[string][]*typesv1.AlertGroup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for projectName, updatedGroups := range groups {
		existing := s.alertGroups[projectName]
		for i, g := range existing {
			for _, updated := range updatedGroups {
				if g.Spec.Name == updated.Spec.Name {
					existing[i] = updated
				}
			}
		}
	}
}

func (s *testState) asProjectService() *mockProjectService {
	return &mockProjectService{
		listProject: func(ctx context.Context, req *projectv1.ListProjectRequest) (*projectv1.ListProjectResponse, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			var items []*projectv1.ListProjectResponse_ListItem
			for _, p := range s.projects {
				items = append(items, &projectv1.ListProjectResponse_ListItem{Project: p})
			}
			return &projectv1.ListProjectResponse{Items: items}, nil
		},
		getProject: func(ctx context.Context, req *projectv1.GetProjectRequest) (*projectv1.GetProjectResponse, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, p := range s.projects {
				if p.Spec.Name == req.Name {
					return &projectv1.GetProjectResponse{Project: p}, nil
				}
			}
			return nil, nil
		},
		listAlertGroup: func(ctx context.Context, req *alertv1.ListAlertGroupRequest) (*alertv1.ListAlertGroupResponse, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			groups := s.alertGroups[req.ProjectName]
			var items []*alertv1.ListAlertGroupResponse_ListItem
			for _, g := range groups {
				items = append(items, &alertv1.ListAlertGroupResponse_ListItem{AlertGroup: g})
			}
			return &alertv1.ListAlertGroupResponse{Items: items}, nil
		},
		getAlertGroup: func(ctx context.Context, req *alertv1.GetAlertGroupRequest) (*alertv1.GetAlertGroupResponse, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			groups := s.alertGroups[req.ProjectName]
			for _, g := range groups {
				if g.Spec.Name == req.Name {
					return &alertv1.GetAlertGroupResponse{AlertGroup: g}, nil
				}
			}
			return nil, nil
		},
	}
}
