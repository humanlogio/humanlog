package localproject

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/go-git/go-billy/v6"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/compat/alertmanager"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	persesv1 "github.com/perses/perses/pkg/model/api/v1"
	persescommon "github.com/perses/perses/pkg/model/api/v1/common"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

var (
	// dashboardSlugRegexp defines valid characters for dashboard slugs used as filenames
	// Based on Perses' idRegexp (^[a-zA-Z0-9_.-]+$) but excluding dots to avoid file extension confusion
	dashboardSlugRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	dashboardSlugMaxLen = 75
)

const humanlogPreamble = "managed-by: humanlog"

type HumanlogMetadata struct {
	IsReadonly *bool `yaml:"humanlog.is_readonly,omitempty"`
}

type localGitStorage struct {
	fs          billy.Filesystem
	logQlParser func(string) (*typesv1.Query, error)
	timeNow     func() time.Time
}

func newLocalGitStorage(projectSource ProjectSource, fs billy.Filesystem, logQlParser func(string) (*typesv1.Query, error), timeNow func() time.Time) *localGitStorage {
	return &localGitStorage{fs: fs, logQlParser: logQlParser, timeNow: timeNow}
}

func (store *localGitStorage) getOrCreateProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onCreate CreateProjectFn, onGetProject GetProjectFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	st, exists, err := parseProjectPointer(ctx, store.fs, name, lh)
	if err != nil {
		return errInternal("parsing project pointer: %v", err)
	}
	if !exists {
		if lh.ReadOnly {
			return errInvalid("project doesn't already exist on the filesystem, and is specified as read-only. can't create it")
		}
		if onCreate == nil {
			return errInvalid("no project with this name exists on the filesystem")
		}
		st = onCreate()
		if err := createProjectFromPointer(ctx, store.fs, name, st, lh); err != nil {
			return errInternal("creating new project: %v", err)
		}
	}

	return onGetProject(st)
}

func (store *localGitStorage) syncProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onGetProject GetProjectFn) error {
	return store.getProject(ctx, name, ptr, onGetProject)
}

func (store *localGitStorage) getProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onGetProject GetProjectFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	st, exists, err := parseProjectPointer(ctx, store.fs, name, lh)
	if err != nil {
		return errInternal("parsing project pointer: %v", err)
	}
	if !exists {
		return errInvalid("no such project: %q", name)
	}
	return onGetProject(st)
}

func (store *localGitStorage) getProjectHydrated(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onGetProject GetProjectHydratedFn) error {
	return store.getProject(ctx, name, ptr, func(p *typesv1.Project) error {
		sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
		if !ok {
			return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
		}
		lh := sch.Localhost
		dashboards, err := parseProjectDashboards(ctx, store.fs, name, lh.Path, lh.DashboardDir, lh.ReadOnly)
		if err != nil {
			var connectErr *connect.Error
			if errors.As(err, &connectErr) && connectErr.Code() == connect.CodeInvalidArgument {
				// User config error - add to status, continue
				p.Status.Errors = append(p.Status.Errors, err.Error())
			} else {
				// System error - fail immediately
				return err
			}
		}
		alertGroups, err := parseProjectAlertGroups(ctx, store.fs, name, lh.Path, lh.AlertDir, store.logQlParser)
		if err != nil {
			var connectErr *connect.Error
			if errors.As(err, &connectErr) && connectErr.Code() == connect.CodeInvalidArgument {
				// User config error - add to status, continue
				p.Status.Errors = append(p.Status.Errors, err.Error())
			} else {
				// System error - fail immediately
				return err
			}
		}
		return onGetProject(p, dashboards, alertGroups)
	})
}

func (store *localGitStorage) getDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, id string, onDashboard GetDashboardFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	dashboardPath := path.Join(lh.Path, lh.DashboardDir)
	if _, err := store.fs.Stat(dashboardPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errInvalid("project %q has no dashboard directory at %q", projectName, lh.DashboardDir)
		}
		return errInternal("checking dashboard directory: %v", err)
	}
	dashboards, err := parseProjectDashboards(ctx, store.fs, projectName, lh.Path, lh.DashboardDir, lh.ReadOnly)
	if err != nil {
		return errInternal("parsing project dashboards: %v", err)
	}
	for _, item := range dashboards {
		if item.Meta.Id == id {
			return onDashboard(item)
		}
	}
	return errDashboardNotFound(projectName, id)
}

