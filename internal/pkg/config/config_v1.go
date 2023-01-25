package config

type ConfigV1 struct {
	Version             int            `json:"version"`
	Skip                *[]string      `json:"skip"`
	Keep                *[]string      `json:"keep"`
	TimeFields          *[]string      `json:"time-fields"`
	MessageFields       *[]string      `json:"message-fields"`
	LevelFields         *[]string      `json:"level-fields"`
	SortLongest         *bool          `json:"sort-longest"`
	SkipUnchanged       *bool          `json:"skip-unchanged"`
	Truncates           *bool          `json:"truncates"`
	LightBg             *bool          `json:"light-bg"`
	ColorMode           *string        `json:"color-mode"`
	TruncateLength      *int           `json:"truncate-length"`
	TimeFormat          *string        `json:"time-format"`
	Palette             *TextPaletteV1 `json:"palette"`
	Interrupt           *bool          `json:"interrupt"`
	SkipCheckForUpdates *bool          `json:"skip_check_updates"`
}

type TextPaletteV1 struct {
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
