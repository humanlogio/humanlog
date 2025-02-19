package stdiosink

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/lucasb-eyer/go-colorful"
)

var DefaultDarkTheme = mustValidTheme(&typesv1.FormatConfig_Theme{
	Key:   fgstyle("#48df61"), // #48df61
	Value: fgstyle("#48df61"), // #48df61
	Time:  fgstyle("#ffffff"), // #ffffff
	Msg:   fgstyle("#ffffff"), // #ffffff
	Levels: &typesv1.FormatConfig_LevelStyle{
		Debug:   fgstyle("#6666ff"),              // #6666ff
		Info:    fgstyle("#00dd00"),              // #00dd00
		Warn:    fgstyle("#ff8800"),              // #ff8800
		Error:   fgstyle("#ff6a6a"),              // #ff6a6a
		Panic:   fgbdstyle("#ff6a6a", "#ffffff"), // #ff6a6a, #ffffff
		Fatal:   fgbdstyle("#ff6a6a", "#ffff00"), // #ff6a6a, #ffff00
		Unknown: fgstyle("#a9a9a9"),              // #a9a9a9
	},
	AbsentMsg:  fgstyle("#a9a9a9"),
	AbsentTime: fgstyle("#a9a9a9"),
})

var DefaultLightTheme = mustValidTheme(&typesv1.FormatConfig_Theme{
	Key:   fgstyle("#146e23"), // #146e23
	Value: fgstyle("#2f913f"), // #2f913f
	Time:  fgstyle("#393838"), // #393838
	Msg:   fgstyle("#575757"), // #575757
	Levels: &typesv1.FormatConfig_LevelStyle{
		Debug:   fgstyle("#6666ff"),              // #6666ff
		Info:    fgstyle("#00dd00"),              // #00dd00
		Warn:    fgstyle("#ff8800"),              // #ff8800
		Error:   fgstyle("#d82626"),              // #d82626
		Panic:   fgbdstyle("#d82626", "#ffffff"), // #d82626, #323232
		Fatal:   fgbdstyle("#d82626", "#ffff00"), // #d82626, #ffff00
		Unknown: fgstyle("#a9a9a9"),              // #a9a9a9
	},
	AbsentMsg:  fgstyle("#a9a9a9"),
	AbsentTime: fgstyle("#a9a9a9"),
})

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

func pbcolorToLipgloss(color *typesv1.FormatConfig_Color) (lipgloss.TerminalColor, error) {
	v, err := colorful.Hex(color.HtmlHexColor)
	if err != nil {
		return nil, err
	}
	return lipgloss.Color(v.Hex()), nil
}

func pbstyleToLipgloss(r *lipgloss.Renderer, style *typesv1.FormatConfig_Style) (lipgloss.Style, error) {
	st := r.NewStyle()
	if style == nil {
		return st, fmt.Errorf("no style defined")
	}
	if style.Background != nil {
		c, err := pbcolorToLipgloss(style.Background)
		if err != nil {
			return st, fmt.Errorf("`background`: %v", err)
		}
		st = st.Background(c)
	}
	if style.Foreground != nil {
		c, err := pbcolorToLipgloss(style.Foreground)
		if err != nil {
			return st, fmt.Errorf("`foreground`: %v", err)
		}
		st = st.Foreground(c)
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
	return st, nil
}

func ThemeFrom(r *lipgloss.Renderer, theme *typesv1.FormatConfig_Theme) (*Theme, error) {
	var err error
	out := &Theme{}
	out.Key, err = pbstyleToLipgloss(r, theme.GetKey())
	if err != nil {
		return nil, fmt.Errorf("style for `key` is invalid: %v", err)
	}
	out.Val, err = pbstyleToLipgloss(r, theme.GetValue())
	if err != nil {
		return nil, fmt.Errorf("style for `val` is invalid: %v", err)
	}
	out.Time, err = pbstyleToLipgloss(r, theme.GetTime())
	if err != nil {
		return nil, fmt.Errorf("style for `time` is invalid: %v", err)
	}
	out.TimeAbsent, err = pbstyleToLipgloss(r, theme.GetAbsentTime())
	if err != nil {
		return nil, fmt.Errorf("style for `time_absent` is invalid: %v", err)
	}
	out.Msg, err = pbstyleToLipgloss(r, theme.GetMsg())
	if err != nil {
		return nil, fmt.Errorf("style for `msg` is invalid: %v", err)
	}
	out.MsgAbsent, err = pbstyleToLipgloss(r, theme.GetAbsentMsg())
	if err != nil {
		return nil, fmt.Errorf("style for `msg_absent` is invalid: %v", err)
	}
	out.DebugLevel, err = pbstyleToLipgloss(r, theme.GetLevels().GetDebug())
	if err != nil {
		return nil, fmt.Errorf("style for `debug_level` is invalid: %v", err)
	}
	out.InfoLevel, err = pbstyleToLipgloss(r, theme.GetLevels().GetInfo())
	if err != nil {
		return nil, fmt.Errorf("style for `info_level` is invalid: %v", err)
	}
	out.WarnLevel, err = pbstyleToLipgloss(r, theme.GetLevels().GetWarn())
	if err != nil {
		return nil, fmt.Errorf("style for `warn_level` is invalid: %v", err)
	}
	out.ErrorLevel, err = pbstyleToLipgloss(r, theme.GetLevels().GetError())
	if err != nil {
		return nil, fmt.Errorf("style for `error_level` is invalid: %v", err)
	}
	out.PanicLevel, err = pbstyleToLipgloss(r, theme.GetLevels().GetPanic())
	if err != nil {
		return nil, fmt.Errorf("style for `panic_level` is invalid: %v", err)
	}
	out.FatalLevel, err = pbstyleToLipgloss(r, theme.GetLevels().GetFatal())
	if err != nil {
		return nil, fmt.Errorf("style for `fatal_level` is invalid: %v", err)
	}
	out.UnknownLevel, err = pbstyleToLipgloss(r, theme.GetLevels().GetUnknown())
	if err != nil {
		return nil, fmt.Errorf("style for `unknown_level` is invalid: %v", err)
	}
	return out, nil
}

func ValidateTheme(v *typesv1.FormatConfig_Theme) error {
	_, err := ThemeFrom(lipgloss.DefaultRenderer(), v)
	return err
}

func mustValidTheme(v *typesv1.FormatConfig_Theme) *typesv1.FormatConfig_Theme {
	_, err := ThemeFrom(lipgloss.DefaultRenderer(), v)
	if err != nil {
		panic(err)
	}
	return v
}

func fgstyle(hex string) *typesv1.FormatConfig_Style {
	return &typesv1.FormatConfig_Style{Foreground: &typesv1.FormatConfig_Color{HtmlHexColor: hex}}
}

func fgbdstyle(fg, bg string) *typesv1.FormatConfig_Style {
	return &typesv1.FormatConfig_Style{
		Foreground: &typesv1.FormatConfig_Color{HtmlHexColor: fg},
		Background: &typesv1.FormatConfig_Color{HtmlHexColor: bg},
	}
}
