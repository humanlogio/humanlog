package stdiosink

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/humanlogio/api/go/pkg/logql"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
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
	Keep              map[string]struct{}
	Skip              map[string]struct{}
	SkipUnchanged     bool
	SortLongest       bool
	TimeFormat        string
	TimeZone          *time.Location
	TruncateLength    int
	Truncates         bool
	AbsentMsgContent  string
	AbsentTimeContent string

	LightTheme func(r *lipgloss.Renderer) (*Theme, error)
	DarkTheme  func(r *lipgloss.Renderer) (*Theme, error)
}

var DefaultStdioOpts = StdioOpts{

	SkipUnchanged:     true,
	SortLongest:       true,
	TimeFormat:        time.Stamp,
	TimeZone:          time.Local,
	TruncateLength:    15,
	Truncates:         false,
	AbsentMsgContent:  "<no msg>",
	AbsentTimeContent: "<no time>",

	LightTheme: func(r *lipgloss.Renderer) (*Theme, error) { return ThemeFrom(r, DefaultLightTheme) },
	DarkTheme:  func(r *lipgloss.Renderer) (*Theme, error) { return ThemeFrom(r, DefaultDarkTheme) },
}

func StdioOptsFrom(cfg *typesv1.FormatConfig) (StdioOpts, []error) {
	var errs []error
	opts := DefaultStdioOpts
	if cfg.SkipFields != nil && len(cfg.SkipFields) > 0 {
		opts.Skip = sliceToSet(cfg.SkipFields)
	}
	if cfg.KeepFields != nil && len(cfg.KeepFields) > 0 {
		opts.Keep = sliceToSet(cfg.KeepFields)
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
		msg = std.theme.MsgAbsent.Render(std.opts.AbsentMsgContent)
	} else {
		msg = std.theme.Msg.Render(data.Msg)
	}

	lvl := strings.ToUpper(data.Lvl)[:imin(4, len(data.Lvl))]
	var level string
	switch strings.ToLower(data.Lvl) {
	case "debug":
		level = std.theme.DebugLevel.Render(lvl)
	case "info":
		level = std.theme.InfoLevel.Render(lvl)
	case "warn", "warning":
		level = std.theme.WarnLevel.Render(lvl)
	case "error":
		level = std.theme.ErrorLevel.Render(lvl)
	case "fatal", "panic":
		level = std.theme.FatalLevel.Render(lvl)
	default:
		level = std.theme.UnknownLevel.Render(lvl)
	}

	var timestr string
	ts := data.Timestamp.AsTime()
	if ts.IsZero() {
		timestr = std.theme.TimeAbsent.Render(std.opts.AbsentTimeContent)
	} else {
		if std.opts.TimeZone != nil {
			ts = ts.In(std.opts.TimeZone)
		}
		timestr = std.theme.Time.Render(ts.Format(std.opts.TimeFormat))
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
		value, err := logql.ResolveVal(kv.Value, logql.MakeFlatGoMap, logql.MakeFlatMapGoSlice)
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

func toString(value *typesv1.Val) (string, error) {
	v, err := logql.ResolveVal(value, nil, nil)
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
		(*ref)[key] = fmt.Sprintf("%v", t)
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
		kstr := std.theme.Key.Render(k)

		var vstr string
		if std.opts.Truncates && len(w) > std.opts.TruncateLength {
			vstr = w[:std.opts.TruncateLength] + "..."
		} else {
			vstr = w
		}
		vstr = std.theme.Val.Render(vstr)
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
		if _, keep := opts.Keep[key]; keep {
			return true
		}
	}
	if len(opts.Skip) != 0 {
		if _, skip := opts.Skip[key]; skip {
			return false
		}
	}
	return true
}

func (opts *StdioOpts) shouldShowUnchanged(key string) bool {
	if len(opts.Keep) != 0 {
		if _, keep := opts.Keep[key]; keep {
			return true
		}
	}
	return false
}

type byLongest []string

func (s byLongest) Len() int           { return len(s) }
func (s byLongest) Less(i, j int) bool { return len(s[i]) < len(s[j]) }
func (s byLongest) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func imin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sliceToSet(arr []string) map[string]struct{} {
	if arr == nil {
		return nil
	}
	out := make(map[string]struct{})
	for _, key := range arr {
		out[key] = struct{}{}
	}
	return out
}
