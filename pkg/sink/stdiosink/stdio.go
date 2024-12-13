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

	"github.com/fatih/color"
	"github.com/humanlogio/api/go/pkg/logql"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/pkg/sink"
)

var (
	eol = [...]byte{'\n'}
)

type Stdio struct {
	w    io.Writer
	opts StdioOpts

	lastRaw   bool
	lastLevel string
	lastKVs   map[string]string
}

type StdioOpts struct {
	Keep           map[string]struct{}
	Skip           map[string]struct{}
	SkipUnchanged  bool
	SortLongest    bool
	TimeFormat     string
	TimeZone       *time.Location
	TruncateLength int
	Truncates      bool

	ColorFlag string
	LightBg   bool
	Palette   Palette
}

var DefaultStdioOpts = StdioOpts{

	SkipUnchanged:  true,
	SortLongest:    true,
	TimeFormat:     time.Stamp,
	TimeZone:       time.Local,
	TruncateLength: 15,
	Truncates:      true,

	ColorFlag: "auto",
	LightBg:   false,
	Palette:   DefaultPalette,
}

func StdioOptsFrom(cfg config.Config) (StdioOpts, []error) {
	var errs []error
	opts := DefaultStdioOpts
	if cfg.Skip != nil {
		opts.Skip = sliceToSet(cfg.Skip)
	}
	if cfg.Keep != nil {
		opts.Keep = sliceToSet(cfg.Keep)
	}
	if cfg.SortLongest != nil {
		opts.SortLongest = *cfg.SortLongest
	}
	if cfg.SkipUnchanged != nil {
		opts.SkipUnchanged = *cfg.SkipUnchanged
	}
	if cfg.Truncates != nil {
		opts.Truncates = *cfg.Truncates
	}
	if cfg.LightBg != nil {
		opts.LightBg = *cfg.LightBg
	}
	if cfg.TruncateLength != nil {
		opts.TruncateLength = *cfg.TruncateLength
	}
	if cfg.TimeFormat != nil {
		opts.TimeFormat = *cfg.TimeFormat
	}
	if cfg.TimeZone != nil {
		var err error
		opts.TimeZone, err = time.LoadLocation(*cfg.TimeZone)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid --time-zone=%q: %v", *cfg.TimeZone, err))
		}
	}
	if cfg.ColorMode != nil {
		colorMode, err := config.GrokColorMode(*cfg.ColorMode)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid --color=%q: %v", *cfg.ColorMode, err))
		}
		switch colorMode {
		case config.ColorModeOff:
			color.NoColor = true
		case config.ColorModeOn:
			color.NoColor = false
		default:
			// 'Auto' default is applied as a global variable initializer function, so nothing
			// to do here.
		}
	}
	if cfg.Palette != nil {
		pl, err := PaletteFrom(*cfg.Palette)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid palette, using default one: %v", err))
		} else {
			opts.Palette = *pl
		}
	}
	return opts, errs
}

var _ sink.Sink = (*Stdio)(nil)

func NewStdio(w io.Writer, opts StdioOpts) *Stdio {
	return &Stdio{
		w:    w,
		opts: opts,
	}
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

	var (
		msgColor       *color.Color
		msgAbsentColor *color.Color
	)
	if std.opts.LightBg {
		msgColor = std.opts.Palette.MsgLightBgColor
		msgAbsentColor = std.opts.Palette.MsgAbsentLightBgColor
	} else {
		msgColor = std.opts.Palette.MsgDarkBgColor
		msgAbsentColor = std.opts.Palette.MsgAbsentDarkBgColor
	}
	var msg string
	if data.Msg == "" {
		msg = msgAbsentColor.Sprint("<no msg>")
	} else {
		msg = msgColor.Sprint(data.Msg)
	}

	lvl := strings.ToUpper(data.Lvl)[:imin(4, len(data.Lvl))]
	var level string
	switch strings.ToLower(data.Lvl) {
	case "debug":
		level = std.opts.Palette.DebugLevelColor.Sprint(lvl)
	case "info":
		level = std.opts.Palette.InfoLevelColor.Sprint(lvl)
	case "warn", "warning":
		level = std.opts.Palette.WarnLevelColor.Sprint(lvl)
	case "error":
		level = std.opts.Palette.ErrorLevelColor.Sprint(lvl)
	case "fatal", "panic":
		level = std.opts.Palette.FatalLevelColor.Sprint(lvl)
	default:
		level = std.opts.Palette.UnknownLevelColor.Sprint(lvl)
	}

	var timeColor *color.Color
	if std.opts.LightBg {
		timeColor = std.opts.Palette.TimeLightBgColor
	} else {
		timeColor = std.opts.Palette.TimeDarkBgColor
	}
	var timestr string
	ts := data.Timestamp.AsTime()
	if ts.IsZero() {
		timestr = "<no time>"
	} else {
		if std.opts.TimeZone != nil {
			ts = ts.In(std.opts.TimeZone)
		}
		timestr = timeColor.Sprint(ts.Format(std.opts.TimeFormat))
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
		// value could be one of the following:
		// - string
		// - int64
		// - float64
		// - bool
		// - time.Time
		// - time.Duration
		// - []any
		// - map[string]any
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
		return fmt.Sprintf("%q", t), nil
	case int64:
		return fmt.Sprintf("%d", t), nil
	case float64:
		return fmt.Sprintf("%g", t), nil
	case bool:
		return fmt.Sprintf("%t", t), nil
	case time.Time:
		return fmt.Sprintf("%q", t.Format(time.RFC3339Nano)), nil
	case time.Duration:
		return fmt.Sprintf("%q", t.String()), nil
	default:
		return "", fmt.Errorf("unsupported type: %T", t)
	}
}

func put(ref *map[string]string, key string, value any) {
	switch t := value.(type) {
	case string:
		(*ref)[key] = fmt.Sprintf("%q", t)
	case int64:
		(*ref)[key] = fmt.Sprintf("%d", t)
	case float64:
		(*ref)[key] = fmt.Sprintf("%g", t)
	case bool:
		(*ref)[key] = fmt.Sprintf("%t", t)
	case time.Time:
		(*ref)[key] = fmt.Sprintf("%q", t.Format(time.RFC3339Nano))
	case time.Duration:
		(*ref)[key] = fmt.Sprintf("%q", t.String())
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
		kstr := std.opts.Palette.KeyColor.Sprint(k)

		var vstr string
		if std.opts.Truncates && len(w) > std.opts.TruncateLength {
			vstr = w[:std.opts.TruncateLength] + "..."
		} else {
			vstr = w
		}
		vstr = std.opts.Palette.ValColor.Sprint(vstr)
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

func sliceToSet(arr *[]string) map[string]struct{} {
	if arr == nil {
		return nil
	}
	out := make(map[string]struct{})
	for _, key := range *arr {
		out[key] = struct{}{}
	}
	return out
}
