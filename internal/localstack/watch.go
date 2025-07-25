package localstack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"connectrpc.com/connect"
	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	dashboardv1 "github.com/humanlogio/api/go/svc/dashboard/v1"
	stackv1 "github.com/humanlogio/api/go/svc/stack/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/compat/alertmanager"
	"github.com/humanlogio/humanlog/internal/localstate"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	persesv1 "github.com/perses/perses/pkg/model/api/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

type watch struct {
	fs          fs.FS
	cfg         *config.Config
	alertState  localstorage.Alertable
	logQlParser func(string) (*typesv1.Query, error)
}

func Watch(ctx context.Context, fs fs.FS, cfg *config.Config, alertState localstorage.Alertable, logQlParser func(string) (*typesv1.Query, error)) localstate.DB {
	return &watch{fs: fs, cfg: cfg, alertState: alertState, logQlParser: logQlParser}
}

func (wt *watch) CreateStack(context.Context, *stackv1.CreateStackRequest) (*stackv1.CreateStackResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost stacks are created via the config file"))
}
func (wt *watch) UpdateStack(context.Context, *stackv1.UpdateStackRequest) (*stackv1.UpdateStackResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost stacks are updated in the config file"))
}
func (wt *watch) DeleteStack(context.Context, *stackv1.DeleteStackRequest) (*stackv1.DeleteStackResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost stacks are deleted in the config file"))
}
func (wt *watch) GetStack(ctx context.Context, req *stackv1.GetStackRequest) (*stackv1.GetStackResponse, error) {
	var (
		stack       *typesv1.Stack
		dashboards  []*typesv1.Dashboard
		alertGroups []*typesv1.AlertGroup
		err         error
	)
	err = wt.withStackByName(ctx, req.Name, func(sc *typesv1.Stack, ptr *typesv1.StacksConfig_LocalhostStackPointer) error {
		stack = sc
		dashboards, err = parseStackDashboards(ctx, wt.fs, sc.Name, ptr.Path, ptr.DashboardDir)
		if err != nil {
			return err
		}
		alertGroups, err = parseStackAlertGroups(ctx, wt.fs, sc.Name, ptr.Path, ptr.AlertDir, wt.logQlParser)
		if err != nil {
			return err
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &stackv1.GetStackResponse{Stack: stack, Dashboards: dashboards, AlertGroups: alertGroups}, nil
}

func (wt *watch) ListStack(ctx context.Context, req *stackv1.ListStackRequest) (*stackv1.ListStackResponse, error) {
	var (
		out  []*stackv1.ListStackResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = wt.withStackConfig(ctx, func(sc *typesv1.StacksConfig) error {
		next, err = cursorForSlice(sc.Stacks, req.Cursor, req.Limit, 10, 100,
			func(sp *typesv1.StacksConfig_LocalhostStackPointer) string { return sp.Name },
			func(sp *typesv1.StacksConfig_LocalhostStackPointer) error {
				st, err := parseStackPointer(ctx, wt.fs, sp)
				if err != nil {
					return fmt.Errorf("parsing stack %q at %q: %v", sp.Name, sp.Path, err)
				}
				out = append(out, &stackv1.ListStackResponse_ListItem{Stack: st})
				return nil
			},
		)
		return err
	})
	return &stackv1.ListStackResponse{Items: out, Next: next}, err
}

func (wt *watch) CreateDashboard(ctx context.Context, req *dashboardv1.CreateDashboardRequest) (*dashboardv1.CreateDashboardResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost dashboard are created on the filesystem"))
}
func (wt *watch) UpdateDashboard(ctx context.Context, req *dashboardv1.UpdateDashboardRequest) (*dashboardv1.UpdateDashboardResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost dashboard are updated on the filesystem"))
}
func (wt *watch) DeleteDashboard(ctx context.Context, req *dashboardv1.DeleteDashboardRequest) (*dashboardv1.DeleteDashboardResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost dashboard are deleted on the filesystem"))
}
func (wt *watch) GetDashboard(ctx context.Context, req *dashboardv1.GetDashboardRequest) (*dashboardv1.GetDashboardResponse, error) {
	var (
		out *typesv1.Dashboard
		err error
	)
	err = wt.withDashboardByID(ctx, req.StackName, req.Id, func(s *typesv1.Dashboard) error {
		out = s
		return nil
	})
	return &dashboardv1.GetDashboardResponse{Dashboard: out}, err
}
func (wt *watch) ListDashboard(ctx context.Context, req *dashboardv1.ListDashboardRequest) (*dashboardv1.ListDashboardResponse, error) {
	var (
		out  []*dashboardv1.ListDashboardResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = wt.withStackByName(ctx, req.StackName, func(sc *typesv1.Stack, ptr *typesv1.StacksConfig_LocalhostStackPointer) error {
		items, err := parseStackDashboards(ctx, wt.fs, sc.Name, ptr.Path, ptr.DashboardDir)
		if err != nil {
			return err
		}
		next, err = cursorForSlice(items, req.Cursor, req.Limit, 10, 100,
			func(sp *typesv1.Dashboard) string { return sp.Id },
			func(sp *typesv1.Dashboard) error {
				out = append(out, &dashboardv1.ListDashboardResponse_ListItem{Dashboard: sp})
				return nil
			},
		)
		return err
	})
	return &dashboardv1.ListDashboardResponse{Items: out, Next: next}, err
}

func (wt *watch) CreateAlertGroup(ctx context.Context, req *alertv1.CreateAlertGroupRequest) (*alertv1.CreateAlertGroupResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost alertgroups are created on the filesystem"))
}
func (wt *watch) UpdateAlertGroup(ctx context.Context, req *alertv1.UpdateAlertGroupRequest) (*alertv1.UpdateAlertGroupResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost alertgroups are updated on the filesystem"))
}
func (wt *watch) DeleteAlertGroup(ctx context.Context, req *alertv1.DeleteAlertGroupRequest) (*alertv1.DeleteAlertGroupResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost alertgroups are deleted on the filesystem"))
}
func (wt *watch) GetAlertGroup(ctx context.Context, req *alertv1.GetAlertGroupRequest) (*alertv1.GetAlertGroupResponse, error) {
	var (
		out *typesv1.AlertGroup
		err error
	)
	err = wt.withAlertGroupByName(ctx, req.StackName, req.Name, func(s *typesv1.AlertGroup) error {
		out = s
		return nil
	})
	return &alertv1.GetAlertGroupResponse{AlertGroup: out}, err
}
func (wt *watch) ListAlertGroup(ctx context.Context, req *alertv1.ListAlertGroupRequest) (*alertv1.ListAlertGroupResponse, error) {
	var (
		out  []*alertv1.ListAlertGroupResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = wt.withStackByName(ctx, req.StackName, func(sc *typesv1.Stack, ptr *typesv1.StacksConfig_LocalhostStackPointer) error {
		items, err := parseStackAlertGroups(ctx, wt.fs, sc.Name, ptr.Path, ptr.AlertDir, wt.logQlParser)
		if err != nil {
			return err
		}
		next, err = cursorForSlice(items, req.Cursor, req.Limit, 10, 100,
			func(sp *typesv1.AlertGroup) string { return sp.Name },
			func(sp *typesv1.AlertGroup) error {
				out = append(out, &alertv1.ListAlertGroupResponse_ListItem{AlertGroup: sp})
				return nil
			},
		)
		return err
	})
	return &alertv1.ListAlertGroupResponse{Items: out, Next: next}, err
}

func (wt *watch) CreateAlertRule(ctx context.Context, req *alertv1.CreateAlertRuleRequest) (*alertv1.CreateAlertRuleResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost alerts are created on the filesystem"))
}
func (wt *watch) UpdateAlertRule(ctx context.Context, req *alertv1.UpdateAlertRuleRequest) (*alertv1.UpdateAlertRuleResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost alerts are updated on the filesystem"))
}
func (wt *watch) DeleteAlertRule(ctx context.Context, req *alertv1.DeleteAlertRuleRequest) (*alertv1.DeleteAlertRuleResponse, error) {
	return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("localhost alerts are deleted on the filesystem"))
}
func (wt *watch) GetAlertRule(ctx context.Context, req *alertv1.GetAlertRuleRequest) (*alertv1.GetAlertRuleResponse, error) {
	var (
		out *typesv1.AlertRule
		err error
	)
	err = wt.withAlertByName(ctx, req.StackName, req.GroupName, req.Name, func(ar *typesv1.AlertRule) error {
		out = ar
		return nil
	})
	return &alertv1.GetAlertRuleResponse{AlertRule: out}, err
}
func (wt *watch) ListAlertRule(ctx context.Context, req *alertv1.ListAlertRuleRequest) (*alertv1.ListAlertRuleResponse, error) {
	var (
		out  []*alertv1.ListAlertRuleResponse_ListItem
		next *typesv1.Cursor
		err  error
	)
	err = wt.withAlertGroupByName(ctx, req.StackName, req.GroupName, func(s *typesv1.AlertGroup) error {
		next, err = cursorForSlice(s.Rules, req.Cursor, req.Limit, 10, 100,
			func(sp *typesv1.AlertRule) string { return sp.Name },
			func(sp *typesv1.AlertRule) error {
				out = append(out, &alertv1.ListAlertRuleResponse_ListItem{AlertRule: sp})
				return nil
			},
		)
		return err
	})
	return &alertv1.ListAlertRuleResponse{Items: out, Next: next}, err
}

func (wt *watch) withStackConfig(ctx context.Context, fn func(*typesv1.StacksConfig) error) error {
	stacks := wt.cfg.GetRuntime().GetExperimentalFeatures().GetStacks()
	if stacks == nil {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("localhost stacks are not enabled, set them up in your config file"))
	}
	return fn(stacks)
}

func (wt *watch) withStackByName(ctx context.Context, name string, fn func(*typesv1.Stack, *typesv1.StacksConfig_LocalhostStackPointer) error) error {
	return wt.withStackConfig(ctx, func(sc *typesv1.StacksConfig) error {
		for _, localpointer := range sc.Stacks {
			if localpointer.Name == name {
				st, err := parseStackPointer(ctx, wt.fs, localpointer)
				if err != nil {
					return fmt.Errorf("parsing stack %q at %q: %v", localpointer.Name, localpointer.Path, err)
				}
				return fn(st, localpointer)
			}
		}
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("localhost doesn't have a stack named %q in its config file", name))
	})
}

func (wt *watch) withDashboardByID(ctx context.Context, stackName, id string, fn func(*typesv1.Dashboard) error) error {
	return wt.withStackByName(ctx, stackName, func(s *typesv1.Stack, sc *typesv1.StacksConfig_LocalhostStackPointer) error {
		dashboards, err := parseStackDashboards(ctx, wt.fs, sc.Name, sc.Path, sc.DashboardDir)
		if err != nil {
			return err
		}
		for _, el := range dashboards {
			if el.Id == id {
				return fn(el)
			}
		}
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("stack %q has no dashboard with ID %q", stackName, id))
	})
}

func (wt *watch) withAlertGroupByName(ctx context.Context, stackName, name string, fn func(*typesv1.AlertGroup) error) error {
	return wt.withStackByName(ctx, stackName, func(s *typesv1.Stack, sc *typesv1.StacksConfig_LocalhostStackPointer) error {
		alertGroups, err := parseStackAlertGroups(ctx, wt.fs, sc.Name, sc.Path, sc.AlertDir, wt.logQlParser)
		if err != nil {
			return err
		}
		for _, el := range alertGroups {
			if el.Name == name {
				return fn(el)
			}
		}
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("no alert group with name %q in stack %q", name, stackName))
	})
}

func (wt *watch) withAlertByName(ctx context.Context, stackName, groupName, name string, fn func(*typesv1.AlertRule) error) error {
	return wt.withAlertGroupByName(ctx, stackName, groupName, func(ag *typesv1.AlertGroup) error {
		for _, el := range ag.Rules {
			if el.Name == name {
				return fn(el)
			}
		}
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("no alert with name %q in stack %q, group %q", name, stackName, groupName))
	})
}

