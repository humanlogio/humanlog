package localproject

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/mitchellh/go-homedir"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type remoteGitStorage struct {
	fs          billy.Filesystem
	logQlParser func(string) (*typesv1.Query, error)
	timeNow     func() time.Time

	mu    sync.Mutex
	repos map[string]*remoteGit
}

type remoteGit struct {
	mu      sync.Mutex
	project *typesv1.Project
	ptr     *typesv1.ProjectPointer_RemoteGit
	storage storage.Storer
	r       *git.Repository
	w       *git.Worktree
}

func newRemoteGitStorage(projectSource ProjectSource, fs billy.Filesystem, logQlParser func(string) (*typesv1.Query, error), timeNow func() time.Time) (*remoteGitStorage, error) {
	return &remoteGitStorage{
		fs:          fs,
		logQlParser: logQlParser,
		timeNow:     timeNow,
		repos:       make(map[string]*remoteGit),
	}, nil
}

func (store *remoteGitStorage) getAuth(url string) (transport.AuthMethod, error) {
	if strings.HasPrefix(url, "git@") || strings.HasPrefix(url, "ssh://") {
		// Try SSH agent first, fall back to SSH keys
		if auth, err := ssh.NewSSHAgentAuth("git"); err == nil {
			return auth, nil
		}
		home, err := homedir.Dir()
		if err != nil {
			return nil, fmt.Errorf("looking up home dir: %v", err)
		}
		sshAuth, err := ssh.NewPublicKeysFromFile("git", filepath.Join(home, ".ssh", "id_rsa"), "")
		if err != nil {
			return nil, fmt.Errorf("no SSH authentication available: %v", err)
		}
		return sshAuth, nil
	}
	return nil, nil
}

func (store *remoteGitStorage) checkout(ctx context.Context, name string, ptr *typesv1.ProjectPointer_RemoteGit) (*remoteGit, error) {
	store.mu.Lock()
	rem, ok := store.repos[name]
	if ok {
		store.mu.Unlock()
		return rem, nil
	}
	rem = &remoteGit{
		project: &typesv1.Project{
			Meta: &typesv1.ProjectMeta{},
			Spec: &typesv1.ProjectSpec{
				Name: name,
				Pointer: &typesv1.ProjectPointer{
					Scheme: &typesv1.ProjectPointer_Remote{
						Remote: ptr,
					},
				},
			},
			Status: &typesv1.ProjectStatus{
				CreatedAt: timestamppb.New(store.timeNow()),
				UpdatedAt: timestamppb.New(store.timeNow()),
			},
		},
		ptr:     ptr,
		storage: memory.NewStorage(),
	}
	rem.mu.Lock()
	defer rem.mu.Unlock()
	store.repos[name] = rem
	store.mu.Unlock()

	auth, err := store.getAuth(ptr.RemoteUrl)
	if err != nil {
		return nil, err
	}

	r, err := git.CloneContext(ctx, rem.storage, store.fs, &git.CloneOptions{
		Auth:         auth,
		URL:          ptr.RemoteUrl,
		Depth:        1,
		NoCheckout:   true,
		SingleBranch: false, // to resolve the ref
		Tags:         git.NoTags,
	})
	if err != nil {
		return nil, fmt.Errorf("cloning repository: %v", err)
	}
	rem.r = r
	return store.syncWithLock(ctx, name, ptr, rem)
}

func (store *remoteGitStorage) sync(ctx context.Context, name string, ptr *typesv1.ProjectPointer_RemoteGit) (*remoteGit, error) {
	store.mu.Lock()
	rem, ok := store.repos[name]
	if !ok {
		store.mu.Unlock()
		return nil, fmt.Errorf("can't sync unknown project named %q", name)
	}
	rem.mu.Lock()
	defer rem.mu.Unlock()
	return store.syncWithLock(ctx, name, ptr, rem)
}

func (store *remoteGitStorage) syncWithLock(ctx context.Context, name string, ptr *typesv1.ProjectPointer_RemoteGit, rem *remoteGit) (*remoteGit, error) {
	commit, err := rem.r.ResolveRevision(plumbing.Revision(ptr.Ref))
	if err != nil {
		return nil, fmt.Errorf("resolving revision %q in repository: %v", ptr.Ref, err)
	}
	r := rem.r
	w, err := r.Worktree()
	if err != nil {
		return nil, fmt.Errorf("obtaining repository worktree: %v", err)
	}
	rem.w = w
	rem.project.Status.UpdatedAt = timestamppb.New(store.timeNow())
	err = w.Checkout(&git.CheckoutOptions{
		Hash: *commit,
		SparseCheckoutDirectories: []string{
			ptr.DashboardDir,
			ptr.AlertDir,
		},
	})
	if err != nil && !errors.Is(err, git.ErrSparseResetDirectoryNotFound) {
		return nil, fmt.Errorf("doing sparse checkout of dashboards and alerts dir: %v", err)
	}

	if _, err := rem.w.Filesystem.Stat(ptr.DashboardDir); errors.Is(err, os.ErrNotExist) {
		rem.project.Status.Errors = append(rem.project.Status.Errors, projectErrDashboardDirMissing(ptr.DashboardDir))
	}
	if _, err := rem.w.Filesystem.Stat(ptr.AlertDir); errors.Is(err, os.ErrNotExist) {
		rem.project.Status.Errors = append(rem.project.Status.Errors, projectErrAlertDirMissing(ptr.AlertDir))
	}

	return rem, nil
}

