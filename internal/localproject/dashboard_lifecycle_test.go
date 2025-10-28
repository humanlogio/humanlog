package localproject

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/google/go-cmp/cmp"
	dashboardv1 "github.com/humanlogio/api/go/svc/dashboard/v1"
	projectv1 "github.com/humanlogio/api/go/svc/project/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/localstate"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	persesv1 "github.com/perses/perses/pkg/model/api/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDashboardLifecycle(t *testing.T) {
	now := time.Date(2025, 10, 21, 10, 56, 42, 123456, time.UTC)
	timeNow := func() time.Time { return now }

	cmpOpts := []cmp.Option{
		protocmp.Transform(),
		protocmp.IgnoreFields(&typesv1.DashboardStatus{}, "created_at", "updated_at"),
		protocmp.IgnoreFields(&typesv1.ProjectStatus{}, "created_at", "updated_at"),
		protocmp.IgnoreFields(&typesv1.DashboardSpec{}, "perses_json"),
	}

	mkPersesJSON := func() []byte {
		return []byte(`{
			"kind": "Dashboard",
			"metadata": {
				"project": "test-project",
				"name": "test-dashboard"
			},
			"spec": {
				"display": {
					"name": "Test Dashboard"
				}
			}
		}`)
	}

	mkPersesJSONWithName := func(name, displayName string) []byte {
		return []byte(fmt.Sprintf(`{
			"kind": "Dashboard",
			"metadata": {
				"project": "test-project",
				"name": "%s"
			},
			"spec": {
				"display": {
					"name": "%s"
				}
			}
		}`, name, displayName))
	}

	type fileExpectation struct {
		path        string
		shouldExist bool
		contains    []string
	}

	type dashboardExpectation struct {
		projectName string
		id          string
		expected    *typesv1.Dashboard
	}

	type errorExpectation struct {
		shouldFail bool
		contains   string // substring of expected error message
	}

	type fileOperation struct {
		action string // "write", "delete"
		path   string
		data   []byte
	}

	type transition struct {
		name            string
		at              time.Duration
		operation       func(context.Context, *testing.T, localstate.DB, billy.Filesystem) error
		fileOperation   *fileOperation
		expectError     *errorExpectation
		expectFile      *fileExpectation
		expectDashboard *dashboardExpectation
	}

	tests := []struct {
		name        string
		initFS      fsState
		initProject *typesv1.ProjectsConfig_Project
		transitions []transition
	}{
		{
			name: "create managed dashboard writes marker",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			transitions: []transition{
				{
					name: "create dashboard",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateDashboard(ctx, &dashboardv1.CreateDashboardRequest{
							ProjectName: "test-project",
							Spec: &typesv1.DashboardSpec{
								Name:       "Test Dashboard",
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"# managed-by: humanlog"},
					},
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "test-dashboard"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "test-dashboard"),
							"Test Dashboard",
							"",
							false,
							mkPersesJSON(),
							"project1/dashboards/test-dashboard.yaml",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "update managed dashboard preserves marker",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/test-dashboard.yaml": []byte(`# managed-by: humanlog
kind: Dashboard
metadata:
  project: test-project
  name: test-dashboard
spec:
  display:
    name: Original Name
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "update dashboard",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Updated Name",
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"# managed-by: humanlog", "Updated Name"},
					},
				},
			},
		},
		{
			name: "user can edit generated dashboard by setting is_readonly=false",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/generated.yaml": []byte(`# Generated by Terraform
# DO NOT EDIT
kind: Dashboard
metadata:
  project: test-project
  name: generated
spec:
  display:
    name: Generated Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "update with is_readonly=false succeeds",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "generated"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Modified",
								IsReadonly: false,
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/generated.yaml",
						shouldExist: true,
						contains:    []string{"# managed-by: humanlog", "# humanlog.is_readonly: false", "Modified"},
					},
				},
			},
		},
		{
			name: "delete managed dashboard removes file",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/test-dashboard.yaml": []byte(`# managed-by: humanlog
kind: Dashboard
metadata:
  project: test-project
  name: test-dashboard
spec:
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "delete dashboard",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.DeleteDashboard(ctx, &dashboardv1.DeleteDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: false,
					},
				},
			},
		},
		{
			name: "discover managed dashboard has managed origin",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/managed.yaml": []byte(`# managed-by: humanlog
kind: Dashboard
metadata:
  project: test-project
  name: managed
spec:
  display:
    name: Managed Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "list dashboards",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "managed"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "managed"),
							"Managed Dashboard",
							"",
							false,
							nil,
							"project1/dashboards/managed.yaml",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "discover generated dashboard with Generated by marker",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", true),
			),
			initFS: fsState{
				"project1/dashboards/terraform.yaml": []byte(`# Generated by Terraform
kind: Dashboard
metadata:
  project: test-project
  name: terraform
spec:
  display:
    name: Terraform Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "list dashboards",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "terraform"),
						expected: generatedDashboard(
							dashboardID("test-project", "test-project", "terraform"),
							"Terraform Dashboard",
							"",
							true,
							nil,
							"project1/dashboards/terraform.yaml",
							`Contains "# Generated by Terraform"`,
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "discover generated dashboard with DO NOT EDIT marker",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", true),
			),
			initFS: fsState{
				"project1/dashboards/codegen.yaml": []byte(`# DO NOT EDIT
kind: Dashboard
metadata:
  project: test-project
  name: codegen
spec:
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "list dashboards",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "codegen"),
						expected: generatedDashboard(
							dashboardID("test-project", "test-project", "codegen"),
							"codegen",
							"",
							true,
							nil,
							"project1/dashboards/codegen.yaml",
							`Contains "# DO NOT EDIT"`,
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "discover generated dashboard with @generated marker",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", true),
			),
			initFS: fsState{
				"project1/dashboards/auto.yaml": []byte(`# @generated
kind: Dashboard
metadata:
  project: test-project
  name: auto
spec:
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "list dashboards",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "auto"),
						expected: generatedDashboard(
							dashboardID("test-project", "test-project", "auto"),
							"auto",
							"",
							true,
							nil,
							"project1/dashboards/auto.yaml",
							`Contains "# @generated"`,
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "managed dashboard defaults to writable",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/managed.yaml": []byte(`# managed-by: humanlog
kind: Dashboard
metadata:
  project: test-project
  name: managed
spec:
  display:
    name: Managed Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "get dashboard",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "managed"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "managed"),
							"Managed Dashboard",
							"",
							false,
							nil,
							"project1/dashboards/managed.yaml",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "generated dashboard defaults to readonly",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/generated.yaml": []byte(`# Generated by tool
kind: Dashboard
metadata:
  project: test-project
  name: generated
spec:
  display:
    name: Generated Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "get dashboard",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "generated"),
						expected: generatedDashboard(
							dashboardID("test-project", "test-project", "generated"),
							"Generated Dashboard",
							"",
							true,
							nil,
							"project1/dashboards/generated.yaml",
							`Contains "# Generated by tool"`,
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "managed dashboard with explicit is_readonly=true",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/readonly.yaml": []byte(`# managed-by: humanlog
# humanlog.is_readonly: true
kind: Dashboard
metadata:
  project: test-project
  name: readonly
spec:
  display:
    name: Readonly Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "get dashboard",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "readonly"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "readonly"),
							"Readonly Dashboard",
							"",
							true,
							nil,
							"project1/dashboards/readonly.yaml",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "managed dashboard with explicit is_readonly=false",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/writable.yaml": []byte(`# managed-by: humanlog
# humanlog.is_readonly: false
kind: Dashboard
metadata:
  project: test-project
  name: writable
spec:
  display:
    name: Writable Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "get dashboard",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "writable"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "writable"),
							"Writable Dashboard",
							"",
							false,
							nil,
							"project1/dashboards/writable.yaml",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "dashboard with unrelated comments ignored",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/commented.yaml": []byte(`# This is an awesome dashboard
