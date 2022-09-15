package sink

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/model"
)

var (
	eol = [...]byte{'\n'}
)

type Stdio struct {
	w    io.Writer
	opts StdioOpts

	lastEvent *model.Event
	lastKVs   map[string]string
}

type StdioOpts struct {
	Skip           map[string]struct{}
	Keep           map[string]struct{}
	SkipUnchanged  bool
	SortLongest    bool
	TimeFormat     string
	Truncates      bool
	TruncateLength int

	LightBg bool
	Palette Palette
}

var DefaultStdioOpts = StdioOpts{
	SkipUnchanged:  true,
	SortLongest:    true,
	Truncates:      true,
	LightBg:        false,
	TruncateLength: 15,
	TimeFormat:     time.Stamp,

	Palette: DefaultPalette,
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

var _ Sink = (*Stdio)(nil)

func NewStdio(w io.Writer, opts StdioOpts) *Stdio {
	return &Stdio{
		w:    w,
		opts: opts,
	}
}

func (std *Stdio) Receive(ev *model.Event) error {
	if ev.Structured == nil {
		if _, err := std.w.Write(ev.Raw); err != nil {
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

	lvl := strings.ToUpper(data.Level)[:imin(4, len(data.Level))]
	var level string
	switch data.Level {
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

	_, _ = fmt.Fprintf(out, "%s |%s| %s\t %s",
		timeColor.Sprint(data.Time.Format(std.opts.TimeFormat)),
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

	kvs := make(map[string]string, len(data.KVs))
	for _, kv := range data.KVs {
		kvs[kv.Key] = kv.Value
	}
	std.lastEvent = ev
	std.lastKVs = kvs
	return nil
}

func (std *Stdio) joinKVs(data *model.Structured, sep string) []string {

	kv := make([]string, 0, len(data.KVs))
	for _, pair := range data.KVs {
		k, v := pair.Key, pair.Value
		if !std.opts.shouldShowKey(k) {
			continue
		}

		if std.opts.SkipUnchanged {
			if lastV, ok := std.lastKVs[k]; ok && lastV == v && !std.opts.shouldShowUnchanged(k) {
				continue
			}
		}
		kstr := std.opts.Palette.KeyColor.Sprint(k)

		var vstr string
		if std.opts.Truncates && len(v) > std.opts.TruncateLength {
			vstr = v[:std.opts.TruncateLength] + "..."
		} else {
			vstr = v
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

func PaletteFrom(pl config.TextPalette) (*Palette, error) {
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
