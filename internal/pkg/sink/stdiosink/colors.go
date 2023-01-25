package stdiosink

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/muesli/termenv"
)

var (
	DefaultLightTheme = func(output *termenv.Output) *Theme {
		return &Theme{
			KeyColor:          fatihColorerFn(color.New(color.FgGreen)),
			ValColor:          fatihColorerFn(color.New(color.FgHiWhite)),
			TimeBgColor:       fatihColorerFn(color.New(color.FgBlack)),
			MsgBgColor:        fatihColorerFn(color.New(color.FgBlack)),
			MsgAbsentBgColor:  fatihColorerFn(color.New(color.FgHiBlack)),
			DebugLevelColor:   fatihColorerFn(color.New(color.FgMagenta)),
			InfoLevelColor:    fatihColorerFn(color.New(color.FgCyan)),
			WarnLevelColor:    fatihColorerFn(color.New(color.FgYellow)),
			ErrorLevelColor:   fatihColorerFn(color.New(color.FgRed)),
			PanicLevelColor:   fatihColorerFn(color.New(color.BgRed)),
			FatalLevelColor:   fatihColorerFn(color.New(color.BgHiRed, color.FgHiWhite)),
			UnknownLevelColor: fatihColorerFn(color.New(color.FgMagenta)),
		}
	}
	DefaultDarkTheme = func(output *termenv.Output) *Theme {
		return &Theme{
			KeyColor:          fatihColorerFn(color.New(color.FgGreen)),
			ValColor:          fatihColorerFn(color.New(color.FgHiWhite)),
			TimeBgColor:       fatihColorerFn(color.New(color.FgWhite)),
			MsgBgColor:        fatihColorerFn(color.New(color.FgHiWhite)),
			MsgAbsentBgColor:  fatihColorerFn(color.New(color.FgWhite)),
			DebugLevelColor:   fatihColorerFn(color.New(color.FgMagenta)),
			InfoLevelColor:    fatihColorerFn(color.New(color.FgCyan)),
			WarnLevelColor:    fatihColorerFn(color.New(color.FgYellow)),
			ErrorLevelColor:   fatihColorerFn(color.New(color.FgRed)),
			PanicLevelColor:   fatihColorerFn(color.New(color.BgRed)),
			FatalLevelColor:   fatihColorerFn(color.New(color.BgHiRed, color.FgHiWhite)),
			UnknownLevelColor: fatihColorerFn(color.New(color.FgMagenta)),
		}
	}
)

type ColorerFn func(string) string

func fatihColorerFn(cl *color.Color) ColorerFn {
	return func(in string) string { return cl.Sprint(in) }
}

type Theme struct {
	KeyColor          ColorerFn
	ValColor          ColorerFn
	TimeBgColor       ColorerFn
	MsgBgColor        ColorerFn
	MsgAbsentBgColor  ColorerFn
	DebugLevelColor   ColorerFn
	InfoLevelColor    ColorerFn
	WarnLevelColor    ColorerFn
	ErrorLevelColor   ColorerFn
	PanicLevelColor   ColorerFn
	FatalLevelColor   ColorerFn
	UnknownLevelColor ColorerFn
}

func ThemeFrom(output *termenv.Output, theme *config.TextThemeV2) (*Theme, error) {
	var err error
	out := &Theme{}
	out.KeyColor, err = parseColor(output, theme.KeyColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "key", err)
	}
	out.ValColor, err = parseColor(output, theme.ValColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "val", err)
	}
	out.TimeBgColor, err = parseColor(output, theme.TimeColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "time", err)
	}
	out.MsgBgColor, err = parseColor(output, theme.MsgColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "msg", err)
	}
	out.MsgAbsentBgColor, err = parseColor(output, theme.MsgAbsentColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "msg_absent", err)
	}
	out.DebugLevelColor, err = parseColor(output, theme.DebugLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "debug_level", err)
	}
	out.InfoLevelColor, err = parseColor(output, theme.InfoLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "info_level", err)
	}
	out.WarnLevelColor, err = parseColor(output, theme.WarnLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "warn_level", err)
	}
	out.ErrorLevelColor, err = parseColor(output, theme.ErrorLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "error_level", err)
	}
	out.PanicLevelColor, err = parseColor(output, theme.PanicLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "panic_level", err)
	}
	out.FatalLevelColor, err = parseColor(output, theme.FatalLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "fatal_level", err)
	}
	out.UnknownLevelColor, err = parseColor(output, theme.UnknownLevelColor)
	if err != nil {
		return nil, fmt.Errorf("in theme key %q, %v", "unknown_level", err)
	}
	return out, err
}

func parseColor(output *termenv.Output, csnames string) (ColorerFn, error) {
	names := strings.Split(csnames, ",")

	switch len(names) {
	case 0:
		return func(s string) string { return s }, nil
	case 1:
		name := names[0]
		if attr, ok := colorAttributeIndex[name]; ok {
			return fatihColorerFn(color.New(attr)), nil
		}
		c := output.Color(name)
		if c == nil {
			return nil, fmt.Errorf("not a valid color: %q", name)
		}
		return func(s string) string {
			return output.String(s).Foreground(c).String()
		}, nil
	case 2:
		fg, bg := names[0], names[1]
		fgAttr, ok := fgColorAttributeIndex[fg]
		if ok {
			bgAttr, ok := bgColorAttributeIndex[bg]
			if !ok {
				return nil, fmt.Errorf("color %q isn't supported", bg)
			}
			return fatihColorerFn(color.New(fgAttr, bgAttr)), nil
		}
		fgc := output.Color(fg)
		if fgc == nil {
			return nil, fmt.Errorf("not a valid foreground color: %q", fg)
		}
		bgc := output.Color(bg)
		if bgc == nil {
			return nil, fmt.Errorf("not a valid background color: %q", bg)
		}
		return func(s string) string {
			return output.String(s).Foreground(fgc).Background(bgc).String()
		}, nil
	default:
		return nil, fmt.Errorf("colors must be in the form \"foreground,background\", e.g.: \"#ffffff,#0000ff\"")
	}
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

var fgColorAttributeIndex = map[string]color.Attribute{
	"black":      color.FgBlack,
	"red":        color.FgRed,
	"green":      color.FgGreen,
	"yellow":     color.FgYellow,
	"blue":       color.FgBlue,
	"magenta":    color.FgMagenta,
	"cyan":       color.FgCyan,
	"white":      color.FgWhite,
	"hi_black":   color.FgHiBlack,
	"hi_red":     color.FgHiRed,
	"hi_green":   color.FgHiGreen,
	"hi_yellow":  color.FgHiYellow,
	"hi_blue":    color.FgHiBlue,
	"hi_magenta": color.FgHiMagenta,
	"hi_cyan":    color.FgHiCyan,
	"hi_white":   color.FgHiWhite,
}

var bgColorAttributeIndex = map[string]color.Attribute{
	"black":      color.BgBlack,
	"red":        color.BgRed,
	"green":      color.BgGreen,
	"yellow":     color.BgYellow,
	"blue":       color.BgBlue,
	"magenta":    color.BgMagenta,
	"cyan":       color.BgCyan,
	"white":      color.BgWhite,
	"hi_black":   color.BgHiBlack,
	"hi_red":     color.BgHiRed,
	"hi_green":   color.BgHiGreen,
	"hi_yellow":  color.BgHiYellow,
	"hi_blue":    color.BgHiBlue,
	"hi_magenta": color.BgHiMagenta,
	"hi_cyan":    color.BgHiCyan,
	"hi_white":   color.BgHiWhite,
}