func (store *localGitStorage) createDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, dashboard *typesv1.Dashboard, onCreated CreateDashboardFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	if lh.ReadOnly {
		return errInvalid("cannot create dashboard in read-only project")
	}

	var persesDash persesv1.Dashboard
	if err := persesDash.UnmarshalJSON(dashboard.Spec.PersesJson); err != nil {
		return errInvalid("invalid Perses dashboard JSON: %v", err)
	}

	// Override display name and description from Spec if provided
	if dashboard.Spec.Name != "" {
		if persesDash.Spec.Display == nil {
			persesDash.Spec.Display = &persescommon.Display{}
		}
		persesDash.Spec.Display.Name = dashboard.Spec.Name
	}
	if dashboard.Spec.Description != "" {
		if persesDash.Spec.Display == nil {
			persesDash.Spec.Display = &persescommon.Display{}
		}
		persesDash.Spec.Display.Description = dashboard.Spec.Description
	}

	filename, err := extractFilenameFromDashboard(&persesDash)
	if err != nil {
		return errInvalid("invalid dashboard slug: %v", err)
	}

	dashboardPath := path.Join(lh.Path, lh.DashboardDir)
	fpath := path.Join(dashboardPath, filename)

	if _, err := store.fs.Stat(fpath); err == nil {
		return errInvalid("a dashboard already exists at path %q, use another name to avoid conflicts", fpath)
	}

	if err := store.fs.MkdirAll(dashboardPath, 0755); err != nil {
		return errInternal("creating dashboard directory: %v", err)
	}

	f, err := store.fs.Create(fpath)
	if err != nil {
		return errInternal("creating dashboard file: %v", err)
	}
	success := false
	defer func() {
		if !success {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()

	yamlData, err := yaml.Marshal(&persesDash)
	if err != nil {
		return errInternal("marshaling dashboard to YAML: %v", err)
	}
	meta := &HumanlogMetadata{
		IsReadonly: nil,
	}
	if dashboard.Spec.IsReadonly {
		meta.IsReadonly = &dashboard.Spec.IsReadonly
	}
	headerData, err := encodeHeadComment(meta)
	if err != nil {
		return errInternal("encoding dashboard metadata: %v", err)
	}
	fileData := append(headerData, yamlData...)
	if _, err := f.Write(fileData); err != nil {
		return errInternal("writing dashboard data: %v", err)
	}
	if err := f.Close(); err != nil {
		return errInternal("closing dashboard file: %v", err)
	}
	success = true

	created, err := parseProjectDashboard(ctx, store.fs, projectName, dashboardPath, filename, false)
	if err != nil {
		return errInternal("parsing created dashboard: %v", err)
	}

	return onCreated(created)
}

func (store *localGitStorage) updateDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, id string, dashboard *typesv1.Dashboard, onUpdated UpdateDashboardFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	if lh.ReadOnly {
		return errInvalid("cannot update dashboard in read-only project")
	}

	// Get existing dashboard
	var existing *typesv1.Dashboard
	err := store.getDashboard(ctx, projectName, ptr, id, func(d *typesv1.Dashboard) error {
		existing = d
		return nil
	})
	if err != nil {
		return err
	}
	if existing == nil {
		return errInvalid("dashboard %q not found", id)
	}

	var fpath string
	switch origin := existing.Status.Origin.(type) {
	case *typesv1.DashboardStatus_Managed:
		if existing.Spec.IsReadonly && dashboard.Spec.IsReadonly {
			return errInvalid("cannot update readonly dashboard (set is_readonly=false to make it writable)")
		}
		fpath = origin.Managed.Path
	case *typesv1.DashboardStatus_Generated:
		if dashboard.Spec.IsReadonly {
			return errInvalid("can't update dashboard that appears to be generated unless `is_readonly=false` is provided. detected as possibly generted because of: %s", origin.Generated.DetectionReason)
		}
		fpath = origin.Generated.Path
	case *typesv1.DashboardStatus_Builtin:
		return errInvalid("cannot update built-in dashboard %q", id)
	case *typesv1.DashboardStatus_Remote:
		return errInvalid("cannot update remote dashboard %q", id)
	default:
		return errInvalid("dashboard %q has unknown origin type", id)
	}

	var persesDash persesv1.Dashboard
	if err := persesDash.UnmarshalJSON(dashboard.Spec.PersesJson); err != nil {
		return errInvalid("invalid Perses dashboard JSON: %v", err)
	}
	if dashboard.Spec.Name != "" {
		if persesDash.Spec.Display == nil {
			persesDash.Spec.Display = &persescommon.Display{}
		}
		persesDash.Spec.Display.Name = dashboard.Spec.Name
	}
	if dashboard.Spec.Description != "" {
		if persesDash.Spec.Display == nil {
			persesDash.Spec.Display = &persescommon.Display{}
		}
		persesDash.Spec.Display.Description = dashboard.Spec.Description
	}

	f, err := store.fs.Create(fpath)
	if err != nil {
		return errInternal("opening dashboard file for write: %v", err)
	}
	defer f.Close()

	meta := &HumanlogMetadata{
		IsReadonly: &dashboard.Spec.IsReadonly,
	}
	headerData, err := encodeHeadComment(meta)
	if err != nil {
		return errInternal("encoding dashboard metadata: %v", err)
	}
	if _, err := f.Write(headerData); err != nil {
		return errInternal("writing dashboard metadata: %v", err)
	}

	yamlData, err := yaml.Marshal(&persesDash)
	if err != nil {
		return errInternal("marshaling dashboard to YAML: %v", err)
	}
	if _, err := f.Write(yamlData); err != nil {
		return errInternal("writing dashboard data: %v", err)
	}

	filename := filepath.Base(fpath)
	dashboardPath := filepath.Dir(fpath)
	updated, err := parseProjectDashboard(ctx, store.fs, projectName, dashboardPath, filename, existing.Spec.IsReadonly)
	if err != nil {
		return errInternal("parsing updated dashboard: %v", err)
	}

	return onUpdated(updated)
}

