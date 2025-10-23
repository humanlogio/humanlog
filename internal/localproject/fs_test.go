package localproject

import (
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/require"
)

func TestLocalStorage(t *testing.T) {
	constructor := func(t *testing.T, files map[string]string, dashboardDir, alertDir string) (projectStorage, *typesv1.ProjectPointer, func()) {
		fs := setupLocalFilesystem(t, files)
		store := setupLocalStorage(t, fs)
		ptr := mkLocalProjectPointer(dashboardDir, alertDir)
		return store, ptr, func() {}
	}

	runStorageTestSuite(t, constructor)
}

func setupLocalFilesystem(t *testing.T, files map[string]string) billy.Filesystem {
	t.Helper()
	fs := memfs.New()

	for path, content := range files {
		err := writeFile(fs, path, []byte(content))
		require.NoError(t, err)
	}

	return fs
}

func setupLocalStorage(t *testing.T, fs billy.Filesystem) *localGitStorage {
	t.Helper()
	now := time.Date(2025, 10, 22, 10, 0, 0, 0, time.UTC)
	timeNow := func() time.Time { return now }
	return newLocalGitStorage(nil, fs, parseQuery, timeNow)
}

func mkLocalProjectPointer(dashboardDir, alertDir string) *typesv1.ProjectPointer {
	return &typesv1.ProjectPointer{
		Scheme: &typesv1.ProjectPointer_Localhost{
			Localhost: &typesv1.ProjectPointer_LocalGit{
				Path:         "",
				DashboardDir: dashboardDir,
				AlertDir:     alertDir,
				ReadOnly:     false,
			},
		},
	}
}
