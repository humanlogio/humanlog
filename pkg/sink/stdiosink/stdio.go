package stdiosink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/logqleval"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/ryanuber/go-glob"
)

var (
	eol = [...]byte{'\n'}
)

type Stdio struct {
	w     io.Writer
	opts  StdioOpts
	rd    *lipgloss.Renderer
	theme Theme

	lastRaw   bool
	lastLevel string
	lastKVs   map[string]string
}

type StdioOpts struct {
	Keep                    []string
	Skip                    []string
	SkipUnchanged           bool
	SortLongest             bool
	TimeFormat              string
	TimeZone                *time.Location
	TruncateLength          int
	Truncates               bool
	AbsentMsgContent        string
	AbsentTimeContent       string
	AbsentParentSpanContent string

	LightTheme func(r *lipgloss.Renderer) (*Theme, error)
	DarkTheme  func(r *lipgloss.Renderer) (*Theme, error)
}

var DefaultStdioOpts = StdioOpts{

	SkipUnchanged:           true,
	SortLongest:             true,
	TimeFormat:              time.Stamp,
	TimeZone:                time.Local,
	TruncateLength:          15,
	Truncates:               false,
	AbsentMsgContent:        "<no msg>",
	AbsentTimeContent:       "<no time>",
	AbsentParentSpanContent: "<no parent>",

	LightTheme: func(r *lipgloss.Renderer) (*Theme, error) { return ThemeFrom(r, DefaultLightTheme) },
	DarkTheme:  func(r *lipgloss.Renderer) (*Theme, error) { return ThemeFrom(r, DefaultDarkTheme) },
}

func StdioOptsFrom(cfg *typesv1.FormatConfig) (StdioOpts, []error) {
	var errs []error
	opts := DefaultStdioOpts
	if cfg.SkipFields != nil && len(cfg.SkipFields) > 0 {
		opts.Skip = cfg.SkipFields
	}
	if cfg.KeepFields != nil && len(cfg.KeepFields) > 0 {
		opts.Keep = cfg.KeepFields
	}
	if cfg.SortLongest != nil {
		opts.SortLongest = *cfg.SortLongest
	}
	if cfg.SkipUnchanged != nil {
		opts.SkipUnchanged = *cfg.SkipUnchanged
	}
	if cfg.Truncation != nil {
		opts.Truncates = true
		if cfg.Truncation.Length != 0 {
			opts.TruncateLength = int(cfg.Truncation.Length)
		}
	}
	if cfg.Time != nil {
		if cfg.Time.Format != nil {
			opts.TimeFormat = *cfg.Time.Format
		}
		if cfg.Time.Timezone != nil {
			var err error
			opts.TimeZone, err = time.LoadLocation(*cfg.Time.Timezone)
			if err != nil {
				errs = append(errs, fmt.Errorf("invalid --time-zone=%q: %v", *cfg.Time.Timezone, err))
			}
		}
		if cfg.Time.AbsentDefaultValue != nil {
			opts.AbsentMsgContent = *cfg.Time.AbsentDefaultValue
		}
	}
	if cfg.Message != nil {
		if cfg.Message.AbsentDefaultValue != nil {
			opts.AbsentMsgContent = *cfg.Message.AbsentDefaultValue
		}
	}

	if cfg.GetThemes() != nil {
		if cfg.GetThemes().GetDark() != nil {
			opts.DarkTheme = func(r *lipgloss.Renderer) (*Theme, error) { return ThemeFrom(r, cfg.GetThemes().GetDark()) }
		}
		if cfg.GetThemes().GetLight() != nil {
			opts.LightTheme = func(r *lipgloss.Renderer) (*Theme, error) { return ThemeFrom(r, cfg.GetThemes().GetLight()) }
		}
	}
	return opts, errs
}

var _ sink.Sink = (*Stdio)(nil)

func NewStdio(w io.Writer, opts StdioOpts) (*Stdio, error) {
	rd := lipgloss.NewRenderer(w)
	var (
		theme *Theme
		err   error
	)
	if rd.HasDarkBackground() {
		theme, err = opts.DarkTheme(rd)
	} else {
		theme, err = opts.LightTheme(rd)
	}
	if err != nil {
		return nil, err
	}
	return &Stdio{
		w:     w,
		opts:  opts,
		rd:    rd,
		theme: *theme,
	}, err
}

func (std *Stdio) Close(ctx context.Context) error {
	return nil
}

func (std *Stdio) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
	return std.ReceiveWithPostProcess(ctx, ev, nil)
}

