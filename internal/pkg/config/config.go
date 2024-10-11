package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var DefaultConfig = Config{
	Version:             1,
	Skip:                ptr([]string{}),
	Keep:                ptr([]string{}),
	TimeFields:          ptr([]string{"time", "ts", "@timestamp", "timestamp", "Timestamp"}),
	MessageFields:       ptr([]string{"message", "msg", "Body"}),
	LevelFields:         ptr([]string{"level", "lvl", "loglevel", "severity", "SeverityText"}),
	SortLongest:         ptr(true),
	SkipUnchanged:       ptr(true),
	Truncates:           ptr(true),
	LightBg:             ptr(false),
	ColorMode:           ptr("auto"),
	TruncateLength:      ptr(15),
	TimeFormat:          ptr(time.Stamp),
	Interrupt:           ptr(false),
	SkipCheckForUpdates: ptr(false),
	Palette:             nil,
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
		return dflt, nil
	}
	defer configFile.Close()
	var cfg Config
	if err := json.NewDecoder(configFile).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decoding config file: %v", err)
	}
	return cfg.populateEmpty(dflt), nil
}

type Config struct {
	Version             int          `json:"version"`
	Skip                *[]string    `json:"skip"`
	Keep                *[]string    `json:"keep"`
	TimeFields          *[]string    `json:"time-fields"`
	MessageFields       *[]string    `json:"message-fields"`
	LevelFields         *[]string    `json:"level-fields"`
	SortLongest         *bool        `json:"sort-longest"`
	SkipUnchanged       *bool        `json:"skip-unchanged"`
	Truncates           *bool        `json:"truncates"`
	LightBg             *bool        `json:"light-bg"`
	ColorMode           *string      `json:"color-mode"`
	TruncateLength      *int         `json:"truncate-length"`
	TimeFormat          *string      `json:"time-format"`
	TimeZone            *string      `json:"time-zone"`
	Palette             *TextPalette `json:"palette"`
	Interrupt           *bool        `json:"interrupt"`
	SkipCheckForUpdates *bool        `json:"skip_check_updates"`

	ExperimentalFeatures *Features `json:"experimental_features"`
}

type Features struct {
	SendLogsToCloud      *bool `json:"send_logs_to_cloud"`
	ServeLocalhostOnPort *int  `json:"serve_localhost_on_port"`
}

func (cfg Config) populateEmpty(other *Config) *Config {
	out := *(&cfg)
	if out.Skip == nil && out.Keep == nil {
		// skip and keep are mutually exclusive, so these are
		// either both set by default, or not at all
		out.Skip = other.Skip
		out.Keep = other.Keep
	}
	if other.TimeFields != nil {
		if out.TimeFields == nil {
			out.TimeFields = ptr(make([]string, 0, len(*other.TimeFields)))
		}
		*out.TimeFields = append(*out.TimeFields, *other.TimeFields...)
	}
	if out.MessageFields == nil && other.MessageFields != nil {
		if out.MessageFields == nil {
			out.MessageFields = ptr(make([]string, 0, len(*other.MessageFields)))
		}
		*out.MessageFields = append(*out.MessageFields, *other.MessageFields...)
	}
	if out.LevelFields == nil && other.LevelFields != nil {
		if out.LevelFields == nil {
			out.LevelFields = ptr(make([]string, 0, len(*other.LevelFields)))
		}
		*out.LevelFields = append(*out.LevelFields, *other.LevelFields...)
	}
	if out.SortLongest == nil && other.SortLongest != nil {
		out.SortLongest = other.SortLongest
	}
	if out.SkipUnchanged == nil && other.SkipUnchanged != nil {
		out.SkipUnchanged = other.SkipUnchanged
	}
	if out.Truncates == nil && other.Truncates != nil {
		out.Truncates = other.Truncates
	}
	if out.LightBg == nil && other.LightBg != nil {
		out.LightBg = other.LightBg
	}
	if out.ColorMode == nil && other.ColorMode != nil {
		out.ColorMode = other.ColorMode
	}
	if out.TruncateLength == nil && other.TruncateLength != nil {
		out.TruncateLength = other.TruncateLength
	}
	if out.TimeFormat == nil && other.TimeFormat != nil {
		out.TimeFormat = other.TimeFormat
	}
	if out.TimeZone == nil && other.TimeZone != nil {
		out.TimeZone = other.TimeZone
	}
	if out.Palette == nil && other.Palette != nil {
		out.Palette = other.Palette
	}
	if out.Interrupt == nil && other.Interrupt != nil {
		out.Interrupt = other.Interrupt
	}
	if out.SkipCheckForUpdates == nil && other.SkipCheckForUpdates != nil {
		out.SkipCheckForUpdates = other.SkipCheckForUpdates
	}
	if out.ExperimentalFeatures == nil && other.ExperimentalFeatures != nil {
		out.ExperimentalFeatures = other.ExperimentalFeatures
	}
	return &out
}

type TextPalette struct {
	KeyColor              []string `json:"key"`
	ValColor              []string `json:"val"`
	TimeLightBgColor      []string `json:"time_light_bg"`
	TimeDarkBgColor       []string `json:"time_dark_bg"`
	MsgLightBgColor       []string `json:"msg_light_bg"`
	MsgAbsentLightBgColor []string `json:"msg_absent_light_bg"`
	MsgDarkBgColor        []string `json:"msg_dark_bg"`
	MsgAbsentDarkBgColor  []string `json:"msg_absent_dark_bg"`
	DebugLevelColor       []string `json:"debug_level"`
	InfoLevelColor        []string `json:"info_level"`
	WarnLevelColor        []string `json:"warn_level"`
	ErrorLevelColor       []string `json:"error_level"`
	PanicLevelColor       []string `json:"panic_level"`
	FatalLevelColor       []string `json:"fatal_level"`
	UnknownLevelColor     []string `json:"unknown_level"`
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
