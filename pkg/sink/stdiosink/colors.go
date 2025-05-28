package stdiosink

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/lucasb-eyer/go-colorful"
)

var defaultDarkLogTheme = &typesv1.FormatConfig_LogTheme{
	Key:   fgstyle("#48df61"), // #48df61
	Value: fgstyle("#8c887c"), // #8c887c
	Time:  fgstyle("#9e9e9e"), // #9e9e9e
	Msg:   fgstyle("#ffffff"), // #ffffff
	Levels: &typesv1.FormatConfig_LevelStyle{
		Debug:   fgstyle("#d33682"),              // #d33682
		Info:    fgstyle("#2aa198"),              // #2aa198
		Warn:    fgstyle("#ff8800"),              // #ff8800
		Error:   fgstyle("#ff6a6a"),              // #ff6a6a
		Panic:   fgbgstyle("#ff6a6a", "#ffffff"), // #ff6a6a, #ffffff
		Fatal:   fgbgstyle("#ff6a6a", "#ffff00"), // #ff6a6a, #ffff00
		Unknown: fgstyle("#a9a9a9"),              // #a9a9a9
	},
	AbsentMsg:  fgstyle("#a9a9a9"), // #a9a9a9
	AbsentTime: fgstyle("#a9a9a9"), // #a9a9a9
}

var defaultLightLogTheme = &typesv1.FormatConfig_LogTheme{
	Key:   fgstyle("#146e23"), // #146e23
	Value: fgstyle("#878376"), // #878376
	Time:  fgstyle("#565454"), // #565454
	Msg:   fgstyle("#000000"), // #000000
	Levels: &typesv1.FormatConfig_LevelStyle{
		Debug:   fgstyle("#d33682"),              // #d33682
		Info:    fgstyle("#2aa198"),              // #2aa198
		Warn:    fgstyle("#ff8800"),              // #ff8800
		Error:   fgstyle("#d82626"),              // #d82626
		Panic:   fgbgstyle("#d82626", "#ffffff"), // #d82626, #323232
		Fatal:   fgbgstyle("#d82626", "#ffff00"), // #d82626, #ffff00
		Unknown: fgstyle("#a9a9a9"),              // #a9a9a9
	},
	AbsentMsg:  fgstyle("#a9a9a9"), // #a9a9a9
	AbsentTime: fgstyle("#a9a9a9"), // #a9a9a9
}

var defaultDarkSpanTheme = &typesv1.FormatConfig_SpanTheme{
	TraceId:            fgstyle("#2aa198"), // #2aa198
	SpanId:             fgstyle("#2aa198"), // #2aa198
	TraceState:         fgstyle("#2aa198"), // #2aa198
	ParentSpanId:       fgstyle("#2aa198"), // #2aa198
	AbsentParentSpanId: fgstyle("#888888"), // #888888
	Name:               fgstyle("#2aa198"), // #2aa198
	Kind:               fgstyle("#2aa198"), // #2aa198
	ServiceName:        fgstyle("#2aa198"), // #2aa198
	ScopeName:          fgstyle("#2aa198"), // #2aa198
	ScopeVersion:       fgstyle("#2aa198"), // #2aa198
	Time:               fgstyle("#2aa198"), // #2aa198
	Duration:           fgstyle("#2aa198"), // #2aa198
	ResourceKey:        fgstyle("#2aa198"), // #2aa198
	ResourceVal:        fgstyle("#2aa198"), // #2aa198
	AttributeKey:       fgstyle("#2aa198"), // #2aa198
	AttributeVal:       fgstyle("#2aa198"), // #2aa198
	StatusMessage:      fgstyle("#2aa198"), // #2aa198
	StatusCode:         fgstyle("#2aa198"), // #2aa198
	EventTime:          fgstyle("#2aa198"), // #2aa198
	EventName:          fgstyle("#2aa198"), // #2aa198
	EventKey:           fgstyle("#2aa198"), // #2aa198
	EventVal:           fgstyle("#2aa198"), // #2aa198
	LinkTraceId:        fgstyle("#2aa198"), // #2aa198
	LinkSpanId:         fgstyle("#2aa198"), // #2aa198
	LinkTraceState:     fgstyle("#2aa198"), // #2aa198
	LinkKey:            fgstyle("#2aa198"), // #2aa198
	LinkVal:            fgstyle("#2aa198"), // #2aa198
}

