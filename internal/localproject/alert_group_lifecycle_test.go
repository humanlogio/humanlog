package localproject

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/google/go-cmp/cmp"
	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	projectv1 "github.com/humanlogio/api/go/svc/project/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/localstate"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAlertGroupLifecycle(t *testing.T) {
	now := time.Date(2025, 10, 24, 10, 0, 0, 0, time.UTC)

	cmpOpts := []cmp.Option{
		protocmp.Transform(),
		protocmp.IgnoreFields(&typesv1.AlertGroupStatus{}, "created_at", "updated_at"),
		protocmp.IgnoreFields(&typesv1.ProjectStatus{}, "created_at", "updated_at"),
	}

	// Helper to create a simple query expression (matches parseQuery in tests)
	mkQuery := func(expr string) *typesv1.Query {
		return &typesv1.Query{
			Query: &typesv1.Statements{
				Statements: []*typesv1.Statement{
					{
						Stmt: &typesv1.Statement_Filter{
							Filter: &typesv1.FilterOperator{
								Expr: &typesv1.Expr{
									Expr: &typesv1.Expr_Identifier{
										Identifier: &typesv1.Identifier{
											Name: expr,
										},
									},
								},
							},
						},
					},
				},
			},
		}
	}

	// Helper to create Obj from map
	mkObj := func(m map[string]string) *typesv1.Obj {
		if m == nil {
			return nil
		}
		kvs := make([]*typesv1.KV, 0, len(m))
		for k, v := range m {
			kvs = append(kvs, &typesv1.KV{
				Key: k,
				Value: &typesv1.Val{
					Kind: &typesv1.Val_Str{Str: v},
					Type: &typesv1.VarType{Type: &typesv1.VarType_Scalar{Scalar: typesv1.ScalarType_str}},
				},
			})
		}
		return &typesv1.Obj{Kvs: kvs}
	}

	type ruleSpec struct {
		id          string
		name        string
		expr        string
		labels      map[string]string
		annotations map[string]string
	}

	mkAlertGroupSpec := func(name string, interval time.Duration, ruleSpecs ...ruleSpec) *typesv1.AlertGroupSpec {
		rules := make([]*typesv1.AlertGroupSpec_NamedAlertRuleSpec, 0, len(ruleSpecs))
		for _, rs := range ruleSpecs {
			rules = append(rules, &typesv1.AlertGroupSpec_NamedAlertRuleSpec{
				Id: rs.id,
				Spec: &typesv1.AlertRuleSpec{
					Name:        rs.name,
					Expr:        mkQuery(rs.expr),
					Labels:      mkObj(rs.labels),
					Annotations: mkObj(rs.annotations),
					For:         durationpb.New(5 * time.Minute),
				},
			})
		}
		return &typesv1.AlertGroupSpec{
			Name:     name,
			Interval: durationpb.New(interval),
			Rules:    rules,
		}
	}

	type fileExpectation struct {
		path        string
		shouldExist bool
		contains    []string
	}

	type alertGroupExpectation struct {
		projectName string
		groupName   string
		expected    *typesv1.AlertGroup
	}

	type errorExpectation struct {
		shouldFail bool
		contains   string
	}

	type fileOperation struct {
		action string // "write", "delete"
		path   string
		data   []byte
	}

	type transition struct {
		name             string
		at               time.Duration
		operation        func(context.Context, *testing.T, localstate.DB, billy.Filesystem) error
		fileOperation    *fileOperation
		expectError      *errorExpectation
		expectFile       *fileExpectation
		expectAlertGroup *alertGroupExpectation
	}

	tests := []struct {
		name        string
		cfg         *typesv1.ProjectsConfig
		transitions []transition
	}{
		{
			name: "create managed alert group",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "create managed alert group",
					at:   0,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateAlertGroup(ctx, &alertv1.CreateAlertGroupRequest{
							ProjectName: "test-project",
							Spec: mkAlertGroupSpec("cpu-alerts", 1*time.Minute,
								ruleSpec{
									id:          "HighCPU",
									name:        "HighCPU",
									expr:        "cpu_usage > 80",
									labels:      map[string]string{"severity": "warning"},
									annotations: map[string]string{"summary": "CPU usage is high"},
								},
							),
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "test-project/alerts/cpu-alerts.yaml",
						shouldExist: true,
						contains:    []string{"# managed-by: humanlog", "name: cpu-alerts", "interval: 1m"},
					},
					expectAlertGroup: &alertGroupExpectation{
						projectName: "test-project",
						groupName:   "cpu-alerts",
						expected: managedAlertGroup(
							"cpu-alerts",
							"cpu-alerts",
							false,
							1*time.Minute,
							"test-project/alerts/cpu-alerts.yaml",
							now,
							now,
							&typesv1.AlertGroupSpec_NamedAlertRuleSpec{
								Id: "HighCPU",
								Spec: &typesv1.AlertRuleSpec{
									Name:        "HighCPU",
									Expr:        mkQuery("cpu_usage > 80"),
									Labels:      mkObj(map[string]string{"severity": "warning"}),
									Annotations: mkObj(map[string]string{"summary": "CPU usage is high"}),
									For:         durationpb.New(5 * time.Minute),
								},
							},
						),
					},
				},
			},
		},
		{
			name: "update managed alert group preserves marker",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "create managed alert group",
					at:   0,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateAlertGroup(ctx, &alertv1.CreateAlertGroupRequest{
							ProjectName: "test-project",
							Spec:        mkAlertGroupSpec("cpu-alerts", 1*time.Minute),
						})
						return err
					},
				},
				{
					name: "update alert group",
					at:   1 * time.Second,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateAlertGroup(ctx, &alertv1.UpdateAlertGroupRequest{
							ProjectName: "test-project",
							Name:        "cpu-alerts",
							Spec:        mkAlertGroupSpec("cpu-alerts", 2*time.Minute),
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "test-project/alerts/cpu-alerts.yaml",
						shouldExist: true,
						contains:    []string{"# managed-by: humanlog", "interval: 2m"},
					},
				},
			},
		},
		{
			name: "discover external alert group as readonly",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "write external alert group file",
					at:   0,
					fileOperation: &fileOperation{
						action: "write",
						path:   "test-project/alerts/external.yaml",
						data: []byte(`groups:
  - name: external
    interval: 30s
    rules:
      - alert: ExternalAlert
        expr: 'up == 0'
`),
					},
					expectAlertGroup: &alertGroupExpectation{
						projectName: "test-project",
						groupName:   "external",
						expected: discoveredAlertGroup(
							"external",
							"external",
							true,
							30*time.Second,
							"test-project/alerts/external.yaml",
							now,
							now,
							&typesv1.AlertGroupSpec_NamedAlertRuleSpec{
								Id: "ExternalAlert",
								Spec: &typesv1.AlertRuleSpec{
									Name: "ExternalAlert",
									Expr: mkQuery("up == 0"),
									For:  durationpb.New(0),
								},
							},
						),
					},
				},
			},
		},
		{
			name: "adopt discovered alert group via update with is_readonly=false",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "write external alert group",
					at:   0,
					fileOperation: &fileOperation{
						action: "write",
						path:   "test-project/alerts/external.yaml",
						data: []byte(`groups:
  - name: external
    interval: 30s
    rules:
      - alert: ExternalAlert
        expr: 'up == 0'
`),
					},
				},
				{
					name: "adopt via update with is_readonly=false",
					at:   1 * time.Second,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateAlertGroup(ctx, &alertv1.UpdateAlertGroupRequest{
							ProjectName: "test-project",
							Name:        "external",
							Spec: &typesv1.AlertGroupSpec{
								Name:       "external",
								Interval:   durationpb.New(30 * time.Second),
								IsReadonly: false,
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "test-project/alerts/external.yaml",
						shouldExist: true,
						contains:    []string{"# managed-by: humanlog", "# humanlog.is_readonly: false"},
					},
					expectAlertGroup: &alertGroupExpectation{
						projectName: "test-project",
						groupName:   "external",
						expected: managedAlertGroup(
							"external",
							"external",
							false,
							30*time.Second,
							"test-project/alerts/external.yaml",
							now,
							now.Add(1*time.Second),
						),
					},
				},
			},
		},
		{
			name: "detect generated alert groups",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "write file with generation marker",
					at:   0,
					fileOperation: &fileOperation{
						action: "write",
						path:   "test-project/alerts/generated.yaml",
						data: []byte(`# generated-by: some-tool
groups:
  - name: generated
    interval: 1m
    rules:
      - alert: GeneratedAlert
        expr: 'metric > 100'
`),
					},
					expectAlertGroup: &alertGroupExpectation{
						projectName: "test-project",
						groupName:   "generated",
						expected: generatedAlertGroup(
							"generated",
							"generated",
							true,
							1*time.Minute,
							"test-project/alerts/generated.yaml",
							"file contains '# generated-by' marker",
							now,
							now,
							&typesv1.AlertGroupSpec_NamedAlertRuleSpec{
								Id: "GeneratedAlert",
								Spec: &typesv1.AlertRuleSpec{
									Name: "GeneratedAlert",
									Expr: mkQuery("metric > 100"),
									For:  durationpb.New(0),
								},
							},
						),
					},
				},
			},
		},
		{
			name: "lock managed alert group (set readonly=true)",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "create editable alert group",
					at:   0,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateAlertGroup(ctx, &alertv1.CreateAlertGroupRequest{
							ProjectName: "test-project",
							Spec:        mkAlertGroupSpec("cpu-alerts", 1*time.Minute),
						})
						return err
					},
				},
				{
					name: "lock via update with is_readonly=true",
					at:   1 * time.Second,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateAlertGroup(ctx, &alertv1.UpdateAlertGroupRequest{
							ProjectName: "test-project",
							Name:        "cpu-alerts",
							Spec: &typesv1.AlertGroupSpec{
								Name:       "cpu-alerts",
								Interval:   durationpb.New(1 * time.Minute),
								IsReadonly: true,
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "test-project/alerts/cpu-alerts.yaml",
						shouldExist: true,
						contains:    []string{"# humanlog.is_readonly: true"},
					},
					expectAlertGroup: &alertGroupExpectation{
						projectName: "test-project",
						groupName:   "cpu-alerts",
						expected: managedAlertGroup(
							"cpu-alerts",
							"cpu-alerts",
							true,
							1*time.Minute,
							"test-project/alerts/cpu-alerts.yaml",
							now,
							now.Add(1*time.Second),
						),
					},
				},
			},
		},
		{
			name: "delete managed alert group",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "create alert group",
					at:   0,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateAlertGroup(ctx, &alertv1.CreateAlertGroupRequest{
							ProjectName: "test-project",
							Spec:        mkAlertGroupSpec("cpu-alerts", 1*time.Minute),
						})
						return err
					},
				},
				{
					name: "delete alert group",
					at:   1 * time.Second,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.DeleteAlertGroup(ctx, &alertv1.DeleteAlertGroupRequest{
							ProjectName: "test-project",
							Name:        "cpu-alerts",
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "test-project/alerts/cpu-alerts.yaml",
						shouldExist: false,
					},
				},
			},
		},
		{
			name: "cannot delete readonly alert group",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "write external alert group",
					at:   0,
					fileOperation: &fileOperation{
						action: "write",
						path:   "test-project/alerts/external.yaml",
						data: []byte(`groups:
  - name: external
    interval: 30s
    rules:
      - alert: ExternalAlert
        expr: 'up == 0'
`),
					},
				},
				{
					name: "attempt to delete readonly group",
					at:   1 * time.Second,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.DeleteAlertGroup(ctx, &alertv1.DeleteAlertGroupRequest{
							ProjectName: "test-project",
							Name:        "external",
						})
						return err
					},
					expectError: &errorExpectation{
						shouldFail: true,
						contains:   "readonly",
					},
					expectFile: &fileExpectation{
						path:        "test-project/alerts/external.yaml",
						shouldExist: true,
					},
				},
			},
		},
		{
			name: "create alert group with multiple rules",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "create group with 3 rules",
					at:   0,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateAlertGroup(ctx, &alertv1.CreateAlertGroupRequest{
							ProjectName: "test-project",
							Spec: mkAlertGroupSpec("system-alerts", 1*time.Minute,
								struct {
									id          string
									name        string
									expr        string
									labels      map[string]string
									annotations map[string]string
								}{"high-cpu", "HighCPU", "cpu > 80", map[string]string{"severity": "warning"}, nil},
								struct {
									id          string
									name        string
									expr        string
									labels      map[string]string
									annotations map[string]string
								}{"high-memory", "HighMemory", "memory > 90", map[string]string{"severity": "critical"}, nil},
								struct {
									id          string
									name        string
									expr        string
									labels      map[string]string
									annotations map[string]string
								}{"disk-full", "DiskFull", "disk_usage > 95", map[string]string{"severity": "critical"}, nil},
							),
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "test-project/alerts/system-alerts.yaml",
						shouldExist: true,
						contains:    []string{"# managed-by: humanlog", "HighCPU", "HighMemory", "DiskFull"},
					},
				},
			},
		},
		{
			name: "update alert group interval",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "create with 1m interval",
					at:   0,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateAlertGroup(ctx, &alertv1.CreateAlertGroupRequest{
							ProjectName: "test-project",
							Spec:        mkAlertGroupSpec("cpu-alerts", 1*time.Minute),
						})
						return err
					},
				},
				{
					name: "update to 30s interval",
					at:   1 * time.Second,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateAlertGroup(ctx, &alertv1.UpdateAlertGroupRequest{
							ProjectName: "test-project",
							Name:        "cpu-alerts",
							Spec:        mkAlertGroupSpec("cpu-alerts", 30*time.Second),
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "test-project/alerts/cpu-alerts.yaml",
						shouldExist: true,
						contains:    []string{"interval: 30s"},
					},
				},
			},
		},
		{
			name: "cannot create alert group in readonly project",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", true)),
				},
			},
			transitions: []transition{
				{
					name: "attempt to create in readonly project",
					at:   0,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateAlertGroup(ctx, &alertv1.CreateAlertGroupRequest{
							ProjectName: "test-project",
							Spec:        mkAlertGroupSpec("cpu-alerts", 1*time.Minute),
						})
						return err
					},
					expectError: &errorExpectation{
						shouldFail: true,
						contains:   "readonly",
					},
				},
			},
		},
		{
			name: "delete and recreate alert group",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "create alert group",
					at:   0,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateAlertGroup(ctx, &alertv1.CreateAlertGroupRequest{
							ProjectName: "test-project",
							Spec:        mkAlertGroupSpec("cpu-alerts", 1*time.Minute),
						})
						return err
					},
				},
				{
					name: "delete alert group",
					at:   1 * time.Second,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.DeleteAlertGroup(ctx, &alertv1.DeleteAlertGroupRequest{
							ProjectName: "test-project",
							Name:        "cpu-alerts",
						})
						return err
					},
				},
				{
					name: "recreate with different interval",
					at:   2 * time.Second,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateAlertGroup(ctx, &alertv1.CreateAlertGroupRequest{
							ProjectName: "test-project",
							Spec:        mkAlertGroupSpec("cpu-alerts", 2*time.Minute),
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "test-project/alerts/cpu-alerts.yaml",
						shouldExist: true,
						contains:    []string{"interval: 2m"},
					},
				},
			},
		},
		{
			name: "round-trip alert group lifecycle",
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					projectConfig("test-project",
						localProjectPointer("test-project", "dashboards", "alerts", false)),
				},
			},
			transitions: []transition{
				{
					name: "create as editable",
					at:   0,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateAlertGroup(ctx, &alertv1.CreateAlertGroupRequest{
							ProjectName: "test-project",
							Spec: &typesv1.AlertGroupSpec{
								Name:       "cpu-alerts",
								Interval:   durationpb.New(1 * time.Minute),
								IsReadonly: false,
							},
						})
						return err
					},
					expectAlertGroup: &alertGroupExpectation{
						projectName: "test-project",
						groupName:   "cpu-alerts",
						expected: managedAlertGroup(
							"cpu-alerts",
							"cpu-alerts",
							false,
							1*time.Minute,
							"test-project/alerts/cpu-alerts.yaml",
							now,
							now,
						),
					},
				},
				{
					name: "lock it",
					at:   1 * time.Second,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateAlertGroup(ctx, &alertv1.UpdateAlertGroupRequest{
							ProjectName: "test-project",
							Name:        "cpu-alerts",
							Spec: &typesv1.AlertGroupSpec{
								Name:       "cpu-alerts",
								Interval:   durationpb.New(1 * time.Minute),
								IsReadonly: true,
							},
						})
						return err
					},
					expectAlertGroup: &alertGroupExpectation{
						projectName: "test-project",
						groupName:   "cpu-alerts",
						expected: managedAlertGroup(
							"cpu-alerts",
							"cpu-alerts",
							true,
							1*time.Minute,
							"test-project/alerts/cpu-alerts.yaml",
							now,
							now.Add(1*time.Second),
						),
					},
				},
				{
					name: "unlock it",
					at:   2 * time.Second,
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateAlertGroup(ctx, &alertv1.UpdateAlertGroupRequest{
							ProjectName: "test-project",
							Name:        "cpu-alerts",
							Spec: &typesv1.AlertGroupSpec{
								Name:       "cpu-alerts",
								Interval:   durationpb.New(1 * time.Minute),
								IsReadonly: false,
							},
						})
						return err
					},
					expectAlertGroup: &alertGroupExpectation{
						projectName: "test-project",
						groupName:   "cpu-alerts",
						expected: managedAlertGroup(
							"cpu-alerts",
							"cpu-alerts",
							false,
							1*time.Minute,
							"test-project/alerts/cpu-alerts.yaml",
							now,
							now.Add(2*time.Second),
						),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fs := memfs.New()

			// Create project directory structure
			for _, proj := range tt.cfg.Projects {
				require.NoError(t, fs.MkdirAll(filepath.Join(proj.Pointer.GetLocalhost().Path, "alerts"), 0755))
			}

			var db localstate.DB
			var currentTime time.Time = now

			for _, tr := range tt.transitions {
				// Advance time
				currentTime = now.Add(tr.at)
				timeNowAtTransition := func() time.Time { return currentTime }

				// Recreate watch with new time
				db = newWatch(ctx, t, tt.cfg, fs, timeNowAtTransition)

				// Ensure project is synced (which registers it in alert state)
				for _, proj := range tt.cfg.Projects {
					_, err := db.SyncProject(ctx, &projectv1.SyncProjectRequest{
						Name: proj.Name,
					})
					require.NoError(t, err, "failed to sync project %q", proj.Name)
				}

				// Execute file operation if specified
				if tr.fileOperation != nil {
					switch tr.fileOperation.action {
					case "write":
						err := writeFile(fs, tr.fileOperation.path, tr.fileOperation.data)
						require.NoError(t, err, "transition %q: failed to write file", tr.name)
						// Re-create watch to pick up file changes
						db = newWatch(ctx, t, tt.cfg, fs, timeNowAtTransition)
					case "delete":
						err := fs.Remove(tr.fileOperation.path)
						require.NoError(t, err, "transition %q: failed to delete file", tr.name)
						// Re-create watch to pick up file changes
						db = newWatch(ctx, t, tt.cfg, fs, timeNowAtTransition)
					}
				}

				// Execute operation if specified
				if tr.operation != nil {
					err := tr.operation(ctx, t, db, fs)
					if tr.expectError != nil {
						if tr.expectError.shouldFail {
							require.Error(t, err, "transition %q: expected error but got none", tr.name)
							if tr.expectError.contains != "" {
								require.Contains(t, err.Error(), tr.expectError.contains,
									"transition %q: error message mismatch", tr.name)
							}
						} else {
							require.NoError(t, err, "transition %q: unexpected error", tr.name)
						}
					} else {
						require.NoError(t, err, "transition %q: unexpected error", tr.name)
					}
				}

				// Check file expectations
				if tr.expectFile != nil {
					if tr.expectFile.shouldExist {
						data, err := readFile(fs, tr.expectFile.path)
						require.NoError(t, err, "transition %q: expected file to exist at %s", tr.name, tr.expectFile.path)
						content := string(data)
						for _, substr := range tr.expectFile.contains {
							require.Contains(t, content, substr,
								"transition %q: file %s missing expected content", tr.name, tr.expectFile.path)
						}
					} else {
						_, err := fs.Stat(tr.expectFile.path)
						require.True(t, os.IsNotExist(err),
							"transition %q: file %s should not exist", tr.name, tr.expectFile.path)
					}
				}

				// Check alert group expectations
				if tr.expectAlertGroup != nil {
					resp, err := db.GetAlertGroup(ctx, &alertv1.GetAlertGroupRequest{
						ProjectName: tr.expectAlertGroup.projectName,
						Name:        tr.expectAlertGroup.groupName,
					})
					require.NoError(t, err, "transition %q: failed to get alert group", tr.name)

					if diff := cmp.Diff(tr.expectAlertGroup.expected, resp.AlertGroup, cmpOpts...); diff != "" {
						t.Errorf("transition %q: alert group mismatch (-want +got):\n%s", tr.name, diff)
					}
				}
			}
		})
	}
}

