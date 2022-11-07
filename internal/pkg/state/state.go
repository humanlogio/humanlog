package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

var DefaultState = State{
	Version: 1,
}

func GetDefaultStateFilepath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("$HOME not set, can't determine a state file path")
	}
	stateDirpath := filepath.Join(home, ".state", "humanlog")
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
		return dflt, WriteStateFile(path, dflt)
	}
	defer stateFile.Close()
	var cfg State
	if err := json.NewDecoder(stateFile).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decoding state file: %v", err)
	}
	return cfg.populateEmpty(dflt), nil
}

func WriteStateFile(path string, state *State) error {
	content, err := json.MarshalIndent(state, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling state file: %v", err)
	}
	if err := ioutil.WriteFile(path, content, 0600); err != nil {
		return fmt.Errorf("writing to state file %q: %v", path, err)
	}
	return nil
}

type State struct {
	Version   int    `json:"version"`
	AccountID *int64 `json:"account_id"`
	MachineID *int64 `json:"machine_id"`
}

func (cfg State) populateEmpty(other *State) *State {
	out := *(&cfg)
	if out.AccountID == nil && other.AccountID != nil {
		out.AccountID = other.AccountID
	}
	if out.MachineID == nil && other.MachineID != nil {
		out.MachineID = other.MachineID
	}
	return &out
}
