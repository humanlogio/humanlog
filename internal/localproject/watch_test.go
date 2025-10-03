package localproject

import (
	"context"
	"net/url"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-git/go-billy/v6/memfs"
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
		return []byte(alertGroupYAML)
	}
	now := time.Date(2025, 9, 25, 17, 24, 19, 0, time.UTC)
	timeNow := func() time.Time { return now }

	cmpOpts := []cmp.Option{
		protocmp.Transform(),
		protocmp.IgnoreFields(&typesv1.DashboardStatus{}, "created_at", "updated_at"),
		protocmp.IgnoreFields(&typesv1.ProjectStatus{}, "created_at", "updated_at"),
	}

	type subtest struct {
		name  string
		check func(context.Context, *testing.T, localstate.DB)
	}

	tests := []struct {
		name    string
		fs      fstest.MapFS
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
				Projects: projectConfigs(
					projectConfig("my project",
						localProjectPointer("project1dir", "dashdir", "alertdir", true),
					),
					projectConfig("my other project",
						localProjectPointer("project2dir", "nested/dashdir", "nested/alertdir", true),
					),
				),
			},
			subtest: []subtest{
				{
					name: "list projects",
					check: func(ctx context.Context, t *testing.T, d localstate.DB) {
						want := []*projectv1.ListProjectResponse_ListItem{
							{Project: &typesv1.Project{
								Meta: &typesv1.ProjectMeta{},
								Spec: &typesv1.ProjectSpec{
									Name: "my project",
									Pointer: &typesv1.ProjectPointer{Scheme: &typesv1.ProjectPointer_Localhost{
										Localhost: &typesv1.ProjectPointer_LocalGit{
											Path:         "project1dir",
											AlertDir:     "alertdir",
											DashboardDir: "dashdir",
											ReadOnly:     true,
										},
									}},
								},
								Status: &typesv1.ProjectStatus{
									CreatedAt: timestamppb.New(now),
									UpdatedAt: timestamppb.New(now),
								},
							}},
							{Project: &typesv1.Project{
								Meta: &typesv1.ProjectMeta{},
								Spec: &typesv1.ProjectSpec{
									Name: "my other project",
									Pointer: &typesv1.ProjectPointer{Scheme: &typesv1.ProjectPointer_Localhost{
										Localhost: &typesv1.ProjectPointer_LocalGit{
											Path:         "project2dir",
											AlertDir:     "nested/alertdir",
											DashboardDir: "nested/dashdir",
											ReadOnly:     true,
										},
									}},
								},
								Status: &typesv1.ProjectStatus{
									CreatedAt: timestamppb.New(now),
									UpdatedAt: timestamppb.New(now),
								},
							}},
						}
						res, err := d.ListProject(ctx, &projectv1.ListProjectRequest{})
						require.NoError(t, err)
						got := res.Items

						diff := cmp.Diff(want, got, cmpOpts...)
						require.Empty(t, diff)
					},
				},
				{
					name: "get project and details",
					check: func(ctx context.Context, t *testing.T, d localstate.DB) {
						want := &projectv1.GetProjectResponse{
							Project: project(
								"my project",
								localProjectPointer("project1dir", "dashdir", "alertdir", true),
								now, now,
							),
							Dashboards: dashboards(
								dashboard(
									dashboardID("my project", "my project", "my_dashboard"),
									"my dashboard",
									"it's a nice dashboard",
									true,
									mkDashboardDataJSON(),
									"project1dir/dashdir/dash1.json",
									now, now,
								),
								dashboard(
									dashboardID("my project", "my project", "my_dashboard"),
									"my dashboard",
									"it's a nice dashboard",
									true,
									mkDashboardDataYAML(),
									"project1dir/dashdir/dash2.yaml",
									now,
									now,
								),
								dashboard(
									dashboardID("my project", "my project", "my_dashboard"),
									"my dashboard",
									"it's a nice dashboard",
									true,
									mkDashboardDataYAML(),
									"project1dir/dashdir/dash3.yml",
									now,
									now,
								),
							),
							AlertGroups: alertGroups(
								alertGroup(
									"my-group-name",
									30*time.Second,
									&typesv1.Obj{},
									[]*typesv1.AlertRule{
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
								),
								alertGroup(
									"my-another-name",
									30*time.Second,
									&typesv1.Obj{},
									[]*typesv1.AlertRule{
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
								),
								alertGroup(
									"my-group-name",
									30*time.Second,
									&typesv1.Obj{},
									[]*typesv1.AlertRule{
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
								),
								alertGroup(
									"my-another-name",
									30*time.Second,
									&typesv1.Obj{},
									[]*typesv1.AlertRule{
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
								),
							),
						}
						got, err := d.GetProject(ctx, &projectv1.GetProjectRequest{Name: "my project"})
						require.NoError(t, err)
						diff := cmp.Diff(want, got, cmpOpts...)
						require.Empty(t, diff)
					},
				},
				{
					name: "get dashboard by id",
					check: func(ctx context.Context, t *testing.T, d localstate.DB) {
						want := dashboard(
							dashboardID("my project", "my project", "my_dashboard"),
							"my dashboard",
							"it's a nice dashboard",
							true,
							mkDashboardDataJSON(),
							"project1dir/dashdir/dash1.json",
							now,
							now,
						)
						res, err := d.GetDashboard(ctx, &dashboardv1.GetDashboardRequest{
							EnvironmentId: 0,
							ProjectName:   "my project",
							Id:            dashboardID("my project", "my project", "my_dashboard"),
						})
						require.NoError(t, err)
						got := res.Dashboard
						diff := cmp.Diff(want, got, cmpOpts...)
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
							return d.Spec.Name == "my dashboard"
						})
						require.NotEqual(t, -1, i)

						db := gotProject.Dashboards[i]

						want := dashboard(
							dashboardID("my project", "my project", "my_dashboard"),
							"my dashboard",
							"it's a nice dashboard",
							true,
							mkDashboardDataJSON(),
							"project1dir/dashdir/dash1.json",
							now,
							now,
						)
						res, err := d.GetDashboard(ctx, &dashboardv1.GetDashboardRequest{
							EnvironmentId: 0,
							ProjectName:   gotProject.Project.Spec.Name,
							Id:            db.Meta.Id,
						})
						require.NoError(t, err)
						got := res.Dashboard

						diff := cmp.Diff(want, got, cmpOpts...)
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

			fs := memfs.New()
			for name, f := range tt.fs {
				ff, err := fs.Create(name)
				require.NoError(t, err)
				_, err = ff.Write(f.Data)
				require.NoError(t, err)
				err = ff.Close()

				require.NoError(t, err)

			}

			db, err := internalWatch(ctx, fs, cfg, alertState, parseQuery, timeNow)
			require.NoError(t, err)
			for _, tt := range tt.subtest {
				t.Run(tt.name, func(t *testing.T) {
					tt.check(t.Context(), t, db)
				})
			}
		})
	}
}

func TestDashboardIDIsURLSafe(t *testing.T) {
	want := dashboardID("hello world", "i love potatoes", "my garden dashboards, potato focused")
	got := url.QueryEscape(want)
	require.Equal(t, want, got, "should not require escaping")
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

const alertGroupYAML = `groups:
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

func projectConfigs(in ...*typesv1.ProjectsConfig_Project) []*typesv1.ProjectsConfig_Project {
	return in
}

func projectConfig(projectName string, ptr *typesv1.ProjectPointer) *typesv1.ProjectsConfig_Project {
	return &typesv1.ProjectsConfig_Project{
		Name:    projectName,
		Pointer: ptr,
	}
}

func localProjectPointer(path, dashboard, alert string, readOnly bool) *typesv1.ProjectPointer {
	return &typesv1.ProjectPointer{
		Scheme: &typesv1.ProjectPointer_Localhost{
			Localhost: &typesv1.ProjectPointer_LocalGit{
				Path:         path,
				DashboardDir: dashboard,
				AlertDir:     alert,
				ReadOnly:     readOnly,
			},
		},
	}
}

func remoteProjectPointer(remoteURL, ref, dashboard, alert string) *typesv1.ProjectPointer {
	return &typesv1.ProjectPointer{
		Scheme: &typesv1.ProjectPointer_Remote{
			Remote: &typesv1.ProjectPointer_RemoteGit{
				RemoteUrl:    remoteURL,
				Ref:          ref,
				DashboardDir: dashboard,

				AlertDir: alert,
			},
		},
	}
}

func project(
	name string,
	ptr *typesv1.ProjectPointer,
	createdAt, updatedAt time.Time,
) *typesv1.Project {
	return &typesv1.Project{
		Meta: &typesv1.ProjectMeta{},
		Spec: &typesv1.ProjectSpec{
			Name:    name,
			Pointer: ptr,
		},
		Status: &typesv1.ProjectStatus{
			CreatedAt: timestamppb.New(createdAt),
			UpdatedAt: timestamppb.New(updatedAt),
		},
	}
}

func dashboards(in ...*typesv1.Dashboard) []*typesv1.Dashboard {
	return in
}

func dashboard(
	id string,
	name, desc string, isReadonly bool,
	persesJSON []byte,
	file string,
	createdAt, updatedAt time.Time,
) *typesv1.Dashboard {
	return &typesv1.Dashboard{
		Meta: &typesv1.DashboardMeta{
			Id: id,
		},
		Spec: &typesv1.DashboardSpec{
			Name:        name,
			Description: desc,
			IsReadonly:  isReadonly,
			PersesJson:  persesJSON,
			Source:      &typesv1.DashboardSpec_File{File: file},
		},
		Status: &typesv1.DashboardStatus{
			CreatedAt: timestamppb.New(createdAt),
			UpdatedAt: timestamppb.New(updatedAt),
		},
	}
}

func alertGroups(in ...*typesv1.AlertGroup) []*typesv1.AlertGroup {
	return in
}

func alertGroup(name string, interval time.Duration, labels *typesv1.Obj, rules []*typesv1.AlertRule) *typesv1.AlertGroup {
	return &typesv1.AlertGroup{
		Meta: &typesv1.AlertGroupMeta{},
		Spec: &typesv1.AlertGroupSpec{
			Name:     name,
			Interval: durationpb.New(interval),
			Labels:   labels,
			Rules:    rules,
		},
		Status: &typesv1.AlertGroupStatus{},
	}
}