func parseStackPointer(ctx context.Context, ffs fs.FS, ptr *typesv1.StacksConfig_LocalhostStackPointer) (*typesv1.Stack, error) {
	stackDir, err := fs.Stat(ffs, ptr.Path)
	if err != nil {
		return nil, fmt.Errorf("looking up stack directory %q on filesystem: %v", ptr.Path, err)
	}
	st := &typesv1.Stack{
		Name: ptr.Name,
		Pointer: &typesv1.StackPointer{Scheme: &typesv1.StackPointer_Localhost{Localhost: &typesv1.StackPointer_LocalGit{
			Path:         ptr.Path,
			DashboardDir: ptr.DashboardDir,
			AlertDir:     ptr.AlertDir,
		}}},
		CreatedAt: timestamppb.New(stackDir.ModTime()),
		UpdatedAt: timestamppb.New(stackDir.ModTime()),
	}
	return st, nil
}

func parseStackDashboards(ctx context.Context, ffs fs.FS, stackName, stackPath, dashboardDir string) ([]*typesv1.Dashboard, error) {
	dashboardPath := path.Join(stackPath, dashboardDir)
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
			item, err = parseStackDashboard(ctx, ffs, stackName, dashboardPath, filename)
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

func parseStackDashboard(ctx context.Context, ffs fs.FS, stackName, dashboardPath, filename string) (*typesv1.Dashboard, error) {
	persesToProto := func(in *persesv1.Dashboard, stackName, filepath string, data []byte) (*typesv1.Dashboard, error) {
		return &typesv1.Dashboard{
			Id:          path.Join(stackName, in.Metadata.Project, in.Metadata.Name),
			Name:        in.Spec.Display.Name,
			Description: in.Spec.Display.Description,
			IsReadonly:  true,
			Source:      &typesv1.Dashboard_File{File: filepath},
			CreatedAt:   timestamppb.New(in.Metadata.CreatedAt),
			UpdatedAt:   timestamppb.New(in.Metadata.UpdatedAt),
			PersesJson:  data,
		}, nil
	}
	parseFile := func(ctx context.Context, ffs fs.FS, stackName, dirpath, filename string, parser func(ctx context.Context, data []byte) (*persesv1.Dashboard, error)) (*typesv1.Dashboard, error) {
		fpath := path.Join(dirpath, filename)
		data, err := fs.ReadFile(ffs, fpath)
		if err != nil {
			return nil, fmt.Errorf("opening dashboard file at %q: %v", fpath, err)
		}
		out, err := parser(ctx, data)
		if err != nil {
			return nil, fmt.Errorf("parsing dashboard file at %q: %v", fpath, err)
		}
		return persesToProto(out, stackName, fpath, data)
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
		return parseFile(ctx, ffs, stackName, dashboardPath, filename, parseJSONDashboard)
	case ".yaml", ".yml":
		return parseFile(ctx, ffs, stackName, dashboardPath, filename, parseYAMLDashboard)
	default:
		return nil, fmt.Errorf("invalid file extension for a dashboard: expecting .yaml, .yml or .json, got %q", fileext)
	}
}

func parseStackAlertGroups(ctx context.Context, ffs fs.FS, stackName, stackPath, alertGroupDir string, logQlParser func(string) (*typesv1.Query, error)) ([]*typesv1.AlertGroup, error) {
	alertGroupPath := path.Join(stackPath, alertGroupDir)
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
			items, err = parseStackAlertGroupsFromFile(ctx, ffs, alertGroupPath, filename, logQlParser)
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

func parseStackAlertGroupsFromFile(ctx context.Context, ffs fs.FS, alertGroupPath, filename string, logQlParser func(string) (*typesv1.Query, error)) ([]*typesv1.AlertGroup, error) {
	filepath := path.Join(alertGroupPath, filename)
	file, err := ffs.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("opening alert group file at %q: %v", filepath, err)
	}
	return alertmanager.ParseRules(file, logQlParser)
}

type stringPage struct {
	LastID string `json:"lastID"`
}

func cursorForSlice[E any](sl []E, cursor *typesv1.Cursor, limit, minLimit, maxLimit int32, toStringID func(E) string, forEach func(E) error) (next *typesv1.Cursor, err error) {
	limit = max(limit, maxLimit)
	limit = min(limit, minLimit)
	var fromID string
	if cursor != nil {
		var p stringPage
		if err := json.Unmarshal(cursor.Opaque, &p); err != nil {
			return nil, err
		}
		fromID = p.LastID
	}
	var i int
	if fromID != "" {
		i = slices.IndexFunc(sl, func(e E) bool { return toStringID(e) == fromID }) + 1
	}
	from := i
	to := min(i+int(limit), len(sl))
	out := sl[from:to]
	for _, el := range out {
		if err := forEach(el); err != nil {
			return nil, err
		}
	}
	if len(out) == int(limit) && limit != 0 {
		next = new(typesv1.Cursor)
		p := stringPage{LastID: toStringID(out[len(out)-1])}
		next.Opaque, err = json.Marshal(p)
		if err != nil {
			return nil, err
		}
	}
	return next, nil
}
