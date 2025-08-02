package config

import (
	"encoding/json"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

type registeredVersion struct {
	constructor func() any
	parse       func(p []byte, v any) error
	migrate     func(any) (any, int, error)
}

const currentConfigVersion = 2

type CurrentConfig = typesv1.LocalhostConfig

var versioned = map[int]registeredVersion{
	0: {
		// before introduction of the version field
		constructor: func() any { return new(deprecatedV1Config) },
		parse: func(p []byte, v any) error {
			return json.Unmarshal(p, v)
		},
		migrate: func(a any) (any, int, error) {
			old := a.(*deprecatedV1Config)
			old.Version = 1
			return old, 1, nil
		},
	},
	1: {
		// before turning into a proto value
		constructor: func() any { return new(deprecatedV1Config) },
		parse: func(p []byte, v any) error {
			return json.Unmarshal(p, v)
		},
		migrate: func(a any) (any, int, error) {
			old := a.(*deprecatedV1Config)
			return migrateV1toV2(old, 2), 2, nil
		},
	},
	2: {
		// after exposing via localhost api
		constructor: func() any { return new(typesv1.LocalhostConfig) },
		parse: func(p []byte, v any) error {
			return protojson.UnmarshalOptions{AllowPartial: true, DiscardUnknown: true}.Unmarshal(p, v.(*typesv1.LocalhostConfig))
		},
	},
}

func migrateV1toV2(old *deprecatedV1Config, v int) *typesv1.LocalhostConfig {
	fmtCfg := &typesv1.FormatConfig{
		SortLongest:   old.SortLongest,
		SkipUnchanged: old.SkipUnchanged,
		Time: &typesv1.FormatConfig_Time{
			Format:   old.TimeFormat,
			Timezone: old.TimeZone,
		},
	}
	if old.Skip != nil {
		fmtCfg.SkipFields = *old.Skip
	}
	if old.Keep != nil {
		fmtCfg.KeepFields = *old.Keep
	}
	if old.Truncates != nil && *old.Truncates {
		fmtCfg.Truncation = &typesv1.FormatConfig_Truncation{Length: 15}
		if old.TruncateLength != nil {
			fmtCfg.Truncation.Length = int64(*old.TruncateLength)
		}
	}
	parseCfg := &typesv1.ParseConfig{
		Timestamp: &typesv1.ParseConfig_Time{},
		Message:   &typesv1.ParseConfig_Message{},
		Level:     &typesv1.ParseConfig_Level{},
	}
	if old.TimeFields != nil {
		parseCfg.Timestamp.FieldNames = *old.TimeFields
	}
	if old.MessageFields != nil {
		parseCfg.Message.FieldNames = *old.MessageFields
	}
	if old.LevelFields != nil {
		parseCfg.Level.FieldNames = *old.LevelFields
	}
	runtimeCfg := &typesv1.RuntimeConfig{
		Interrupt:            old.Interrupt,
		SkipCheckForUpdates:  old.SkipCheckForUpdates,
		Features:             &typesv1.RuntimeConfig_Features{},
		ExperimentalFeatures: &typesv1.RuntimeConfig_ExperimentalFeatures{},
	}
	if old.ExperimentalFeatures != nil {
		if old.ExperimentalFeatures.ReleaseChannel != nil {
			runtimeCfg.ExperimentalFeatures.ReleaseChannel = old.ExperimentalFeatures.ReleaseChannel
		}
		if old.ExperimentalFeatures.SendLogsToCloud != nil {
			runtimeCfg.ExperimentalFeatures.SendLogsToCloud = old.ExperimentalFeatures.SendLogsToCloud
		}
		if old.ExperimentalFeatures.ServeLocalhost != nil {
			oldslh := old.ExperimentalFeatures.ServeLocalhost
			cfg, err := structpb.NewStruct(oldslh.Cfg)
			if err != nil {
				panic(err)
			}
			runtimeCfg.ExperimentalFeatures.ServeLocalhost = &typesv1.ServeLocalhostConfig{
				Port:          int64(oldslh.Port),
				Engine:        oldslh.Engine,
				EngineConfig:  cfg,
				ShowInSystray: oldslh.ShowInSystray,
				LogDir:        oldslh.LogDir,
			}
		}
	}

	cfg := &typesv1.LocalhostConfig{
		Version:   int64(v),
		Formatter: fmtCfg,
		Parser:    parseCfg,
		Runtime:   runtimeCfg,
	}
	return cfg
}

type deprecatedV1Config struct {
	Version             int                      `json:"version"`
	Skip                *[]string                `json:"skip"`
	Keep                *[]string                `json:"keep"`
	TimeFields          *[]string                `json:"time-fields"`
	MessageFields       *[]string                `json:"message-fields"`
	LevelFields         *[]string                `json:"level-fields"`
	SortLongest         *bool                    `json:"sort-longest"`
	SkipUnchanged       *bool                    `json:"skip-unchanged"`
	Truncates           *bool                    `json:"truncates"`
	LightBg             *bool                    `json:"light-bg"`
	ColorMode           *string                  `json:"color-mode"`
	TruncateLength      *int                     `json:"truncate-length"`
	TimeFormat          *string                  `json:"time-format"`
	TimeZone            *string                  `json:"time-zone"`
	Palette             *deprecatedV1TextPalette `json:"palette"`
	Interrupt           *bool                    `json:"interrupt"`
	SkipCheckForUpdates *bool                    `json:"skip_check_updates"`

	ExperimentalFeatures *deprecatedV1Features `json:"experimental_features"`
}

type deprecatedV1Features struct {
	ReleaseChannel  *string                     `json:"release_channel"`
	SendLogsToCloud *bool                       `json:"send_logs_to_cloud"`
	ServeLocalhost  *deprecatedV1ServeLocalhost `json:"serve_localhost"`
}

type deprecatedV1ServeLocalhost struct {
	Port          int                    `json:"port"`
	Engine        string                 `json:"engine"`
	Cfg           map[string]interface{} `json:"engine_config"`
	ShowInSystray *bool                  `json:"show_in_systray"`
	LogDir        *string                `json:"log_dir"`
}

type deprecatedV1TextPalette struct {
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
