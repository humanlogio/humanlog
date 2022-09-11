package humanlog

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

var DefaultPalette = &Palette{
	KeyColor:              color.New(color.FgGreen),
	ValColor:              color.New(color.FgHiWhite),
	TimeLightBgColor:      color.New(color.FgBlack),
	TimeDarkBgColor:       color.New(color.FgWhite),
	MsgLightBgColor:       color.New(color.FgBlack),
	MsgAbsentLightBgColor: color.New(color.FgHiBlack),
	MsgDarkBgColor:        color.New(color.FgHiWhite),
	MsgAbsentDarkBgColor:  color.New(color.FgWhite),
	DebugLevelColor:       color.New(color.FgMagenta),
	InfoLevelColor:        color.New(color.FgCyan),
	WarnLevelColor:        color.New(color.FgYellow),
	ErrorLevelColor:       color.New(color.FgRed),
	PanicLevelColor:       color.New(color.BgRed),
	FatalLevelColor:       color.New(color.BgHiRed, color.FgHiWhite),
	UnknownLevelColor:     color.New(color.FgMagenta),
}

type Palette struct {
	KeyColor              *color.Color
	ValColor              *color.Color
	TimeLightBgColor      *color.Color
	TimeDarkBgColor       *color.Color
	MsgLightBgColor       *color.Color
	MsgAbsentLightBgColor *color.Color
	MsgDarkBgColor        *color.Color
	MsgAbsentDarkBgColor  *color.Color
	DebugLevelColor       *color.Color
	InfoLevelColor        *color.Color
	WarnLevelColor        *color.Color
	ErrorLevelColor       *color.Color
	PanicLevelColor       *color.Color
	FatalLevelColor       *color.Color
	UnknownLevelColor     *color.Color
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

func (pl TextPalette) compile() (*Palette, error) {
	var err error
	out := &Palette{}
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

func (colorMode ColorMode) Apply() {
	switch colorMode {
	case ColorModeOff:
		color.NoColor = true
	case ColorModeOn:
		color.NoColor = false
	default:
		// 'Auto' default is applied as a global variable initializer function, so nothing
		// to do here.
	}
}
