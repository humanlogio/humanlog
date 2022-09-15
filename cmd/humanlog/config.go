package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aybabtme/humanlog"
	"github.com/fatih/color"
)

var defaultConfig = &Config{
	Version:        1,
	Skip:           ptr([]string{}),
	Keep:           ptr([]string{}),
	TimeFields:     ptr([]string{"time", "ts", "@timestamp", "timestamp"}),
	MessageFields:  ptr([]string{"message", "msg"}),
	LevelFields:    ptr([]string{"level", "lvl", "loglevel", "severity"}),
	SortLongest:    ptr(true),
	SkipUnchanged:  ptr(true),
	Truncates:      ptr(true),
	LightBg:        ptr(false),
	ColorFlag:      ptr("auto"),
	TruncateLength: ptr(15),
	TimeFormat:     ptr(time.Stamp),
	Interrupt:      ptr(false),
	Palette:        nil,
}

var _ = defaultConfig.toHandlerOptions() // ensure it's valid

func getDefaultConfigFilepath() (string, error) {
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

func readConfigFile(path string, dflt *Config) (*Config, error) {
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
	var cfg Config
	if err := json.NewDecoder(configFile).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decoding config file: %v", err)
	}
	return cfg.populateEmpty(dflt), nil
}

type Config struct {
	Version        int          `json:"version"`
	Skip           *[]string    `json:"skip"`
	Keep           *[]string    `json:"keep"`
	TimeFields     *[]string    `json:"time-fields"`
	MessageFields  *[]string    `json:"message-fields"`
	LevelFields    *[]string    `json:"level-fields"`
	SortLongest    *bool        `json:"sort-longest"`
	SkipUnchanged  *bool        `json:"skip-unchanged"`
	Truncates      *bool        `json:"truncates"`
	LightBg        *bool        `json:"light-bg"`
	ColorFlag      *string      `json:"color-mode"`
	TruncateLength *int         `json:"truncate-length"`
	TimeFormat     *string      `json:"time-format"`
	Palette        *TextPalette `json:"palette"`
	Interrupt      *bool        `json:"interrupt"`
}

