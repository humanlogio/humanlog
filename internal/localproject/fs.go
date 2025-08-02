package localproject

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/compat/alertmanager"
	persesv1 "github.com/perses/perses/pkg/model/api/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

type localGitStorage struct {
	fs          fs.FS
	logQlParser func(string) (*typesv1.Query, error)
}

func newLocalGitStorage(projectSource ProjectSource, fs fs.FS, logQlParser func(string) (*typesv1.Query, error)) *localGitStorage {
	return &localGitStorage{fs: fs, logQlParser: logQlParser}
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
		dashboards, err := parseProjectDashboards(ctx, store.fs, name, lh.Path, lh.DashboardDir)
		if err != nil {
			return errInternal("parsing project dashboards: %v", err)
		}
		alertGroups, err := parseProjectAlertGroups(ctx, store.fs, name, lh.Path, lh.AlertDir, store.logQlParser)
		if err != nil {
			return errInternal("parsing project alert groups: %v", err)
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
	dashboards, err := parseProjectDashboards(ctx, store.fs, projectName, lh.Path, lh.DashboardDir)
	if err != nil {
		return errInternal("parsing project dashboards: %v", err)
	}
	for _, item := range dashboards {
		if item.Id == id {
			return onDashboard(item)
		}
	}
	return nil
}
func (store *localGitStorage) getAlertGroup(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, groupName string, onAlertGroup GetAlertGroupFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	items, err := parseProjectAlertGroups(ctx, store.fs, projectName, lh.Path, lh.AlertDir, store.logQlParser)
	if err != nil {
		return errInternal("parsing project alert groups: %v", err)
	}
	for _, item := range items {
		if item.Name == groupName {
			return onAlertGroup(item)
		}
	}
	return nil
}

func (store *localGitStorage) getAlertRule(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, groupName, ruleName string, onAlertRule GetAlertRuleFn) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	items, err := parseProjectAlertGroups(ctx, store.fs, projectName, lh.Path, lh.AlertDir, store.logQlParser)
	if err != nil {
		return errInternal("parsing project alert groups: %v", err)
	}
	onGroup := func(group *typesv1.AlertGroup) error {
		for _, rule := range group.Rules {
			if rule.Name == ruleName {
				return onAlertRule(rule)
			}
		}
		return errNotFound("no alert rule in group %q has this name: %q", groupName, ruleName)
	}

	for _, item := range items {
		if item.Name == groupName {
			return onGroup(item)
		}
	}
	return errNotFound("no alert group with this name: %q", groupName)
}

func createProjectFromPointer(ctx context.Context, ffs fs.FS, projectName string, project *typesv1.Project, ptr *typesv1.ProjectPointer_LocalGit) error {
	panic("todo")
}

