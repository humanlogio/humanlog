package humanlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/aybabtme/rgbterm"
)

// JSONHandler can handle logs emmited by logrus.TextFormatter loggers.
type JSONHandler struct {
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

func (h *JSONHandler) clear() {
	h.Level = ""
	h.Time = time.Time{}
	h.Message = ""
	h.last = h.Fields
	h.Fields = make(map[string]string)
	if h.buf != nil {
		h.buf.Reset()
	}
}

// TryHandle tells if this line was handled by this handler.
func (h *JSONHandler) TryHandle(d []byte) bool {
	if !bytes.Contains(d, []byte(`"level":"`)) {
		return false
	}
	if !bytes.Contains(d, []byte(`"time":"`)) {
		return false
	}
	if !bytes.Contains(d, []byte(`"msg":"`)) {
		return false
	}
	err := h.UnmarshalJSON(d)
	if err != nil {
		h.clear()
		return false
	}
	return true
}

// UnmarshalJSON sets the fields of the handler.
func (h *JSONHandler) UnmarshalJSON(data []byte) error {
	raw := make(map[string]interface{})
	err := json.Unmarshal(data, &raw)
	if err != nil {
		return err
	}

	timeStr, ok := raw["time"].(string)
	if ok {
		h.Time, ok = tryParseTime(timeStr)
		if !ok {
			return fmt.Errorf("field time is not a known timestamp: %v", timeStr)
		}
	}
	h.Level, _ = raw["level"].(string)
	h.Message, _ = raw["msg"].(string)
	delete(raw, "time")
	delete(raw, "level")
	delete(raw, "msg")

	if h.Fields == nil {
		h.Fields = make(map[string]string)
	}

	for key, val := range raw {
		h.Fields[key] = fmt.Sprintf("%v", val)
	}

	return nil
}

// Prettify the output in a logrus like fashion.
func (h *JSONHandler) Prettify(skipUnchanged bool) []byte {
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
		msg = rgbterm.FgString("<no msg>", 190, 190, 190)
	} else {
		msg = rgbterm.FgString(h.Message, 255, 255, 255)
	}

	lvl := strings.ToUpper(h.Level)[:imin(4, len(h.Level))]
	var level string
	switch h.Level {
	case "debug":
		level = rgbterm.FgString(lvl, 221, 28, 119)
	case "info":
		level = rgbterm.FgString(lvl, 20, 172, 190)
	case "warn", "warning":
		level = rgbterm.FgString(lvl, 255, 245, 32)
	case "error":
		level = rgbterm.FgString(lvl, 255, 0, 0)
	case "fatal", "panic":
		level = rgbterm.BgString(lvl, 255, 0, 0)
	default:
		level = rgbterm.FgString(lvl, 221, 28, 119)
	}

	_, _ = fmt.Fprintf(h.out, "%s |%s| %s\t %s",
		rgbterm.FgString(h.Time.Format(time.Stamp), 99, 99, 99),
		level,
		msg,
		strings.Join(h.joinKVs(skipUnchanged, "="), "\t "),
	)

	_ = h.out.Flush()

	return h.buf.Bytes()
}

func (h *JSONHandler) joinKVs(skipUnchanged bool, sep string) []string {

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

		kstr := rgbterm.FgString(k, h.Opts.KeyRGB.R, h.Opts.KeyRGB.G, h.Opts.KeyRGB.B)

		var vstr string
		if h.Opts.Truncates && len(v) > h.Opts.TruncateLength {
			vstr = v[:h.Opts.TruncateLength] + "..."
		} else {
			vstr = v
		}
		vstr = rgbterm.FgString(vstr, h.Opts.ValRGB.R, h.Opts.ValRGB.G, h.Opts.ValRGB.B)
		kv = append(kv, kstr+sep+vstr)
	}

	sort.Strings(kv)

	if h.Opts.SortLongest {
		sort.Stable(byLongest(kv))
	}

	return kv
}
