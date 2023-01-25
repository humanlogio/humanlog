package stdiosink

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/model"
	"github.com/humanlogio/humanlog/internal/pkg/sink"
	"github.com/muesli/termenv"
)

var (
	eol = [...]byte{'\n'}
)

type Stdio struct {
	output *termenv.Output

	opts StdioOpts

	lastRaw   bool
	lastLevel string
	lastKVs   map[string]string
}

type StdioOpts struct {
	Keep           map[string]struct{}
	Skip           map[string]struct{}
	Highlight      map[string]struct{}
	SkipUnchanged  bool
	SortLongest    bool
	TimeFormat     string
	TruncateLength int
	Truncates      bool

	Theme Theme
}

var DefaultStdioOpts = StdioOpts{
	SkipUnchanged:  true,
	SortLongest:    true,
	TimeFormat:     time.Stamp,
	TruncateLength: 15,
	Truncates:      true,
}

func StdioOptsFrom(cfg config.Config, theme Theme) (StdioOpts, []error) {
	var errs []error
	opts := DefaultStdioOpts
	if cfg.Skip != nil {
		opts.Skip = sliceToSet(cfg.Skip)
	}
	if cfg.Keep != nil {
		opts.Keep = sliceToSet(cfg.Keep)
	}
	if cfg.Highlight != nil {
		opts.Highlight = sliceToSet(cfg.Highlight)
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
	if cfg.TruncateLength != nil {
		opts.TruncateLength = *cfg.TruncateLength
	}
	if cfg.TimeFormat != nil {
		opts.TimeFormat = *cfg.TimeFormat
	}
	opts.Theme = theme
	return opts, errs
}

var _ sink.Sink = (*Stdio)(nil)

func NewStdio(output *termenv.Output, opts StdioOpts) *Stdio {
	return &Stdio{
		output: output,
		opts:   opts,
	}
}

func containsHighlight(ev *model.Event, hl map[string]struct{}) bool {
	for key := range hl {
		if bytes.Contains(ev.Raw, []byte(key)) {
			return true
		}
	}
	return false
}

func (std *Stdio) Receive(ctx context.Context, ev *model.Event) error {
	if ev.Structured == nil {
		std.lastRaw = true
		std.lastLevel = ""
		std.lastKVs = nil

		if _, err := std.output.Write(ev.Raw); err != nil {
			return err
		}
		if _, err := std.output.Write(eol[:]); err != nil {
			return err
		}
		return nil
	}
	data := ev.Structured

	buf := bytes.NewBuffer(nil)
	out := tabwriter.NewWriter(buf, 0, 1, 0, '\t', 0)

	// shouldHighlight := true
	// if len(std.opts.Highlight) > 0 {
	// 	shouldHighlight = containsHighlight(ev, std.opts.Highlight)
	// }
	msgColor := std.opts.Theme.MsgBgColor
	msgAbsentColor := std.opts.Theme.MsgAbsentBgColor
	timeColor := std.opts.Theme.TimeBgColor

	var msg string
	if data.Msg == "" {
		msg = msgAbsentColor("<no msg>")
	} else {
		msg = msgColor(data.Msg)
	}

	lvl := strings.ToUpper(data.Level)[:imin(4, len(data.Level))]
	var level string
	// if !shouldHighlight {
	// level = dim(lvl)
	// } else {
	level = std.colorForLevel(lvl)(lvl)
	// }

	_, _ = fmt.Fprintf(out, "%s |%s| %s\t %s",
		timeColor(data.Time.Format(std.opts.TimeFormat)),
		level,
		msg,
		strings.Join(std.joinKVs(data, "="), "\t "),
	)

	if err := out.Flush(); err != nil {
		return err
	}

	buf.Write(eol[:])

	if _, err := buf.WriteTo(std.output); err != nil {
		return err
	}

	kvs := make(map[string]string, len(data.KVs))
	for _, kv := range data.KVs {
		kvs[kv.Key] = kv.Value
	}
	std.lastRaw = false
	std.lastLevel = ev.Structured.Level
	std.lastKVs = kvs
	return nil
}

func (std *Stdio) colorForLevel(lvl string) ColorerFn {
	switch strings.ToLower(lvl) {
	case "debug":
		return std.opts.Theme.DebugLevelColor
	case "info":
		return std.opts.Theme.InfoLevelColor
	case "warn", "warning":
		return std.opts.Theme.WarnLevelColor
	case "error":
		return std.opts.Theme.ErrorLevelColor
	case "fatal", "panic":
		return std.opts.Theme.FatalLevelColor
	default:
		return std.opts.Theme.UnknownLevelColor
	}
}

func (std *Stdio) joinKVs(data *model.Structured, sep string) []string {
	wasSameLevel := std.lastLevel == data.Level
	skipUnchanged := !std.lastRaw && std.opts.SkipUnchanged && wasSameLevel

	kv := make([]string, 0, len(data.KVs))
	for _, pair := range data.KVs {
		k, v := pair.Key, pair.Value
		if !std.opts.shouldShowKey(k) {
			continue
		}

		if skipUnchanged {
			if lastV, ok := std.lastKVs[k]; ok && lastV == v && !std.opts.shouldShowUnchanged(k) {
				continue
			}
		}
		kstr := std.opts.Theme.KeyColor(k)

		var vstr string
		if std.opts.Truncates && len(v) > std.opts.TruncateLength {
			vstr = v[:std.opts.TruncateLength] + "..."
		} else {
			vstr = v
		}
		vstr = std.opts.Theme.ValColor(vstr)
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