func managedAlertGroup(
	id string,
	name string,
	isReadonly bool,
	interval time.Duration,
	path string,
	createdAt, updatedAt time.Time,
	rules ...*typesv1.AlertGroupSpec_NamedAlertRuleSpec,
) *typesv1.AlertGroup {
	return managedAlertGroupWithStatuses(id, name, isReadonly, interval, path, createdAt, updatedAt, nil, rules...)
}

func managedAlertGroupWithStatuses(
	id string,
	name string,
	isReadonly bool,
	interval time.Duration,
	path string,
	createdAt, updatedAt time.Time,
	customStatuses map[string]*typesv1.AlertRuleStatus,
	rules ...*typesv1.AlertGroupSpec_NamedAlertRuleSpec,
) *typesv1.AlertGroup {
	// Build rule statuses from rule specs
	ruleStatuses := make([]*typesv1.AlertGroupStatus_NamedAlertRuleStatus, len(rules))
	for i, rule := range rules {
		status := customStatuses[rule.Id]
		if status == nil {
			status = &typesv1.AlertRuleStatus{
				Status: &typesv1.AlertRuleStatus_Unknown{
					Unknown: &typesv1.AlertUnknown{},
				},
			}
		}
		ruleStatuses[i] = &typesv1.AlertGroupStatus_NamedAlertRuleStatus{
			Id:     rule.Id,
			Status: status,
		}
	}

	return &typesv1.AlertGroup{
		Meta: &typesv1.AlertGroupMeta{
			Id: id,
		},
		Spec: &typesv1.AlertGroupSpec{
			Name:       name,
			Interval:   durationpb.New(interval),
			IsReadonly: isReadonly,
			Rules:      rules,
		},
		Status: &typesv1.AlertGroupStatus{
			CreatedAt: timestamppb.New(createdAt),
			UpdatedAt: timestamppb.New(updatedAt),
			Rules:     ruleStatuses,
			Origin: &typesv1.AlertGroupStatus_Managed{
				Managed: &typesv1.AlertGroupStatus_ManagedAlertGroup{
					Path: path,
				},
			},
		},
	}
}