func (store *localGitStorage) validateProjectPointer(ctx context.Context, ptr *typesv1.ProjectPointer) error {
	sch, ok := ptr.Scheme.(*typesv1.ProjectPointer_Localhost)
	if !ok {
		return errInvalid("local git can only operate with projectpointers for localhost, but got %T", ptr.Scheme)
	}
	lh := sch.Localhost
	ensureIsDir := func(path string) error {
		fi, err := fs.Stat(store.fs, path)
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

func parseProjectPointer(ctx context.Context, ffs fs.FS, projectName string, ptr *typesv1.ProjectPointer_LocalGit) (*typesv1.Project, bool, error) {
	projectDir, err := fs.Stat(ffs, ptr.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("looking up project directory %q on filesystem: %v", ptr.Path, err)
	}
	st := &typesv1.Project{
		Name: projectName,
		Pointer: &typesv1.ProjectPointer{Scheme: &typesv1.ProjectPointer_Localhost{Localhost: &typesv1.ProjectPointer_LocalGit{
			Path:         ptr.Path,
			DashboardDir: ptr.DashboardDir,
			AlertDir:     ptr.AlertDir,
		}}},
		CreatedAt: timestamppb.New(projectDir.ModTime()),
		UpdatedAt: timestamppb.New(projectDir.ModTime()),
	}
	return st, true, nil
}

func parseProjectDashboards(ctx context.Context, ffs fs.FS, projectName, projectPath, dashboardDir string) ([]*typesv1.Dashboard, error) {
	dashboardPath := path.Join(projectPath, dashboardDir)
	files, err := fs.ReadDir(ffs, dashboardPath)
	if err != nil {
		return nil, fmt.Errorf("looking up dashboard directory on filesystem: %v", err)
	}
	var out []*typesv1.Dashboard
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		filename := file.Name()
		fileext := filepath.Ext(filename)
		var (
			item *typesv1.Dashboard
			err  error
		)
		switch fileext {
		case ".json", ".yaml", ".yml":
			item, err = parseProjectDashboard(ctx, ffs, projectName, dashboardPath, filename)
		default:
			// skip it
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("parsing dashboard %q on filesystem: %v", filename, err)
		}
		out = append(out, item)
	}
	return out, nil
}

func parseProjectDashboard(ctx context.Context, ffs fs.FS, projectName, dashboardPath, filename string) (*typesv1.Dashboard, error) {
	persesToProto := func(in *persesv1.Dashboard, projectName, filepath string, data []byte) (*typesv1.Dashboard, error) {

		return &typesv1.Dashboard{
			Id:          dashboardID(projectName, in.Metadata.Project, in.Metadata.Name),
			Name:        in.Spec.Display.Name,
			Description: in.Spec.Display.Description,
			IsReadonly:  true,
			Source:      &typesv1.Dashboard_File{File: filepath},
			CreatedAt:   timestamppb.New(in.Metadata.CreatedAt),
			UpdatedAt:   timestamppb.New(in.Metadata.UpdatedAt),
			PersesJson:  data,
		}, nil
	}
	parseFile := func(ctx context.Context, ffs fs.FS, projectName, dirpath, filename string, parser func(ctx context.Context, data []byte) (*persesv1.Dashboard, error)) (*typesv1.Dashboard, error) {
		fpath := path.Join(dirpath, filename)
		data, err := fs.ReadFile(ffs, fpath)
		if err != nil {
			return nil, fmt.Errorf("opening dashboard file at %q: %v", fpath, err)
		}
		out, err := parser(ctx, data)
		if err != nil {
			return nil, fmt.Errorf("parsing dashboard file at %q: %v", fpath, err)
		}
		return persesToProto(out, projectName, fpath, data)
	}
	parseJSONDashboard := func(ctx context.Context, data []byte) (*persesv1.Dashboard, error) {
		out := new(persesv1.Dashboard)
		return out, out.UnmarshalJSON(data)
	}
	parseYAMLDashboard := func(ctx context.Context, data []byte) (*persesv1.Dashboard, error) {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		out := new(persesv1.Dashboard)
		return out, out.UnmarshalYAML(dec.Decode)
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

func parseProjectAlertGroups(ctx context.Context, ffs fs.FS, projectName, projectPath, alertGroupDir string, logQlParser func(string) (*typesv1.Query, error)) ([]*typesv1.AlertGroup, error) {
	alertGroupPath := path.Join(projectPath, alertGroupDir)
	files, err := fs.ReadDir(ffs, alertGroupPath)
	if err != nil {
		return nil, fmt.Errorf("looking up alert group directory on filesystem: %v", err)
	}
	var out []*typesv1.AlertGroup
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		filename := file.Name()
		fileext := filepath.Ext(filename)
		var (
			items []*typesv1.AlertGroup
			err   error
		)
		switch fileext {
		case ".yaml", ".yml":
			items, err = parseProjectAlertGroupsFromFile(ctx, ffs, alertGroupPath, filename, logQlParser)
		default:
			// skip it
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("parsing alert group %q on filesystem: %v", filename, err)
		}
		out = append(out, items...)
	}
	return out, nil
}

func parseProjectAlertGroupsFromFile(ctx context.Context, ffs fs.FS, alertGroupPath, filename string, logQlParser func(string) (*typesv1.Query, error)) ([]*typesv1.AlertGroup, error) {
	filepath := path.Join(alertGroupPath, filename)
	file, err := ffs.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("opening alert group file at %q: %v", filepath, err)
	}
	return alertmanager.ParseRules(file, logQlParser)
}
