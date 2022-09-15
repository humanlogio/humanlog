package humanlog

import (
	"github.com/fatih/color"
)

var DefaultPalette = Palette{
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