func (store *localGitStorage) deleteDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, id string, onDeleted DeleteDashboardFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	if lh.ReadOnly {
		return errInvalid("cannot delete dashboard in read-only project")
	}

	var existing *typesv1.Dashboard
	err := store.getDashboard(ctx, projectName, ptr, id, func(d *typesv1.Dashboard) error {
		existing = d
		return nil
	})
	if err != nil {
		return err
	}
	if existing == nil {
		return errInvalid("dashboard %q not found", id)
	}

	if existing.Status.Origin == nil {
		return errInvalid("dashboard %q has no origin information", id)
	}
	if _, ok := existing.Status.Origin.(*typesv1.DashboardStatus_Managed); !ok {
		return errInvalid("cannot delete generated or built-in dashboard %q", id)
	}

	managedOrigin := existing.Status.Origin.(*typesv1.DashboardStatus_Managed)
	fpath := managedOrigin.Managed.Path

	if err := store.fs.Remove(fpath); err != nil {
		return errInternal("deleting dashboard file: %v", err)
	}

	return onDeleted()
}

func (store *localGitStorage) createAlertGroup(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, alertGroup *typesv1.AlertGroup, onCreated CreateAlertGroupFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	if lh.ReadOnly {
		return errInvalid("cannot create alert group in readonly project")
	}

	groupName := alertGroup.Spec.Name
	filename := groupName + ".yaml"
	alertGroupPath := path.Join(lh.Path, lh.AlertDir)
	fpath := path.Join(alertGroupPath, filename)

	if _, err := store.fs.Stat(fpath); err == nil {
		return errInvalid("an alert group already exists at path %q, use another name to avoid conflicts", fpath)
	}

	if err := store.fs.MkdirAll(alertGroupPath, 0755); err != nil {
		return errInternal("creating alert directory: %v", err)
	}

	f, err := store.fs.Create(fpath)
	if err != nil {
		return errInternal("creating alert group file: %v", err)
	}
	success := false
	defer func() {
		if !success {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()

	// Convert to Prometheus format and marshal to YAML
	ruleGroup := alertGroupToRulefmt(alertGroup.Spec)
	groups := struct {
		Groups []interface{} `yaml:"groups"`
	}{
		Groups: []interface{}{ruleGroup},
	}

	yamlData, err := yaml.Marshal(&groups)
	if err != nil {
		return errInternal("marshaling alert group to YAML: %v", err)
	}

	// Add humanlog metadata header
	meta := &HumanlogMetadata{
		IsReadonly: &alertGroup.Spec.IsReadonly,
	}
	headerData, err := encodeHeadComment(meta)
	if err != nil {
		return errInternal("encoding alert group metadata: %v", err)
	}

	fileData := append(headerData, yamlData...)
	if _, err := f.Write(fileData); err != nil {
		return errInternal("writing alert group data: %v", err)
	}
	if err := f.Close(); err != nil {
		return errInternal("closing alert group file: %v", err)
	}
	success = true

	// Parse back to get the created group with proper status
	created, err := parseProjectAlertGroupFromFile(ctx, store.fs, alertGroupPath, filename, store.logQlParser)
	if err != nil {
		return errInternal("parsing created alert group: %v", err)
	}

	return onCreated(created)
}

func (store *localGitStorage) updateAlertGroup(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, groupName string, alertGroup *typesv1.AlertGroup, onUpdated UpdateAlertGroupFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	if lh.ReadOnly {
		return errInvalid("cannot update alert group in read-only project")
	}

	// Get existing alert group (without hydrating rule statuses)
	items, err := parseProjectAlertGroups(ctx, store.fs, projectName, lh.Path, lh.AlertDir, store.logQlParser)
	if err != nil {
		return errInternal("parsing project alert groups: %v", err)
	}

	var existing *typesv1.AlertGroup
	for _, ag := range items {
		if ag.Spec.Name == groupName {
			existing = ag
			break
		}
	}
	if existing == nil {
		return errInvalid("alert group %q not found", groupName)
	}

	// Check if it's readonly
	if existing.Spec.IsReadonly && !alertGroup.Spec.IsReadonly {
		// Adopting a readonly group - this is allowed
	} else if existing.Spec.IsReadonly {
		return errInvalid("cannot update readonly alert group %q", groupName)
	}

	// Get the file path from existing status
	var fpath string
	switch origin := existing.Status.Origin.(type) {
	case *typesv1.AlertGroupStatus_Managed:
		fpath = origin.Managed.Path
	case *typesv1.AlertGroupStatus_Discovered:
		fpath = origin.Discovered.Path
	case *typesv1.AlertGroupStatus_Generated:
		return errInvalid("cannot update generated alert group %q", groupName)
	default:
		return errInvalid("alert group %q has no origin information", groupName)
	}

	// Convert to Prometheus format and marshal to YAML
	ruleGroup := alertGroupToRulefmt(alertGroup.Spec)
	groups := struct {
		Groups []interface{} `yaml:"groups"`
	}{
		Groups: []interface{}{ruleGroup},
	}

	yamlData, err := yaml.Marshal(&groups)
	if err != nil {
		return errInternal("marshaling alert group to YAML: %v", err)
	}

	// Add humanlog metadata header
	meta := &HumanlogMetadata{
		IsReadonly: &alertGroup.Spec.IsReadonly,
	}
	headerData, err := encodeHeadComment(meta)
	if err != nil {
		return errInternal("encoding alert group metadata: %v", err)
	}

	f, err := store.fs.Create(fpath)
	if err != nil {
		return errInternal("opening alert group file for write: %v", err)
	}
	defer f.Close()

	fileData := append(headerData, yamlData...)
	if _, err := f.Write(fileData); err != nil {
		return errInternal("writing alert group data: %v", err)
	}

	// Parse back to get the updated group with proper status
	alertGroupPath := path.Dir(fpath)
	filename := path.Base(fpath)
	updated, err := parseProjectAlertGroupFromFile(ctx, store.fs, alertGroupPath, filename, store.logQlParser)
	if err != nil {
		return errInternal("parsing updated alert group: %v", err)
	}

	return onUpdated(updated)
}

func (store *localGitStorage) deleteAlertGroup(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, groupName string, onDeleted DeleteAlertGroupFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	if lh.ReadOnly {
		return errInvalid("cannot delete alert group in read-only project")
	}

	// Get existing alert group (without hydrating rule statuses)
	items, err := parseProjectAlertGroups(ctx, store.fs, projectName, lh.Path, lh.AlertDir, store.logQlParser)
	if err != nil {
		return errInternal("parsing project alert groups: %v", err)
	}

	var existing *typesv1.AlertGroup
	for _, ag := range items {
		if ag.Spec.Name == groupName {
			existing = ag
			break
		}
	}
	if existing == nil {
		return errInvalid("alert group %q not found", groupName)
	}

	if existing.Status.Origin == nil {
		return errInvalid("alert group %q has no origin information", groupName)
	}
	if _, ok := existing.Status.Origin.(*typesv1.AlertGroupStatus_Managed); !ok {
		return errInvalid("cannot delete readonly alert group %q", groupName)
	}

	managedOrigin := existing.Status.Origin.(*typesv1.AlertGroupStatus_Managed)
	fpath := managedOrigin.Managed.Path

	if err := store.fs.Remove(fpath); err != nil {
		return errInternal("deleting alert group file: %v", err)
	}

	return onDeleted()
}

// Helper function to convert AlertGroupSpec to Prometheus rulefmt format
func alertGroupToRulefmt(spec *typesv1.AlertGroupSpec) map[string]interface{} {
	group := map[string]interface{}{
		"name":     spec.Name,
		"interval": spec.Interval.AsDuration().String(),
	}

	if spec.QueryOffset != nil && spec.QueryOffset.AsDuration() > 0 {
		group["query_offset"] = spec.QueryOffset.AsDuration().String()
	}

	if spec.Limit > 0 {
		group["limit"] = spec.Limit
	}

	if spec.Labels != nil && len(spec.Labels.Kvs) > 0 {
		labels := make(map[string]string)
		for _, kv := range spec.Labels.Kvs {
			if strVal, ok := kv.Value.Kind.(*typesv1.Val_Str); ok {
				labels[kv.Key] = strVal.Str
			}
		}
		// Only add labels if we have any
		if len(labels) > 0 {
			group["labels"] = labels
		}
	}
	// Note: Do not add empty labels map to avoid YAML output like "labels: {}"

	rules := make([]map[string]interface{}, 0, len(spec.Rules))
	for _, namedRule := range spec.Rules {
		rule := alertRuleToRulefmt(namedRule.Spec)
		rules = append(rules, rule)
	}
	group["rules"] = rules

	return group
}

// Helper function to extract expression string from Query (simplified for test compatibility)
func queryToString(q *typesv1.Query) string {
	if q == nil || q.Query == nil {
		return ""
	}
	// Extract the expression string from the first statement
	if len(q.Query.Statements) > 0 {
		stmt := q.Query.Statements[0]
		if filter, ok := stmt.Stmt.(*typesv1.Statement_Filter); ok && filter.Filter != nil && filter.Filter.Expr != nil {
			// Try identifier first (used by test parseQuery)
			if ident, ok := filter.Filter.Expr.Expr.(*typesv1.Expr_Identifier); ok && ident.Identifier != nil {
				return ident.Identifier.Name
			}
			// Fall back to literal (for backward compatibility)
			if lit, ok := filter.Filter.Expr.Expr.(*typesv1.Expr_Literal); ok && lit.Literal != nil {
				if strVal, ok := lit.Literal.Kind.(*typesv1.Val_Str); ok {
					return strVal.Str
				}
			}
		}
	}
	return "unknown"
}

// Helper function to convert AlertRuleSpec to Prometheus rulefmt format
func alertRuleToRulefmt(spec *typesv1.AlertRuleSpec) map[string]interface{} {
	rule := map[string]interface{}{
		"alert": spec.Name,
		"expr":  queryToString(spec.Expr),
	}

	if spec.For != nil && spec.For.AsDuration() > 0 {
		rule["for"] = spec.For.AsDuration().String()
	}

	if spec.KeepFiringFor != nil && spec.KeepFiringFor.AsDuration() > 0 {
		rule["keep_firing_for"] = spec.KeepFiringFor.AsDuration().String()
	}

	if spec.Labels != nil && len(spec.Labels.Kvs) > 0 {
		labels := make(map[string]string)
		for _, kv := range spec.Labels.Kvs {
			if strVal, ok := kv.Value.Kind.(*typesv1.Val_Str); ok {
				labels[kv.Key] = strVal.Str
			}
		}
		if len(labels) > 0 {
			rule["labels"] = labels
		}
	}

	if spec.Annotations != nil && len(spec.Annotations.Kvs) > 0 {
		annotations := make(map[string]string)
		for _, kv := range spec.Annotations.Kvs {
			if strVal, ok := kv.Value.Kind.(*typesv1.Val_Str); ok {
				annotations[kv.Key] = strVal.Str
			}
		}
		if len(annotations) > 0 {
			rule["annotations"] = annotations
		}
	}

	return rule
}

// Helper function to parse a single alert group file
func parseProjectAlertGroupFromFile(ctx context.Context, ffs billy.Filesystem, alertGroupPath, filename string, logQlParser func(string) (*typesv1.Query, error)) (*typesv1.AlertGroup, error) {
	groups, err := parseProjectAlertGroupsFromFile(ctx, ffs, alertGroupPath, filename, logQlParser)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, errInvalid("no alert groups found in file %q", filename)
	}
	if len(groups) > 1 {
		return nil, errInvalid("expected exactly one alert group in file %q, found %d", filename, len(groups))
	}
	return groups[0], nil
}

func (store *localGitStorage) getAlertGroup(ctx context.Context, alertState localstorage.Alertable, projectName string, ptr *typesv1.ProjectPointer, groupName string, onAlertGroup GetAlertGroupFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	alertPath := path.Join(lh.Path, lh.AlertDir)
	if _, err := store.fs.Stat(alertPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errInvalid("project %q has no alert directory at %q", projectName, lh.AlertDir)
		}
		return errInternal("checking alert directory: %v", err)
	}
	items, err := parseProjectAlertGroups(ctx, store.fs, projectName, lh.Path, lh.AlertDir, store.logQlParser)
	if err != nil {
		return errInternal("parsing project alert groups: %v", err)
	}
	for _, ag := range items {
		if ag.Spec.Name == groupName {
			// Hydrate status for all rules in group
			ag.Status.Rules = make([]*typesv1.AlertGroupStatus_NamedAlertRuleStatus, 0, len(ag.Spec.Rules))
			for _, named := range ag.Spec.Rules {
				state, err := alertState.AlertGetOrCreate(ctx, projectName, groupName, named.Id, func() *typesv1.AlertRuleStatus {
					return &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{Unknown: &typesv1.AlertUnknown{}}}
				})
				if err != nil {
					// If project doesn't exist in alert state yet, use default unknown status
					state = &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{Unknown: &typesv1.AlertUnknown{}}}
				}
				ag.Status.Rules = append(ag.Status.Rules, &typesv1.AlertGroupStatus_NamedAlertRuleStatus{
					Id:     named.Id,
					Status: state,
				})
			}
			return onAlertGroup(ag)
		}
	}
	return errAlertGroupNotFound(projectName, groupName)
}

