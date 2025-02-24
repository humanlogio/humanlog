// fork from https://github.com/superfly/flyctl/blob/master/internal/update/update.go

package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/blang/semver"
	"github.com/cli/safeexec"
)

func UpgradeInPlace(ctx context.Context, curSV semver.Version, baseSiteURL string, channelName *string, stdout, stderr io.Writer, stdin io.Reader) error {
	shellToUse, ok := os.LookupEnv("SHELL")
	switchToUse := "-c"

	if !ok {
		if runtime.GOOS == "windows" {
			shellToUse = "powershell.exe"
			switchToUse = "-Command"
		} else {
			shellToUse = "/bin/bash"
		}
	}

	command := updateCommand(baseSiteURL)

	tmp, err := os.MkdirTemp("", "*")
	if err != nil {
		return fmt.Errorf("making temporary directory in order to install new binary: %v", err)
	}
	defer os.RemoveAll(tmp)

	cmd := exec.Command(shellToUse, switchToUse, command)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = stdin
	cmd.Env = append(cmd.Env, "INSIDE_HUMANLOG_SELF_UPDATE=true")
	cmd.Env = append(cmd.Env, fmt.Sprintf("HUMANLOG_INSTALL=%s", tmp))
	if channelName != nil {
		cmd.Env = append(cmd.Env, fmt.Sprintf("HUMANLOG_CHANNEL=%s", *channelName))
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running update script: %v", err)
	}
	currentBinaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %v", err)
	}
	newBinaryPath := filepath.Join(tmp, "bin", "humanlog") // command 'install.sh | sh' makes new directory 'bin' under the 'HUMANLOG_INSTALL' path
	_, err = os.Stat(newBinaryPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("new binary not exists: %v", err)
		}
		return fmt.Errorf("stat file at %s: %v", newBinaryPath, err)
	}
	if err := os.Rename(newBinaryPath, currentBinaryPath); err != nil {
		return fmt.Errorf("replace current binary with new one: %v", err)
	}
	return nil
}

func isUnderHomebrew() bool {
	binary, err := os.Executable()
	if err != nil {
		return false
	}

	brewExe, err := safeexec.LookPath("brew")
	if err != nil {
		return false
	}

	brewPrefixBytes, err := exec.Command(brewExe, "--prefix").Output()
	if err != nil {
		return false
	}

	brewBinPrefix := filepath.Join(strings.TrimSpace(string(brewPrefixBytes)), "bin") + string(filepath.Separator)
	return strings.HasPrefix(binary, brewBinPrefix)
}

func updateCommand(baseSiteURL string) string {
	if isUnderHomebrew() {
		return "brew upgrade humanlog"
	}

	if runtime.GOOS == "windows" {
		return fmt.Sprintf("iwr %s/install.ps1 -useb | iex", baseSiteURL)
	} else {
		return fmt.Sprintf(`curl -sSL "%s/install.sh" | sh`, baseSiteURL)
	}
}