func (std *Stdio) ReceiveWithPostProcess(ctx context.Context, ev *typesv1.LogEvent, postProcess func(string) string) error {
	logtheme := std.theme.Logs
	if ev.Structured == nil {
		std.lastRaw = true
		std.lastLevel = ""
		std.lastKVs = nil
		if _, err := std.w.Write(ev.Raw); err != nil {
			return err
		}
		if _, err := std.w.Write(eol[:]); err != nil {
			return err
		}
		return nil
	}
	data := ev.Structured

	buf := bytes.NewBuffer(nil)
	out := tabwriter.NewWriter(buf, 0, 1, 0, '\t', 0)

	var msg string
	if data.Msg == "" {
		msg = logtheme.MsgAbsent.Render(std.opts.AbsentMsgContent)
	} else {
		msg = logtheme.Msg.Render(data.Msg)
	}

	lvl := strings.ToUpper(data.Lvl)[:min(4, len(data.Lvl))]
	var level string
	switch strings.ToLower(data.Lvl) {
	case "debug":
		level = logtheme.DebugLevel.Render(lvl)
	case "info":
		level = logtheme.InfoLevel.Render(lvl)
	case "warn", "warning":
		level = logtheme.WarnLevel.Render(lvl)
	case "error":
		level = logtheme.ErrorLevel.Render(lvl)
	case "fatal", "panic":
		level = logtheme.FatalLevel.Render(lvl)
	default:
		level = logtheme.UnknownLevel.Render(lvl)
	}

	var timestr string
	ts := data.Timestamp.AsTime()
	if ts.IsZero() {
		timestr = logtheme.TimeAbsent.Render(std.opts.AbsentTimeContent)
	} else {
		if std.opts.TimeZone != nil {
			ts = ts.In(std.opts.TimeZone)
		}
		timestr = logtheme.Time.Render(ts.Format(std.opts.TimeFormat))
	}

	pattern := "%s |%s| %s\t %s"
	if postProcess != nil {
		pattern = postProcess(pattern)
	}
	_, _ = fmt.Fprintf(out, pattern,
		timestr,
		level,
		msg,
		strings.Join(std.joinKVs(data, "="), "\t "),
	)

	if err := out.Flush(); err != nil {
		return err
	}

	buf.Write(eol[:])

	if _, err := buf.WriteTo(std.w); err != nil {
		return err
	}

	kvs := make(map[string]string, len(data.Kvs))
	for _, kv := range data.Kvs {
		key := kv.Key
		value, err := logqleval.ResolveVal(kv.Value, logqleval.MakeFlatGoMap, logqleval.MakeFlatMapGoSlice)
		if err != nil {
			return err
		}
		put(&kvs, key, value)
	}
	std.lastRaw = false
	std.lastLevel = ev.Structured.Lvl
	std.lastKVs = kvs
	return nil
}