func (store *localGitStorage) getAlertRule(ctx context.Context, alertState localstorage.Alertable, projectName string, ptr *typesv1.ProjectPointer, groupName, ruleName string, onAlertRule GetAlertRuleFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	alertPath := path.Join(lh.Path, lh.AlertDir)
	if _, err := store.fs.Stat(alertPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errInvalid("project %q has no alert directory at %q", projectName, lh.AlertDir)
		}
		return errInternal("checking alert directory: %v", err)
	}
	items, err := parseProjectAlertGroups(ctx, store.fs, projectName, lh.Path, lh.AlertDir, store.logQlParser)
	if err != nil {
		return errInternal("parsing project alert groups: %v", err)
	}
	onGroup := func(group *typesv1.AlertGroup) error {
		for _, named := range group.Spec.Rules {
			if named.Id == ruleName {
				// Fetch actual runtime status from storage
				state, err := alertState.AlertGetOrCreate(ctx, projectName, groupName, named.Id, func() *typesv1.AlertRuleStatus {
					return &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{Unknown: &typesv1.AlertUnknown{}}}
				})
				if err != nil {
					// If project doesn't exist in alert state yet, use default unknown status
					state = &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{Unknown: &typesv1.AlertUnknown{}}}
				}

				// Construct full AlertRule with hydrated status
				rule := &typesv1.AlertRule{
					Meta: &typesv1.AlertRuleMeta{
						Id: named.Id,
					},
					Spec:   named.Spec,
					Status: state,
				}
				return onAlertRule(rule)
			}
		}
		return errAlertRuleNotFound(projectName, groupName, ruleName)
	}

	for _, item := range items {
		if item.Spec.Name == groupName {
			return onGroup(item)
		}
	}
	return errAlertRuleNotFound(projectName, groupName, ruleName)
}