var defaultLightSpanTheme = &typesv1.FormatConfig_SpanTheme{
	TraceId:            fgstyle("#1b645e"), // #1b645e
	SpanId:             fgstyle("#1b645e"), // #1b645e
	TraceState:         fgstyle("#1b645e"), // #1b645e
	ParentSpanId:       fgstyle("#1b645e"), // #1b645e
	AbsentParentSpanId: fgstyle("#a9a9a9"), // #a9a9a9
	Name:               fgstyle("#000000"), // #000000
	Kind:               fgstyle("#1b645e"), // #1b645e
	ServiceName:        fgstyle("#1b645e"), // #1b645e
	ScopeName:          fgstyle("#1b645e"), // #1b645e
	ScopeVersion:       fgstyle("#1b645e"), // #1b645e
	Time:               fgstyle("#565454"), // #565454
	Duration:           fgstyle("#1b645e"), // #1b645e
	ResourceKey:        fgstyle("#146e23"), // #146e23
	ResourceVal:        fgstyle("#878376"), // #878376
	AttributeKey:       fgstyle("#146e23"), // #146e23
	AttributeVal:       fgstyle("#878376"), // #878376
	StatusMessage:      fgstyle("#1b645e"), // #1b645e
	StatusCode:         fgstyle("#1b645e"), // #1b645e
	EventTime:          fgstyle("#1b645e"), // #1b645e
	EventName:          fgstyle("#1b645e"), // #1b645e
	EventKey:           fgstyle("#1b645e"), // #1b645e
	EventVal:           fgstyle("#1b645e"), // #1b645e
	LinkTraceId:        fgstyle("#1b645e"), // #1b645e
	LinkSpanId:         fgstyle("#1b645e"), // #1b645e
	LinkTraceState:     fgstyle("#1b645e"), // #1b645e
	LinkKey:            fgstyle("#1b645e"), // #1b645e
	LinkVal:            fgstyle("#1b645e"), // #1b645e
}

var defaultDarkTableTheme = &typesv1.FormatConfig_TableTheme{
	ColumnName: fgstyle("#2aa198"), // #2aa198
	ColumnType: fgstyle("#2aa198"), // #2aa198
	Value:      fgstyle("#2aa198"), // #2aa198
}

var defaultLightTableTheme = &typesv1.FormatConfig_TableTheme{
	ColumnName: fgstyle("#1b645e"), // #1b645e
	ColumnType: fgstyle("#1b645e"), // #1b645e
	Value:      fgstyle("#1b645e"), // #1b645e
}

var DefaultDarkTheme = mustValidTheme(&typesv1.FormatConfig_Theme{
	Key:        defaultDarkLogTheme.Key,
	Value:      defaultDarkLogTheme.Value,
	Time:       defaultDarkLogTheme.Time,
	Msg:        defaultDarkLogTheme.Msg,
	AbsentMsg:  defaultDarkLogTheme.AbsentMsg,
	AbsentTime: defaultDarkLogTheme.AbsentTime,
	Levels:     defaultDarkLogTheme.Levels,

	Logs:   defaultDarkLogTheme,
	Spans:  defaultDarkSpanTheme,
	Tables: defaultDarkTableTheme,
})

