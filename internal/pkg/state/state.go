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
)

var DefaultState = State{
	Version: 1,
}

func GetDefaultStateDirpath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("$HOME not set, can't determine a state dir path")
	}
	stateDirpath := filepath.Join(home, ".state", "humanlog")
	return stateDirpath, nil
}

func GetDefaultStateFilepath() (string, error) {
	stateDirpath, err := GetDefaultStateDirpath()
	if err != nil {
		return "", err
	}
	stateFilepath := filepath.Join(stateDirpath, "state.json")
	dfi, err := os.Stat(stateDirpath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("state dir %q can't be read: %v", stateDirpath, err)
	}
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(stateDirpath, 0700); err != nil {
			return "", fmt.Errorf("state dir %q can't be created: %v", stateDirpath, err)
		}
	} else if !dfi.IsDir() {
		return "", fmt.Errorf("state dir %q isn't a directory", stateDirpath)
	}
	ffi, err := os.Stat(stateFilepath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("can't stat state file: %v", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		// do nothing
	} else if !ffi.Mode().IsRegular() {
		return "", fmt.Errorf("state file %q isn't a regular file", stateFilepath)
	}
	return stateFilepath, nil
}

func ReadStateFile(path string, dflt *State) (*State, error) {
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
	if err := os.Rename(newf.Name(), path); err != nil {
		return fmt.Errorf("replacing statefile at %q with %q: %w", path, newf.Name(), err)
	}
	success = true
	return nil
}

type State struct {
	Version                      int             `json:"version"`
	AccountID                    *int64          `json:"account_id"`
	MachineID                    *int64          `json:"machine_id"`
	LatestKnownVersion           *semver.Version `json:"latest_known_version,omitempty"`
	LastestKnownVersionUpdatedAt *time.Time      `json:"latest_known_version_updated_at"`

	IngestionToken *typesv1.AccountToken `json:"ingestion_token,omitempty"`

	// preferences set in the CLI/TUI when querying
	CurrentOrgID     *int64 `json:"current_org_id,omitempty"`
	CurrentAccountID *int64 `json:"current_account_id,omitempty"`
	CurrentMachineID *int64 `json:"current_machine_id,omitempty"`

	// unexported
	path string
}

func (cfg *State) WriteBack() error {
	return WriteStateFile(cfg.path, cfg)
}

func (cfg State) populateEmpty(other *State) *State {
	ptr := &cfg
	out := *ptr
	if out.AccountID == nil && other.AccountID != nil {
		out.AccountID = other.AccountID
	}
	if out.MachineID == nil && other.MachineID != nil {
		out.MachineID = other.MachineID
	}
	if out.LatestKnownVersion == nil && other.LatestKnownVersion != nil {
		out.LatestKnownVersion = other.LatestKnownVersion
	}
	if out.LastestKnownVersionUpdatedAt == nil && other.LastestKnownVersionUpdatedAt != nil {
		out.LastestKnownVersionUpdatedAt = other.LastestKnownVersionUpdatedAt
	}
	return &out
}