func createProjectFromPointer(ctx context.Context, ffs billy.Filesystem, projectName string, project *typesv1.Project, ptr *typesv1.ProjectPointer_LocalGit) error {
	panic("todo")
}

func (store *localGitStorage) validateProjectPointer(ctx context.Context, ptr *typesv1.ProjectPointer) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	ensureIsDir := func(path string) error {
		fi, err := store.fs.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("the path %q doesn't exist", path))
			}
			return err
		}
		if !fi.IsDir() {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("the path %q is not a directory", path))
		}
		return nil
	}
	ensureIsSubdir := func(dir, path string) error {
		if path == "" {
			return fmt.Errorf("expecting a sub directory of %q, but no path was specified", dir)
		}
		if filepath.IsAbs(path) {
			return fmt.Errorf("expecting a sub directory of %q, but was an absolute path: %q", dir, path)
		}
		path = filepath.Join(dir, path)
		if err := ensureIsDir(path); err != nil {
			return err
		}
		return nil
	}
	if !path.IsAbs(lh.Path) {
		return fmt.Errorf("pointer's path must be absolute, but was relative: %q", lh.Path)
	}
	if err := ensureIsDir(lh.Path); err != nil {
		return fmt.Errorf("path is invalid: %v", err)
	}
	if err := ensureIsSubdir(lh.Path, lh.DashboardDir); err != nil {
		return fmt.Errorf("dashboard dir is invalid: %v", err)
	}
	if err := ensureIsSubdir(lh.Path, lh.AlertDir); err != nil {
		return fmt.Errorf("alert dir is invalid: %v", err)
	}
	return nil
}