func discoveredAlertGroup(
	id string,
	name string,
	isReadonly bool,
	interval time.Duration,
	path string,
	createdAt, updatedAt time.Time,
	rules ...*typesv1.AlertGroupSpec_NamedAlertRuleSpec,
) *typesv1.AlertGroup {
	return discoveredAlertGroupWithStatuses(id, name, isReadonly, interval, path, createdAt, updatedAt, nil, rules...)
}

func discoveredAlertGroupWithStatuses(
	id string,
	name string,
	isReadonly bool,
	interval time.Duration,
	path string,
	createdAt, updatedAt time.Time,
	customStatuses map[string]*typesv1.AlertRuleStatus,
	rules ...*typesv1.AlertGroupSpec_NamedAlertRuleSpec,
) *typesv1.AlertGroup {
	// Build rule statuses from rule specs
	ruleStatuses := make([]*typesv1.AlertGroupStatus_NamedAlertRuleStatus, len(rules))
	for i, rule := range rules {
		status := customStatuses[rule.Id]
		if status == nil {
			status = &typesv1.AlertRuleStatus{
				Status: &typesv1.AlertRuleStatus_Unknown{
					Unknown: &typesv1.AlertUnknown{},
				},
			}
		}
		ruleStatuses[i] = &typesv1.AlertGroupStatus_NamedAlertRuleStatus{
			Id:     rule.Id,
			Status: status,
		}
	}

	return &typesv1.AlertGroup{
		Meta: &typesv1.AlertGroupMeta{
			Id: id,
		},
		Spec: &typesv1.AlertGroupSpec{
			Name:       name,
			Interval:   durationpb.New(interval),
			IsReadonly: isReadonly,
			Rules:      rules,
		},
		Status: &typesv1.AlertGroupStatus{
			CreatedAt: timestamppb.New(createdAt),
			UpdatedAt: timestamppb.New(updatedAt),
			Rules:     ruleStatuses,
			Origin: &typesv1.AlertGroupStatus_Discovered{
				Discovered: &typesv1.AlertGroupStatus_DiscoveredAlertGroup{
					Path: path,
				},
			},
		},
	}
}