func (store *remoteGitStorage) getOrCreateProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onCreate CreateProjectFn, onGetProject GetProjectFn) error {
	if err := store.validateProjectPointer(ctx, ptr); err != nil {
		return err
	}
	gitptr := ptr.GetRemote()
	rem, err := store.checkout(ctx, name, gitptr)
	if err != nil {
		return fmt.Errorf("looking up git remote: %v", err)
	}
	if !rem.mu.TryLock() {
		return fmt.Errorf("local checkout is busy, please wait: %v", err)
	}
	defer rem.mu.Unlock()
	return onGetProject(rem.project)

}

func (store *remoteGitStorage) getProjectHydrated(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onGetProject GetProjectHydratedFn) error {
	rem, err := store.checkout(ctx, name, ptr.GetRemote())
	if err != nil {
		return fmt.Errorf("looking up git remote: %v", err)
	}
	if !rem.mu.TryLock() {
		return fmt.Errorf("local checkout is busy, please wait: %v", err)
	}
	defer rem.mu.Unlock()

	gitptr := rem.ptr
	dashboards := []*typesv1.Dashboard{}
	if _, err := rem.w.Filesystem.Stat(gitptr.DashboardDir); err == nil {
		dashboards, err = parseProjectDashboards(ctx, rem.w.Filesystem, name, "", gitptr.DashboardDir, true)
		if err != nil {
			var connectErr *connect.Error
			if errors.As(err, &connectErr) && connectErr.Code() == connect.CodeInvalidArgument {
				rem.project.Status.Errors = append(rem.project.Status.Errors, err.Error())
			} else {
				return err
			}
		}
	}

	alertGroups := []*typesv1.AlertGroup{}
	if _, err := rem.w.Filesystem.Stat(gitptr.AlertDir); err == nil {
		alertGroups, err = parseProjectAlertGroups(ctx, rem.w.Filesystem, name, "", gitptr.AlertDir, store.logQlParser)
		if err != nil {
			var connectErr *connect.Error
			if errors.As(err, &connectErr) && connectErr.Code() == connect.CodeInvalidArgument {
				rem.project.Status.Errors = append(rem.project.Status.Errors, err.Error())
			} else {
				return err
			}
		}
	}

	return onGetProject(rem.project, dashboards, alertGroups)
}

func (store *remoteGitStorage) syncProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onGetProject GetProjectFn) error {
	rem, err := store.checkout(ctx, name, ptr.GetRemote())
	if err != nil {
		return fmt.Errorf("looking up git remote: %v", err)
	}
	if !rem.mu.TryLock() {
		return fmt.Errorf("local checkout is busy, please wait: %v", err)
	}
	defer rem.mu.Unlock()
	return onGetProject(rem.project)
}

func (store *remoteGitStorage) getProject(ctx context.Context, name string, ptr *typesv1.ProjectPointer, onGetProject GetProjectFn) error {
	rem, err := store.checkout(ctx, name, ptr.GetRemote())
	if err != nil {
		return fmt.Errorf("looking up git remote: %v", err)
	}
	if !rem.mu.TryLock() {
		return fmt.Errorf("local checkout is busy, please wait: %v", err)
	}
	defer rem.mu.Unlock()
	return onGetProject(rem.project)
}

func (store *remoteGitStorage) getDashboard(ctx context.Context, name string, ptr *typesv1.ProjectPointer, id string, onDashboard GetDashboardFn) error {
	rem, err := store.checkout(ctx, name, ptr.GetRemote())
	if err != nil {
		return fmt.Errorf("looking up git remote: %v", err)
	}
	if !rem.mu.TryLock() {
		return fmt.Errorf("local checkout is busy, please wait: %v", err)
	}
	defer rem.mu.Unlock()
	gitptr := rem.ptr
	if _, err := rem.w.Filesystem.Stat(gitptr.DashboardDir); err != nil {
		return errInvalid("project %q has no dashboard directory at %q", name, gitptr.DashboardDir)
	}
	dashboards, err := parseProjectDashboards(ctx, rem.w.Filesystem, name, "", gitptr.DashboardDir, true)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errInvalid("dashboard directory %q not found in remote repository", gitptr.DashboardDir)
		}
		return errInvalid("cannot parse dashboards: %v", err)
	}
	for _, d := range dashboards {
		if d.Meta.Id == id {
			return onDashboard(d)
		}
	}
	return errDashboardNotFound(name, id)
}

