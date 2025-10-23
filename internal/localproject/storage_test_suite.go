package localproject

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/require"
)

type storageConstructorFn func(
	t *testing.T,
	files map[string]string,
	dashboardDir, alertDir string,
) (projectStorage, *typesv1.ProjectPointer, func())

func runStorageTestSuite(t *testing.T, constructor storageConstructorFn) {
	t.Run("get_project_hydrated", func(t *testing.T) {
		runGetProjectHydratedTests(t, constructor)
	})
	t.Run("get_dashboard", func(t *testing.T) {
		runGetDashboardTests(t, constructor)
	})
	t.Run("get_alert_group", func(t *testing.T) {
		runGetAlertGroupTests(t, constructor)
	})
	t.Run("get_alert_rule", func(t *testing.T) {
		runGetAlertRuleTests(t, constructor)
	})
}

func runGetProjectHydratedTests(t *testing.T, constructor storageConstructorFn) {
	tests := []struct {
		name               string
		files              map[string]string
		dashboardDir       string
		alertDir           string
		wantDashboards     []suiteExpectedDashboard
		wantAlertGroups    []suiteExpectedAlertGroup
		wantStatusErrCount int
		validate           func(*testing.T, []*typesv1.Dashboard, []*typesv1.AlertGroup)
	}{
		{
			name:               "both_directories_missing",
			files:              map[string]string{"README.md": "# Test"},
			dashboardDir:       "dashboards",
			alertDir:           "alerts",
			wantDashboards:     []suiteExpectedDashboard{},
			wantAlertGroups:    []suiteExpectedAlertGroup{},
			wantStatusErrCount: 2,
		},
		{
			name: "only_dashboard_dir_missing",
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir:       "dashboards",
			alertDir:           "alerts",
			wantDashboards:     []suiteExpectedDashboard{},
			wantAlertGroups:    []suiteExpectedAlertGroup{{name: "group-1"}},
			wantStatusErrCount: 1,
		},
		{
			name: "only_alert_dir_missing",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups:    []suiteExpectedAlertGroup{},
			wantStatusErrCount: 1,
		},
		{
			name: "both_dirs_exist_both_populated",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/g1.yaml":     suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "both_dirs_exist_both_empty",
			files: map[string]string{
				"dashboards/.gitkeep": "",
				"alerts/.gitkeep":     "",
			},
			dashboardDir:       "dashboards",
			alertDir:           "alerts",
			wantDashboards:     []suiteExpectedDashboard{},
			wantAlertGroups:    []suiteExpectedAlertGroup{},
			wantStatusErrCount: 0,
		},
		{
			name: "multiple_dashboards",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": suiteMkDashboardYAML("d-2", "Dashboard 2"),
				"dashboards/d3.yaml": suiteMkDashboardYAML("d-3", "Dashboard 3"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
				{displayName: "Dashboard 2"},
				{displayName: "Dashboard 3"},
			},
			wantAlertGroups:    []suiteExpectedAlertGroup{},
			wantStatusErrCount: 1,
		},
		{
			name: "multiple_alert_groups",
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yaml": suiteMkAlertGroupYAML("group-2", "rule-2"),
				"alerts/g3.yaml": suiteMkAlertGroupYAML("group-3", "rule-3"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			wantDashboards: []suiteExpectedDashboard{},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
				{name: "group-2"},
				{name: "group-3"},
			},
			wantStatusErrCount: 1,
		},
		{
			name: "invalid_yaml_doesnt_crash",
			files: map[string]string{
				"dashboards/bad.yaml":  "this is not valid yaml: [[[",
				"alerts/also-bad.yaml": "invalid: {{{",
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: ""},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: ""},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "partial_results_mix_of_valid_and_invalid",
			files: map[string]string{
				"dashboards/good1.yaml":  suiteMkDashboardYAML("good-1", "Good Dashboard 1"),
				"dashboards/bad.yaml":    "this is not valid yaml: [[[",
				"dashboards/good2.yaml":  suiteMkDashboardYAML("good-2", "Good Dashboard 2"),
				"alerts/good-group.yaml": suiteMkAlertGroupYAML("good-group", "rule-1"),
				"alerts/bad-group.yaml":  "invalid: {{{",
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Good Dashboard 1"},
				{displayName: ""},
				{displayName: "Good Dashboard 2"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
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
			files: map[string]string{
				"dashboards/README.md": "# Docs",
				"dashboards/d1.yaml":   suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/notes.txt":     "Some notes",
				"alerts/g1.yaml":       suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "subdirectories_ignored",
			files: map[string]string{
				"dashboards/subdir/d1.yaml": suiteMkDashboardYAML("d-1", "Nested"),
				"dashboards/d2.yaml":        suiteMkDashboardYAML("d-2", "Dashboard 2"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 2"},
			},
			wantAlertGroups:    []suiteExpectedAlertGroup{},
			wantStatusErrCount: 1,
		},
		{
			name: "yml_extension_supported",
			files: map[string]string{
				"dashboards/d1.yml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/g1.yml":     suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "mixed_yaml_yml_extensions",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yml":  suiteMkDashboardYAML("d-2", "Dashboard 2"),
				"alerts/g1.yaml":     suiteMkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yml":      suiteMkAlertGroupYAML("group-2", "rule-2"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
				{displayName: "Dashboard 2"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
				{name: "group-2"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "uppercase_extension_ignored",
			files: map[string]string{
				"dashboards/d1.YAML": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": suiteMkDashboardYAML("d-2", "Dashboard 2"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 2"},
			},
			wantAlertGroups:    []suiteExpectedAlertGroup{},
			wantStatusErrCount: 1,
		},
		{
			name: "mixed_case_extension_ignored",
			files: map[string]string{
				"dashboards/d1.Yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/g1.YML":      suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir:       "dashboards",
			alertDir:           "alerts",
			wantDashboards:     []suiteExpectedDashboard{},
			wantAlertGroups:    []suiteExpectedAlertGroup{},
			wantStatusErrCount: 0,
		},
		{
			name: "no_extension_yaml_content_ignored",
			files: map[string]string{
				"dashboards/dashboard": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/alertgroup":    suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir:       "dashboards",
			alertDir:           "alerts",
			wantDashboards:     []suiteExpectedDashboard{},
			wantAlertGroups:    []suiteExpectedAlertGroup{},
			wantStatusErrCount: 0,
		},
		{
			name: "backup_extensions_ignored",
			files: map[string]string{
				"dashboards/d1.yaml.bak": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml.tmp": suiteMkDashboardYAML("d-2", "Dashboard 2"),
				"alerts/g1.yaml.old":     suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir:       "dashboards",
			alertDir:           "alerts",
			wantDashboards:     []suiteExpectedDashboard{},
			wantAlertGroups:    []suiteExpectedAlertGroup{},
			wantStatusErrCount: 0,
		},
		{
			name: "spaces_in_filename",
			files: map[string]string{
				"dashboards/my dashboard.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/alert group.yaml":      suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "unicode_in_filename",
			files: map[string]string{
				"dashboards/dashboard-🚀.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/alert-⚡️.yaml":        suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "special_chars_in_filename",
			files: map[string]string{
				"dashboards/dashboard@#$.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/alert-v1.0.yaml":       suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "leading_dot_hidden_files",
			files: map[string]string{
				"dashboards/.hidden-dashboard.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/.hidden-alert.yaml":         suiteMkAlertGroupYAML("group-1", "rule-1"),
				"dashboards/d2.yaml":                suiteMkDashboardYAML("d-2", "Dashboard 2"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
				{displayName: "Dashboard 2"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "trailing_slash_in_paths",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/g1.yaml":     suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards/",
			alertDir:     "alerts/",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "leading_slash_in_paths",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/g1.yaml":     suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "/dashboards",
			alertDir:     "/alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "relative_path_with_dot",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"alerts/g1.yaml":     suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "./dashboards",
			alertDir:     "./alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "same_directory_for_both",
			files: map[string]string{
				"shared/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"shared/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "shared",
			alertDir:     "shared",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: ""},
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: ""},
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
			validate: func(t *testing.T, dashboards []*typesv1.Dashboard, alertGroups []*typesv1.AlertGroup) {
				var corruptDashCount, corruptAlertCount int
				for _, d := range dashboards {
					if d.Spec.Name == "" {
						corruptDashCount++
						require.NotEmpty(t, d.Status.Errors, "corrupt dashboard from alert file should have errors")
					}
				}
				for _, ag := range alertGroups {
					if ag.Spec.Name == "" {
						corruptAlertCount++
						require.NotEmpty(t, ag.Status.Errors, "corrupt alert group from dashboard file should have errors")
					}
				}
				require.Equal(t, 1, corruptDashCount, "should have 1 corrupt dashboard from alert file")
				require.Equal(t, 1, corruptAlertCount, "should have 1 corrupt alert group from dashboard file")
			},
		},
		{
			name: "empty_string_directory_path",
			files: map[string]string{
				"d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "",
			alertDir:     "",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: ""},
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: ""},
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
			validate: func(t *testing.T, dashboards []*typesv1.Dashboard, alertGroups []*typesv1.AlertGroup) {
				var corruptDashCount, corruptAlertCount int
				for _, d := range dashboards {
					if d.Spec.Name == "" {
						corruptDashCount++
						require.NotEmpty(t, d.Status.Errors, "corrupt dashboard from alert file should have errors")
					}
				}
				for _, ag := range alertGroups {
					if ag.Spec.Name == "" {
						corruptAlertCount++
						require.NotEmpty(t, ag.Status.Errors, "corrupt alert group from dashboard file should have errors")
					}
				}
				require.Equal(t, 1, corruptDashCount, "should have 1 corrupt dashboard from alert file")
				require.Equal(t, 1, corruptAlertCount, "should have 1 corrupt alert group from dashboard file")
			},
		},
		{
			name: "dot_as_directory_path",
			files: map[string]string{
				"d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: ".",
			alertDir:     ".",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: ""},
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: ""},
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
			validate: func(t *testing.T, dashboards []*typesv1.Dashboard, alertGroups []*typesv1.AlertGroup) {
				var corruptDashCount, corruptAlertCount int
				for _, d := range dashboards {
					if d.Spec.Name == "" {
						corruptDashCount++
						require.NotEmpty(t, d.Status.Errors, "corrupt dashboard from alert file should have errors")
					}
				}
				for _, ag := range alertGroups {
					if ag.Spec.Name == "" {
						corruptAlertCount++
						require.NotEmpty(t, ag.Status.Errors, "corrupt alert group from dashboard file should have errors")
					}
				}
				require.Equal(t, 1, corruptDashCount, "should have 1 corrupt dashboard from alert file")
				require.Equal(t, 1, corruptAlertCount, "should have 1 corrupt alert group from dashboard file")
			},
		},
		{
			name: "nested_alert_dir_in_dashboard_dir",
			files: map[string]string{
				"dashboards/d1.yaml":        suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "dashboards/alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "group-1"},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "empty_file_zero_bytes",
			files: map[string]string{
				"dashboards/empty.yaml":  "",
				"alerts/also-empty.yaml": "",
				"dashboards/good.yaml":   suiteMkDashboardYAML("d-1", "Dashboard 1"),
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: ""},
				{displayName: "Dashboard 1"},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: ""},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "whitespace_only_file",
			files: map[string]string{
				"dashboards/whitespace.yaml": "   \n\t\n   ",
				"alerts/spaces.yaml":         "     \n     ",
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: ""},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: ""},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "comments_only_file",
			files: map[string]string{
				"dashboards/comments.yaml":  "# This is a comment\n# Another comment\n",
				"alerts/more-comments.yaml": "# Comments only\n",
			},
			dashboardDir: "dashboards",
			alertDir:     "alerts",
			wantDashboards: []suiteExpectedDashboard{
				{displayName: ""},
			},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: ""},
			},
			wantStatusErrCount: 0,
		},
		{
			name: "alert_group_with_many_rules",
			files: map[string]string{
				"alerts/massive-group.yaml": suiteMkAlertGroupYAML("massive", suiteGenerateRuleNames(100)...),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			wantDashboards: []suiteExpectedDashboard{},
			wantAlertGroups: []suiteExpectedAlertGroup{
				{name: "massive"},
			},
			wantStatusErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store, ptr, cleanup := constructor(t, tt.files, tt.dashboardDir, tt.alertDir)
			defer cleanup()

			var gotProject *typesv1.Project
			var gotDashboards []*typesv1.Dashboard
			var gotAlertGroups []*typesv1.AlertGroup

			err := store.getProjectHydrated(ctx, "test-project", ptr,
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

			gotDashboardList := suiteExtractDashboards(gotDashboards)
			require.ElementsMatch(t, tt.wantDashboards, gotDashboardList)

			gotAlertGroupList := suiteExtractAlertGroups(gotAlertGroups)
			require.ElementsMatch(t, tt.wantAlertGroups, gotAlertGroupList)

			if tt.validate != nil {
				tt.validate(t, gotDashboards, gotAlertGroups)
			}
		})
	}
}

type suiteExpectedDashboard struct {
	displayName string
}

type suiteExpectedAlertGroup struct {
	name string
}

func suiteExtractDashboards(dashboards []*typesv1.Dashboard) []suiteExpectedDashboard {
	out := make([]suiteExpectedDashboard, len(dashboards))
	for i, d := range dashboards {
		out[i] = suiteExpectedDashboard{displayName: d.Spec.Name}
	}
	return out
}

func suiteExtractAlertGroups(alertGroups []*typesv1.AlertGroup) []suiteExpectedAlertGroup {
	out := make([]suiteExpectedAlertGroup, len(alertGroups))
	for i, ag := range alertGroups {
		out[i] = suiteExpectedAlertGroup{name: ag.Spec.Name}
	}
	return out
}

func suiteMkDashboardYAML(name, displayName string) string {
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

func suiteMkAlertGroupYAML(groupName string, ruleNames ...string) string {
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

func suiteGenerateRuleNames(count int) []string {
	names := make([]string, count)
	for i := range count {
		names[i] = fmt.Sprintf("rule-%d", i)
	}
	return names
}

func runGetDashboardTests(t *testing.T, constructor storageConstructorFn) {
	tests := []struct {
		name            string
		files           map[string]string
		dashboardDir    string
		alertDir        string
		metadataName    string
		wantDisplayName string
		wantErr         string
		wantErrCode     connect.Code
	}{
		{
			name:            "directory_missing",
			files:           map[string]string{"README.md": "# Test"},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "any-id",
			wantDisplayName: "",
			wantErr:         errInvalid("project %q has no dashboard directory at %q", "test-project", "dashboards").Error(),
			wantErrCode:     connect.CodeInvalidArgument,
		},
		{
			name:            "directory_empty",
			files:           map[string]string{"dashboards/.gitkeep": ""},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "any-id",
			wantDisplayName: "",
			wantErr:         errDashboardNotFound("test-project", dashboardID("test-project", "test-project", "any-id")).Error(),
			wantErrCode:     connect.CodeInvalidArgument,
		},
		{
			name: "dashboard_found",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": suiteMkDashboardYAML("d-2", "Dashboard 2"),
			},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "d-1",
			wantDisplayName: "Dashboard 1",
			wantErr:         "",
		},
		{
			name: "dashboard_not_found_wrong_id",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
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
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
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
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": suiteMkDashboardYAML("d-2", "Dashboard 2"),
				"dashboards/d3.yaml": suiteMkDashboardYAML("d-3", "Dashboard 3"),
			},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "d-1",
			wantDisplayName: "Dashboard 1",
			wantErr:         "",
		},
		{
			name: "find_middle_of_many",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": suiteMkDashboardYAML("d-2", "Dashboard 2"),
				"dashboards/d3.yaml": suiteMkDashboardYAML("d-3", "Dashboard 3"),
			},
			dashboardDir:    "dashboards",
			alertDir:        "alerts",
			metadataName:    "d-2",
			wantDisplayName: "Dashboard 2",
			wantErr:         "",
		},
		{
			name: "find_last_of_many",
			files: map[string]string{
				"dashboards/d1.yaml": suiteMkDashboardYAML("d-1", "Dashboard 1"),
				"dashboards/d2.yaml": suiteMkDashboardYAML("d-2", "Dashboard 2"),
				"dashboards/d3.yaml": suiteMkDashboardYAML("d-3", "Dashboard 3"),
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
			store, ptr, cleanup := constructor(t, tt.files, tt.dashboardDir, tt.alertDir)
			defer cleanup()

			dashID := dashboardID("test-project", "test-project", tt.metadataName)

			var gotDashboard *typesv1.Dashboard
			err := store.getDashboard(ctx, "test-project", ptr, dashID,
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

func runGetAlertGroupTests(t *testing.T, constructor storageConstructorFn) {
	tests := []struct {
		name           string
		files          map[string]string
		dashboardDir   string
		alertDir       string
		groupName      string
		wantAlertGroup *suiteExpectedAlertGroup // nil if expecting error
		wantErr        string
		wantErrCode    connect.Code
	}{
		{
			name:           "directory_missing",
			files:          map[string]string{"README.md": "# Test"},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "any-group",
			wantAlertGroup: nil,
			wantErr:        errInvalid("project %q has no alert directory at %q", "test-project", "alerts").Error(),
			wantErrCode:    connect.CodeInvalidArgument,
		},
		{
			name:           "directory_empty",
			files:          map[string]string{"alerts/.gitkeep": ""},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "any-group",
			wantAlertGroup: nil,
			wantErr:        errAlertGroupNotFound("test-project", "any-group").Error(),
			wantErrCode:    connect.CodeInvalidArgument,
		},
		{
			name: "alert_group_found",
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yaml": suiteMkAlertGroupYAML("group-2", "rule-2"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "group-1",
			wantAlertGroup: &suiteExpectedAlertGroup{name: "group-1"},
			wantErr:        "",
		},
		{
			name: "alert_group_not_found_wrong_name",
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
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
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
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
			files: map[string]string{
				"alerts/multi.yaml": suiteMkAlertGroupYAML("multi-group", "r1", "r2", "r3"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "multi-group",
			wantAlertGroup: &suiteExpectedAlertGroup{name: "multi-group"},
			wantErr:        "",
		},
		{
			name: "find_first_of_many",
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yaml": suiteMkAlertGroupYAML("group-2", "rule-2"),
				"alerts/g3.yaml": suiteMkAlertGroupYAML("group-3", "rule-3"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "group-1",
			wantAlertGroup: &suiteExpectedAlertGroup{name: "group-1"},
			wantErr:        "",
		},
		{
			name: "find_middle_of_many",
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yaml": suiteMkAlertGroupYAML("group-2", "rule-2"),
				"alerts/g3.yaml": suiteMkAlertGroupYAML("group-3", "rule-3"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "group-2",
			wantAlertGroup: &suiteExpectedAlertGroup{name: "group-2"},
			wantErr:        "",
		},
		{
			name: "find_last_of_many",
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
				"alerts/g2.yaml": suiteMkAlertGroupYAML("group-2", "rule-2"),
				"alerts/g3.yaml": suiteMkAlertGroupYAML("group-3", "rule-3"),
			},
			dashboardDir:   "dashboards",
			alertDir:       "alerts",
			groupName:      "group-3",
			wantAlertGroup: &suiteExpectedAlertGroup{name: "group-3"},
			wantErr:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store, ptr, cleanup := constructor(t, tt.files, tt.dashboardDir, tt.alertDir)
			defer cleanup()
			alertState := &suiteMockAlertStorage{}

			var gotAlertGroup *typesv1.AlertGroup
			err := store.getAlertGroup(ctx, alertState, "test-project", ptr, tt.groupName,
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

type suiteMockAlertStorage struct{}

func (m *suiteMockAlertStorage) AlertGetOrCreate(ctx context.Context, projectName, groupName, alertName string, create func() *typesv1.AlertRuleStatus) (*typesv1.AlertRuleStatus, error) {
	return create(), nil
}

func (m *suiteMockAlertStorage) AlertUpdateState(ctx context.Context, stackName, groupName, alertName string, state *typesv1.AlertRuleStatus) error {
	return nil
}

func (m *suiteMockAlertStorage) AlertDeleteStateNotInList(ctx context.Context, stackName, groupName string, keeplist []string) error {
	return nil
}

func runGetAlertRuleTests(t *testing.T, constructor storageConstructorFn) {
	tests := []struct {
		name         string
		files        map[string]string
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
			files:        map[string]string{"README.md": "# Test"},
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
			files:        map[string]string{"alerts/.gitkeep": ""},
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
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
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
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
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
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
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
			files: map[string]string{
				"alerts/multi.yaml": suiteMkAlertGroupYAML("multi-group", "r1", "r2", "r3"),
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
			files: map[string]string{
				"alerts/multi.yaml": suiteMkAlertGroupYAML("multi-group", "r1", "r2", "r3"),
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
			files: map[string]string{
				"alerts/multi.yaml": suiteMkAlertGroupYAML("multi-group", "r1", "r2", "r3"),
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
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
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
			files: map[string]string{
				"alerts/g1.yaml": suiteMkAlertGroupYAML("group-1", "rule-1"),
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
			store, ptr, cleanup := constructor(t, tt.files, tt.dashboardDir, tt.alertDir)
			defer cleanup()
			alertState := &suiteMockAlertStorage{}

			var gotAlertRule *typesv1.AlertRule
			err := store.getAlertRule(ctx, alertState, "test-project", ptr, tt.groupName, tt.ruleName,
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
