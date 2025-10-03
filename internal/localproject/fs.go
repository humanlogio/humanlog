package localproject

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/go-git/go-billy/v6"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/compat/alertmanager"
	persesv1 "github.com/perses/perses/pkg/model/api/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

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
		dashboards, err := parseProjectDashboards(ctx, store.fs, name, lh.Path, lh.DashboardDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				p.Status.Errors = append(p.Status.Errors, fmt.Sprintf("dashboard directory does not exist at path %q", lh.DashboardDir))
			} else {
				p.Status.Errors = append(p.Status.Errors, fmt.Sprintf("parsing project dashboards: %v", err))
			}
		}
		alertGroups, err := parseProjectAlertGroups(ctx, store.fs, name, lh.Path, lh.AlertDir, store.logQlParser)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				p.Status.Errors = append(p.Status.Errors, fmt.Sprintf("alert directory does not exist at path %q", lh.AlertDir))
			} else {
				p.Status.Errors = append(p.Status.Errors, fmt.Sprintf("parsing project alert groups: %v", err))
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
	dashboards, err := parseProjectDashboards(ctx, store.fs, projectName, lh.Path, lh.DashboardDir)
	if err != nil {
		return errInternal("parsing project dashboards: %v", err)
	}
	for _, item := range dashboards {
		if item.Meta.Id == id {
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
		if item.Spec.Name == groupName {
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
		for _, rule := range group.Spec.Rules {
			if rule.Name == ruleName {
				return onAlertRule(rule)
			}
		}
		return errNotFound("no alert rule in group %q has this name: %q", groupName, ruleName)
	}

	for _, item := range items {
		if item.Spec.Name == groupName {
			return onGroup(item)
		}
	}
	return errNotFound("no alert group with this name: %q", groupName)
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

func parseProjectDashboards(ctx context.Context, ffs billy.Filesystem, projectName, projectPath, dashboardDir string) ([]*typesv1.Dashboard, error) {
	dashboardPath := path.Join(projectPath, dashboardDir)
	files, err := ffs.ReadDir(dashboardPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
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

func parseProjectDashboard(ctx context.Context, ffs billy.Filesystem, projectName, dashboardPath, filename string) (*typesv1.Dashboard, error) {
	persesToProto := func(in *persesv1.Dashboard, projectName, filepath string, data []byte) (*typesv1.Dashboard, error) {
		return &typesv1.Dashboard{
			Meta: &typesv1.DashboardMeta{
				Id: dashboardID(projectName, in.Metadata.Project, in.Metadata.Name),
			},
			Spec: &typesv1.DashboardSpec{
				Name:        in.Spec.Display.Name,
				Description: in.Spec.Display.Description,
				IsReadonly:  true,
				Source:      &typesv1.DashboardSpec_File{File: filepath},
				PersesJson:  data,
			},
			Status: &typesv1.DashboardStatus{
				CreatedAt: timestamppb.New(in.Metadata.CreatedAt),
				UpdatedAt: timestamppb.New(in.Metadata.UpdatedAt),
			},
		}, nil
	}
	parseFile := func(ctx context.Context, ffs billy.Filesystem, projectName, dirpath, filename string, parser func(ctx context.Context, data []byte) (*persesv1.Dashboard, error)) (*typesv1.Dashboard, error) {
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

		data, err := io.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("reading dashboard file at %q: %v", fpath, err)
		}
		pout, err := parser(ctx, data)
		if err != nil {
			out.Status.Errors = append(out.Status.Errors, fmt.Sprintf("invalid dashboard found at path %q: %v", fpath, err))
			return out, nil
		}
		return persesToProto(pout, projectName, fpath, data)
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

func parseProjectAlertGroups(ctx context.Context, ffs billy.Filesystem, projectName, projectPath, alertGroupDir string, logQlParser func(string) (*typesv1.Query, error)) ([]*typesv1.AlertGroup, error) {
	alertGroupPath := path.Join(projectPath, alertGroupDir)
	files, err := ffs.ReadDir(alertGroupPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
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

func parseProjectAlertGroupsFromFile(ctx context.Context, ffs billy.Filesystem, alertGroupPath, filename string, logQlParser func(string) (*typesv1.Query, error)) ([]*typesv1.AlertGroup, error) {
	filepath := path.Join(alertGroupPath, filename)
	file, err := ffs.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("opening alert group file at %q: %v", filepath, err)
	}

	out, err := alertmanager.ParseRules(file, logQlParser)
	if err != nil {
		out = append(out, &typesv1.AlertGroup{
			Meta: &typesv1.AlertGroupMeta{},
			Spec: &typesv1.AlertGroupSpec{},
			Status: &typesv1.AlertGroupStatus{
				Errors: []string{fmt.Sprintf("parsing alert group file %q: %v", filepath, err)},
			},
		})
		return out, err
	}
	return out, err
}
