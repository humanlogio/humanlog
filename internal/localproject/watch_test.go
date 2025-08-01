package localproject

import (
	"context"
	"io/fs"
	"net/url"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/go-cmp/cmp"
	dashboardv1 "github.com/humanlogio/api/go/svc/dashboard/v1"
	projectv1 "github.com/humanlogio/api/go/svc/project/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/localstate"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDashboardIDIsURLSafe(t *testing.T) {
	want := dashboardID("hello world", "i love potatoes", "my garden dashboards, potato focused")
	got := url.QueryEscape(want)
	require.Equal(t, want, got, "should not require escaping")
}

func TestWatch(t *testing.T) {
	mkDashboardDataJSON := func() []byte {
		return []byte(`{
			"kind": "Dashboard",
			"metadata": {
				"project": "my project",
				"name": "my_dashboard"
			},
			"spec": {
				"display": {
					"name": "my dashboard",
					"description": "it's a nice dashboard"
				}
			}
		}`)
	}
	mkDashboardDataYAML := func() []byte {
		return []byte(`kind: Dashboard
metadata:
    project: "my project"
    name: "my_dashboard"
spec:
    display:
        name: "my dashboard"
        description: "it's a nice dashboard"`)
	}
	mkAlertGroupData := func() []byte {
		return []byte(alertGroup)
	}

	type subtest struct {
		name  string
		check func(context.Context, *testing.T, localstate.DB)
	}
	tests := []struct {
		name    string
		fs      fs.FS
		cfg     *typesv1.ProjectsConfig
		subtest []subtest
	}{
		{
			name: "some projects",
			fs: fstest.MapFS{
				"project1dir/dashdir/dash1.json":   &fstest.MapFile{Data: mkDashboardDataJSON()},
				"project1dir/dashdir/dash2.yaml":   &fstest.MapFile{Data: mkDashboardDataYAML()},
				"project1dir/dashdir/dash3.yml":    &fstest.MapFile{Data: mkDashboardDataYAML()},
				"project1dir/dashdir/ignored":      &fstest.MapFile{},
				"project1dir/alertdir/alert1.yaml": &fstest.MapFile{Data: mkAlertGroupData()},
				"project1dir/alertdir/alert2.yml":  &fstest.MapFile{Data: mkAlertGroupData()},
				"project1dir/alertdir/ignored":     &fstest.MapFile{},
				"project1dir/ignored":              &fstest.MapFile{},

				"project2dir/nested/dashdir/dash1.json":   &fstest.MapFile{},
				"project2dir/nested/dashdir/dash2.yaml":   &fstest.MapFile{},
				"project2dir/nested/dashdir/dash3.yml":    &fstest.MapFile{},
				"project2dir/nested/dashdir/ignored":      &fstest.MapFile{},
				"project2dir/nested/alertdir/alert1.yaml": &fstest.MapFile{},
				"project2dir/nested/alertdir/alert2.yml":  &fstest.MapFile{},
				"project2dir/nested/alertdir/ignored":     &fstest.MapFile{},
				"project2dir/nested/ignored":              &fstest.MapFile{},
			},
			cfg: &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{
					localProjectPointer("my project", "project1dir", "dashdir", "alertdir", true),
					localProjectPointer("my project", "project2dir", "nested/dashdir", "nested/alertdir", true),
				},
			},
			subtest: []subtest{
				{
					name: "get project and details",
					check: func(ctx context.Context, t *testing.T, d localstate.DB) {
						want := &projectv1.GetProjectResponse{
							Project: &typesv1.Project{
								Name: "my project",
								Pointer: &typesv1.ProjectPointer{Scheme: &typesv1.ProjectPointer_Localhost{
									Localhost: &typesv1.ProjectPointer_LocalGit{
										Path:         "project1dir",
										AlertDir:     "alertdir",
										DashboardDir: "dashdir",
									},
								}},
								CreatedAt: timestamppb.New(time.Time{}),
								UpdatedAt: timestamppb.New(time.Time{}),
							},
							Dashboards: []*typesv1.Dashboard{
								{
									Id:          dashboardID("my project", "my project", "my_dashboard"),
									Name:        "my dashboard",
									Description: "it's a nice dashboard",
									IsReadonly:  true,
									CreatedAt:   timestamppb.New(time.Time{}),
									UpdatedAt:   timestamppb.New(time.Time{}),
									PersesJson:  mkDashboardDataJSON(),
									Source:      &typesv1.Dashboard_File{File: "project1dir/dashdir/dash1.json"},
								},
								{
									Id:          dashboardID("my project", "my project", "my_dashboard"),
									Name:        "my dashboard",
									Description: "it's a nice dashboard",
									IsReadonly:  true,
									CreatedAt:   timestamppb.New(time.Time{}),
									UpdatedAt:   timestamppb.New(time.Time{}),
									PersesJson:  mkDashboardDataYAML(),
									Source:      &typesv1.Dashboard_File{File: "project1dir/dashdir/dash2.yaml"},
								},
								{
									Id:          dashboardID("my project", "my project", "my_dashboard"),
									Name:        "my dashboard",
									Description: "it's a nice dashboard",
									IsReadonly:  true,
									CreatedAt:   timestamppb.New(time.Time{}),
									UpdatedAt:   timestamppb.New(time.Time{}),
									PersesJson:  mkDashboardDataYAML(),
									Source:      &typesv1.Dashboard_File{File: "project1dir/dashdir/dash3.yml"},
								},
							},
							AlertGroups: []*typesv1.AlertGroup{
								{
									Name:     "my-group-name",
									Interval: durationpb.New(30 * time.Second),
									Labels:   &typesv1.Obj{},
									Rules: []*typesv1.AlertRule{
										{
											Name: "HighErrors",
											For:  durationpb.New(5 * time.Minute),
											Expr: mustParseQuery(`filter severity_text == "error"`),
											Labels: &typesv1.Obj{
												Kvs: []*typesv1.KV{
													typesv1.KeyVal("severity", typesv1.ValStr("critical")),
												},
											},
											Annotations: &typesv1.Obj{Kvs: []*typesv1.KV{
												typesv1.KeyVal("description", typesv1.ValStr("stuff's happening with {{ $.labels.service }}")),
											}},
										},
									},
								},
								{
									Name:     "my-another-name",
									Interval: durationpb.New(30 * time.Second),
									Labels:   &typesv1.Obj{},
									Rules: []*typesv1.AlertRule{
										{
											Name: "HighErrors",
											For:  durationpb.New(5 * time.Minute),
											Expr: mustParseQuery(`filter severity_text == "error"`),
											Labels: &typesv1.Obj{Kvs: []*typesv1.KV{
												typesv1.KeyVal("severity", typesv1.ValStr("critical")),
											}},
											Annotations: &typesv1.Obj{},
										},
									},
								},
								{
									Name:     "my-group-name",
									Interval: durationpb.New(30 * time.Second),
									Labels:   &typesv1.Obj{},
									Rules: []*typesv1.AlertRule{
										{
											Name: "HighErrors",
											For:  durationpb.New(5 * time.Minute),
											Expr: mustParseQuery(`filter severity_text == "error"`),
											Labels: &typesv1.Obj{
												Kvs: []*typesv1.KV{
													typesv1.KeyVal("severity", typesv1.ValStr("critical")),
												},
											},
											Annotations: &typesv1.Obj{Kvs: []*typesv1.KV{
												typesv1.KeyVal("description", typesv1.ValStr("stuff's happening with {{ $.labels.service }}")),
											}},
										},
									},
								},
								{
									Name:     "my-another-name",
									Interval: durationpb.New(30 * time.Second),
									Labels:   &typesv1.Obj{},
									Rules: []*typesv1.AlertRule{
										{
											Name: "HighErrors",
											For:  durationpb.New(5 * time.Minute),
											Expr: mustParseQuery(`filter severity_text == "error"`),
											Labels: &typesv1.Obj{Kvs: []*typesv1.KV{
												typesv1.KeyVal("severity", typesv1.ValStr("critical")),
											}},
											Annotations: &typesv1.Obj{},
										},
									},
								},
							},
						}
						got, err := d.GetProject(ctx, &projectv1.GetProjectRequest{Name: "my project"})
						require.NoError(t, err)
						diff := cmp.Diff(want, got, protocmp.Transform())
						require.Empty(t, diff)
					},
				},
				{
					name: "list projects",
					check: func(ctx context.Context, t *testing.T, d localstate.DB) {
						want := []*projectv1.ListProjectResponse_ListItem{
							{Project: &typesv1.Project{
								Name: "my project",
								Pointer: &typesv1.ProjectPointer{Scheme: &typesv1.ProjectPointer_Localhost{
									Localhost: &typesv1.ProjectPointer_LocalGit{
										Path:         "project1dir",
										AlertDir:     "alertdir",
										DashboardDir: "dashdir",
									},
								}},
								CreatedAt: timestamppb.New(time.Time{}),
								UpdatedAt: timestamppb.New(time.Time{}),
							}},
							{Project: &typesv1.Project{
								Name: "my other project",
								Pointer: &typesv1.ProjectPointer{Scheme: &typesv1.ProjectPointer_Localhost{
									Localhost: &typesv1.ProjectPointer_LocalGit{
										Path:         "project2dir",
										AlertDir:     "nested/alertdir",
										DashboardDir: "nested/dashdir",
									},
								}},
								CreatedAt: timestamppb.New(time.Time{}),
								UpdatedAt: timestamppb.New(time.Time{}),
							}},
						}
						res, err := d.ListProject(ctx, &projectv1.ListProjectRequest{})
						require.NoError(t, err)
						got := res.Items
						diff := cmp.Diff(want, got, protocmp.Transform())
						require.Empty(t, diff)
					},
				},
				{
					name: "get dashboard by id",
					check: func(ctx context.Context, t *testing.T, d localstate.DB) {
						want := &typesv1.Dashboard{
							Id:          dashboardID("my project", "my project", "my_dashboard"),
							Name:        "my dashboard",
							Description: "it's a nice dashboard",
							IsReadonly:  true,
							CreatedAt:   timestamppb.New(time.Time{}),
							UpdatedAt:   timestamppb.New(time.Time{}),
							PersesJson:  mkDashboardDataJSON(),
							Source:      &typesv1.Dashboard_File{File: "project1dir/dashdir/dash1.json"},
						}
						res, err := d.GetDashboard(ctx, &dashboardv1.GetDashboardRequest{
							EnvironmentId: 0,
							ProjectName:   "my project",
							Id:            dashboardID("my project", "my project", "my_dashboard"),
						})
						require.NoError(t, err)
						got := res.Dashboard
						diff := cmp.Diff(want, got, protocmp.Transform())
						require.Empty(t, diff)
					},
				},
				{
					name: "get dashboard by id, via project's dashboard list",
					check: func(ctx context.Context, t *testing.T, d localstate.DB) {
						gotProject, err := d.GetProject(ctx, &projectv1.GetProjectRequest{Name: "my project"})
						require.NoError(t, err)
						// gotProject.Dashboards
						i := slices.IndexFunc(gotProject.Dashboards, func(d *typesv1.Dashboard) bool {
							return d.Name == "my dashboard"
						})
						require.NotEqual(t, -1, i)

						db := gotProject.Dashboards[i]

						want := &typesv1.Dashboard{
							Id:          dashboardID("my project", "my project", "my_dashboard"),
							Name:        "my dashboard",
							Description: "it's a nice dashboard",
							IsReadonly:  true,
							CreatedAt:   timestamppb.New(time.Time{}),
							UpdatedAt:   timestamppb.New(time.Time{}),
							PersesJson:  mkDashboardDataJSON(),
							Source:      &typesv1.Dashboard_File{File: "project1dir/dashdir/dash1.json"},
						}
						res, err := d.GetDashboard(ctx, &dashboardv1.GetDashboardRequest{
							EnvironmentId: 0,
							ProjectName:   gotProject.Project.Name,
							Id:            db.Id,
						})
						require.NoError(t, err)
						got := res.Dashboard
						diff := cmp.Diff(want, got, protocmp.Transform())
						require.Empty(t, diff)
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			alertState := localstate.NewMemory().AlertStateStorage()
			cfg := &config.Config{CurrentConfig: &typesv1.LocalhostConfig{
				Runtime: &typesv1.RuntimeConfig{
					ExperimentalFeatures: &typesv1.RuntimeConfig_ExperimentalFeatures{
						Projects: tt.cfg,
					},
				},
			}}
			db := Watch(ctx, tt.fs, cfg, alertState, parseQuery)
			for _, tt := range tt.subtest {
				t.Run(tt.name, func(t *testing.T) {
					tt.check(t.Context(), t, db)
				})
			}
		})
	}
}

func mustParseQuery(s string) *typesv1.Query {
	q, err := parseQuery(s)
	if err != nil {
		panic(err)
	}
	return q
}

func parseQuery(s string) (*typesv1.Query, error) {
	return &typesv1.Query{Query: &typesv1.Statements{
		Statements: []*typesv1.Statement{
			{Stmt: &typesv1.Statement_Filter{
				Filter: &typesv1.FilterOperator{
					Expr: &typesv1.Expr{
						Expr: &typesv1.Expr_Identifier{
							Identifier: &typesv1.Identifier{Name: s},
						},
					},
				},
			}},
		},
	}}, nil
}

const alertGroup = `groups:
  - name: my-group-name
    interval: 30s # defaults to global interval
    rules:
      - alert: HighErrors
        expr: filter severity_text == "error"
        for: 5m
        labels:
          severity: critical
        annotations:
          description: "stuff's happening with {{ $.labels.service }}"

  - name: my-another-name
    interval: 30s # defaults to global interval
    rules:
      - alert: HighErrors
        expr: filter severity_text == "error"
        for: 5m
        labels:
          severity: critical
`

func localProjectPointer(projectName, path, dashboard, alert string, readOnly bool) *typesv1.ProjectsConfig_Project {
	return &typesv1.ProjectsConfig_Project{
		Pointer: &typesv1.ProjectPointer{
			Scheme: &typesv1.ProjectPointer_Localhost{
				Localhost: &typesv1.ProjectPointer_LocalGit{
					Path:         path,
					DashboardDir: dashboard,
					AlertDir:     alert,
					ReadOnly:     readOnly,
				},
			},
		},
	}
}