func generatedAlertGroup(
	id string,
	name string,
	isReadonly bool,
	interval time.Duration,
	path string,
	detectionReason string,
	createdAt, updatedAt time.Time,
	rules ...*typesv1.AlertGroupSpec_NamedAlertRuleSpec,
) *typesv1.AlertGroup {
	return generatedAlertGroupWithStatuses(id, name, isReadonly, interval, path, detectionReason, createdAt, updatedAt, nil, rules...)
}

func generatedAlertGroupWithStatuses(
	id string,
	name string,
	isReadonly bool,
	interval time.Duration,
	path string,
	detectionReason string,
	createdAt, updatedAt time.Time,
	customStatuses map[string]*typesv1.AlertRuleStatus,
	rules ...*typesv1.AlertGroupSpec_NamedAlertRuleSpec,
) *typesv1.AlertGroup {
	// Build rule statuses from rule specs
	ruleStatuses := make([]*typesv1.AlertGroupStatus_NamedAlertRuleStatus, len(rules))
	for i, rule := range rules {
		status := customStatuses[rule.Id]
		if status == nil {
			status = &typesv1.AlertRuleStatus{
				Status: &typesv1.AlertRuleStatus_Unknown{
					Unknown: &typesv1.AlertUnknown{},
				},
			}
		}
		ruleStatuses[i] = &typesv1.AlertGroupStatus_NamedAlertRuleStatus{
			Id:     rule.Id,
			Status: status,
		}
	}

	return &typesv1.AlertGroup{
		Meta: &typesv1.AlertGroupMeta{
			Id: id,
		},
		Spec: &typesv1.AlertGroupSpec{
			Name:       name,
			Interval:   durationpb.New(interval),
			IsReadonly: isReadonly,
			Rules:      rules,
		},
		Status: &typesv1.AlertGroupStatus{
			CreatedAt: timestamppb.New(createdAt),
			UpdatedAt: timestamppb.New(updatedAt),
			Rules:     ruleStatuses,
			Origin: &typesv1.AlertGroupStatus_Generated{
				Generated: &typesv1.AlertGroupStatus_GeneratedAlertGroup{
					Path:            path,
					DetectionReason: detectionReason,
				},
			},
		},
	}
}