func (cfg Config) populateEmpty(other *Config) *Config {
	out := *(&cfg)
	if out.Skip == nil && out.Keep == nil {
		// skip and keep are mutually exclusive, so these are
		// either both set by default, or not at all
		out.Skip = other.Skip
		out.Keep = other.Keep
	}
	if out.TimeFields == nil && other.TimeFields != nil {
		out.TimeFields = other.TimeFields
	}
	if out.MessageFields == nil && other.MessageFields != nil {
		out.MessageFields = other.MessageFields
	}
	if out.LevelFields == nil && other.LevelFields != nil {
		out.LevelFields = other.LevelFields
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
	if out.ColorFlag == nil && other.ColorFlag != nil {
		out.ColorFlag = other.ColorFlag
	}
	if out.TruncateLength == nil && other.TruncateLength != nil {
		out.TruncateLength = other.TruncateLength
	}
	if out.TimeFormat == nil && other.TimeFormat != nil {
		out.TimeFormat = other.TimeFormat
	}
	if out.Palette == nil && other.Palette != nil {
		out.Palette = other.Palette
	}
	return &out
}

func (cfg Config) toHandlerOptions() *humanlog.HandlerOptions {
	opts := humanlog.DefaultOptions
	if cfg.Skip != nil {
		opts.Skip = sliceToSet(cfg.Skip)
	}
	if cfg.Keep != nil {
		opts.Keep = sliceToSet(cfg.Keep)
	}
	if cfg.TimeFields != nil {
		opts.TimeFields = *cfg.TimeFields
	}
	if cfg.MessageFields != nil {
		opts.MessageFields = *cfg.MessageFields
	}
	if cfg.LevelFields != nil {
		opts.LevelFields = *cfg.LevelFields
	}
	if cfg.SortLongest != nil {
		opts.SortLongest = *cfg.SortLongest
	}
	if cfg.SkipUnchanged != nil {
		opts.SkipUnchanged = *cfg.SkipUnchanged
	}
	if cfg.Truncates != nil {
		opts.Truncates = *cfg.Truncates
	}
	if cfg.LightBg != nil {
		opts.LightBg = *cfg.LightBg
	}
	if cfg.TruncateLength != nil {
		opts.TruncateLength = *cfg.TruncateLength
	}
	if cfg.TimeFormat != nil {
		opts.TimeFormat = *cfg.TimeFormat
	}
	if cfg.Palette != nil {
		pl, err := cfg.Palette.compile()
		if err != nil {
			log.Printf("invalid palette, using default one: %v", err)
		} else {
			opts.Palette = *pl
		}
	}
	return opts
}

func sliceToSet(arr *[]string) map[string]struct{} {
	if arr == nil {
		return nil
	}
	out := make(map[string]struct{})
	for _, key := range *arr {
		out[key] = struct{}{}
	}
	return out
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

func (pl TextPalette) compile() (*humanlog.Palette, error) {
	var err error
	out := &humanlog.Palette{}
	out.KeyColor, err = attributesToColor(pl.KeyColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "key", err)
	}
	out.ValColor, err = attributesToColor(pl.ValColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "val", err)
	}
	out.TimeLightBgColor, err = attributesToColor(pl.TimeLightBgColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "time_light_bg", err)
	}
	out.TimeDarkBgColor, err = attributesToColor(pl.TimeDarkBgColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "time_dark_bg", err)
	}
	out.MsgLightBgColor, err = attributesToColor(pl.MsgLightBgColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "msg_light_bg", err)
	}
	out.MsgAbsentLightBgColor, err = attributesToColor(pl.MsgAbsentLightBgColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "msg_absent_light_bg", err)
	}
	out.MsgDarkBgColor, err = attributesToColor(pl.MsgDarkBgColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "msg_dark_bg", err)
	}
	out.MsgAbsentDarkBgColor, err = attributesToColor(pl.MsgAbsentDarkBgColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "msg_absent_dark_bg", err)
	}
	out.DebugLevelColor, err = attributesToColor(pl.DebugLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "debug_level", err)
	}
	out.InfoLevelColor, err = attributesToColor(pl.InfoLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "info_level", err)
	}
	out.WarnLevelColor, err = attributesToColor(pl.WarnLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "warn_level", err)
	}
	out.ErrorLevelColor, err = attributesToColor(pl.ErrorLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "error_level", err)
	}
	out.PanicLevelColor, err = attributesToColor(pl.PanicLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "panic_level", err)
	}
	out.FatalLevelColor, err = attributesToColor(pl.FatalLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "fatal_level", err)
	}
	out.UnknownLevelColor, err = attributesToColor(pl.UnknownLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in palette key %q, %v", "unknown_level", err)
	}
	return out, err
}

func attributesToColor(names []string) (*color.Color, error) {
	attrs := make([]color.Attribute, 0, len(names))
	for _, name := range names {
		attr, ok := colorAttributeIndex[name]
		if !ok {
			return nil, fmt.Errorf("color %q isn't supported", name)
		}
		attrs = append(attrs, attr)
	}
	return color.New(attrs...), nil
}

var colorAttributeIndex = map[string]color.Attribute{
	"fg_black":      color.FgBlack,
	"fg_red":        color.FgRed,
	"fg_green":      color.FgGreen,
	"fg_yellow":     color.FgYellow,
	"fg_blue":       color.FgBlue,
	"fg_magenta":    color.FgMagenta,
	"fg_cyan":       color.FgCyan,
	"fg_white":      color.FgWhite,
	"fg_hi_black":   color.FgHiBlack,
	"fg_hi_red":     color.FgHiRed,
	"fg_hi_green":   color.FgHiGreen,
	"fg_hi_yellow":  color.FgHiYellow,
	"fg_hi_blue":    color.FgHiBlue,
	"fg_hi_magenta": color.FgHiMagenta,
	"fg_hi_cyan":    color.FgHiCyan,
	"fg_hi_white":   color.FgHiWhite,
	"bg_black":      color.BgBlack,
	"bg_red":        color.BgRed,
	"bg_green":      color.BgGreen,
	"bg_yellow":     color.BgYellow,
	"bg_blue":       color.BgBlue,
	"bg_magenta":    color.BgMagenta,
	"bg_cyan":       color.BgCyan,
	"bg_white":      color.BgWhite,
	"bg_hi_black":   color.BgHiBlack,
	"bg_hi_red":     color.BgHiRed,
	"bg_hi_green":   color.BgHiGreen,
	"bg_hi_yellow":  color.BgHiYellow,
	"bg_hi_blue":    color.BgHiBlue,
	"bg_hi_magenta": color.BgHiMagenta,
	"bg_hi_cyan":    color.BgHiCyan,
	"bg_hi_white":   color.BgHiWhite,
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