# Created by the team
# Version 1.0
kind: Dashboard
metadata:
  project: test-project
  name: commented
spec:
  display:
    name: Commented Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "get dashboard",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "commented"),
						expected: generatedDashboard(
							dashboardID("test-project", "test-project", "commented"),
							"Commented Dashboard",
							"",
							true,
							nil,
							"project1/dashboards/commented.yaml",
							"No humanlog metadata or generation markers found",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "dashboard with both humanlog and unrelated comments",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/mixed.yaml": []byte(`# This dashboard is maintained by our team
# managed-by: humanlog
# humanlog.is_readonly: false
# Last updated: 2025-01-15
kind: Dashboard
metadata:
  project: test-project
  name: mixed
spec:
  display:
    name: Mixed Comments Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "get dashboard",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "mixed"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "mixed"),
							"Mixed Comments Dashboard",
							"",
							false,
							nil,
							"project1/dashboards/mixed.yaml",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "lifecycle: generated → adopted as writable",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/external.yaml": []byte(`# Generated by Terraform
# DO NOT EDIT
kind: Dashboard
metadata:
  project: test-project
  name: external
spec:
  display:
    name: External Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "initially generated and readonly",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "external"),
						expected: generatedDashboard(
							dashboardID("test-project", "test-project", "external"),
							"External Dashboard",
							"",
							true,
							nil,
							"project1/dashboards/external.yaml",
							`Contains "# Generated by Terraform\n# DO NOT EDIT"`,
							now,
							now,
						),
					},
				},
				{
					name: "user adopts dashboard, becomes managed and writable",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "external"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Adopted Dashboard",
								IsReadonly: false,
								PersesJson: mkPersesJSONWithName("external", "Adopted Dashboard"),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/external.yaml",
						shouldExist: true,
						contains:    []string{"# managed-by: humanlog", "# humanlog.is_readonly: false", "Adopted Dashboard"},
					},
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "external"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "external"),
							"Adopted Dashboard",
							"",
							false,
							nil,
							"project1/dashboards/external.yaml",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "lifecycle: generated → adopted → locked down",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/evolving.yaml": []byte(`# Generated by automation
kind: Dashboard
metadata:
  project: test-project
  name: evolving