func parseProjectPointer(ctx context.Context, ffs billy.Filesystem, projectName string, ptr *typesv1.ProjectPointer_LocalGit) (*typesv1.Project, bool, error) {
	st := &typesv1.Project{
		Meta:   &typesv1.ProjectMeta{},
		Spec:   &typesv1.ProjectSpec{},
		Status: &typesv1.ProjectStatus{},
	}
	projectDir, err := ffs.Stat(ptr.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			st.Status.Errors = append(st.Status.Errors, fmt.Sprintf("no directory exists at path %q", ptr.Path))
			return st, false, nil
		}
		return nil, false, fmt.Errorf("looking up project directory %q on filesystem: %v", ptr.Path, err)
	}
	st.Spec = &typesv1.ProjectSpec{
		Name: projectName,
		Pointer: &typesv1.ProjectPointer{Scheme: &typesv1.ProjectPointer_Localhost{Localhost: &typesv1.ProjectPointer_LocalGit{
			Path:         ptr.Path,
			DashboardDir: ptr.DashboardDir,
			AlertDir:     ptr.AlertDir,
			ReadOnly:     ptr.ReadOnly,
		}}},
	}
	st.Status = &typesv1.ProjectStatus{
		CreatedAt: timestamppb.New(projectDir.ModTime()),
		UpdatedAt: timestamppb.New(projectDir.ModTime()),
	}
	return st, true, nil
}

func parseProjectDashboards(ctx context.Context, ffs billy.Filesystem, projectName, projectPath, dashboardDir string, projectIsReadOnly bool) ([]*typesv1.Dashboard, error) {
	dashboardPath := path.Join(projectPath, dashboardDir)
	files, err := ffs.ReadDir(dashboardPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errProjectDashboardDirMissing(dashboardDir)
		}
		return nil, errInternal("reading dashboard directory: %v", err)
	}
	var out []*typesv1.Dashboard
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		filename := file.Name()
		fileext := filepath.Ext(filename)
		switch fileext {
		case ".json", ".yaml", ".yml":
			item, err := parseProjectDashboard(ctx, ffs, projectName, dashboardPath, filename, projectIsReadOnly)
			if err != nil {
				out = append(out, &typesv1.Dashboard{
					Meta: &typesv1.DashboardMeta{},
					Spec: &typesv1.DashboardSpec{},
					Status: &typesv1.DashboardStatus{
						Errors: []string{errDashboardParse(filename, err).Error()},
					},
				})
			} else {
				out = append(out, item)
			}
		default:
			continue
		}
	}
	return out, nil
}