var DefaultLightTheme = mustValidTheme(&typesv1.FormatConfig_Theme{
	Key:        defaultLightLogTheme.Key,
	Value:      defaultLightLogTheme.Value,
	Time:       defaultLightLogTheme.Time,
	Msg:        defaultLightLogTheme.Msg,
	AbsentMsg:  defaultLightLogTheme.AbsentMsg,
	AbsentTime: defaultLightLogTheme.AbsentTime,
	Levels:     defaultLightLogTheme.Levels,

	Logs:   defaultLightLogTheme,
	Spans:  defaultLightSpanTheme,
	Tables: defaultLightTableTheme,
})

var noColorTheme = &Theme{
	Logs: &ThemeLog{
		Key:          noColorStyle(),
		Val:          noColorStyle(),
		Time:         noColorStyle(),
		TimeAbsent:   noColorStyle(),
		Msg:          noColorStyle(),
		MsgAbsent:    noColorStyle(),
		DebugLevel:   noColorStyle(),
		InfoLevel:    noColorStyle(),
		WarnLevel:    noColorStyle(),
		ErrorLevel:   noColorStyle(),
		PanicLevel:   noColorStyle(),
		FatalLevel:   noColorStyle(),
		UnknownLevel: noColorStyle(),
	},
	Spans: &ThemeSpan{
		TraceId:            noColorStyle(),
		SpanId:             noColorStyle(),
		TraceState:         noColorStyle(),
		ParentSpanId:       noColorStyle(),
		ParentSpanIdAbsent: noColorStyle(),
		Name:               noColorStyle(),
		Kind:               noColorStyle(),
		ServiceName:        noColorStyle(),
		ScopeName:          noColorStyle(),
		ScopeVersion:       noColorStyle(),
		Time:               noColorStyle(),
		Duration:           noColorStyle(),
		ResourceKey:        noColorStyle(),
		ResourceVal:        noColorStyle(),
		AttributeKey:       noColorStyle(),
		AttributeVal:       noColorStyle(),
		StatusMessage:      noColorStyle(),
		StatusCode:         noColorStyle(),
		EventTime:          noColorStyle(),
		EventName:          noColorStyle(),
		EventKey:           noColorStyle(),
		EventVal:           noColorStyle(),
		LinkTraceID:        noColorStyle(),
		LinkSpanID:         noColorStyle(),
		LinkTraceState:     noColorStyle(),
		LinkKey:            noColorStyle(),
		LinkVal:            noColorStyle(),
	},
	Table: &ThemeTable{
		ColumnName: noColorStyle(),
		ColumnType: noColorStyle(),
		Value:      noColorStyle(),
	},
}

type Theme struct {
	Logs  *ThemeLog
	Spans *ThemeSpan
	Table *ThemeTable
}

