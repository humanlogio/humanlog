// fork from https://github.com/superfly/flyctl/blob/master/internal/update/update.go

package selfupdate

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cli/safeexec"
)

func UpgradeInPlace(ctx context.Context, baseSiteURL string, channelName *string, stdout, stderr io.Writer, stdin io.Reader) error {
	if runtime.GOOS == "windows" {
		if err := renameCurrentBinaries(); err != nil {
			return err
		}
	}

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

	cmd := exec.Command(shellToUse, switchToUse, command)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = stdin
	if channelName != nil {
		cmd.Env = append(cmd.Env, fmt.Sprintf("HUMANLOG_CHANNEL=%s", *channelName))
	}
	return cmd.Run()
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

// can't replace binary on windows, need to move
func renameCurrentBinaries() error {
	binaries, err := currentWindowsBinaries()
	if err != nil {
		return err
	}

	for _, p := range binaries {
		if err := os.Rename(p, p+".old"); err != nil {
			return err
		}
	}

	return nil
}

func currentWindowsBinaries() ([]string, error) {
	binaryPath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	return []string{binaryPath}, nil
}
