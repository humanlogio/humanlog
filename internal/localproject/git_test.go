package localproject

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRemoteGitStorage_GetProjectHydrated(t *testing.T) {
	tests := []struct {
		name               string
		gitFiles           map[string]string
		dashboardDir       string
		alertDir           string
		wantDashboards     []expectedDashboard
		wantAlertGroups    []expectedAlertGroup
		wantStatusErrCount int
		validate           func(*testing.T, []*typesv1.Dashboard, []*typesv1.AlertGroup)
	}{
		{
			name:               "both_directories_missing",
			gitFiles:           map[string]string{"README.md": "# Test"},
			dashboardDir:       "dashboards",
			alertDir:           "alerts",
			wantDashboards:     []expectedDashboard{},
			wantAlertGroups:    []expectedAlertGroup{},
			wantStatusErrCount: 2,
		},
		{
			name: "only_dashboard_dir_missing",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir:       "dashboards",
			alertDir:           "alerts",
			wantDashboards:     []expectedDashboard{},
			wantAlertGroups:    []expectedAlertGroup{{name: "group-1"}},
			wantStatusErrCount: 1,
		},
		{
			name: "only_alert_dir_missing",
			gitFiles: map[string]string{
				"dashboards/d1.yaml": mkDashboardYAML("d-1", "Dashboard 1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []expectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups:    []expectedAlertGroup{},
			wantStatusErrCount: 1,
		},
		{
			name: "both_dirs_exist_both_populated",
			gitFiles: map[string]string{
				"dashboards/d1.yaml": mkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/g1.yaml":     mkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []expectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []expectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "both_dirs_exist_both_empty",
			gitFiles: map[string]string{
				"dashboards/.gitkeep": "",
				"alerts/.gitkeep":     "",
			},
			dashboardDir:       "dashboards",
			alertDir:           "alerts",
			wantDashboards:     []expectedDashboard{},
			wantAlertGroups:    []expectedAlertGroup{},
			wantStatusErrCount: 0,
		},
		{
			name: "multiple_dashboards",
			gitFiles: map[string]string{
				"dashboards/d1.yaml": mkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": mkDashboardYAML("d-2", "Dashboard 2"),
				"dashboards/d3.yaml": mkDashboardYAML("d-3", "Dashboard 3"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []expectedDashboard{
				{displayName: "Dashboard 1"},
				{displayName: "Dashboard 2"},
				{displayName: "Dashboard 3"},
			},
			wantAlertGroups:    []expectedAlertGroup{},
			wantStatusErrCount: 1,
		},
		{
			name: "multiple_alert_groups",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yaml": mkAlertGroupYAML("group-2", "rule-2"),
				"alerts/g3.yaml": mkAlertGroupYAML("group-3", "rule-3"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			wantDashboards: []expectedDashboard{},
			wantAlertGroups: []expectedAlertGroup{
				{name: "group-1"},
				{name: "group-2"},
				{name: "group-3"},
			},
			wantStatusErrCount: 1,
		},
		{
			name: "invalid_yaml_doesnt_crash",
			gitFiles: map[string]string{
				"dashboards/bad.yaml":  "this is not valid yaml: [[[",
				"alerts/also-bad.yaml": "invalid: {{{",
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []expectedDashboard{
				{displayName: ""},
			},
			wantAlertGroups: []expectedAlertGroup{
				{name: ""},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "partial_results_mix_of_valid_and_invalid",
			gitFiles: map[string]string{
				"dashboards/good1.yaml": mkDashboardYAML("good-1", "Good Dashboard 1"),
				"dashboards/bad.yaml":   "this is not valid yaml: [[[",
				"dashboards/good2.yaml": mkDashboardYAML("good-2", "Good Dashboard 2"),
				"alerts/good-group.yaml": mkAlertGroupYAML("good-group", "rule-1"),
				"alerts/bad-group.yaml":  "invalid: {{{",
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []expectedDashboard{
				{displayName: "Good Dashboard 1"},
				{displayName: ""},
				{displayName: "Good Dashboard 2"},
			},
			wantAlertGroups: []expectedAlertGroup{
				{name: "good-group"},
				{name: ""},
			},
			wantStatusErrCount: 0,
			validate: func(t *testing.T, dashboards []*typesv1.Dashboard, alertGroups []*typesv1.AlertGroup) {
				for _, d := range dashboards {
					if d.Spec.Name == "" {
						require.NotEmpty(t, d.Status.Errors, "corrupt dashboard should have errors")
					} else {
						require.Empty(t, d.Status.Errors, "valid dashboard should not have errors")
					}
				}
				for _, ag := range alertGroups {
					if ag.Spec.Name == "" {
						require.NotEmpty(t, ag.Status.Errors, "corrupt alert group should have errors")
					} else {
						require.Empty(t, ag.Status.Errors, "valid alert group should not have errors")
					}
				}
			},
		},
		{
			name: "non_yaml_files_ignored",
			gitFiles: map[string]string{
				"dashboards/README.md": "# Docs",
				"dashboards/d1.yaml":   mkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/notes.txt":     "Some notes",
				"alerts/g1.yaml":       mkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []expectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []expectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "subdirectories_ignored",
			gitFiles: map[string]string{
				"dashboards/subdir/d1.yaml": mkDashboardYAML("d-1", "Nested"),
				"dashboards/d2.yaml":        mkDashboardYAML("d-2", "Dashboard 2"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []expectedDashboard{
				{displayName: "Dashboard 2"},
			},
			wantAlertGroups:    []expectedAlertGroup{},
			wantStatusErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			r, _ := mkGitRepoWithFiles(t, tt.gitFiles)
			store := setupRemoteGitStorage(t, r, tt.dashboardDir, tt.alertDir)

			var gotProject *typesv1.Project
			var gotDashboards []*typesv1.Dashboard
			var gotAlertGroups []*typesv1.AlertGroup

			err := store.getProjectHydrated(ctx, "test-project", mkProjectPointer(tt.dashboardDir, tt.alertDir),
				func(p *typesv1.Project, dashboards []*typesv1.Dashboard, alertGroups []*typesv1.AlertGroup) error {
					gotProject = p
					gotDashboards = dashboards
					gotAlertGroups = alertGroups
					return nil
				})

			require.NoError(t, err)
			require.NotNil(t, gotProject)
			require.Len(t, gotProject.Status.Errors, tt.wantStatusErrCount,
				"project.Status.Errors count, got: %v", gotProject.Status.Errors)

			gotDashboardList := extractDashboards(gotDashboards)
			require.ElementsMatch(t, tt.wantDashboards, gotDashboardList)

			gotAlertGroupList := extractAlertGroups(gotAlertGroups)
			require.ElementsMatch(t, tt.wantAlertGroups, gotAlertGroupList)

			if tt.validate != nil {
				tt.validate(t, gotDashboards, gotAlertGroups)
			}
		})
	}
}

func TestRemoteGitStorage_GetDashboard(t *testing.T) {
	tests := []struct {
		name            string
		gitFiles        map[string]string
		dashboardDir    string
		alertDir        string
		metadataName    string
		wantDisplayName string
		wantErr         string
		wantErrCode     connect.Code
	}{
		{
			name:            "directory_missing",
			gitFiles:        map[string]string{"README.md": "# Test"},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "any-id",
			wantDisplayName: "",
			wantErr:         errInvalid("project %q has no dashboard directory at %q", "test-project", "dashboards").Error(),
			wantErrCode:     connect.CodeInvalidArgument,
		},
		{
			name:            "directory_empty",
			gitFiles:        map[string]string{"dashboards/.gitkeep": ""},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "any-id",
			wantDisplayName: "",
			wantErr:         errDashboardNotFound("test-project", dashboardID("test-project", "test-project", "any-id")).Error(),
			wantErrCode:     connect.CodeInvalidArgument,
		},
		{
			name: "dashboard_found",
			gitFiles: map[string]string{
				"dashboards/d1.yaml": mkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": mkDashboardYAML("d-2", "Dashboard 2"),
			},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "d-1",
			wantDisplayName: "Dashboard 1",
			wantErr:         "",
		},
		{
			name: "dashboard_not_found_wrong_id",
			gitFiles: map[string]string{
				"dashboards/d1.yaml": mkDashboardYAML("d-1", "Dashboard 1"),
			},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "non-existent",
			wantDisplayName: "",
			wantErr:         errDashboardNotFound("test-project", dashboardID("test-project", "test-project", "non-existent")).Error(),
			wantErrCode:     connect.CodeInvalidArgument,
		},
		{
			name: "dashboard_not_found_empty_id",
			gitFiles: map[string]string{
				"dashboards/d1.yaml": mkDashboardYAML("d-1", "Dashboard 1"),
			},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "",
			wantDisplayName: "",
			wantErr:         errDashboardNotFound("test-project", dashboardID("test-project", "test-project", "")).Error(),
			wantErrCode:     connect.CodeInvalidArgument,
		},
		{
			name: "find_first_of_many",
			gitFiles: map[string]string{
				"dashboards/d1.yaml": mkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": mkDashboardYAML("d-2", "Dashboard 2"),
				"dashboards/d3.yaml": mkDashboardYAML("d-3", "Dashboard 3"),
			},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "d-1",
			wantDisplayName: "Dashboard 1",
			wantErr:         "",
		},
		{
			name: "find_middle_of_many",
			gitFiles: map[string]string{
				"dashboards/d1.yaml": mkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": mkDashboardYAML("d-2", "Dashboard 2"),
				"dashboards/d3.yaml": mkDashboardYAML("d-3", "Dashboard 3"),
			},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "d-2",
			wantDisplayName: "Dashboard 2",
			wantErr:         "",
		},
		{
			name: "find_last_of_many",
			gitFiles: map[string]string{
				"dashboards/d1.yaml": mkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": mkDashboardYAML("d-2", "Dashboard 2"),
				"dashboards/d3.yaml": mkDashboardYAML("d-3", "Dashboard 3"),
			},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "d-3",
			wantDisplayName: "Dashboard 3",
			wantErr:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			r, _ := mkGitRepoWithFiles(t, tt.gitFiles)
			store := setupRemoteGitStorage(t, r, tt.dashboardDir, tt.alertDir)

			dashID := dashboardID("test-project", "test-project", tt.metadataName)

			var gotDashboard *typesv1.Dashboard
			err := store.getDashboard(ctx, "test-project", mkProjectPointer(tt.dashboardDir, tt.alertDir), dashID,
				func(d *typesv1.Dashboard) error {
					gotDashboard = d
					return nil
				})

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Equal(t, tt.wantErr, err.Error(), "error message mismatch")
				require.Equal(t, tt.wantErrCode, connect.CodeOf(err), "error code mismatch")
			} else {
				require.NoError(t, err)
				require.NotNil(t, gotDashboard)
				require.Equal(t, dashID, gotDashboard.Meta.Id)
				require.Equal(t, tt.wantDisplayName, gotDashboard.Spec.Name)
			}
		})
	}
}

func TestRemoteGitStorage_GetAlertGroup(t *testing.T) {
	tests := []struct {
		name         string
		gitFiles     map[string]string
		dashboardDir string
		alertDir     string
		groupName    string
		// Expected outputs
		wantAlertGroup *expectedAlertGroup // nil if expecting error
		wantErr        string              // exact error via helper
		wantErrCode    connect.Code
	}{
		{
			name:           "directory_missing",
			gitFiles:       map[string]string{"README.md": "# Test"},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "any-group",
			wantAlertGroup: nil,
			wantErr:        errInvalid("project %q has no alert directory at %q", "test-project", "alerts").Error(),
			wantErrCode:    connect.CodeInvalidArgument,
		},
		{
			name:           "directory_empty",
			gitFiles:       map[string]string{"alerts/.gitkeep": ""},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "any-group",
			wantAlertGroup: nil,
			wantErr:        errAlertGroupNotFound("test-project", "any-group").Error(),
			wantErrCode:    connect.CodeInvalidArgument,
		},
		{
			name: "alert_group_found",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yaml": mkAlertGroupYAML("group-2", "rule-2"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "group-1",
			wantAlertGroup: &expectedAlertGroup{name: "group-1"},
			wantErr:        "",
		},
		{
			name: "alert_group_not_found_wrong_name",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "non-existent",
			wantAlertGroup: nil,
			wantErr:        errAlertGroupNotFound("test-project", "non-existent").Error(),
			wantErrCode:    connect.CodeInvalidArgument,
		},
		{
			name: "alert_group_not_found_empty_name",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "",
			wantAlertGroup: nil,
			wantErr:        errAlertGroupNotFound("test-project", "").Error(),
			wantErrCode:    connect.CodeInvalidArgument,
		},
		{
			name: "alert_group_with_multiple_rules",
			gitFiles: map[string]string{
				"alerts/multi.yaml": mkAlertGroupYAML("multi-group", "r1", "r2", "r3"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "multi-group",
			wantAlertGroup: &expectedAlertGroup{name: "multi-group"},
			wantErr:        "",
		},
		{
			name: "find_first_of_many",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yaml": mkAlertGroupYAML("group-2", "rule-2"),
				"alerts/g3.yaml": mkAlertGroupYAML("group-3", "rule-3"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "group-1",
			wantAlertGroup: &expectedAlertGroup{name: "group-1"},
			wantErr:        "",
		},
		{
			name: "find_middle_of_many",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yaml": mkAlertGroupYAML("group-2", "rule-2"),
				"alerts/g3.yaml": mkAlertGroupYAML("group-3", "rule-3"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "group-2",
			wantAlertGroup: &expectedAlertGroup{name: "group-2"},
			wantErr:        "",
		},
		{
			name: "find_last_of_many",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yaml": mkAlertGroupYAML("group-2", "rule-2"),
				"alerts/g3.yaml": mkAlertGroupYAML("group-3", "rule-3"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "group-3",
			wantAlertGroup: &expectedAlertGroup{name: "group-3"},
			wantErr:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			r, _ := mkGitRepoWithFiles(t, tt.gitFiles)
			store := setupRemoteGitStorage(t, r, tt.dashboardDir, tt.alertDir)
			alertState := &mockAlertStorage{}

			var gotAlertGroup *typesv1.AlertGroup
			err := store.getAlertGroup(ctx, alertState, "test-project", mkProjectPointer(tt.dashboardDir, tt.alertDir), tt.groupName,
				func(ag *typesv1.AlertGroup) error {
					gotAlertGroup = ag
					return nil
				})

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Equal(t, tt.wantErr, err.Error())
				require.Equal(t, tt.wantErrCode, connect.CodeOf(err))
			} else {
				require.NoError(t, err)
				require.NotNil(t, gotAlertGroup)
				require.Equal(t, tt.wantAlertGroup.name, gotAlertGroup.Spec.Name)
			}
		})
	}
}

func TestRemoteGitStorage_GetAlertRule(t *testing.T) {
	tests := []struct {
		name         string
		gitFiles     map[string]string
		dashboardDir string
		alertDir     string
		groupName    string
		ruleName     string
		wantRuleID   string
		wantErr      string
		wantErrCode  connect.Code
	}{
		{
			name:         "directory_missing",
			gitFiles:     map[string]string{"README.md": "# Test"},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			groupName:    "any-group",
			ruleName:     "any-rule",
			wantRuleID:   "",
			wantErr:      errInvalid("project %q has no alert directory at %q", "test-project", "alerts").Error(),
			wantErrCode:  connect.CodeInvalidArgument,
		},
		{
			name:         "directory_empty",
			gitFiles:     map[string]string{"alerts/.gitkeep": ""},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			groupName:    "any-group",
			ruleName:     "any-rule",
			wantRuleID:   "",
			wantErr:      errAlertRuleNotFound("test-project", "any-group", "any-rule").Error(),
			wantErrCode:  connect.CodeInvalidArgument,
		},
		{
			name: "group_not_found",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			groupName:    "non-existent",
			ruleName:     "rule-1",
			wantRuleID:   "",
			wantErr:      errAlertRuleNotFound("test-project", "non-existent", "rule-1").Error(),
			wantErrCode:  connect.CodeInvalidArgument,
		},
		{
			name: "rule_not_found",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			groupName:    "group-1",
			ruleName:     "non-existent",
			wantRuleID:   "",
			wantErr:      errAlertRuleNotFound("test-project", "group-1", "non-existent").Error(),
			wantErrCode:  connect.CodeInvalidArgument,
		},
		{
			name: "rule_found",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			groupName:    "group-1",
			ruleName:     "rule-1",
			wantRuleID:   "rule-1",
			wantErr:      "",
		},
		{
			name: "find_first_rule_in_multi_rule_group",
			gitFiles: map[string]string{
				"alerts/multi.yaml": mkAlertGroupYAML("multi-group", "r1", "r2", "r3"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			groupName:    "multi-group",
			ruleName:     "r1",
			wantRuleID:   "r1",
			wantErr:      "",
		},
		{
			name: "find_middle_rule_in_multi_rule_group",
			gitFiles: map[string]string{
				"alerts/multi.yaml": mkAlertGroupYAML("multi-group", "r1", "r2", "r3"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			groupName:    "multi-group",
			ruleName:     "r2",
			wantRuleID:   "r2",
			wantErr:      "",
		},
		{
			name: "find_last_rule_in_multi_rule_group",
			gitFiles: map[string]string{
				"alerts/multi.yaml": mkAlertGroupYAML("multi-group", "r1", "r2", "r3"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			groupName:    "multi-group",
			ruleName:     "r3",
			wantRuleID:   "r3",
			wantErr:      "",
		},
		{
			name: "empty_group_name",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			groupName:    "",
			ruleName:     "rule-1",
			wantRuleID:   "",
			wantErr:      errAlertRuleNotFound("test-project", "", "rule-1").Error(),
			wantErrCode:  connect.CodeInvalidArgument,
		},
		{
			name: "empty_rule_name",
			gitFiles: map[string]string{
				"alerts/g1.yaml": mkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			groupName:    "group-1",
			ruleName:     "",
			wantRuleID:   "",
			wantErr:      errAlertRuleNotFound("test-project", "group-1", "").Error(),
			wantErrCode:  connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			r, _ := mkGitRepoWithFiles(t, tt.gitFiles)
			store := setupRemoteGitStorage(t, r, tt.dashboardDir, tt.alertDir)
			alertState := &mockAlertStorage{}

			var gotAlertRule *typesv1.AlertRule
			err := store.getAlertRule(ctx, alertState, "test-project", mkProjectPointer(tt.dashboardDir, tt.alertDir), tt.groupName, tt.ruleName,
				func(ar *typesv1.AlertRule) error {
					gotAlertRule = ar
					return nil
				})

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Equal(t, tt.wantErr, err.Error())
				require.Equal(t, tt.wantErrCode, connect.CodeOf(err))
			} else {
				require.NoError(t, err)
				require.NotNil(t, gotAlertRule)
				require.Equal(t, tt.wantRuleID, gotAlertRule.Meta.Id)
			}
		})
	}
}

func mkGitRepoWithFiles(t *testing.T, files map[string]string) (*git.Repository, billy.Filesystem) {
	t.Helper()

	fs := memfs.New()
	stor := memory.NewStorage()

	r, err := git.Init(stor, git.WithWorkTree(fs))
	require.NoError(t, err)

	w, err := r.Worktree()
	require.NoError(t, err)

	for path, content := range files {
		err := writeFile(fs, path, []byte(content))
		require.NoError(t, err)
	}

	for path := range files {
		_, err := w.Add(path)
		require.NoError(t, err)
	}

	_, err = w.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	return r, fs
}

func mkDashboardYAML(name, displayName string) string {
	return fmt.Sprintf(`kind: Dashboard
metadata:
  project: test-project
  name: %s
spec:
  display:
    name: %s
  panels: {}
  layouts: []
  duration: 0s
`, name, displayName)
}

func mkAlertGroupYAML(groupName string, ruleNames ...string) string {
	rules := ""
	for _, ruleName := range ruleNames {
		rules += fmt.Sprintf(`      - alert: %s
        expr: "true"
        for: 1m
        annotations:
          summary: Test alert
`, ruleName)
	}
	return fmt.Sprintf(`groups:
  - name: %s
    rules:
%s`, groupName, rules)
}

func setupRemoteGitStorage(t *testing.T, r *git.Repository, dashboardDir, alertDir string) *remoteGitStorage {
	t.Helper()

	now := time.Date(2025, 10, 22, 10, 0, 0, 0, time.UTC)
	timeNow := func() time.Time { return now }

	store, err := newRemoteGitStorage(nil, memfs.New(), parseQuery, timeNow)
	require.NoError(t, err)

	w, err := r.Worktree()
	require.NoError(t, err)

	ptr := &typesv1.ProjectPointer_RemoteGit{
		RemoteUrl:    "memory://test",
		Ref:          "refs/heads/master",
		DashboardDir: dashboardDir,
		AlertDir:     alertDir,
	}

	// Simulate what syncWithLock does: check for missing directories and add errors to status
	projectStatus := &typesv1.ProjectStatus{
		CreatedAt: timestamppb.New(timeNow()),
		UpdatedAt: timestamppb.New(timeNow()),
	}
	if _, err := w.Filesystem.Stat(ptr.DashboardDir); errors.Is(err, os.ErrNotExist) {
		projectStatus.Errors = append(projectStatus.Errors, projectErrDashboardDirMissing(ptr.DashboardDir))
	}
	if _, err := w.Filesystem.Stat(ptr.AlertDir); errors.Is(err, os.ErrNotExist) {
		projectStatus.Errors = append(projectStatus.Errors, projectErrAlertDirMissing(ptr.AlertDir))
	}

	store.mu.Lock()
	store.repos["test-project"] = &remoteGit{
		project: &typesv1.Project{
			Meta: &typesv1.ProjectMeta{},
			Spec: &typesv1.ProjectSpec{
				Name: "test-project",
				Pointer: &typesv1.ProjectPointer{
					Scheme: &typesv1.ProjectPointer_Remote{
						Remote: ptr,
					},
				},
			},
			Status: projectStatus,
		},
		ptr:     ptr,
		storage: r.Storer,
		r:       r,
		w:       w,
	}
	store.mu.Unlock()

	return store
}

func mkProjectPointer(dashboardDir, alertDir string) *typesv1.ProjectPointer {
	return &typesv1.ProjectPointer{
		Scheme: &typesv1.ProjectPointer_Remote{
			Remote: &typesv1.ProjectPointer_RemoteGit{
				RemoteUrl:    "memory://test",
				Ref:          "refs/heads/master",
				DashboardDir: dashboardDir,
				AlertDir:     alertDir,
			},
		},
	}
}

type mockAlertStorage struct{}

func (m *mockAlertStorage) AlertGetOrCreate(ctx context.Context, projectName, groupName, alertName string, create func() *typesv1.AlertRuleStatus) (*typesv1.AlertRuleStatus, error) {
	return create(), nil
}

func (m *mockAlertStorage) AlertUpdateState(ctx context.Context, projectName, groupName, alertName string, state *typesv1.AlertRuleStatus) error {
	return nil
}

func (m *mockAlertStorage) AlertDeleteStateNotInList(ctx context.Context, projectName, groupName string, keeplist []string) error {
	return nil
}

type expectedDashboard struct {
	displayName string
}

type expectedAlertGroup struct {
	name string
}

func extractDashboards(dashboards []*typesv1.Dashboard) []expectedDashboard {
	result := make([]expectedDashboard, len(dashboards))
	for i, d := range dashboards {
		result[i] = expectedDashboard{
			displayName: d.Spec.Name,
		}
	}
	return result
}

func extractAlertGroups(alertGroups []*typesv1.AlertGroup) []expectedAlertGroup {
	result := make([]expectedAlertGroup, len(alertGroups))
	for i, ag := range alertGroups {
		result[i] = expectedAlertGroup{
			name: ag.Spec.Name,
		}
	}
	return result
}
