package humanlog

import (
	"bytes"
	"fmt"
	"github.com/aybabtme/rgbterm"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
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
	if !bytes.Contains(d, []byte(`level="`)) {
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
func (h *LogrusHandler) HandleLogfmt(key, val []byte) error {
	switch {
	case bytes.Equal(key, []byte("level")):
		return h.setLevel(val)
	case bytes.Equal(key, []byte("msg")):
		return h.setMessage(val)
	case bytes.Equal(key, []byte("time")):
		return h.setTime(val)
	default:
		return h.setField(key, val)
	}
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

	var msg string
	if h.Message == "" {
		msg = rgbterm.String("<no msg>", 190, 190, 190)
	} else {
		msg = rgbterm.String(h.Message, 255, 255, 255)
	}

	lvl := strings.ToUpper(h.Level)[:imin(4, len(h.Level))]
	var level string
	switch h.Level {
	case "debug":
		level = rgbterm.String(lvl, 221, 28, 119)
	case "info":
		level = rgbterm.String(lvl, 20, 172, 190)
	case "warn", "warning":
		level = rgbterm.String(lvl, 255, 245, 32)
	case "error":
		level = rgbterm.String(lvl, 255, 0, 0)
	case "fatal", "panic":
		level = rgbterm.BgString(lvl, 255, 0, 0)
	default:
		level = rgbterm.String(lvl, 221, 28, 119)
	}

	_, _ = fmt.Fprintf(h.out, "%s |%s| %s\t %s",
		rgbterm.String(h.Time.Format(time.Stamp), 99, 99, 99),
		level,
		msg,
		strings.Join(h.joinKVs(skipUnchanged, "="), "\t "),
	)

	_ = h.out.Flush()

	return h.buf.Bytes()
}

func (h *LogrusHandler) setLevel(val []byte) error   { h.Level = string(val); return nil }
func (h *LogrusHandler) setMessage(val []byte) error { h.Message = string(val); return nil }
func (h *LogrusHandler) setTime(val []byte) (err error) {
	h.Time, err = tryParseTime(string(val))
	return
}

func (h *LogrusHandler) setField(key, val []byte) error {
	if h.Fields == nil {
		h.Fields = make(map[string]string)
	}
	h.Fields[string(key)] = string(val)
	return nil
}

func (h *LogrusHandler) joinKVs(skipUnchanged bool, sep string) []string {

	kv := make([]string, 0, len(h.Fields))
	for k, v := range h.Fields {
		if !h.Opts.shouldShowKey(k) {
			continue
		}

		if skipUnchanged {
			if lastV, ok := h.last[k]; ok && lastV == v {
				continue
			}
		}

		kstr := rgbterm.String(k, h.Opts.KeyRGB.R, h.Opts.KeyRGB.G, h.Opts.KeyRGB.B)

		var vstr string
		if h.Opts.Truncates && len(v) > h.Opts.TruncateLength {
			vstr = v[:h.Opts.TruncateLength] + "..."
		} else {
			vstr = v
		}
		vstr = rgbterm.String(vstr, h.Opts.ValRGB.R, h.Opts.ValRGB.G, h.Opts.ValRGB.B)
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
