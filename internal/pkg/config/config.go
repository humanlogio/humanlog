package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/sink/stdiosink"
	"google.golang.org/protobuf/proto"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

func init() {
	_, err := GetDefaultConfig("")
	if err != nil {
		panic(err)
	}
	_, err = GetDefaultLocalhostConfig()
	if err != nil {
		panic(err)
	}
}

const (
	runLocalhostByDefault    = true
	sendLogsToCloudByDefault = false
)

func GetDefaultConfig(releaseChannel string) (*Config, error) {
	var (
		serveLocalhostCfg *typesv1.ServeLocalhostConfig
		sendLogsToCloud   *bool
		err               error
	)
	if runLocalhostByDefault {
		serveLocalhostCfg, err = GetDefaultLocalhostConfig()
		if err != nil {
			return nil, err
		}
	}
	if sendLogsToCloudByDefault {
		sendLogsToCloud = ptr(true)
	}
	return &Config{
		Version: currentConfigVersion,
		CurrentConfig: &typesv1.LocalhostConfig{
			Version: currentConfigVersion,
			Formatter: &typesv1.FormatConfig{
				Themes: &typesv1.FormatConfig_Themes{
					Light: stdiosink.DefaultLightTheme,
					Dark:  stdiosink.DefaultDarkTheme,
				},
				SkipFields:    nil,
				KeepFields:    nil,
				SortLongest:   ptr(true),
				SkipUnchanged: ptr(true),
				Truncation:    nil,
				Time: &typesv1.FormatConfig_Time{
					Format: ptr(time.StampMilli),
				},
				TerminalColorMode: typesv1.FormatConfig_COLORMODE_AUTO.Enum(),
			},
			Parser: &typesv1.ParseConfig{
				Timestamp: &typesv1.ParseConfig_Time{
					FieldNames: []string{"time", "ts", "@timestamp", "timestamp", "Timestamp"},
				},
				Message: &typesv1.ParseConfig_Message{
					FieldNames: []string{"message", "msg", "Body"},
				},
				Level: &typesv1.ParseConfig_Level{
					FieldNames: []string{"level", "lvl", "loglevel", "severity", "SeverityText"},
				},
			},
			Runtime: &typesv1.RuntimeConfig{
				Interrupt:           ptr(false),
				SkipCheckForUpdates: ptr(false),
				Features:            &typesv1.RuntimeConfig_Features{},
				ExperimentalFeatures: &typesv1.RuntimeConfig_ExperimentalFeatures{
					ReleaseChannel:  &releaseChannel,
					SendLogsToCloud: sendLogsToCloud,
					ServeLocalhost:  serveLocalhostCfg,
				},
			},
		},
	}, nil
}

