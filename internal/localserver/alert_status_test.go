package localserver

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ruleStatusExpectation struct {
	hasEvaluated  bool
	rowsScanned   int64
	rowsReturned  int64
	latencyMs     int64
	hasError      bool
	errorContains string
}

func TestAlertGroupStatusUserView(t *testing.T) {
	start := time.Date(2025, 10, 20, 14, 37, 42, 123456789, time.UTC)

	tests := []struct {
		name            string
		alertGroup      *typesv1.AlertGroup
		simulate        func(ctx context.Context, t *testing.T, s *AlertScheduler, storage *mockStorage, now time.Time)
		wantStatus      map[string]ruleStatusExpectation
		wantGroupErrors []string
	}{
		{
			name: "user sees query metrics after successful evaluation",
			alertGroup: mkalertgroup("group1",
				withAlertRule("cpu_high", "cpu > 80"),
			),
			simulate: func(ctx context.Context, t *testing.T, s *AlertScheduler, storage *mockStorage, now time.Time) {
				triggerEvaluation(ctx, t, s, storage, "proj1", "group1", now, map[string]evaluationOutcome{
					"cpu_high": {
						metrics: &typesv1.QueryMetrics{
							RowsScanned:  1000,
							RowsReturned: 5,
							TotalLatency: durationpb.New(50 * time.Millisecond),
						},
					},
				})
			},
			wantStatus: map[string]ruleStatusExpectation{
				"cpu_high": {
					hasEvaluated: true,
					rowsScanned:  1000,
					rowsReturned: 5,
					latencyMs:    50,
					hasError:     false,
				},
			},
		},
		{
			name: "user sees error when query fails",
			alertGroup: mkalertgroup("group1",
				withAlertRule("bad_query", "invalid syntax"),
			),
			simulate: func(ctx context.Context, t *testing.T, s *AlertScheduler, storage *mockStorage, now time.Time) {
				triggerEvaluation(ctx, t, s, storage, "proj1", "group1", now, map[string]evaluationOutcome{
					"bad_query": {
						error: "syntax error: unexpected token",
					},
				})
			},
			wantStatus: map[string]ruleStatusExpectation{
				"bad_query": {
					hasEvaluated:  true,
					hasError:      true,
					errorContains: "syntax error",
				},
			},
		},
		{
			name: "user sees mixed success and failure across rules",
			alertGroup: mkalertgroup("group1",
				withAlertRule("rule1", "query1"),
				withAlertRule("rule2", "query2"),
				withAlertRule("rule3", "query3"),
			),
			simulate: func(ctx context.Context, t *testing.T, s *AlertScheduler, storage *mockStorage, now time.Time) {
				triggerEvaluation(ctx, t, s, storage, "proj1", "group1", now, map[string]evaluationOutcome{
					"rule1": {
						metrics: &typesv1.QueryMetrics{RowsScanned: 100},
					},
					"rule2": {
						error: "timeout",
					},
					"rule3": {
						metrics: &typesv1.QueryMetrics{RowsScanned: 50000},
					},
				})
			},
			wantStatus: map[string]ruleStatusExpectation{
				"rule1": {hasEvaluated: true, rowsScanned: 100, hasError: false},
				"rule2": {hasEvaluated: true, hasError: true, errorContains: "timeout"},
				"rule3": {hasEvaluated: true, rowsScanned: 50000, hasError: false},
			},
		},
		{
			name: "user sees nothing before first evaluation",
			alertGroup: mkalertgroup("group1",
				withAlertRule("rule1", "query1"),
			),
			simulate: func(ctx context.Context, t *testing.T, s *AlertScheduler, storage *mockStorage, now time.Time) {
				// Don't trigger any evaluation
			},
			wantStatus: map[string]ruleStatusExpectation{
				"rule1": {hasEvaluated: false},
			},
		},
		{
			name: "group-level errors appear separately",
			alertGroup: mkalertgroup("group1",
				withAlertRule("rule1", "query1"),
			),
			simulate: func(ctx context.Context, t *testing.T, s *AlertScheduler, storage *mockStorage, now time.Time) {
				t.Skip("Group-level errors are no longer tracked in the evaluator")
			},
			wantStatus: map[string]ruleStatusExpectation{
				"rule1": {hasEvaluated: false},
			},
			wantGroupErrors: []string{"failed to fetch alert group"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			state, storage, scheduler := createTestEnvironment(t, start, "proj1", tt.alertGroup)

			tt.simulate(ctx, t, scheduler, storage, start)

			resp, err := state.GetAlertGroup(ctx, &alertv1.GetAlertGroupRequest{
				ProjectName: "proj1",
				Name:        tt.alertGroup.Spec.Name,
			})
			require.NoError(t, err)

			if len(tt.wantGroupErrors) > 0 {
				require.Equal(t, tt.wantGroupErrors, resp.AlertGroup.Status.Errors, "group-level errors")
			}

			for ruleID, want := range tt.wantStatus {
				ruleStatus := findRuleStatus(resp.AlertGroup.Status.Rules, ruleID)
				require.NotNil(t, ruleStatus, "rule %s should have status entry", ruleID)

				if !want.hasEvaluated {
					assert.Nil(t, ruleStatus.LastEvaluatedAt, "rule %s should not be evaluated yet", ruleID)
					assert.Nil(t, ruleStatus.LastEvaluationMetrics, "rule %s should not have metrics", ruleID)
					continue
				}

				require.NotNil(t, ruleStatus.LastEvaluatedAt, "rule %s should have evaluation timestamp", ruleID)

				if want.hasError {
					require.NotNil(t, ruleStatus.Error, "rule %s should have error", ruleID)
					assert.Contains(t, *ruleStatus.Error, want.errorContains, "rule %s error message", ruleID)
					assert.Nil(t, ruleStatus.LastEvaluationMetrics, "rule %s shouldn't have metrics when failed", ruleID)
				} else {
					assert.Nil(t, ruleStatus.Error, "rule %s should not have error", ruleID)
					require.NotNil(t, ruleStatus.LastEvaluationMetrics, "rule %s should have metrics", ruleID)
					assert.Equal(t, want.rowsScanned, ruleStatus.LastEvaluationMetrics.RowsScanned, "rule %s rows scanned", ruleID)
					if want.rowsReturned > 0 {
						assert.Equal(t, want.rowsReturned, ruleStatus.LastEvaluationMetrics.RowsReturned, "rule %s rows returned", ruleID)
					}
					if want.latencyMs > 0 {
						assert.Equal(t, want.latencyMs, ruleStatus.LastEvaluationMetrics.TotalLatency.AsDuration().Milliseconds(), "rule %s latency", ruleID)
					}
				}
			}

		})
	}
}