type ThemeLog struct {
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

type ThemeSpan struct {
	TraceId            lipgloss.Style
	SpanId             lipgloss.Style
	TraceState         lipgloss.Style
	ParentSpanId       lipgloss.Style
	ParentSpanIdAbsent lipgloss.Style
	Name               lipgloss.Style
	Kind               lipgloss.Style
	ServiceName        lipgloss.Style
	ScopeName          lipgloss.Style
	ScopeVersion       lipgloss.Style
	Time               lipgloss.Style
	Duration           lipgloss.Style
	ResourceKey        lipgloss.Style
	ResourceVal        lipgloss.Style
	AttributeKey       lipgloss.Style
	AttributeVal       lipgloss.Style
	StatusMessage      lipgloss.Style
	StatusCode         lipgloss.Style

	EventTime lipgloss.Style
	EventName lipgloss.Style
	EventKey  lipgloss.Style
	EventVal  lipgloss.Style

	LinkTraceID    lipgloss.Style
	LinkSpanID     lipgloss.Style
	LinkTraceState lipgloss.Style
	LinkKey        lipgloss.Style
	LinkVal        lipgloss.Style
}

type ThemeTable struct {
	ColumnName lipgloss.Style
	ColumnType lipgloss.Style
	Value      lipgloss.Style
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

func ThemeLogFrom(r *lipgloss.Renderer, theme *typesv1.FormatConfig_Theme) (*ThemeLog, error) {
	var err error
	out := &ThemeLog{}
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

func ThemeSpanFrom(r *lipgloss.Renderer, theme *typesv1.FormatConfig_Theme) (*ThemeSpan, error) {
	var err error
	spans := theme.GetSpans()
	out := &ThemeSpan{}
	out.TraceId, err = pbstyleToLipgloss(r, spans.GetTraceId())
	if err != nil {
		return nil, fmt.Errorf("style for `trace_id` is invalid: %v", err)
	}
	out.SpanId, err = pbstyleToLipgloss(r, spans.GetSpanId())
	if err != nil {
		return nil, fmt.Errorf("style for `span_id` is invalid: %v", err)
	}
	out.TraceState, err = pbstyleToLipgloss(r, spans.GetTraceState())
	if err != nil {
		return nil, fmt.Errorf("style for `trace_state` is invalid: %v", err)
	}
	out.ParentSpanId, err = pbstyleToLipgloss(r, spans.GetParentSpanId())
	if err != nil {
		return nil, fmt.Errorf("style for `parent_span_id` is invalid: %v", err)
	}
	out.ParentSpanIdAbsent, err = pbstyleToLipgloss(r, spans.GetAbsentParentSpanId())
	if err != nil {
		return nil, fmt.Errorf("style for `absent_parent_span_id` is invalid: %v", err)
	}
	out.Name, err = pbstyleToLipgloss(r, spans.GetName())
	if err != nil {
		return nil, fmt.Errorf("style for `name` is invalid: %v", err)
	}
	out.Kind, err = pbstyleToLipgloss(r, spans.GetKind())
	if err != nil {
		return nil, fmt.Errorf("style for `kind` is invalid: %v", err)
	}
	out.ServiceName, err = pbstyleToLipgloss(r, spans.GetServiceName())
	if err != nil {
		return nil, fmt.Errorf("style for `service_name` is invalid: %v", err)
	}
	out.ScopeName, err = pbstyleToLipgloss(r, spans.GetScopeName())
	if err != nil {
		return nil, fmt.Errorf("style for `scope_name` is invalid: %v", err)
	}
	out.ScopeVersion, err = pbstyleToLipgloss(r, spans.GetScopeVersion())
	if err != nil {
		return nil, fmt.Errorf("style for `scope_version` is invalid: %v", err)
	}
	out.Time, err = pbstyleToLipgloss(r, spans.GetTime())
	if err != nil {
		return nil, fmt.Errorf("style for `time` is invalid: %v", err)
	}
	out.Duration, err = pbstyleToLipgloss(r, spans.GetDuration())
	if err != nil {
		return nil, fmt.Errorf("style for `duration` is invalid: %v", err)
	}
	out.ResourceKey, err = pbstyleToLipgloss(r, spans.GetResourceKey())
	if err != nil {
		return nil, fmt.Errorf("style for `resource_key` is invalid: %v", err)
	}
	out.ResourceVal, err = pbstyleToLipgloss(r, spans.GetResourceVal())
	if err != nil {
		return nil, fmt.Errorf("style for `resource_val` is invalid: %v", err)
	}
	out.AttributeKey, err = pbstyleToLipgloss(r, spans.GetAttributeKey())
	if err != nil {
		return nil, fmt.Errorf("style for `attribute_key` is invalid: %v", err)
	}
	out.AttributeVal, err = pbstyleToLipgloss(r, spans.GetAttributeVal())
	if err != nil {
		return nil, fmt.Errorf("style for `attribute_val` is invalid: %v", err)
	}
	out.StatusMessage, err = pbstyleToLipgloss(r, spans.GetStatusMessage())
	if err != nil {
		return nil, fmt.Errorf("style for `status_message` is invalid: %v", err)
	}
	out.StatusCode, err = pbstyleToLipgloss(r, spans.GetStatusCode())
	if err != nil {
		return nil, fmt.Errorf("style for `status_code` is invalid: %v", err)
	}
	out.EventTime, err = pbstyleToLipgloss(r, spans.GetEventTime())
	if err != nil {
		return nil, fmt.Errorf("style for `event_time` is invalid: %v", err)
	}
	out.EventName, err = pbstyleToLipgloss(r, spans.GetEventName())
	if err != nil {
		return nil, fmt.Errorf("style for `event_name` is invalid: %v", err)
	}
	out.EventKey, err = pbstyleToLipgloss(r, spans.GetEventKey())
	if err != nil {
		return nil, fmt.Errorf("style for `event_key` is invalid: %v", err)
	}
	out.EventVal, err = pbstyleToLipgloss(r, spans.GetEventVal())
	if err != nil {
		return nil, fmt.Errorf("style for `event_val` is invalid: %v", err)
	}
	out.LinkTraceID, err = pbstyleToLipgloss(r, spans.GetLinkTraceId())
	if err != nil {
		return nil, fmt.Errorf("style for `link_trace_id` is invalid: %v", err)
	}
	out.LinkSpanID, err = pbstyleToLipgloss(r, spans.GetLinkSpanId())
	if err != nil {
		return nil, fmt.Errorf("style for `link_span_id` is invalid: %v", err)
	}
	out.LinkTraceState, err = pbstyleToLipgloss(r, spans.GetLinkTraceState())
	if err != nil {
		return nil, fmt.Errorf("style for `link_trace_state` is invalid: %v", err)
	}
	out.LinkKey, err = pbstyleToLipgloss(r, spans.GetLinkKey())
	if err != nil {
		return nil, fmt.Errorf("style for `link_key` is invalid: %v", err)
	}
	out.LinkVal, err = pbstyleToLipgloss(r, spans.GetLinkVal())
	if err != nil {
		return nil, fmt.Errorf("style for `link_val` is invalid: %v", err)
	}
	return out, nil
}

func ThemeTableFrom(r *lipgloss.Renderer, theme *typesv1.FormatConfig_Theme) (*ThemeTable, error) {
	var err error
	tables := theme.GetTables()
	out := &ThemeTable{}
	out.ColumnName, err = pbstyleToLipgloss(r, tables.GetColumnName())
	if err != nil {
		return nil, fmt.Errorf("style for `column_name` is invalid: %v", err)
	}
	out.ColumnType, err = pbstyleToLipgloss(r, tables.GetColumnType())
	if err != nil {
		return nil, fmt.Errorf("style for `column_type` is invalid: %v", err)
	}
	out.Value, err = pbstyleToLipgloss(r, tables.GetValue())
	if err != nil {
		return nil, fmt.Errorf("style for `value` is invalid: %v", err)
	}

	return out, nil
}

func ThemeFrom(r *lipgloss.Renderer, theme *typesv1.FormatConfig_Theme) (*Theme, error) {
	logs, err := ThemeLogFrom(r, theme)
	if err != nil {
		return nil, fmt.Errorf("log theme: %v", err)
	}
	spans, err := ThemeSpanFrom(r, theme)
	if err != nil {
		return nil, fmt.Errorf("span theme: %v", err)
	}
	tables, err := ThemeTableFrom(r, theme)
	if err != nil {
		return nil, fmt.Errorf("table theme: %v", err)
	}
	out := &Theme{
		Logs:  logs,
		Spans: spans,
		Table: tables,
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

func fgbgstyle(fg, bg string) *typesv1.FormatConfig_Style {
	return &typesv1.FormatConfig_Style{
		Foreground: &typesv1.FormatConfig_Color{HtmlHexColor: fg},
		Background: &typesv1.FormatConfig_Color{HtmlHexColor: bg},
	}
}

func noColorStyle() lipgloss.Style {
	st := lipgloss.NewStyle()
	st.Foreground(lipgloss.NoColor{})
	st.Background(lipgloss.NoColor{})
	return st
}