func (std *Stdio) ReceiveSpan(ctx context.Context, span *typesv1.Span) error {
	spantheme := std.theme.Spans
	buf := bytes.NewBuffer(nil)
	spanOut := tabwriter.NewWriter(buf, 0, 1, 0, '|', 0)
	resourcesOut := tabwriter.NewWriter(buf, 0, 1, 0, '\t', 0)
	attributesOut := tabwriter.NewWriter(buf, 0, 1, 0, '\t', 0)
	eventsOut := tabwriter.NewWriter(buf, 0, 1, 0, '\t', 0)
	linksOut := tabwriter.NewWriter(buf, 0, 1, 0, '\t', 0)

	var (
		startTime     string
		serviceName   = spantheme.ServiceName.Render(span.ServiceName)
		kind          = spantheme.Kind.Render(typesv1.Span_SpanKind_name[int32(span.Kind)])
		name          = spantheme.Name.Render(span.Name)
		duration      = spantheme.Duration.Render(span.Timing.Duration.AsDuration().String())
		statusCode    = spantheme.StatusCode.Render(typesv1.Span_Status_Code_name[int32(span.Status.Code)])
		statusMessage = spantheme.StatusMessage.Render(span.Status.Message)
		traceID       = spantheme.TraceId.Render(span.TraceId)
		spanID        = spantheme.SpanId.Render(span.SpanId)
		parentSpanID  string
		traceState    = spantheme.TraceState.Render(span.TraceState)
		scopeName     = spantheme.ScopeName.Render(span.Scope.Name)
		scopeVersion  = spantheme.ScopeVersion.Render(span.Scope.Version)
	)

	if len(span.ParentSpanId) > 0 {
		parentSpanID = spantheme.ParentSpanId.Render(span.SpanId)
	} else {
		parentSpanID = spantheme.ParentSpanIdAbsent.Render(std.opts.AbsentParentSpanContent)
	}

	ts := span.Timing.Start.AsTime()
	if std.opts.TimeZone != nil {
		ts = ts.In(std.opts.TimeZone)
	}
	startTime = spantheme.Time.Render(ts.Format(std.opts.TimeFormat))

	pattern := "%s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s"
	_, _ = fmt.Fprintf(spanOut, pattern,
		startTime,
		serviceName,
		kind,
		name,
		duration,
		statusCode,
		statusMessage,
		traceID,
		spanID,
		parentSpanID,
		traceState,
		scopeName,
		scopeVersion,
	)

	if err := spanOut.Flush(); err != nil {
		return err
	}

	buf.Write(eol[:])

	if len(span.ResourceAttributes) > 0 {
		resKVs := "\tresource:"
		for _, ra := range span.ResourceAttributes {
			resKVs += "\t"
			key := spantheme.ResourceKey.Render(ra.Key)
			strVal, err := toString(ra.Value)
			if err != nil {
				return err
			}
			val := spantheme.ResourceVal.Render(strVal)
			resKVs += key + "=" + val
		}
		_, _ = resourcesOut.Write([]byte(resKVs))
		if err := resourcesOut.Flush(); err != nil {
			return err
		}
		buf.Write(eol[:])
	}

	if len(span.SpanAttributes) > 0 {
		spanKVs := "\tattributes:"
		for _, sa := range span.SpanAttributes {
			spanKVs += "\t"
			key := spantheme.AttributeKey.Render(sa.Key)
			strVal, err := toString(sa.Value)
			if err != nil {
				return err
			}
			val := spantheme.AttributeVal.Render(strVal)
			spanKVs += key + "=" + val
		}
		_, _ = attributesOut.Write([]byte(spanKVs))
		if err := attributesOut.Flush(); err != nil {
			return err
		}
		buf.Write(eol[:])
	}

	if len(span.Events) > 0 {
		for _, event := range span.Events {
			var (
				time = spantheme.EventTime.Render(event.GetTimestamp().AsTime().Format(std.opts.TimeFormat))
				name = spantheme.EventName.Render(event.Name)
				kvs  string
			)
			for _, kv := range event.Kvs {
				kvs += "\t"
				key := spantheme.EventKey.Render(kv.Key)
				strVal, err := toString(kv.Value)
				if err != nil {
					return err
				}
				val := spantheme.EventVal.Render(strVal)
				kvs += key + "=" + val
			}
			pattern := "\tevent: %s | %s %s"
			_, _ = fmt.Fprintf(eventsOut, pattern,
				time,
				name,
				kvs,
			)
		}
		if err := eventsOut.Flush(); err != nil {
			return err
		}
		buf.Write(eol[:])
	}

	if len(span.Links) > 0 {
		for _, link := range span.Links {
			var (
				traceID    = spantheme.LinkTraceID.Render(link.TraceId)
				spanID     = spantheme.LinkSpanID.Render(link.SpanId)
				traceState = spantheme.LinkTraceState.Render(link.TraceState)
				kvs        string
			)

			for _, kv := range link.Kvs {
				kvs += "\t"
				key := spantheme.EventKey.Render(kv.Key)
				strVal, err := toString(kv.Value)
				if err != nil {
					return err
				}
				val := spantheme.EventVal.Render(strVal)
				kvs += key + "=" + val
			}
			pattern := "\tlink: %s | %s | %s | %s"
			_, _ = fmt.Fprintf(linksOut, pattern,
				traceID,
				spanID,
				traceState,
				kvs,
			)
		}
		if err := linksOut.Flush(); err != nil {
			return err
		}
		buf.Write(eol[:])
	}

	if _, err := buf.WriteTo(std.w); err != nil {
		return err
	}
	return nil
}

