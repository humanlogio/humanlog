package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config = ConfigV2

var DefaultConfig = Config{
	Version:             2,
	Skip:                ptr([]string{}),
	Keep:                ptr([]string{}),
	TimeFields:          ptr([]string{"time", "ts", "@timestamp", "timestamp"}),
	MessageFields:       ptr([]string{"message", "msg"}),
	LevelFields:         ptr([]string{"level", "lvl", "loglevel", "severity"}),
	SortLongest:         ptr(true),
	SkipUnchanged:       ptr(true),
	Truncates:           ptr(true),
	ColorMode:           ptr("auto"),
	TruncateLength:      ptr(15),
	TimeFormat:          ptr(time.Stamp),
	Interrupt:           ptr(false),
	SkipCheckForUpdates: ptr(false),
	Themes:              nil,
}

func GetDefaultConfigFilepath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("$HOME not set, can't determine a config file path")
	}
	configDirpath := filepath.Join(home, ".config", "humanlog")
	configFilepath := filepath.Join(configDirpath, "config.json")
	dfi, err := os.Stat(configDirpath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("config dir %q can't be read: %v", configDirpath, err)
	}
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(configDirpath, 0700); err != nil {
			return "", fmt.Errorf("config dir %q can't be created: %v", configDirpath, err)
		}
	} else if !dfi.IsDir() {
		return "", fmt.Errorf("config dir %q isn't a directory", configDirpath)
	}
	ffi, err := os.Stat(configFilepath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("can't stat config file: %v", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		// do nothing
	} else if !ffi.Mode().IsRegular() {
		return "", fmt.Errorf("config file %q isn't a regular file", configFilepath)
	}
	return configFilepath, nil
}

func ReadConfigFile(path string, dflt *Config) (*Config, error) {
	configFile, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("opening config file %q: %v", path, err)
		}

		cfgContent, err := json.MarshalIndent(dflt, "", "\t")
		if err != nil {
			return nil, fmt.Errorf("marshaling default config file: %v", err)
		}
		if err := ioutil.WriteFile(path, cfgContent, 0600); err != nil {
			return nil, fmt.Errorf("writing default to config file %q: %v", path, err)
		}
		return dflt, nil
	}
	defer configFile.Close()

	data, err := ioutil.ReadAll(configFile)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg, err := readConfigAndUpdate(data)
	if err != nil {
		return nil, fmt.Errorf("decoding config file: %w", err)
	}
	if cfg == nil {
		cfg = new(Config)
	}
	return cfg.populateEmpty(dflt), nil
}

func readConfigAndUpdate(data []byte) (*ConfigV2, error) {
	var versionCheck *ConfigVersioner
	if err := json.Unmarshal(data, &versionCheck); err != nil {
		return nil, fmt.Errorf("verifying config version: %w", err)
	}
	switch versionCheck.Version {
	case 1:
		var v1 ConfigV1
		if err := json.Unmarshal(data, &v1); err != nil {
			return nil, err
		}
		return getV2fromV1(v1), nil
	case 2:
		var v2 ConfigV2
		return &v2, json.Unmarshal(data, &v2)
	default:
		return nil, nil
	}
}

type ConfigVersioner struct {
	Version int `json:"version"`
}

type ColorMode int

const (
	ColorModeOff ColorMode = iota
	ColorModeOn
	ColorModeAuto
)

func GrokColorMode(colorMode string) (ColorMode, error) {
	switch strings.ToLower(colorMode) {
	case "on", "always", "force", "true", "yes", "1":
		return ColorModeOn, nil
	case "off", "never", "false", "no", "0":
		return ColorModeOff, nil
	case "auto", "tty", "maybe", "":
		return ColorModeAuto, nil
	default:
		return ColorModeAuto, fmt.Errorf("'%s' is not a color mode (try 'on', 'off' or 'auto')", colorMode)
	}
}

func ptr[T any](v T) *T {
	return &v
}