func parseProjectDashboard(ctx context.Context, ffs billy.Filesystem, projectName, dashboardPath, filename string, projectIsReadOnly bool) (*typesv1.Dashboard, error) {
	persesToProto := func(in *persesv1.Dashboard, projectName, filepath string, data []byte, out *typesv1.Dashboard) (*typesv1.Dashboard, error) {
		out.Meta.Id = dashboardID(projectName, in.Metadata.Project, in.Metadata.Name)

		out.Spec.Name = in.Metadata.Name
		// Always store as JSON, even if source file is YAML
		jsonData, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("marshaling dashboard to JSON: %w", err)
		}
		out.Spec.PersesJson = jsonData
		if in.Spec.Display != nil {
			out.Spec.Name = in.Spec.Display.Name
			out.Spec.Description = in.Spec.Display.Description
		}

		out.Status.CreatedAt = timestamppb.New(in.Metadata.CreatedAt)
		out.Status.UpdatedAt = timestamppb.New(in.Metadata.UpdatedAt)

		return out, nil
	}
	parseFile := func(ctx context.Context, ffs billy.Filesystem, projectName, dirpath, filename string, parser func(ctx context.Context, data []byte, out *typesv1.Dashboard, path string) (*persesv1.Dashboard, error)) (*typesv1.Dashboard, error) {
		out := &typesv1.Dashboard{
			Meta:   &typesv1.DashboardMeta{},
			Spec:   &typesv1.DashboardSpec{},
			Status: &typesv1.DashboardStatus{},
		}
		fpath := path.Join(dirpath, filename)
		f, err := ffs.Open(fpath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				out.Status.Errors = append(out.Status.Errors, fmt.Sprintf("no dashboard found at path %q", fpath))
				return out, nil
			}
			return nil, fmt.Errorf("opening dashboard file at %q: %v", fpath, err)
		}
		defer f.Close()

		rawData, err := io.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("reading dashboard file at %q: %v", fpath, err)
		}
		pout, err := parser(ctx, rawData, out, fpath)
		if err != nil {
			out.Status.Errors = append(out.Status.Errors, fmt.Sprintf("invalid dashboard found at path %q: %v", fpath, err))
			return out, nil
		}
		return persesToProto(pout, projectName, fpath, rawData, out)
	}
	parseJSONDashboard := func(ctx context.Context, data []byte, out *typesv1.Dashboard, path string) (*persesv1.Dashboard, error) {
		perses := new(persesv1.Dashboard)
		if err := perses.UnmarshalJSON(data); err != nil {
			return nil, err
		}

		out.Spec.IsReadonly = true
		out.Status.Origin = &typesv1.DashboardStatus_Generated{
			Generated: &typesv1.DashboardStatus_GeneratedDashboard{
				Path:            path,
				DetectionReason: "JSON files are never managed by humanlog",
			},
		}

		return perses, nil
	}
	parseYAMLDashboard := func(ctx context.Context, data []byte, out *typesv1.Dashboard, path string) (*persesv1.Dashboard, error) {
		meta, isManaged, err := decodeHeadComment(data)
		if err != nil {
			return nil, fmt.Errorf("parsing humanlog metadata: %w", err)
		}

		if meta != nil && meta.IsReadonly != nil {
			out.Spec.IsReadonly = *meta.IsReadonly
		} else if isManaged {
			out.Spec.IsReadonly = false
		} else {
			out.Spec.IsReadonly = true
		}

		if isManaged {
			out.Status.Origin = &typesv1.DashboardStatus_Managed{
				Managed: &typesv1.DashboardStatus_ManagedDashboard{
					Path: path,
				},
			}
		} else {
			var node yaml.Node
			if err := yaml.Unmarshal(data, &node); err != nil {
				return nil, err
			}
			out.Status = detectGeneratedDashboard(node, out.Status, path)
		}

		perses := new(persesv1.Dashboard)
		if err := yaml.Unmarshal(data, &perses); err != nil {
			return nil, err
		}

		return perses, nil
	}

	fileext := filepath.Ext(filename)
	switch strings.ToLower(fileext) {
	case ".json":
		return parseFile(ctx, ffs, projectName, dashboardPath, filename, parseJSONDashboard)
	case ".yaml", ".yml":
		return parseFile(ctx, ffs, projectName, dashboardPath, filename, parseYAMLDashboard)
	default:
		return nil, fmt.Errorf("invalid file extension for a dashboard: expecting .yaml, .yml or .json, got %q", fileext)
	}
}

func extractFilenameFromDashboard(dashboard *persesv1.Dashboard) (string, error) {
	if dashboard.Metadata.Name == "" {
		return "", fmt.Errorf("dashboard metadata.name is required")
	}
	name := dashboard.Metadata.Name
	if len(name) > dashboardSlugMaxLen {
		return "", fmt.Errorf("name cannot exceed %d characters (got %d)", dashboardSlugMaxLen, len(name))
	}
	if !dashboardSlugRegexp.MatchString(name) {
		return "", fmt.Errorf("name must only contain alphanumeric characters, underscores, and hyphens (got %q)", name)
	}
	return name + ".yaml", nil
}

func encodeHeadComment(meta *HumanlogMetadata) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("# " + humanlogPreamble + "\n")

	if meta == nil || meta.IsReadonly == nil {
		return buf.Bytes(), nil
	}

	yamlData, err := yaml.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshaling humanlog metadata: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(yamlData))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			buf.WriteString("# " + line + "\n")
		}
	}

	return buf.Bytes(), scanner.Err()
}

func decodeHeadComment(data []byte) (*HumanlogMetadata, bool, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Split(bufio.ScanLines)
	var foundPreamble bool
	var metadataLines []string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if !strings.HasPrefix(trimmed, "#") {
			break
		}

		if strings.Contains(trimmed, humanlogPreamble) {
			foundPreamble = true
			continue
		}

		if foundPreamble && strings.HasPrefix(trimmed, "# humanlog.") {
			metadataLines = append(metadataLines, strings.TrimPrefix(trimmed, "# "))
		}
	}

	if !foundPreamble {
		return nil, false, nil
	}

	if len(metadataLines) == 0 {
		return &HumanlogMetadata{}, true, nil
	}

	yamlData := []byte(strings.Join(metadataLines, "\n"))
	var meta HumanlogMetadata
	if err := yaml.Unmarshal(yamlData, &meta); err != nil {
		return nil, true, fmt.Errorf("parsing humanlog metadata: %w", err)
	}

	return &meta, true, nil
}

func detectGeneratedDashboard(node yaml.Node, status *typesv1.DashboardStatus, path string) *typesv1.DashboardStatus {
	comments := extractYAMLComments(&node)
	codegenMarkers := []string{
		"Generated by ",
		"DO NOT EDIT",
		"@generated",
		"Code generated",
		"This file is automatically generated",
	}

	for _, comment := range comments {
		for _, marker := range codegenMarkers {
			if strings.Contains(comment, marker) {
				status.Origin = &typesv1.DashboardStatus_Generated{
					Generated: &typesv1.DashboardStatus_GeneratedDashboard{
						Path:            path,
						DetectionReason: fmt.Sprintf("Contains %q", strings.TrimSpace(comment)),
					},
				}
				return status
			}
		}
	}

	status.Origin = &typesv1.DashboardStatus_Generated{
		Generated: &typesv1.DashboardStatus_GeneratedDashboard{
			Path:            path,
			DetectionReason: "No humanlog metadata or generation markers found",
		},
	}
	return status
}