type evaluationOutcome struct {
	metrics *typesv1.QueryMetrics
	error   string
}

func findRuleStatus(rules []*typesv1.AlertGroupStatus_NamedAlertRuleStatus, ruleID string) *typesv1.AlertRuleStatus {
	for _, r := range rules {
		if r.Id == ruleID {
			return r.Status
		}
	}
	return nil
}

func withAlertRule(id, query string) func(*typesv1.AlertGroupSpec) {
	return func(spec *typesv1.AlertGroupSpec) {
		spec.Rules = append(spec.Rules, &typesv1.AlertGroupSpec_NamedAlertRuleSpec{
			Id: id,
			Spec: &typesv1.AlertRuleSpec{
				Name: id,
				Expr: &typesv1.Query{},
			},
		})
	}
}

func createTestEnvironment(t *testing.T, now time.Time, projectName string, group *typesv1.AlertGroup) (*mockProjectService, *mockStorage, *AlertScheduler) {
	project := mkproject(projectName)

	storage := &mockStorage{}

	state := &mockProjectService{
		projects:    []*typesv1.Project{project},
		alertGroups: map[string][]*typesv1.AlertGroup{projectName: {group}},
		storage:     storage,
	}

	scheduler := newAlertScheduler(
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
		state,
		storage,
		func() time.Time { return now },
		func() <-chan time.Time { return nil }, // Won't actually reconcile in these tests
		func(projectName, groupName string, interval time.Duration) <-chan time.Time { return nil },
		func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error { return nil },
	)

	return state, storage, scheduler
}

func triggerEvaluation(ctx context.Context, t *testing.T, s *AlertScheduler, storage *mockStorage, projectName, groupName string, now time.Time, outcomes map[string]evaluationOutcome) {
	s.mu.Lock()
	_, exists := s.evaluators[alertGroupKey(projectName, groupName)]
	if !exists {
		eval := &alertGroupEvaluator{
			projectName: projectName,
			groupName:   groupName,
			done:        make(chan struct{}),
		}
		s.evaluators[alertGroupKey(projectName, groupName)] = eval
	}
	s.mu.Unlock()

	for ruleID, outcome := range outcomes {
		status := &typesv1.AlertRuleStatus{
			LastEvaluatedAt: timestamppb.New(now),
		}

		if outcome.error != "" {
			status.Error = &outcome.error
		} else {
			status.LastEvaluationMetrics = outcome.metrics
		}

		err := storage.AlertUpdateState(ctx, projectName, groupName, ruleID, status)
		require.NoError(t, err)
	}
}