spec:
  display:
    name: Evolving Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "initially generated and readonly",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "evolving"),
						expected: generatedDashboard(
							dashboardID("test-project", "test-project", "evolving"),
							"Evolving Dashboard",
							"",
							true,
							nil,
							"project1/dashboards/evolving.yaml",
							`Contains "# Generated by automation"`,
							now,
							now,
						),
					},
				},
				{
					name: "adopt as writable",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "evolving"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Now Managed",
								IsReadonly: false,
								PersesJson: mkPersesJSONWithName("evolving", "Now Managed"),
							},
						})
						return err
					},
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "evolving"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "evolving"),
							"Now Managed",
							"",
							false,
							nil,
							"project1/dashboards/evolving.yaml",
							now,
							now,
						),
					},
				},
				{
					name: "lock down by setting readonly",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "evolving"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Now Locked",
								IsReadonly: true,
								PersesJson: mkPersesJSONWithName("evolving", "Now Locked"),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/evolving.yaml",
						shouldExist: true,
						contains:    []string{"# managed-by: humanlog", "# humanlog.is_readonly: true", "Now Locked"},
					},
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "evolving"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "evolving"),
							"Now Locked",
							"",
							true,
							nil,
							"project1/dashboards/evolving.yaml",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "lifecycle: create readonly → edit fails → unlock → edit succeeds",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			transitions: []transition{
				{
					name: "create dashboard with is_readonly=true",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateDashboard(ctx, &dashboardv1.CreateDashboardRequest{
							ProjectName: "test-project",
							Spec: &typesv1.DashboardSpec{
								Name:       "Locked Template",
								IsReadonly: true,
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"# managed-by: humanlog", "# humanlog.is_readonly: true", "Locked Template"},
					},
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "test-dashboard"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "test-dashboard"),
							"Locked Template",
							"",
							true,
							nil,
							"project1/dashboards/test-dashboard.yaml",
							now,
							now,
						),
					},
				},
				{
					name: "attempt to edit fails",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Should Not Work",
								IsReadonly: true,  // Trying to edit while keeping readonly
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectError: &errorExpectation{
						shouldFail: true,
						contains:   "readonly",
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"Locked Template"}, // unchanged
					},
				},
				{
					name: "unlock via API",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Unlocking",
								IsReadonly: false,
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"# humanlog.is_readonly: false", "Unlocking"},
					},
				},
				{
					name: "edit succeeds after unlock",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Successfully Edited",
								IsReadonly: false,
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"Successfully Edited"},
					},
				},
			},
		},
		{
			name: "lifecycle: generated stays readonly when not adopted",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/protected.yaml": []byte(`# DO NOT EDIT
kind: Dashboard
metadata:
  project: test-project
  name: protected
spec:
  display:
    name: Protected Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "initially readonly",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "protected"),
						expected: generatedDashboard(
							dashboardID("test-project", "test-project", "protected"),
							"Protected Dashboard",
							"",
							true,
							nil,
							"project1/dashboards/protected.yaml",
							`Contains "# DO NOT EDIT"`,
							now,
							now,
						),
					},
				},
				{
					name: "attempt update without adopting fails",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "protected"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Should Fail",
								IsReadonly: true,  // Trying to update while still readonly
								PersesJson: mkPersesJSONWithName("protected", "Should Fail"),
							},
						})
						return err
					},
					expectError: &errorExpectation{
						shouldFail: true,
						contains:   "generated",  // Different error message for generated dashboards
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/protected.yaml",
						shouldExist: true,
						contains:    []string{"Protected Dashboard"}, // unchanged
					},
				},
			},
		},
		{
			name: "lifecycle: writable → lock → cannot edit → unlock → can edit",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/toggle.yaml": []byte(`# managed-by: humanlog
# humanlog.is_readonly: false
kind: Dashboard
metadata:
  project: test-project
  name: toggle
spec:
  display:
    name: Toggle Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "initially writable",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "toggle"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "toggle"),
							"Toggle Dashboard",
							"",
							false,
							nil,
							"project1/dashboards/toggle.yaml",
							now,
							now,
						),
					},
				},
				{
					name: "lock it",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "toggle"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Now Locked",
								IsReadonly: true,
								PersesJson: mkPersesJSONWithName("toggle", "Now Locked"),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/toggle.yaml",
						shouldExist: true,
						contains:    []string{"# humanlog.is_readonly: true", "Now Locked"},
					},
				},
				{
					name: "cannot edit while locked",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "toggle"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Should Fail",
								IsReadonly: true,  // Explicitly keeping it readonly
								PersesJson: mkPersesJSONWithName("toggle", "Should Fail"),
							},
						})
						return err
					},
					expectError: &errorExpectation{
						shouldFail: true,
						contains:   "readonly",
					},
				},
				{
					name: "unlock via API",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "toggle"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Unlocking",
								IsReadonly: false,
								PersesJson: mkPersesJSONWithName("toggle", "Unlocking"),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/toggle.yaml",
						shouldExist: true,
						contains:    []string{"# humanlog.is_readonly: false", "Unlocking"},
					},
				},
				{
					name: "can edit again after unlock",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "toggle"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Final Edit",
								IsReadonly: false,
								PersesJson: mkPersesJSONWithName("toggle", "Final Edit"),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/toggle.yaml",
						shouldExist: true,
						contains:    []string{"Final Edit"},
					},
				},
			},
		},
		{
			name: "lifecycle: manual YAML edit changes state",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/manual.yaml": []byte(`# managed-by: humanlog
# humanlog.is_readonly: true
kind: Dashboard
metadata:
  project: test-project
  name: manual
spec:
  display:
    name: Manually Edited
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "initially readonly",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "manual"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "manual"),
							"Manually Edited",
							"",
							true,
							nil,
							"project1/dashboards/manual.yaml",
							now,
							now,
						),
					},
				},
				{
					name: "user manually changes is_readonly to false in YAML",
					fileOperation: &fileOperation{
						action: "write",
						path:   "project1/dashboards/manual.yaml",
						data: []byte(`# managed-by: humanlog
# humanlog.is_readonly: false
kind: Dashboard
metadata:
  project: test-project
  name: manual
spec:
  display:
    name: Manually Edited
  panels: {}
  layouts: []
  duration: 0s
`),
					},
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "manual"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "manual"),
							"Manually Edited",
							"",
							false,
							nil,
							"project1/dashboards/manual.yaml",
							now,
							now,
						),
					},
				},
				{
					name: "user can now edit via API",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "manual"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Updated via API",
								IsReadonly: false,
								PersesJson: mkPersesJSONWithName("manual", "Updated via API"),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/manual.yaml",
						shouldExist: true,
						contains:    []string{"Updated via API"},
					},
				},
			},
		},
		{
			name: "lifecycle: create → edit → edit → delete",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			transitions: []transition{
				{
					name: "create dashboard",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateDashboard(ctx, &dashboardv1.CreateDashboardRequest{
							ProjectName: "test-project",
							Spec: &typesv1.DashboardSpec{
								Name:       "Normal Dashboard",
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"Normal Dashboard"},
					},
				},
				{
					name: "first edit",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
							Spec: &typesv1.DashboardSpec{
								Name:       "First Edit",
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"First Edit"},
					},
				},
				{
					name: "second edit",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Second Edit",
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"Second Edit"},
					},
				},
				{
					name: "delete dashboard",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.DeleteDashboard(ctx, &dashboardv1.DeleteDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: false,
					},
				},
			},
		},
		{
			name: "lifecycle: explicit is_readonly overrides defaults",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			transitions: []transition{
				{
					name: "create managed with explicit is_readonly=true",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateDashboard(ctx, &dashboardv1.CreateDashboardRequest{
							ProjectName: "test-project",
							Spec: &typesv1.DashboardSpec{
								Name:       "Explicitly Readonly",
								IsReadonly: true,
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"# humanlog.is_readonly: true"},
					},
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "test-dashboard"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "test-dashboard"),
							"Explicitly Readonly",
							"",
							true,
							nil,
							"project1/dashboards/test-dashboard.yaml",
							now,
							now,
						),
					},
				},
				{
					name: "change to explicit is_readonly=false",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Explicitly Writable",
								IsReadonly: false,
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"# humanlog.is_readonly: false"},
					},
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "test-dashboard"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "test-dashboard"),
							"Explicitly Writable",
							"",
							false,
							nil,
							"project1/dashboards/test-dashboard.yaml",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "lifecycle: deletion and recreation with same name",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/reusable.yaml": []byte(`# managed-by: humanlog
