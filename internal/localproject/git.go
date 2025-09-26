package localproject

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
	typesv1 "github.com/humanlogio/api/go/types/v1"
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
			Name: name,
			Pointer: &typesv1.ProjectPointer{
				Scheme: &typesv1.ProjectPointer_Remote{
					Remote: ptr,
				},
			},
			CreatedAt: timestamppb.New(store.timeNow()),
			UpdatedAt: timestamppb.New(store.timeNow()),
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
	commit, err := r.ResolveRevision(plumbing.Revision(ptr.Ref))
	if err != nil {
		return nil, fmt.Errorf("resolving revision %q in repository: %v", ptr.Ref, err)
	}
	rem.r = r
	w, err := r.Worktree()
	if err != nil {
		return nil, fmt.Errorf("obtaining repository worktree: %v", err)
	}
	rem.w = w
	rem.project.UpdatedAt = timestamppb.New(store.timeNow())
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
	dashboards, err := parseProjectDashboards(ctx, rem.w.Filesystem, name, "", gitptr.DashboardDir)
	if err != nil {
		return errInternal("parsing project dashboards: %v", err)
	}
	alertGroups, err := parseProjectAlertGroups(ctx, rem.w.Filesystem, name, "", gitptr.AlertDir, store.logQlParser)
	if err != nil {
		return errInternal("parsing project alert groups: %v", err)
	}
	return onGetProject(rem.project, dashboards, alertGroups)
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
	dashboards, err := parseProjectDashboards(ctx, rem.w.Filesystem, name, "", gitptr.DashboardDir)
	if err != nil {
		return errInternal("parsing project dashboards: %v", err)
	}
	for _, d := range dashboards {
		if d.Id == id {
			return onDashboard(d)
		}
	}
	return fmt.Errorf("project %q has no dashboard with ID %q", name, id)
}

func (store *remoteGitStorage) getAlertGroup(ctx context.Context, name string, ptr *typesv1.ProjectPointer, groupName string, onAlertGroup GetAlertGroupFn) error {
	rem, err := store.checkout(ctx, name, ptr.GetRemote())
	if err != nil {
		return fmt.Errorf("looking up git remote: %v", err)
	}
	if !rem.mu.TryLock() {
		return fmt.Errorf("local checkout is busy, please wait: %v", err)
	}
	defer rem.mu.Unlock()
	gitptr := rem.ptr
	alertGroups, err := parseProjectAlertGroups(ctx, rem.w.Filesystem, name, "", gitptr.AlertDir, store.logQlParser)
	if err != nil {
		return errInternal("parsing project alertGroups: %v", err)
	}
	for _, ag := range alertGroups {
		if ag.Name == groupName {
			return onAlertGroup(ag)
		}
	}
	return fmt.Errorf("project %q has no alert group with name %q", name, groupName)
}

func (store *remoteGitStorage) getAlertRule(ctx context.Context, name string, ptr *typesv1.ProjectPointer, groupName, ruleName string, onAlertRule GetAlertRuleFn) error {
	rem, err := store.checkout(ctx, name, ptr.GetRemote())
	if err != nil {
		return fmt.Errorf("looking up git remote: %v", err)
	}
	if !rem.mu.TryLock() {
		return fmt.Errorf("local checkout is busy, please wait: %v", err)
	}
	defer rem.mu.Unlock()
	gitptr := rem.ptr
	alertGroups, err := parseProjectAlertGroups(ctx, rem.w.Filesystem, name, "", gitptr.AlertDir, store.logQlParser)
	if err != nil {
		return errInternal("parsing project alertGroups: %v", err)
	}
	for _, ag := range alertGroups {
		if ag.Name == groupName {
			for _, rule := range ag.Rules {
				if rule.Name == ruleName {
					return onAlertRule(rule)
				}
			}

		}
	}
	return fmt.Errorf("project %q has no alert rule in group %q with name %q", name, groupName, ruleName)
}

func (store *remoteGitStorage) validateProjectPointer(ctx context.Context, ptr *typesv1.ProjectPointer) error {
	gitptr := ptr.GetRemote()
	if gitptr == nil {
		return fmt.Errorf("expecting a Git remote pointer, got a %T", ptr.Scheme)
	}
	return nil
}
