package humanlog

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
)

// LogrusHandler can handle logs emmited by logrus.TextFormatter loggers.
type LogrusHandler struct {
	buf     *bytes.Buffer
	out     *tabwriter.Writer
	truncKV int

	Opts *HandlerOptions

	Level   string
	Time    time.Time
	Message string
	Fields  map[string]string

	last map[string]string
}

func (h *LogrusHandler) clear() {
	h.Level = ""
	h.Time = time.Time{}
	h.Message = ""
	h.last = h.Fields
	h.Fields = make(map[string]string)
	h.buf.Reset()
}

// CanHandle tells if this line can be handled by this handler.
func (h *LogrusHandler) CanHandle(d []byte) bool {
	if !bytes.Contains(d, []byte(`level=`)) {
		return false
	}
	if !bytes.Contains(d, []byte(`time="`)) {
		return false
	}
	if !bytes.Contains(d, []byte(`msg="`)) {
		return false
	}
	return true
}

// HandleLogfmt sets the fields of the handler.
func (h *LogrusHandler) visit(key, val []byte) bool {
	switch {
	case bytes.Equal(key, []byte("level")):
		h.setLevel(val)
	case bytes.Equal(key, []byte("lvl")):
		h.setLevel(val)
	case bytes.Equal(key, []byte("msg")):
		h.setMessage(val)
	case bytes.Equal(key, []byte("message")):
		h.setMessage(val)
	case bytes.Equal(key, []byte("time")):
		h.setTime(val)
	case bytes.Equal(key, []byte("ts")):
		h.setTime(val)
	default:
		h.setField(key, val)
	}
	return true
}

// Prettify the output in a logrus like fashion.
func (h *LogrusHandler) Prettify(skipUnchanged bool) []byte {
	defer h.clear()
	if h.out == nil {
		if h.Opts == nil {
			h.Opts = DefaultOptions
		}
		h.buf = bytes.NewBuffer(nil)
		h.out = tabwriter.NewWriter(h.buf, 0, 1, 0, '\t', 0)
	}

	var (
		msgColor       *color.Color
		msgAbsentColor *color.Color
	)
	if h.Opts.LightBg {
		msgColor = h.Opts.MsgLightBgColor
		msgAbsentColor = h.Opts.MsgAbsentLightBgColor
	} else {
		msgColor = h.Opts.MsgDarkBgColor
		msgAbsentColor = h.Opts.MsgAbsentDarkBgColor
	}

	var msg string
	if h.Message == "" {
		msg = msgAbsentColor.Sprint("<no msg>")
	} else {
		msg = msgColor.Sprint(h.Message)
	}

	lvl := strings.ToUpper(h.Level)[:imin(4, len(h.Level))]
	var level string
	switch h.Level {
	case "debug":
		level = h.Opts.DebugLevelColor.Sprint(lvl)
	case "info":
		level = h.Opts.InfoLevelColor.Sprint(lvl)
	case "warn", "warning":
		level = h.Opts.WarnLevelColor.Sprint(lvl)
	case "error":
		level = h.Opts.ErrorLevelColor.Sprint(lvl)
	case "fatal", "panic":
		level = h.Opts.FatalLevelColor.Sprint(lvl)
	default:
		level = h.Opts.UnknownLevelColor.Sprint(lvl)
	}

	var timeColor *color.Color
	if h.Opts.LightBg {
		timeColor = h.Opts.TimeLightBgColor
	} else {
		timeColor = h.Opts.TimeDarkBgColor
	}
	_, _ = fmt.Fprintf(h.out, "%s |%s| %s\t %s",
		timeColor.Sprint(h.Time.Format(h.Opts.TimeFormat)),
		level,
		msg,
		strings.Join(h.joinKVs(skipUnchanged, "="), "\t "),
	)

	_ = h.out.Flush()

	return h.buf.Bytes()
}

func (h *LogrusHandler) setLevel(val []byte)   { h.Level = string(val) }
func (h *LogrusHandler) setMessage(val []byte) { h.Message = string(val) }
func (h *LogrusHandler) setTime(val []byte) (parsed bool) {
	valStr := string(val)
	if valFloat, err := strconv.ParseFloat(valStr, 64); err == nil {
		h.Time, parsed = tryParseTime(valFloat)
	} else {
		h.Time, parsed = tryParseTime(string(val))
	}
	return
}

func (h *LogrusHandler) setField(key, val []byte) {
	if h.Fields == nil {
		h.Fields = make(map[string]string)
	}
	h.Fields[string(key)] = string(val)
}

func (h *LogrusHandler) joinKVs(skipUnchanged bool, sep string) []string {

	kv := make([]string, 0, len(h.Fields))
	for k, v := range h.Fields {
		if !h.Opts.shouldShowKey(k) {
			continue
		}

		if skipUnchanged {
			if lastV, ok := h.last[k]; ok && lastV == v && !h.Opts.shouldShowUnchanged(k) {
				continue
			}
		}

		kstr := h.Opts.KeyColor.Sprint(k)

		var vstr string
		if h.Opts.Truncates && len(v) > h.Opts.TruncateLength {
			vstr = v[:h.Opts.TruncateLength] + "..."
		} else {
			vstr = v
		}
		vstr = h.Opts.ValColor.Sprint(vstr)
		kv = append(kv, kstr+sep+vstr)
	}

	sort.Strings(kv)

	if h.Opts.SortLongest {
		sort.Stable(byLongest(kv))
	}

	return kv
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