kind: Dashboard
metadata:
  project: test-project
  name: reusable
spec:
  display:
    name: Original Dashboard
  panels: {}
  layouts: []
  duration: 0s
`),
			},
			transitions: []transition{
				{
					name: "verify original exists",
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "reusable"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "reusable"),
							"Original Dashboard",
							"",
							false,
							nil,
							"project1/dashboards/reusable.yaml",
							now,
							now,
						),
					},
				},
				{
					name: "delete dashboard",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.DeleteDashboard(ctx, &dashboardv1.DeleteDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "reusable"),
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/reusable.yaml",
						shouldExist: false,
					},
				},
				{
					name: "recreate with same name",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateDashboard(ctx, &dashboardv1.CreateDashboardRequest{
							ProjectName: "test-project",
							Spec: &typesv1.DashboardSpec{
								Name:       "New Dashboard",
								PersesJson: []byte(`{
									"kind": "Dashboard",
									"metadata": {
										"project": "test-project",
										"name": "reusable"
									},
									"spec": {
										"display": {
											"name": "New Dashboard"
										}
									}
								}`),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/reusable.yaml",
						shouldExist: true,
						contains:    []string{"New Dashboard"},
					},
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "reusable"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "reusable"),
							"New Dashboard",
							"",
							false,
							nil,
							"project1/dashboards/reusable.yaml",
							now,
							now,
						),
					},
				},
			},
		},
		{
			name: "lifecycle: preserve is_readonly through multiple edits",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			transitions: []transition{
				{
					name: "create as readonly",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.CreateDashboard(ctx, &dashboardv1.CreateDashboardRequest{
							ProjectName: "test-project",
							Spec: &typesv1.DashboardSpec{
								Name:       "Locked Dashboard",
								IsReadonly: true,
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectDashboard: &dashboardExpectation{
						projectName: "test-project",
						id:          dashboardID("test-project", "test-project", "test-dashboard"),
						expected: managedDashboard(
							dashboardID("test-project", "test-project", "test-dashboard"),
							"Locked Dashboard",
							"",
							true,
							nil,
							"project1/dashboards/test-dashboard.yaml",
							now,
							now,
						),
					},
				},
				{
					name: "unlock and update in one request",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Updated Content",
								IsReadonly: false,
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"# humanlog.is_readonly: false", "Updated Content"},
					},
				},
				{
					name: "lock it again",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						_, err := db.UpdateDashboard(ctx, &dashboardv1.UpdateDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "test-dashboard"),
							Spec: &typesv1.DashboardSpec{
								Name:       "Final Content",
								IsReadonly: true,
								PersesJson: mkPersesJSON(),
							},
						})
						return err
					},
					expectFile: &fileExpectation{
						path:        "project1/dashboards/test-dashboard.yaml",
						shouldExist: true,
						contains:    []string{"# humanlog.is_readonly: true", "Final Content"},
					},
				},
			},
		},
		{
			name: "persesJson is always JSON regardless of file format",
			initProject: projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
			initFS: fsState{
				"project1/dashboards/yaml-dashboard.yaml": []byte(`# managed-by: humanlog
kind: Dashboard
metadata:
  name: yaml-dashboard
  project: test-project
  createdAt: 0001-01-01T00:00:00Z
  updatedAt: 0001-01-01T00:00:00Z
  version: 0
spec:
  display:
    name: YAML Dashboard
    description: This dashboard is stored as YAML
  datasources:
    prometheus:
      default: true
      plugin:
        kind: PrometheusDatasource
        spec:
          directUrl: http://localhost:9090
  panels: {}
  layouts:
    - kind: Grid
      spec:
        display:
          title: Test Section
          collapse:
            open: true
        items: []
  duration: 1h
  refreshInterval: 30s
