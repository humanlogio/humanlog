package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetDefaultStateFilepath(t *testing.T) {
	tmp, err := os.MkdirTemp(os.TempDir(), "*")
	require.NoError(t, err)

	if runtime.GOOS == "windows" {
		err = os.Setenv("USERPROFILE", tmp)
	} else {
		err = os.Setenv("HOME", tmp) // set HOME env value to tmp for this test
	}
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.Equal(t, tmp, home) // check temporary home directory set as desired

	got, err := GetDefaultStateFilepath()
	require.NoError(t, err)
	want := filepath.Join("~", ".state", "humanlog", "state.json")
	require.Equal(t, want, got)
}
