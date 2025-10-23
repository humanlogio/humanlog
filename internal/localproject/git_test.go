package localproject

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRemoteGitStorage(t *testing.T) {
	constructor := func(t *testing.T, files map[string]string, dashboardDir, alertDir string) (projectStorage, *typesv1.ProjectPointer, func()) {
		r, _ := mkGitRepoWithFiles(t, files)
		store := setupRemoteGitStorage(t, r, dashboardDir, alertDir)
		ptr := mkProjectPointer(dashboardDir, alertDir)
		return store, ptr, func() {}
	}

	runStorageTestSuite(t, constructor)
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
		projectStatus.Errors = append(projectStatus.Errors, errProjectDashboardDirMissing(ptr.DashboardDir).Error())
	}
	if _, err := w.Filesystem.Stat(ptr.AlertDir); errors.Is(err, os.ErrNotExist) {
		projectStatus.Errors = append(projectStatus.Errors, errProjectAlertDirMissing(ptr.AlertDir).Error())
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
