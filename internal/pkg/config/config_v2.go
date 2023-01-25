package config

import "strings"

type ConfigV2 struct {
	Version             int         `json:"version"`
	Skip                *[]string   `json:"skip"`
	Keep                *[]string   `json:"keep"`
	Highlight           *[]string   `json:"highlight"`
	TimeFields          *[]string   `json:"time-fields"`
	MessageFields       *[]string   `json:"message-fields"`
	LevelFields         *[]string   `json:"level-fields"`
	SortLongest         *bool       `json:"sort-longest"`
	SkipUnchanged       *bool       `json:"skip-unchanged"`
	Truncates           *bool       `json:"truncates"`
	SelectedTheme       *string     `json:"selected-theme"`
	ColorMode           *string     `json:"color-mode"`
	TruncateLength      *int        `json:"truncate-length"`
	TimeFormat          *string     `json:"time-format"`
	Themes              *TextThemes `json:"themes"`
	Interrupt           *bool       `json:"interrupt"`
	SkipCheckForUpdates *bool       `json:"skip_check_updates"`
}

func getV2fromV1(v1 ConfigV1) *ConfigV2 {
	var selectedTheme *string
	if v1.LightBg != nil {
		if *v1.LightBg {
			selectedTheme = ptr("light")
		} else {
			selectedTheme = ptr("dark")
		}
	} else {
		selectedTheme = ptr("auto")
	}
	var palettes *TextThemes
	if v1.Palette != nil {
		old := v1.Palette
		palettes = &TextThemes{
			Light: getV2ThemeFromV1Palette(false, old),
			Dark:  getV2ThemeFromV1Palette(true, old),
		}
	}

	return &ConfigV2{
		Version:             2,
		Skip:                v1.Skip,
		Keep:                v1.Keep,
		Highlight:           nil,
		TimeFields:          v1.TimeFields,
		MessageFields:       v1.MessageFields,
		LevelFields:         v1.LevelFields,
		SortLongest:         v1.SortLongest,
		SkipUnchanged:       v1.SkipUnchanged,
		Truncates:           v1.Truncates,
		SelectedTheme:       selectedTheme,
		ColorMode:           v1.ColorMode,
		TruncateLength:      v1.TruncateLength,
		TimeFormat:          v1.TimeFormat,
		Themes:              palettes,
		Interrupt:           v1.Interrupt,
		SkipCheckForUpdates: v1.SkipCheckForUpdates,
	}
}

func (cfg ConfigV2) populateEmpty(other *ConfigV2) *ConfigV2 {
	out := *(&cfg)
	if out.Skip == nil && out.Keep == nil {
		// skip and keep are mutually exclusive, so these are
		// either both set by default, or not at all
		out.Skip = other.Skip
		out.Keep = other.Keep
	}
	if out.Highlight == nil && other.Highlight != nil {
		out.Highlight = other.Highlight
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
	if out.SelectedTheme == nil && other.SelectedTheme != nil {
		out.SelectedTheme = other.SelectedTheme
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
	if out.Themes == nil && other.Themes != nil {
		out.Themes = other.Themes
	}
	if out.Interrupt == nil && other.Interrupt != nil {
		out.Interrupt = other.Interrupt
	}
	if out.SkipCheckForUpdates == nil && other.SkipCheckForUpdates != nil {
		out.SkipCheckForUpdates = other.SkipCheckForUpdates
	}
	return &out
}

type TextThemes struct {
	Light *TextThemeV2 `json:"light"`
	Dark  *TextThemeV2 `json:"dark"`
}

type TextThemeV2 struct {
	KeyColor          string `json:"key"`
	ValColor          string `json:"val"`
	TimeColor         string `json:"time"`
	MsgColor          string `json:"msg"`
	MsgAbsentColor    string `json:"msg_absent"`
	DebugLevelColor   string `json:"debug_level"`
	InfoLevelColor    string `json:"info_level"`
	WarnLevelColor    string `json:"warn_level"`
	ErrorLevelColor   string `json:"error_level"`
	PanicLevelColor   string `json:"panic_level"`
	FatalLevelColor   string `json:"fatal_level"`
	UnknownLevelColor string `json:"unknown_level"`
}

func getV2ThemeFromV1Palette(isDark bool, v1 *TextPaletteV1) *TextThemeV2 {
	var (
		timeColor      []string
		msgColor       []string
		msgAbsentColor []string
	)
	if isDark {
		timeColor = v1.TimeDarkBgColor
		msgColor = v1.MsgDarkBgColor
		msgAbsentColor = v1.MsgAbsentDarkBgColor
	} else {
		timeColor = v1.TimeLightBgColor
		msgColor = v1.MsgLightBgColor
		msgAbsentColor = v1.MsgAbsentLightBgColor
	}
	return &TextThemeV2{
		KeyColor:          strings.Join(v1.KeyColor, ","),
		ValColor:          strings.Join(v1.ValColor, ","),
		TimeColor:         strings.Join(timeColor, ","),
		MsgColor:          strings.Join(msgColor, ","),
		MsgAbsentColor:    strings.Join(msgAbsentColor, ","),
		DebugLevelColor:   strings.Join(v1.DebugLevelColor, ","),
		InfoLevelColor:    strings.Join(v1.InfoLevelColor, ","),
		WarnLevelColor:    strings.Join(v1.WarnLevelColor, ","),
		ErrorLevelColor:   strings.Join(v1.ErrorLevelColor, ","),
		PanicLevelColor:   strings.Join(v1.PanicLevelColor, ","),
		FatalLevelColor:   strings.Join(v1.FatalLevelColor, ","),
		UnknownLevelColor: strings.Join(v1.UnknownLevelColor, ","),
	}
}