func (std *Stdio) ReceiveTable(ctx context.Context, table *typesv1.Table) error {
	tabletheme := std.theme.Table
	buf := bytes.NewBuffer(nil)
	out := tabwriter.NewWriter(buf, 0, 1, 0, '|', 0)

	header := "| "
	for i, col := range table.Type.Columns {
		if i != 0 {
			header += " | "
		}
		name := tabletheme.ColumnName.Render(col.Name)
		typ := tabletheme.ColumnType.Render(col.Type.String())
		header += name + ": " + typ
	}
	header += " |"

	longestLine := len(header)

	out.Write([]byte(header))
	out.Write(eol[:])

	for _, row := range table.Rows {
		rowStr := "| "
		for i, col := range row.Items {
			if i != 0 {
				rowStr += " | "
			}
			strVal, err := toString(col)
			if err != nil {
				return err
			}
			name := tabletheme.Value.Render(strVal)
			rowStr += name
		}
		rowStr += " |"
		longestLine = max(len(rowStr), longestLine)
		out.Write([]byte(rowStr))
		out.Write(eol[:])
	}

	if err := out.Flush(); err != nil {
		return err
	}

	if _, err := buf.WriteTo(std.w); err != nil {
		return err
	}
	return nil
}

func toString(value *typesv1.Val) (string, error) {
	v, err := logqleval.ResolveVal(value, logqleval.MakeFlatGoMap, logqleval.MakeFlatGoSlice)
	if err != nil {
		return "", err
	}
	switch t := v.(type) {
	case string:
		return t, nil
	case int64:
		return fmt.Sprintf("%d", t), nil
	case float64:
		return fmt.Sprintf("%g", t), nil
	case bool:
		return fmt.Sprintf("%t", t), nil
	case time.Time:
		return t.Format(time.RFC3339Nano), nil
	case time.Duration:
		return t.String(), nil
	case map[string]any:
		v, err := json.Marshal(t)
		return string(v), err
	case nil:
		return "null", nil
	default:
		return "", fmt.Errorf("unsupported type: %T", t)
	}
}

func put(ref *map[string]string, key string, value any) {
	switch t := value.(type) {
	case string:
		(*ref)[key] = t
	case int64:
		(*ref)[key] = fmt.Sprintf("%d", t)
	case float64:
		(*ref)[key] = fmt.Sprintf("%g", t)
	case bool:
		(*ref)[key] = fmt.Sprintf("%t", t)
	case time.Time:
		(*ref)[key] = t.Format(time.RFC3339Nano)
	case time.Duration:
		(*ref)[key] = t.String()
	case map[string]any:
		for k, v := range t {
			put(ref, key+"."+k, v)
		}
	default:
		if value == nil {
			(*ref)[key] = "null"
		} else {
			(*ref)[key] = fmt.Sprintf("%v", t)
		}
	}
}

func (std *Stdio) joinKVs(data *typesv1.StructuredLogEvent, sep string) []string {
	wasSameLevel := std.lastLevel == data.Lvl
	skipUnchanged := !std.lastRaw && std.opts.SkipUnchanged && wasSameLevel

	kv := make([]string, 0, len(data.Kvs))
	for _, pair := range data.Kvs {
		k, v := pair.Key, pair.Value
		if !std.opts.shouldShowKey(k) {
			continue
		}
		w, err := toString(v)
		if err != nil {
			continue
		}

		if skipUnchanged {
			if lastV, ok := std.lastKVs[k]; ok && lastV == w && !std.opts.shouldShowUnchanged(k) {
				continue
			}
		}
		kstr := std.theme.Logs.Key.Render(k)

		var vstr string
		if std.opts.Truncates && len(w) > std.opts.TruncateLength {
			vstr = w[:std.opts.TruncateLength] + "..."
		} else {
			vstr = w
		}
		vstr = std.theme.Logs.Val.Render(vstr)
		kv = append(kv, kstr+sep+vstr)
	}

	sort.Strings(kv)

	if std.opts.SortLongest {
		sort.Stable(byLongest(kv))
	}

	return kv
}

func (opts *StdioOpts) shouldShowKey(key string) bool {
	if len(opts.Keep) != 0 {
		for _, keep := range opts.Keep {
			if glob.Glob(keep, key) {
				return true
			}
		}
	}
	if len(opts.Skip) != 0 {
		for _, skip := range opts.Skip {
			if glob.Glob(skip, key) {
				return false
			}
		}
	}
	return true
}

func (opts *StdioOpts) shouldShowUnchanged(key string) bool {
	if len(opts.Keep) != 0 {
		for _, keep := range opts.Keep {
			if glob.Glob(keep, key) {
				return true
			}
		}
	}
	return false
}

type byLongest []string

func (s byLongest) Len() int           { return len(s) }
func (s byLongest) Less(i, j int) bool { return len(s[i]) < len(s[j]) }
func (s byLongest) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