func (store *remoteGitStorage) createDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, dashboard *typesv1.Dashboard, onCreated CreateDashboardFn) error {
	return errInvalid("cannot create dashboard in remote project")
}

func (store *remoteGitStorage) updateDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, id string, dashboard *typesv1.Dashboard, onUpdated UpdateDashboardFn) error {
	return errInvalid("cannot update dashboard in remote project")
}

func (store *remoteGitStorage) deleteDashboard(ctx context.Context, projectName string, ptr *typesv1.ProjectPointer, id string, onDeleted DeleteDashboardFn) error {
	return errInvalid("cannot delete dashboard in remote project")
}

func (store *remoteGitStorage) getAlertGroup(ctx context.Context, alertState localstorage.Alertable, name string, ptr *typesv1.ProjectPointer, groupName string, onAlertGroup GetAlertGroupFn) error {
	rem, err := store.checkout(ctx, name, ptr.GetRemote())
	if err != nil {
		return fmt.Errorf("looking up git remote: %v", err)
	}
	if !rem.mu.TryLock() {
		return fmt.Errorf("local checkout is busy, please wait: %v", err)
	}
	defer rem.mu.Unlock()
	gitptr := rem.ptr
	if _, err := rem.w.Filesystem.Stat(gitptr.AlertDir); err != nil {
		return errInvalid("project %q has no alert directory at %q", name, gitptr.AlertDir)
	}
	alertGroups, err := parseProjectAlertGroups(ctx, rem.w.Filesystem, name, "", gitptr.AlertDir, store.logQlParser)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errInvalid("alert directory %q not found in remote repository", gitptr.AlertDir)
		}
		return errInvalid("cannot parse alert groups: %v", err)
	}
	for _, ag := range alertGroups {
		if ag.Spec.Name == groupName {
			// Hydrate status for all rules in group
			ag.Status.Rules = make([]*typesv1.AlertGroupStatus_NamedAlertRuleStatus, 0, len(ag.Spec.Rules))
			for _, named := range ag.Spec.Rules {
				state, err := alertState.AlertGetOrCreate(ctx, name, groupName, named.Id, func() *typesv1.AlertRuleStatus {
					return &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{Unknown: &typesv1.AlertUnknown{}}}
				})
				if err != nil {
					return errInternal("fetching alert status for rule %q: %w", named.Id, err)
				}
				ag.Status.Rules = append(ag.Status.Rules, &typesv1.AlertGroupStatus_NamedAlertRuleStatus{
					Id:     named.Id,
					Status: state,
				})
			}
			return onAlertGroup(ag)
		}
	}
	return errAlertGroupNotFound(name, groupName)
}

func (store *remoteGitStorage) getAlertRule(ctx context.Context, alertState localstorage.Alertable, name string, ptr *typesv1.ProjectPointer, groupName, ruleName string, onAlertRule GetAlertRuleFn) error {
	rem, err := store.checkout(ctx, name, ptr.GetRemote())
	if err != nil {
		return fmt.Errorf("looking up git remote: %v", err)
	}
	if !rem.mu.TryLock() {
		return fmt.Errorf("local checkout is busy, please wait: %v", err)
	}
	defer rem.mu.Unlock()
	gitptr := rem.ptr
	if _, err := rem.w.Filesystem.Stat(gitptr.AlertDir); err != nil {
		return errInvalid("project %q has no alert directory at %q", name, gitptr.AlertDir)
	}
	alertGroups, err := parseProjectAlertGroups(ctx, rem.w.Filesystem, name, "", gitptr.AlertDir, store.logQlParser)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errInvalid("alert directory %q not found in remote repository", gitptr.AlertDir)
		}
		return errInvalid("cannot parse alert groups: %v", err)
	}
	for _, ag := range alertGroups {
		if ag.Spec.Name == groupName {
			for _, named := range ag.Spec.Rules {
				if named.Id == ruleName {
					// Fetch actual runtime status from storage
					state, err := alertState.AlertGetOrCreate(ctx, name, groupName, named.Id, func() *typesv1.AlertRuleStatus {
						return &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{Unknown: &typesv1.AlertUnknown{}}}
					})
					if err != nil {
						return errInternal("fetching alert status for rule %q: %w", named.Id, err)
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

		}
	}
	return errAlertRuleNotFound(name, groupName, ruleName)
}

func (store *remoteGitStorage) validateProjectPointer(ctx context.Context, ptr *typesv1.ProjectPointer) error {
	gitptr := ptr.GetRemote()
	if gitptr == nil {
		return fmt.Errorf("expecting a Git remote pointer, got a %T", ptr.Scheme)
	}
	return nil
}
