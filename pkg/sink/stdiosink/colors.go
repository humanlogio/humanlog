package stdiosink

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	typesv1 "github.com/humanlogio/api/go/types/v1"
)

var DefaultLightTheme = func(r *lipgloss.Renderer) Theme {
	return Theme{
		Key:          r.NewStyle().Foreground(lipgloss.ANSIColor(32)),
		Val:          r.NewStyle().Foreground(lipgloss.ANSIColor(97)),
		Time:         r.NewStyle().Foreground(lipgloss.ANSIColor(30)),
		TimeAbsent:   r.NewStyle().Foreground(lipgloss.ANSIColor(90)),
		Msg:          r.NewStyle().Foreground(lipgloss.ANSIColor(30)),
		MsgAbsent:    r.NewStyle().Foreground(lipgloss.ANSIColor(90)),
		DebugLevel:   r.NewStyle().Foreground(lipgloss.ANSIColor(35)),
		InfoLevel:    r.NewStyle().Foreground(lipgloss.ANSIColor(36)),
		WarnLevel:    r.NewStyle().Foreground(lipgloss.ANSIColor(33)),
		ErrorLevel:   r.NewStyle().Foreground(lipgloss.ANSIColor(31)),
		PanicLevel:   r.NewStyle().Background(lipgloss.ANSIColor(41)),
		FatalLevel:   r.NewStyle().Foreground(lipgloss.ANSIColor(97)).Background(lipgloss.ANSIColor(101)),
		UnknownLevel: r.NewStyle().Foreground(lipgloss.ANSIColor(35)),
	}
}

var DefaultDarkTheme = func(r *lipgloss.Renderer) Theme {
	return Theme{
		Key:          r.NewStyle().Foreground(lipgloss.ANSIColor(32)),
		Val:          r.NewStyle().Foreground(lipgloss.ANSIColor(97)),
		Time:         r.NewStyle().Foreground(lipgloss.ANSIColor(37)),
		TimeAbsent:   r.NewStyle().Foreground(lipgloss.ANSIColor(37)),
		Msg:          r.NewStyle().Foreground(lipgloss.ANSIColor(97)),
		MsgAbsent:    r.NewStyle().Foreground(lipgloss.ANSIColor(37)),
		DebugLevel:   r.NewStyle().Foreground(lipgloss.ANSIColor(35)),
		InfoLevel:    r.NewStyle().Foreground(lipgloss.ANSIColor(36)),
		WarnLevel:    r.NewStyle().Foreground(lipgloss.ANSIColor(33)),
		ErrorLevel:   r.NewStyle().Foreground(lipgloss.ANSIColor(31)),
		PanicLevel:   r.NewStyle().Background(lipgloss.ANSIColor(41)),
		FatalLevel:   r.NewStyle().Foreground(lipgloss.ANSIColor(97)).Background(lipgloss.ANSIColor(101)),
		UnknownLevel: r.NewStyle().Foreground(lipgloss.ANSIColor(35)),
	}
}

type Theme struct {
	Key          lipgloss.Style
	Val          lipgloss.Style
	Time         lipgloss.Style
	TimeAbsent   lipgloss.Style
	Msg          lipgloss.Style
	MsgAbsent    lipgloss.Style
	DebugLevel   lipgloss.Style
	InfoLevel    lipgloss.Style
	WarnLevel    lipgloss.Style
	ErrorLevel   lipgloss.Style
	PanicLevel   lipgloss.Style
	FatalLevel   lipgloss.Style
	UnknownLevel lipgloss.Style
}

func pbcolorToLipgloss(color *typesv1.FormatConfig_Color) lipgloss.TerminalColor {
	v := color.Rgba
	r := v >> 24
	g := v >> 16
	b := v >> 8
	a := v
	_ = a // alpha is ignored
	hex := fmt.Sprintf("#%x%x%x", r, g, b)
	return lipgloss.Color(hex)
}

func pbstyleToLipgloss(r *lipgloss.Renderer, style *typesv1.FormatConfig_Style) lipgloss.Style {
	st := r.NewStyle()
	if style.Background != nil {
		st = st.Background(pbcolorToLipgloss(style.Background))
	}
	if style.Foreground != nil {
		st = st.Foreground(pbcolorToLipgloss(style.Foreground))
	}
	if style.Bold != nil {
		st = st.Bold(*style.Bold)
	}
	if style.Italic != nil {
		st = st.Italic(*style.Italic)
	}
	if style.Faint != nil {
		st = st.Faint(*style.Faint)
	}
	if style.Blink != nil {
		st = st.Blink(*style.Blink)
	}
	if style.Strikethrough != nil {
		st = st.Strikethrough(*style.Strikethrough)
	}
	if style.Underline != nil {
		st = st.Underline(*style.Underline)
	}
	return st
}

func ThemeFrom(r *lipgloss.Renderer, theme *typesv1.FormatConfig_Theme) Theme {
	return Theme{
		Key:          pbstyleToLipgloss(r, theme.GetKey()),
		Val:          pbstyleToLipgloss(r, theme.GetValue()),
		Time:         pbstyleToLipgloss(r, theme.GetTime()),
		TimeAbsent:   pbstyleToLipgloss(r, theme.GetAbsentTime()),
		Msg:          pbstyleToLipgloss(r, theme.GetMsg()),
		MsgAbsent:    pbstyleToLipgloss(r, theme.GetAbsentMsg()),
		DebugLevel:   pbstyleToLipgloss(r, theme.GetLevels().GetDebug()),
		InfoLevel:    pbstyleToLipgloss(r, theme.GetLevels().GetInfo()),
		WarnLevel:    pbstyleToLipgloss(r, theme.GetLevels().GetWarn()),
		ErrorLevel:   pbstyleToLipgloss(r, theme.GetLevels().GetError()),
		PanicLevel:   pbstyleToLipgloss(r, theme.GetLevels().GetPanic()),
		FatalLevel:   pbstyleToLipgloss(r, theme.GetLevels().GetFatal()),
		UnknownLevel: pbstyleToLipgloss(r, theme.GetLevels().GetUnknown()),
	}
}
