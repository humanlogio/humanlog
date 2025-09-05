package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/blang/semver"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/mitchellh/go-homedir"
)

var DefaultState = State{
	Version: 1,
}

func GetDefaultStateDirpath() (string, error) {
	_, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("$HOME not set, can't determine a state dir path")
	}
	stateDirpath := filepath.Join("~", ".state", "humanlog")
	return stateDirpath, nil
}

func GetDefaultStateFilepath() (string, error) {
	stateDirpath, err := GetDefaultStateDirpath()
	if err != nil {
		return "", err
	}
	stateDirpathExpanded, err := homedir.Expand(stateDirpath)
	if err != nil {
		return "", err
	}
	dfi, err := os.Stat(stateDirpathExpanded)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("state dir %q can't be read: %v", stateDirpathExpanded, err)
	}
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(stateDirpathExpanded, 0700); err != nil {
			return "", fmt.Errorf("state dir %q can't be created: %v", stateDirpathExpanded, err)
		}
	} else if !dfi.IsDir() {
		return "", fmt.Errorf("state dir %q isn't a directory", stateDirpathExpanded)
	}
	stateFilepath := filepath.Join(stateDirpath, "state.json")
	stateFilepathExpanded := filepath.Join(stateDirpathExpanded, "state.json")
	ffi, err := os.Stat(stateFilepathExpanded)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("can't stat state file: %v", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		// do nothing
	} else if !ffi.Mode().IsRegular() {
		return "", fmt.Errorf("state file %q isn't a regular file", stateFilepathExpanded)
	}
	return stateFilepath, nil
}

func ReadStateFile(path string, dflt *State) (*State, error) {
	path, err := homedir.Expand(path)
	if err != nil {
		return nil, err
	}
	stateFile, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("opening state file %q: %v", path, err)
		}
		cfg := (State{path: path}).populateEmpty(dflt)
		return cfg, WriteStateFile(path, cfg)
	}
	defer stateFile.Close()
	var cfg State
	if err := json.NewDecoder(stateFile).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decoding state file: %v", err)
	}
	cfg.path = path
	return cfg.populateEmpty(dflt), nil
}

func WriteStateFile(path string, state *State) error {
	content, err := json.MarshalIndent(state, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling state file: %v", err)
	}
	path, err = homedir.Expand(path)
	if err != nil {
		return err
	}
	newf, err := os.CreateTemp(filepath.Dir(path), "humanlog_statefile")
	if err != nil {
		return fmt.Errorf("creating temporary file for statefile: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.Remove(newf.Name())
		}
	}()
	if _, err := newf.Write(content); err != nil {
		return fmt.Errorf("writing to temporary statefile: %w", err)
	}
	if err := newf.Close(); err != nil {
		return fmt.Errorf("closing temporary statefile: %w", err)
	}
	if err := os.Chmod(newf.Name(), 0600); err != nil {
		return fmt.Errorf("setting permissions on temporary statefile: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("ensuring target parent dir exists: %v", err)
	}
	if err := os.Rename(newf.Name(), path); err != nil {
		return fmt.Errorf("replacing statefile at %q with %q: %w", path, newf.Name(), err)
	}
	success = true
	return nil
}

type State struct {
	// to convert across old vs. new version of `type State struct`
	Version int `json:"version"`

	// set for ingestion purpose
	MachineID      *int64                    `json:"machine_id"`
	IngestionToken *typesv1.EnvironmentToken `json:"ingestion_token,omitempty"`

	// update mechanism
	LatestKnownVersion           *semver.Version `json:"latest_known_version,omitempty"`
	LastestKnownVersionUpdatedAt *time.Time      `json:"latest_known_version_updated_at"`

	// prompts
	LastPromptedToSignupAt          *time.Time `json:"last_prompted_to_signup_at"`
	LastPromptedToEnableLocalhostAt *time.Time `json:"last_prompted_to_enable_localhost_at"`

	LoggedInUsername *string `json:"logged_in_username"`

	// preferences set in the CLI/TUI when querying
	CurrentEnvironmentID *int64 `json:"current_environment_id,omitempty"`
	CurrentMachineID     *int64 `json:"current_machine_id,omitempty"`

	// unexported, the filepath where the `State` get's serialized and saved to
	path string
}

func (cfg *State) WriteBack() error {
	return WriteStateFile(cfg.path, cfg)
}

func (cfg State) populateEmpty(other *State) *State {
	ptr := &cfg
	out := *ptr
	if out.MachineID == nil && other.MachineID != nil {
		out.MachineID = other.MachineID
	}
	if out.LatestKnownVersion == nil && other.LatestKnownVersion != nil {
		out.LatestKnownVersion = other.LatestKnownVersion
	}
	if out.LastestKnownVersionUpdatedAt == nil && other.LastestKnownVersionUpdatedAt != nil {
		out.LastestKnownVersionUpdatedAt = other.LastestKnownVersionUpdatedAt
	}
	if out.CurrentEnvironmentID == nil && other.CurrentEnvironmentID != nil {
		out.CurrentEnvironmentID = other.CurrentEnvironmentID
	}
	if out.CurrentMachineID == nil && other.CurrentMachineID != nil {
		out.CurrentMachineID = other.CurrentMachineID
	}
	return &out
}