`),
				"project1/dashboards/json-dashboard.json": []byte(`{
  "kind": "Dashboard",
  "metadata": {
    "name": "json-dashboard",
    "project": "test-project",
    "createdAt": "0001-01-01T00:00:00Z",
    "updatedAt": "0001-01-01T00:00:00Z",
    "version": 0
  },
  "spec": {
    "display": {
      "name": "JSON Dashboard",
      "description": "This dashboard is stored as JSON"
    },
    "datasources": {
      "prometheus": {
        "default": true,
        "plugin": {
          "kind": "PrometheusDatasource",
          "spec": {
            "directUrl": "http://localhost:9090"
          }
        }
      }
    },
    "panels": {},
    "layouts": [
      {
        "kind": "Grid",
        "spec": {
          "display": {
            "title": "Test Section",
            "collapse": {
              "open": true
            }
          },
          "items": []
        }
      }
    ],
    "duration": "1h",
    "refreshInterval": "30s"
  }
}`),
			},
			transitions: []transition{
				{
					name: "YAML file persesJson field contains valid JSON",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						resp, err := db.GetDashboard(ctx, &dashboardv1.GetDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "yaml-dashboard"),
						})
						require.NoError(t, err)
						require.NotNil(t, resp.Dashboard)

						// Verify persesJson is valid JSON (not YAML)
						persesJSON := resp.Dashboard.Spec.PersesJson
						require.NotEmpty(t, persesJSON, "persesJson should not be empty")

						var persesDash persesv1.Dashboard
						err = persesDash.UnmarshalJSON(persesJSON)
						require.NoError(t, err, "persesJson must be valid JSON even when source file is YAML")

						// Verify content is correct
						require.Equal(t, "yaml-dashboard", persesDash.Metadata.Name)
						require.Equal(t, "YAML Dashboard", persesDash.Spec.Display.Name)
						require.Equal(t, "This dashboard is stored as YAML", persesDash.Spec.Display.Description)

						return nil
					},
				},
				{
					name: "JSON file persesJson field contains valid JSON",
					operation: func(ctx context.Context, t *testing.T, db localstate.DB, fs billy.Filesystem) error {
						resp, err := db.GetDashboard(ctx, &dashboardv1.GetDashboardRequest{
							ProjectName: "test-project",
							Id:          dashboardID("test-project", "test-project", "json-dashboard"),
						})
						require.NoError(t, err)
						require.NotNil(t, resp.Dashboard)

						// Verify persesJson is valid JSON
						persesJSON := resp.Dashboard.Spec.PersesJson
						require.NotEmpty(t, persesJSON, "persesJson should not be empty")

						var persesDash persesv1.Dashboard
						err = persesDash.UnmarshalJSON(persesJSON)
						require.NoError(t, err, "persesJson must be valid JSON")

						// Verify content is correct
						require.Equal(t, "json-dashboard", persesDash.Metadata.Name)
						require.Equal(t, "JSON Dashboard", persesDash.Spec.Display.Name)
						require.Equal(t, "This dashboard is stored as JSON", persesDash.Spec.Display.Description)

						return nil
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fs := memfs.New()

			// Initialize filesystem
			if tt.initFS != nil {
				for path, data := range tt.initFS {
					require.NoError(t, writeFile(fs, path, data))
				}
			}

			// Create config
			cfg := &typesv1.ProjectsConfig{
				Projects: []*typesv1.ProjectsConfig_Project{tt.initProject},
			}

			// Create watch
			db := newWatch(ctx, t, cfg, fs, timeNow)

			// Execute transitions
			for _, trans := range tt.transitions {
				t.Run(trans.name, func(t *testing.T) {
					// Advance time if specified
					if trans.at > 0 {
						now = now.Add(trans.at)
						timeNow = func() time.Time { return now }
					}

					// Execute file operation if specified
					if trans.fileOperation != nil {
						switch trans.fileOperation.action {
						case "write":
							err := writeFile(fs, trans.fileOperation.path, trans.fileOperation.data)
							require.NoError(t, err)
						case "delete":
							err := fs.Remove(trans.fileOperation.path)
							require.NoError(t, err)
						default:
							t.Fatalf("unknown file operation: %s", trans.fileOperation.action)
						}
					}

					// Execute API operation if specified
					if trans.operation != nil {
						err := trans.operation(ctx, t, db, fs)

						// Check error expectation
						if trans.expectError != nil && trans.expectError.shouldFail {
							require.Error(t, err)
							if trans.expectError.contains != "" {
								require.Contains(t, err.Error(), trans.expectError.contains)
							}
						} else {
							require.NoError(t, err)
						}
					}

					// Verify file expectations
					if trans.expectFile != nil {
						exists, err := fileExists(fs, trans.expectFile.path)
						require.NoError(t, err)
						require.Equal(t, trans.expectFile.shouldExist, exists)

						if trans.expectFile.shouldExist && len(trans.expectFile.contains) > 0 {
							data, err := readFile(fs, trans.expectFile.path)
							require.NoError(t, err)
							for _, substr := range trans.expectFile.contains {
								require.Contains(t, string(data), substr)
							}
						}
					}

					// Verify dashboard expectations
					if trans.expectDashboard != nil {
						resp, err := db.GetDashboard(ctx, &dashboardv1.GetDashboardRequest{
							ProjectName: trans.expectDashboard.projectName,
							Id:          trans.expectDashboard.id,
						})
						require.NoError(t, err)
						require.NotNil(t, resp, "GetDashboard response should not be nil")
						require.NotNil(t, resp.Dashboard, "Dashboard in response should not be nil")
						require.Empty(t, cmp.Diff(trans.expectDashboard.expected, resp.Dashboard, cmpOpts...))
					}
				})
			}
		})
	}
}

type fsState map[string][]byte

// Helper functions

func managedDashboard(
	id string,
	name, desc string,
	isReadonly bool,
	persesJSON []byte,
	path string,
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
		},
		Status: &typesv1.DashboardStatus{
			CreatedAt: timestamppb.New(createdAt),
			UpdatedAt: timestamppb.New(updatedAt),
			Origin: &typesv1.DashboardStatus_Managed{
				Managed: &typesv1.DashboardStatus_ManagedDashboard{
					Path: path,
				},
			},
		},
	}
}

func generatedDashboard(
	id string,
	name, desc string,
	isReadonly bool,
	persesJSON []byte,
	path string,
	detectionReason string,
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
		},
		Status: &typesv1.DashboardStatus{
			CreatedAt: timestamppb.New(createdAt),
			UpdatedAt: timestamppb.New(updatedAt),
			Origin: &typesv1.DashboardStatus_Generated{
				Generated: &typesv1.DashboardStatus_GeneratedDashboard{
					Path:            path,
					DetectionReason: detectionReason,
				},
			},
		},
	}
}

func newWatch(ctx context.Context, t *testing.T, cfg *typesv1.ProjectsConfig, fs billy.Filesystem, timeNow func() time.Time) localstate.DB {
	alertState := localstate.NewMemory().AlertRuleStatusStorage()
	fullCfg := &config.Config{CurrentConfig: &typesv1.LocalhostConfig{
		Runtime: &typesv1.RuntimeConfig{
			ExperimentalFeatures: &typesv1.RuntimeConfig_ExperimentalFeatures{
				Projects: cfg,
			},
		},
	}}
	db, err := internalWatch(ctx, fs, fullCfg, alertState, parseQuery, timeNow)
	require.NoError(t, err)
	return db
}

func writeFile(fs billy.Filesystem, path string, data []byte) error {
	// Create parent directories
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		parts := strings.Split(dir, string(filepath.Separator))
		current := ""
		for _, part := range parts {
			if part == "" {
				continue
			}
			if current == "" {
				current = part
			} else {
				current = filepath.Join(current, part)
			}
			_ = fs.MkdirAll(current, 0755)
		}
	}

	f, err := fs.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func readFile(fs billy.Filesystem, path string) ([]byte, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	buf := make([]byte, stat.Size())
	_, err = f.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func fileExists(fs billy.Filesystem, path string) (bool, error) {
	_, err := fs.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func TestDashboardIsolationBetweenProjects(t *testing.T) {
	now := time.Date(2025, 10, 21, 10, 56, 42, 123456, time.UTC)
	timeNow := func() time.Time { return now }

	tests := []struct {
		name           string
		initProjects   []*typesv1.ProjectsConfig_Project
		createInProject string
		dashboardName  string
		dashboardSlug  string
		verifyProjects map[string]int // project name -> expected dashboard count
	}{
		{
			name: "dashboard created in one project does not leak to others",
			initProjects: []*typesv1.ProjectsConfig_Project{
				projectConfig("project-a", localProjectPointer("project-a-dir", "dashboards", "alerts", false)),
				projectConfig("project-b", localProjectPointer("project-b-dir", "dashboards", "alerts", false)),
			},
			createInProject: "project-a",
			dashboardName:   "Project A Dashboard",
			dashboardSlug:   "dashboard-a",
			verifyProjects: map[string]int{
				"project-a": 1,
				"project-b": 0,
			},
		},
		{
			name: "dashboard created in second project does not leak to first",
			initProjects: []*typesv1.ProjectsConfig_Project{
				projectConfig("project-x", localProjectPointer("project-x-dir", "dashboards", "alerts", false)),
				projectConfig("project-y", localProjectPointer("project-y-dir", "dashboards", "alerts", false)),
			},
			createInProject: "project-y",
			dashboardName:   "Project Y Dashboard",
			dashboardSlug:   "dashboard-y",
			verifyProjects: map[string]int{
				"project-x": 0,
				"project-y": 1,
			},
		},
		{
			name: "dashboard isolation with three projects",
			initProjects: []*typesv1.ProjectsConfig_Project{
				projectConfig("alpha", localProjectPointer("alpha-dir", "dashboards", "alerts", false)),
				projectConfig("beta", localProjectPointer("beta-dir", "dashboards", "alerts", false)),
				projectConfig("gamma", localProjectPointer("gamma-dir", "dashboards", "alerts", false)),
			},
			createInProject: "beta",
			dashboardName:   "Beta Dashboard",
			dashboardSlug:   "beta-dash",
			verifyProjects: map[string]int{
				"alpha": 0,
				"beta":  1,
				"gamma": 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fs := memfs.New()

			// Create project directories
			for _, proj := range tt.initProjects {
				ptr := proj.Pointer.GetLocalhost()
				require.NoError(t, fs.MkdirAll(filepath.Join(ptr.Path, ptr.DashboardDir), 0755))
				require.NoError(t, fs.MkdirAll(filepath.Join(ptr.Path, ptr.AlertDir), 0755))
			}

			cfg := &typesv1.ProjectsConfig{
				Projects: tt.initProjects,
			}

			db := newWatch(ctx, t, cfg, fs, timeNow)

			// Create a dashboard in the specified project
			_, err := db.CreateDashboard(ctx, &dashboardv1.CreateDashboardRequest{
				ProjectName: tt.createInProject,
				Spec: &typesv1.DashboardSpec{
					Name: tt.dashboardName,
					PersesJson: []byte(fmt.Sprintf(`{
						"kind": "Dashboard",
						"metadata": {
							"project": "%s",
							"name": "%s"
						},
						"spec": {
							"display": {
								"name": "%s"
							},
							"panels": {},
							"layouts": [],
							"duration": "0s"
						}
					}`, tt.createInProject, tt.dashboardSlug, tt.dashboardName)),
				},
			})
			require.NoError(t, err)

			// Verify dashboard counts in all projects
			for projectName, expectedCount := range tt.verifyProjects {
				resp, err := db.GetProject(ctx, &projectv1.GetProjectRequest{Name: projectName})
				require.NoError(t, err)
				require.Len(t, resp.Dashboards, expectedCount,
					"project %q should have exactly %d dashboards", projectName, expectedCount)

				if expectedCount == 1 {
					require.Equal(t, tt.dashboardName, resp.Dashboards[0].Spec.Name)
				}
			}

			// Verify file was only created in the correct project directory
			for _, proj := range tt.initProjects {
				ptr := proj.Pointer.GetLocalhost()
				dashPath := filepath.Join(ptr.Path, ptr.DashboardDir, tt.dashboardSlug+".yaml")
				exists, err := fileExists(fs, dashPath)
				require.NoError(t, err)

				if proj.Name == tt.createInProject {
					require.True(t, exists, "dashboard file should exist in %s", proj.Name)
				} else {
					require.False(t, exists, "dashboard file should NOT exist in %s", proj.Name)
				}
			}
		})
	}
}

func TestProjectDirectoryConflictWarnings(t *testing.T) {
	now := time.Date(2025, 10, 21, 10, 56, 42, 123456, time.UTC)
	timeNow := func() time.Time { return now }

	type operation string
	const (
		opGet      operation = "GetProject"
		opCreate   operation = "CreateProject"
		opValidate operation = "ValidateProject"
		opUpdate   operation = "UpdateProject"
		opList     operation = "ListProject"
	)

	tests := []struct {
		name            string
		initProjects    []*typesv1.ProjectsConfig_Project
		checkProject    string
		expectWarnings  int
		warningContains []string
	}{
		{
			name: "no warnings when projects have separate directories",
			initProjects: []*typesv1.ProjectsConfig_Project{
				projectConfig("project-a", localProjectPointer("/project-a-dir", "dashboards", "alerts", false)),
				projectConfig("project-b", localProjectPointer("/project-b-dir", "dashboards", "alerts", false)),
			},
			checkProject:   "project-a",
			expectWarnings: 0,
		},
		{
			name: "warning when projects share dashboard directory",
			initProjects: []*typesv1.ProjectsConfig_Project{
				projectConfig("project-a", localProjectPointer("/shared", "dashboards", "alerts-a", false)),
				projectConfig("project-b", localProjectPointer("/shared", "dashboards", "alerts-b", false)),
			},
			checkProject:    "project-a",
			expectWarnings:  1,
			warningContains: []string{sharedDashboardDirWarning("project-b", filepath.Join("/shared", "dashboards"))},
		},
		{
			name: "warning when projects share alert directory",
			initProjects: []*typesv1.ProjectsConfig_Project{
				projectConfig("project-x", localProjectPointer("/shared", "dashboards-x", "alerts", false)),
				projectConfig("project-y", localProjectPointer("/shared", "dashboards-y", "alerts", false)),
			},
			checkProject:    "project-x",
			expectWarnings:  1,
			warningContains: []string{sharedAlertDirWarning("project-y", filepath.Join("/shared", "alerts"))},
		},
		{
			name: "multiple warnings when projects share both directories",
			initProjects: []*typesv1.ProjectsConfig_Project{
				projectConfig("proj-1", localProjectPointer("/shared", "dashboards", "alerts", false)),
				projectConfig("proj-2", localProjectPointer("/shared", "dashboards", "alerts", false)),
			},
			checkProject:   "proj-1",
			expectWarnings: 2,
			warningContains: []string{
				sharedDashboardDirWarning("proj-2", filepath.Join("/shared", "dashboards")),
				sharedAlertDirWarning("proj-2", filepath.Join("/shared", "alerts")),
			},
		},
		{
			name: "warnings for multiple conflicting projects",
			initProjects: []*typesv1.ProjectsConfig_Project{
				projectConfig("main", localProjectPointer("/dir", "dashboards", "alerts", false)),
				projectConfig("clone-1", localProjectPointer("/dir", "dashboards", "alerts", false)),
				projectConfig("clone-2", localProjectPointer("/dir", "dashboards", "alerts", false)),
			},
			checkProject:   "main",
			expectWarnings: 4, // 2 warnings per conflicting project (dashboard + alert)
			warningContains: []string{
				sharedDashboardDirWarning("clone-1", filepath.Join("/dir", "dashboards")),
				sharedAlertDirWarning("clone-1", filepath.Join("/dir", "alerts")),
				sharedDashboardDirWarning("clone-2", filepath.Join("/dir", "dashboards")),
				sharedAlertDirWarning("clone-2", filepath.Join("/dir", "alerts")),
			},
		},
	}

	operations := []operation{opGet, opCreate, opValidate, opUpdate, opList}

	for _, tt := range tests {
		for _, op := range operations {
			testName := fmt.Sprintf("%s/%s", tt.name, op)
			t.Run(testName, func(t *testing.T) {
				ctx := context.Background()
				fs := memfs.New()

				// For CreateProject and ValidateProject, we need one less project in the initial config
				initProjects := tt.initProjects
				if op == opCreate || op == opValidate {
					// Find the project we're going to create/validate and exclude it from init
					var filtered []*typesv1.ProjectsConfig_Project
					for _, proj := range tt.initProjects {
						if proj.Name != tt.checkProject {
							filtered = append(filtered, proj)
						}
					}
					initProjects = filtered
				}

				// Create project directories
				for _, proj := range tt.initProjects {
					ptr := proj.Pointer.GetLocalhost()
					require.NoError(t, fs.MkdirAll(filepath.Join(ptr.Path, ptr.DashboardDir), 0755))
					require.NoError(t, fs.MkdirAll(filepath.Join(ptr.Path, ptr.AlertDir), 0755))
				}

				cfg := &typesv1.ProjectsConfig{
					Projects: initProjects,
				}

				db := newWatch(ctx, t, cfg, fs, timeNow)

				var project *typesv1.Project

				switch op {
				case opGet:
					resp, err := db.GetProject(ctx, &projectv1.GetProjectRequest{Name: tt.checkProject})
					require.NoError(t, err)
					require.NotNil(t, resp.Project)
					project = resp.Project

				case opCreate:
					// Find the project spec we're creating
					var projectToCreate *typesv1.ProjectsConfig_Project
					for _, proj := range tt.initProjects {
						if proj.Name == tt.checkProject {
							projectToCreate = proj
							break
						}
					}
					require.NotNil(t, projectToCreate, "test case must include the project being created")

					resp, err := db.CreateProject(ctx, &projectv1.CreateProjectRequest{
						Spec: &typesv1.ProjectSpec{
							Name:    projectToCreate.Name,
							Pointer: projectToCreate.Pointer,
						},
					})
					require.NoError(t, err)
					require.NotNil(t, resp.Project)
					project = resp.Project

				case opValidate:
					// Find the project spec we're validating
					var projectToValidate *typesv1.ProjectsConfig_Project
					for _, proj := range tt.initProjects {
						if proj.Name == tt.checkProject {
							projectToValidate = proj
							break
						}
					}
					require.NotNil(t, projectToValidate, "test case must include the project being validated")

					resp, err := db.ValidateProject(ctx, &projectv1.ValidateProjectRequest{
						Spec: &typesv1.ProjectSpec{
							Name:    projectToValidate.Name,
							Pointer: projectToValidate.Pointer,
						},
					})
					require.NoError(t, err)
					require.NotNil(t, resp.Status)
					// Create a dummy project with the returned status for consistent assertion logic
					project = &typesv1.Project{
						Status: resp.Status,
					}

				case opUpdate:
					// Find the project spec to update
					var projectToUpdate *typesv1.ProjectsConfig_Project
					for _, proj := range tt.initProjects {
						if proj.Name == tt.checkProject {
							projectToUpdate = proj
							break
						}
					}
					require.NotNil(t, projectToUpdate, "test case must include the project being updated")

					// Update the project (keeping same pointer to test warnings are populated)
					resp, err := db.UpdateProject(ctx, &projectv1.UpdateProjectRequest{
						Name: tt.checkProject,
						Spec: &typesv1.ProjectSpec{
							Name:    projectToUpdate.Name,
							Pointer: projectToUpdate.Pointer,
						},
					})
					require.NoError(t, err)
					require.NotNil(t, resp.Project)
					project = resp.Project

				case opList:
					resp, err := db.ListProject(ctx, &projectv1.ListProjectRequest{})
					require.NoError(t, err)
					require.NotEmpty(t, resp.Items)

					// Find the project we're checking
					for _, item := range resp.Items {
						if item.Project.Spec.Name == tt.checkProject {
							project = item.Project
							break
						}
					}
					require.NotNil(t, project, "project %q not found in list", tt.checkProject)
				}

				// Verify warning count
				require.Len(t, project.Status.Warnings, tt.expectWarnings,
					"expected %d warnings but got %d: %v", tt.expectWarnings, len(project.Status.Warnings), project.Status.Warnings)

				// Verify warning content
				if len(tt.warningContains) > 0 {
					allWarnings := strings.Join(project.Status.Warnings, " ")
					for _, expectedSubstr := range tt.warningContains {
						require.Contains(t, allWarnings, expectedSubstr,
							"warning should contain %q", expectedSubstr)
					}
				}
			})
		}
	}
}

func TestDashboardSlugValidation(t *testing.T) {
	ctx := context.Background()
	fs := memfs.New()

	cfg := &typesv1.ProjectsConfig{
		Projects: []*typesv1.ProjectsConfig_Project{
			projectConfig("test-project",
				localProjectPointer("project1", "dashboards", "alerts", false),
			),
		},
	}

	db := newWatch(ctx, t, cfg, fs, func() time.Time { return time.Now() })

	// Test that invalid names with dots are rejected
	_, err := db.CreateDashboard(ctx, &dashboardv1.CreateDashboardRequest{
		ProjectName: "test-project",
		Spec: &typesv1.DashboardSpec{
			Name: "Test Dashboard",
			PersesJson: []byte(`{
				"kind": "Dashboard",
				"metadata": {
					"project": "test-project",
					"name": "my.test.dashboard"
				},
				"spec": {
					"display": {
						"name": "Test Dashboard"
					},
					"panels": {},
					"layouts": [],
					"duration": "0s"
				}
			}`),
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must only contain alphanumeric characters, underscores, and hyphens")

	// Test that valid names work
	_, err = db.CreateDashboard(ctx, &dashboardv1.CreateDashboardRequest{
		ProjectName: "test-project",
		Spec: &typesv1.DashboardSpec{
			Name: "Test Dashboard",
			PersesJson: []byte(`{
				"kind": "Dashboard",
				"metadata": {
					"project": "test-project",
					"name": "my-test-dashboard"
				},
				"spec": {
					"display": {
						"name": "Test Dashboard"
					},
					"panels": {},
					"layouts": [],
					"duration": "0s"
				}
			}`),
		},
	})
	require.NoError(t, err)

	// Verify the file was created
	exists, err := fileExists(fs, "project1/dashboards/my-test-dashboard.yaml")
	require.NoError(t, err)
	require.True(t, exists, "dashboard file should exist")

	// Verify we can retrieve it by the hash-based ID
	expectedID := dashboardID("test-project", "test-project", "my-test-dashboard")
	resp, err := db.GetDashboard(ctx, &dashboardv1.GetDashboardRequest{
		ProjectName: "test-project",
		Id:          expectedID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Dashboard)
	require.Equal(t, expectedID, resp.Dashboard.Meta.Id)
}