func GetDefaultLocalhostConfig() (*typesv1.ServeLocalhostConfig, error) {
	stateDir, err := state.GetDefaultStateDirpath()
	if err != nil {
		return nil, err
	}
	dbpath := filepath.Join(stateDir, "data", "db.humanlog")
	logDir := filepath.Join(stateDir, "logs")

	engineConfig, err := structpb.NewStruct(map[string]any{
		"path": dbpath,
	})
	if err != nil {
		return nil, err
	}
	return &typesv1.ServeLocalhostConfig{
		Port:          32764,
		Engine:        "advanced",
		EngineConfig:  engineConfig,
		ShowInSystray: ptr(true),
		LogDir:        ptr(logDir),
		Otlp: &typesv1.ServeLocalhostConfig_OTLP{
			GrpcPort: 4317,
			HttpPort: 4318,
		},
	}, nil
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

func ReadConfigFile(path string, dflt *Config, writebackIfMigrated bool) (*Config, error) {
	if dflt.path == "" {
		dflt.path = path
	}
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
	cfg.path = path
	if cfg.migrated && writebackIfMigrated {
		if err := cfg.WriteBack(); err != nil {
			return nil, fmt.Errorf("writing back migrated config file: %v", err)
		}
	}
	return cfg.populateEmpty(dflt), nil
}

func WriteConfigFile(path string, config *Config) error {
	content, err := json.MarshalIndent(config, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling config file: %v", err)
	}

	newf, err := os.CreateTemp(filepath.Dir(path), "humanlog_configfile")
	if err != nil {
		return fmt.Errorf("creating temporary file for configfile: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.Remove(newf.Name())
		}
	}()
	if _, err := newf.Write(content); err != nil {
		return fmt.Errorf("writing to temporary configfile: %w", err)
	}
	if err := newf.Close(); err != nil {
		return fmt.Errorf("closing temporary configfile: %w", err)
	}
	if err := os.Chmod(newf.Name(), 0600); err != nil {
		return fmt.Errorf("setting permissions on temporary configfile: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("ensuring target parent dir exists: %v", err)
	}
	if err := os.Rename(newf.Name(), path); err != nil {
		return fmt.Errorf("replacing configfile at %q with %q: %w", path, newf.Name(), err)
	}
	success = true
	return nil
}

type Config struct {
	Version int `json:"version"`
	*CurrentConfig
	// unexported, the filepath where the `Config` get's serialized and saved to
	path     string
	migrated bool
}

var _ json.Unmarshaler = (*Config)(nil)

func (cfg *Config) UnmarshalJSON(p []byte) error {
	type versionLookup struct {
		Version int `json:"version"`
	}
	var lookup versionLookup
	if err := json.Unmarshal(p, &lookup); err != nil {
		return err
	}
	version := lookup.Version
	reg, ok := versioned[version]
	if !ok {
		return fmt.Errorf("unsupported config version %d", version)
	}
	v := reg.constructor()
	if err := reg.parse(p, v); err != nil {
		return err
	}
	for version != currentConfigVersion {
		cfg.migrated = true
		next, nextV, err := reg.migrate(v)
		if err != nil {
			return fmt.Errorf("migrating from config version %d to %d: %v", version, nextV, err)
		}
		nextReg, ok := versioned[nextV]
		if !ok {
			return fmt.Errorf("deadend, no way to go from version %d to %d", version, nextV)
		}
		reg = nextReg
		v = next
		version = nextV
	}
	// we reached the current config format
	cfg.Version = version
	cfg.CurrentConfig = v.(*CurrentConfig)
	return nil
}

func (cfg *Config) WriteBack() error {
	return WriteConfigFile(cfg.path, cfg)
}

func (cfg Config) populateEmpty(other *Config) *Config {
	out := &Config{Version: cfg.Version, path: cfg.path}
	if out.CurrentConfig == nil {
		out.CurrentConfig = new(typesv1.LocalhostConfig)
	}
	if other.path != "" {
		out.path = other.path
	}
	out.CurrentConfig = mergeLocalhostConfig(other.CurrentConfig, cfg.CurrentConfig)
	return out
}

func mergeLocalhostConfig(prev, next *typesv1.LocalhostConfig) *typesv1.LocalhostConfig {
	out := proto.Clone(prev).(*typesv1.LocalhostConfig)
	if out == nil {
		out = new(typesv1.LocalhostConfig)
	}
	if next == nil {
		return out
	}
	if next.Formatter != nil {
		out.Formatter = mergeFormatter(out.Formatter, next.Formatter)
	}
	if next.Parser != nil {
		out.Parser = mergeParser(out.Parser, next.Parser)
	}
	if next.Runtime != nil {
		out.Runtime = mergeRuntime(out.Runtime, next.Runtime)
	}
	return out
}

func mergeFormatter(prev, next *typesv1.FormatConfig) *typesv1.FormatConfig {
	out := proto.Clone(prev).(*typesv1.FormatConfig)
	if out == nil {
		out = new(typesv1.FormatConfig)
	}
	if next.Themes != nil {
		out.Themes = mergeThemes(prev.GetThemes(), next.Themes)
	}
	if next.SkipFields != nil {
		out.SkipFields = mergeStringSlices(prev.GetSkipFields(), next.SkipFields)
	}
	if next.SkipFields != nil {
		out.SkipFields = mergeStringSlices(prev.GetSkipFields(), next.SkipFields)
	}
	if next.SortLongest != nil {
		out.SortLongest = next.SortLongest
	}
	if next.SkipUnchanged != nil {
		out.SkipUnchanged = next.SkipUnchanged
	}
	if next.Truncation != nil {
		out.Truncation = mergeFormatTruncation(prev.GetTruncation(), next.Truncation)
	}
	if next.Time != nil {
		out.Time = mergeFormatTime(prev.GetTime(), next.Time)
	}
	if next.Message != nil {
		out.Message = mergeFormatMessage(prev.GetMessage(), next.Message)
	}
	if next.TerminalColorMode != nil {
		out.TerminalColorMode = next.TerminalColorMode
	}
	return out
}

func mergeParser(prev, next *typesv1.ParseConfig) *typesv1.ParseConfig {
	out := proto.Clone(prev).(*typesv1.ParseConfig)
	if out == nil {
		out = new(typesv1.ParseConfig)
	}
	if next.Timestamp != nil {
		out.Timestamp = mergeParseTimestamp(prev.GetTimestamp(), next.Timestamp)
	}
	if next.Message != nil {
		out.Message = mergeParseMessage(prev.GetMessage(), next.Message)
	}
	if next.Level != nil {
		out.Level = mergeParseLevel(prev.GetLevel(), next.Level)
	}
	return out
}

func mergeRuntime(prev, next *typesv1.RuntimeConfig) *typesv1.RuntimeConfig {
	out := proto.Clone(prev).(*typesv1.RuntimeConfig)
	if out == nil {
		out = new(typesv1.RuntimeConfig)
	}
	if next.Interrupt != nil {
		out.Interrupt = next.Interrupt
	}
	if next.SkipCheckForUpdates != nil {
		out.SkipCheckForUpdates = next.SkipCheckForUpdates
	}
	if next.Features != nil {
		out.Features = mergeRuntimeFeatures(prev.GetFeatures(), next.Features)
	}
	if next.ExperimentalFeatures != nil {
		out.ExperimentalFeatures = mergeRuntimeExperimentalFeatures(prev.GetExperimentalFeatures(), next.ExperimentalFeatures)
	}
	if next.ApiClient != nil {
		out.ApiClient = mergeRuntimeClientConfig(prev.GetApiClient(), next.ApiClient)
	}
	return out
}
func mergeThemes(prev, next *typesv1.FormatConfig_Themes) *typesv1.FormatConfig_Themes {
	out := proto.Clone(prev).(*typesv1.FormatConfig_Themes)
	if out == nil {
		out = new(typesv1.FormatConfig_Themes)
	}
	if next.Light != nil {
		out.Light = mergeTheme(prev.GetLight(), next.Light)
	}
	if next.Dark != nil {
		out.Dark = mergeTheme(prev.GetDark(), next.Dark)
	}
	return out
}
func mergeTheme(prev, next *typesv1.FormatConfig_Theme) *typesv1.FormatConfig_Theme {
	out := proto.Clone(prev).(*typesv1.FormatConfig_Theme)
	if out == nil {
		out = new(typesv1.FormatConfig_Theme)
	}
	if next.Key != nil {
		out.Key = mergeStyle(prev.GetKey(), next.Key)
	}
	if next.Value != nil {
		out.Value = mergeStyle(prev.GetValue(), next.Value)
	}
	if next.Time != nil {
		out.Time = mergeStyle(prev.GetTime(), next.Time)
	}
	if next.Msg != nil {
		out.Msg = mergeStyle(prev.GetMsg(), next.Msg)
	}
	if next.Levels != nil {
		out.Levels = mergeLevelStyle(prev.GetLevels(), next.Levels)
	}
	if next.AbsentMsg != nil {
		out.AbsentMsg = mergeStyle(prev.GetAbsentMsg(), next.AbsentMsg)
	}
	if next.AbsentTime != nil {
		out.AbsentTime = mergeStyle(prev.GetAbsentTime(), next.AbsentTime)
	}
	return out
}
func mergeStyle(prev, next *typesv1.FormatConfig_Style) *typesv1.FormatConfig_Style {
	// we don't merge, we just overwrite. otherwise the behavior will be confusing af
	return proto.Clone(next).(*typesv1.FormatConfig_Style)
}
func mergeLevelStyle(prev, next *typesv1.FormatConfig_LevelStyle) *typesv1.FormatConfig_LevelStyle {
	// we don't merge, we just overwrite. otherwise the behavior will be confusing af
	return proto.Clone(next).(*typesv1.FormatConfig_LevelStyle)
}
func mergeStringSlices(prev, next []string) []string {
	if len(next) != 0 {
		// we don't merge, we just overwrite. otherwise the behavior will be confusing af
		return next
	}
	return prev
}
func mergeFormatTruncation(prev, next *typesv1.FormatConfig_Truncation) *typesv1.FormatConfig_Truncation {
	// we don't merge, we just overwrite. otherwise the behavior will be confusing af
	return proto.Clone(next).(*typesv1.FormatConfig_Truncation)
}
func mergeFormatTime(prev, next *typesv1.FormatConfig_Time) *typesv1.FormatConfig_Time {
	out := proto.Clone(prev).(*typesv1.FormatConfig_Time)
	if out == nil {
		out = new(typesv1.FormatConfig_Time)
	}
	if next.Format != nil {
		out.Format = next.Format
	}
	if next.Timezone != nil {
		out.Timezone = next.Timezone
	}
	if next.AbsentDefaultValue != nil {
		out.AbsentDefaultValue = next.AbsentDefaultValue
	}
	return out
}
func mergeFormatMessage(prev, next *typesv1.FormatConfig_Message) *typesv1.FormatConfig_Message {
	out := proto.Clone(prev).(*typesv1.FormatConfig_Message)
	if out == nil {
		out = new(typesv1.FormatConfig_Message)
	}
	if next.AbsentDefaultValue != nil {
		out.AbsentDefaultValue = next.AbsentDefaultValue
	}
	return out
}
func mergeParseTimestamp(prev, next *typesv1.ParseConfig_Time) *typesv1.ParseConfig_Time {
	out := proto.Clone(prev).(*typesv1.ParseConfig_Time)
	if out == nil {
		out = new(typesv1.ParseConfig_Time)
	}
	if next.FieldNames != nil {
		out.FieldNames = mergeStringSlices(prev.GetFieldNames(), next.FieldNames)
	}
	return out
}
func mergeParseMessage(prev, next *typesv1.ParseConfig_Message) *typesv1.ParseConfig_Message {
	out := proto.Clone(prev).(*typesv1.ParseConfig_Message)
	if out == nil {
		out = new(typesv1.ParseConfig_Message)
	}
	if next.FieldNames != nil {
		out.FieldNames = mergeStringSlices(prev.GetFieldNames(), next.FieldNames)
	}
	return out
}
func mergeParseLevel(prev, next *typesv1.ParseConfig_Level) *typesv1.ParseConfig_Level {
	out := proto.Clone(prev).(*typesv1.ParseConfig_Level)
	if out == nil {
		out = new(typesv1.ParseConfig_Level)
	}
	if next.FieldNames != nil {
		out.FieldNames = mergeStringSlices(prev.GetFieldNames(), next.FieldNames)
	}
	return out
}
func mergeRuntimeFeatures(prev, next *typesv1.RuntimeConfig_Features) *typesv1.RuntimeConfig_Features {
	out := proto.Clone(next).(*typesv1.RuntimeConfig_Features)
	return out
}
func mergeRuntimeExperimentalFeatures(prev, next *typesv1.RuntimeConfig_ExperimentalFeatures) *typesv1.RuntimeConfig_ExperimentalFeatures {
	out := proto.Clone(prev).(*typesv1.RuntimeConfig_ExperimentalFeatures)
	if out == nil {
		out = new(typesv1.RuntimeConfig_ExperimentalFeatures)
	}
	if next.ReleaseChannel != nil {
		out.ReleaseChannel = next.ReleaseChannel
	}
	if next.SendLogsToCloud != nil {
		out.SendLogsToCloud = next.SendLogsToCloud
	}
	if next.ServeLocalhost != nil {
		out.ServeLocalhost = mergeRuntimeServeLocalhostConfig(prev.GetServeLocalhost(), next.ServeLocalhost)
	}
	return out
}

func mergeRuntimeClientConfig(prev, next *typesv1.RuntimeConfig_ClientConfig) *typesv1.RuntimeConfig_ClientConfig {
	out := proto.Clone(prev).(*typesv1.RuntimeConfig_ClientConfig)
	if out == nil {
		out = new(typesv1.RuntimeConfig_ClientConfig)
	}
	if next.HttpProtocol != nil {
		out.HttpProtocol = next.HttpProtocol
	}
	if next.RpcProtocol != nil {
		out.RpcProtocol = next.RpcProtocol
	}
	return out
}

func mergeRuntimeServeLocalhostConfig(prev, next *typesv1.ServeLocalhostConfig) *typesv1.ServeLocalhostConfig {
	out := proto.Clone(prev).(*typesv1.ServeLocalhostConfig)
	if out == nil {
		out = new(typesv1.ServeLocalhostConfig)
	}
	// next overrides everything, but not
	// - ShowInSystray
	// - LogDir
	out.Port = next.Port
	out.Engine = next.Engine
	out.EngineConfig = next.EngineConfig
	if next.ShowInSystray != nil {
		out.ShowInSystray = next.ShowInSystray
	}
	if next.LogDir != nil {
		out.LogDir = next.LogDir
	}
	if next.Otlp != nil {
		out.Otlp = mergeRuntimeServeLocalhostConfigOltp(prev.GetOtlp(), next.Otlp)
	}
	return out
}

func mergeRuntimeServeLocalhostConfigOltp(prev, next *typesv1.ServeLocalhostConfig_OTLP) *typesv1.ServeLocalhostConfig_OTLP {
	out := proto.Clone(prev).(*typesv1.ServeLocalhostConfig_OTLP)
	if out == nil {
		out = new(typesv1.ServeLocalhostConfig_OTLP)
	}
	out.GrpcPort = next.GrpcPort
	out.HttpPort = next.HttpPort
	return out
}

func ParseColorMode(colorMode string) (typesv1.FormatConfig_ColorMode, error) {
	switch strings.ToLower(colorMode) {
	case "on", "always", "force", "true", "yes", "1":
		return typesv1.FormatConfig_COLORMODE_ENABLED, nil
	case "off", "never", "false", "no", "0":
		return typesv1.FormatConfig_COLORMODE_DISABLED, nil
	case "auto", "tty", "maybe", "":
		return typesv1.FormatConfig_COLORMODE_AUTO, nil
	case "dark":
		return typesv1.FormatConfig_COLORMODE_FORCE_DARK, nil
	case "light":
		return typesv1.FormatConfig_COLORMODE_FORCE_LIGHT, nil
	default:
		return typesv1.FormatConfig_COLORMODE_AUTO, fmt.Errorf("'%s' is not a color mode (try one of ['on', 'off', 'auto', 'dark', 'light'])", colorMode)
	}
}

func ptr[T any](v T) *T {
	return &v
}