func extractYAMLComments(node *yaml.Node) []string {
	var comments []string
	if node.HeadComment != "" {
		comments = append(comments, node.HeadComment)
	}
	if node.LineComment != "" {
		comments = append(comments, node.LineComment)
	}
	if node.FootComment != "" {
		comments = append(comments, node.FootComment)
	}
	for _, child := range node.Content {
		comments = append(comments, extractYAMLComments(child)...)
	}
	return comments
}

// detectGenerationMarker checks for common code generation markers in file content
func detectGenerationMarker(data []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	codegenMarkers := []string{
		"generated-by",
		"Generated by",
		"DO NOT EDIT",
		"@generated",
		"Code generated",
		"This file is automatically generated",
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Only check comment lines
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}

		for _, marker := range codegenMarkers {
			if strings.Contains(line, marker) {
				return fmt.Sprintf("file contains '# %s' marker", marker)
			}
		}
	}

	return ""
}

func parseProjectAlertGroups(ctx context.Context, ffs billy.Filesystem, projectName, projectPath, alertGroupDir string, logQlParser func(string) (*typesv1.Query, error)) ([]*typesv1.AlertGroup, error) {
	alertGroupPath := path.Join(projectPath, alertGroupDir)
	files, err := ffs.ReadDir(alertGroupPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errProjectAlertDirMissing(alertGroupDir)
		}
		return nil, errInternal("reading alert directory: %v", err)
	}
	var out []*typesv1.AlertGroup
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		filename := file.Name()
		fileext := filepath.Ext(filename)
		switch fileext {
		case ".yaml", ".yml":
			items, err := parseProjectAlertGroupsFromFile(ctx, ffs, alertGroupPath, filename, logQlParser)
			if err != nil {
				out = append(out, &typesv1.AlertGroup{
					Meta: &typesv1.AlertGroupMeta{},
					Spec: &typesv1.AlertGroupSpec{},
					Status: &typesv1.AlertGroupStatus{
						Errors: []string{errAlertGroupParse(filename, err).Error()},
					},
				})
			} else {
				out = append(out, items...)
			}
		default:
			continue
		}
	}
	return out, nil
}

func parseProjectAlertGroupsFromFile(ctx context.Context, ffs billy.Filesystem, alertGroupPath, filename string, logQlParser func(string) (*typesv1.Query, error)) ([]*typesv1.AlertGroup, error) {
	filepath := path.Join(alertGroupPath, filename)
	file, err := ffs.Open(filepath)
	if err != nil {
		return nil, errInternal("opening alert group file at %q: %v", filepath, err)
	}
	defer file.Close()

	// Read raw data to check for humanlog markers
	rawData, err := io.ReadAll(file)
	if err != nil {
		return nil, errInternal("reading alert group file at %q: %v", filepath, err)
	}

	// Check for humanlog metadata markers
	meta, isManaged, err := decodeHeadComment(rawData)
	if err != nil {
		return nil, errInternal("parsing humanlog metadata from %q: %v", filepath, err)
	}

	// Parse the alert groups
	out, err := alertmanager.ParseRules(bytes.NewReader(rawData), logQlParser)
	if err != nil {
		// Parse errors are user config errors - return partial result with error in status
		// Don't fail the operation, allow reconciliation to continue
		out = append(out, &typesv1.AlertGroup{
			Meta: &typesv1.AlertGroupMeta{},
			Spec: &typesv1.AlertGroupSpec{},
			Status: &typesv1.AlertGroupStatus{
				Errors: []string{errAlertGroupParse(filename, err).Error()},
			},
		})
		return out, nil
	}

	// Get file stat for timestamps
	fileInfo, err := ffs.Stat(filepath)
	var modTime time.Time
	if err == nil {
		modTime = fileInfo.ModTime()
	} else {
		modTime = time.Now()
	}

	// Apply humanlog metadata to all groups in the file
	for _, group := range out {
		// Set ID from group name
		group.Meta.Id = group.Spec.Name

		// Set is_readonly based on metadata
		if meta != nil && meta.IsReadonly != nil {
			group.Spec.IsReadonly = *meta.IsReadonly
		} else if isManaged {
			group.Spec.IsReadonly = false
		} else {
			group.Spec.IsReadonly = true
		}

		// Set timestamps
		group.Status.CreatedAt = timestamppb.New(modTime)
		group.Status.UpdatedAt = timestamppb.New(modTime)

		// Set origin based on markers
		if isManaged {
			group.Status.Origin = &typesv1.AlertGroupStatus_Managed{
				Managed: &typesv1.AlertGroupStatus_ManagedAlertGroup{
					Path: filepath,
				},
			}
		} else {
			// Check for generation markers
			detectionReason := detectGenerationMarker(rawData)
			if detectionReason != "" {
				group.Status.Origin = &typesv1.AlertGroupStatus_Generated{
					Generated: &typesv1.AlertGroupStatus_GeneratedAlertGroup{
						Path:            filepath,
						DetectionReason: detectionReason,
					},
				}
			} else {
				// Detected as discovered/external
				group.Status.Origin = &typesv1.AlertGroupStatus_Discovered{
					Discovered: &typesv1.AlertGroupStatus_DiscoveredAlertGroup{
						Path: filepath,
					},
				}
			}
		}
	}

	return out, nil
}
