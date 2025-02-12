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

func UpgradeInPlace(ctx context.Context, curSemanticVersion string, baseSiteURL string, channelName *string, stdout, stderr io.Writer, stdin io.Reader, detach bool) error {

	curSemanticVersion = strings.ReplaceAll(curSemanticVersion, ".", "-")
	cur, renamed, err := renameCurrentBinaryWithSuffix("-" + curSemanticVersion)
	if err != nil {
		return fmt.Errorf("renaming current binary: %v", err)
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
	cmd.Env = append(cmd.Env, "INSIDE_HUMANLOG_SELF_UPDATE=true")
	if channelName != nil {
		cmd.Env = append(cmd.Env, fmt.Sprintf("HUMANLOG_CHANNEL=%s", *channelName))
	}
	if detach {
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting update command: %v", err)
		}
		if err := cmd.Process.Release(); err != nil {
			return fmt.Errorf("releasing update process: %v", err)
		}
		return nil
	} else {
		if err := cmd.Run(); err != nil {
			// failed to install new binary, rollback renamed binary's name to previous name
			os.Rename(renamed, cur)
			return fmt.Errorf("running update script: %v", err)
		}
		return nil
	}
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

func renameCurrentBinaryWithSuffix(suffix string) (string, string, error) {
	binaryPath, err := os.Executable()
	if err != nil {
		return binaryPath, "", err
	}
	newBinaryPath := binaryPath + suffix
	if err := os.Rename(binaryPath, newBinaryPath); err != nil {
		return binaryPath, newBinaryPath, fmt.Errorf("renaming current binary to %s: %v", newBinaryPath, err)
	}
	return binaryPath, newBinaryPath, nil
}
