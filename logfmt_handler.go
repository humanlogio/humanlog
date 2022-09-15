package humanlog

import (
	"bytes"
	"strconv"
	"time"

	"github.com/go-logfmt/logfmt"
	"github.com/humanlogio/humanlog/internal/pkg/model"
)

// LogfmtHandler can handle logs emmited by logrus.TextFormatter loggers.
type LogfmtHandler struct {
	Opts *HandlerOptions

	Level   string
	Time    time.Time
	Message string
	Fields  map[string]string
}

func (h *LogfmtHandler) clear() {
	h.Level = ""
	h.Time = time.Time{}
	h.Message = ""
	h.Fields = make(map[string]string)
}

// CanHandle tells if this line can be handled by this handler.
func (h *LogfmtHandler) TryHandle(d []byte, out *model.Structured) bool {
	if !bytes.ContainsRune(d, '=') {
		return false
	}
	if !h.UnmarshalLogfmt(d) {
		h.clear()
		return false
	}
	out.Time = h.Time
	out.Msg = h.Message
	out.Level = h.Level
	for k, v := range h.Fields {
		out.KVs = append(out.KVs, model.KV{Key: k, Value: v})
	}
	return true
}

// HandleLogfmt sets the fields of the handler.
func (h *LogfmtHandler) UnmarshalLogfmt(data []byte) bool {
	if h.Fields == nil {
		h.Fields = make(map[string]string)
	}
	dec := logfmt.NewDecoder(bytes.NewReader(data))
	for dec.ScanRecord() {
	next_kv:
		for dec.ScanKeyval() {
			key := dec.Key()
			val := dec.Value()
			if h.Time.IsZero() {
				foundTime := checkEachUntilFound(h.Opts.TimeFields, func(field string) bool {
					time, ok := tryParseTime(string(val))
					if ok {
						h.Time = time
					}
					return ok
				})
				if foundTime {
					continue next_kv
				}
			}

			if len(h.Message) == 0 {
				foundMessage := checkEachUntilFound(h.Opts.MessageFields, func(field string) bool {
					if !bytes.Equal(key, []byte(field)) {
						return false
					}
					h.Message = string(val)
					return true
				})
				if foundMessage {
					continue next_kv
				}
			}

			if len(h.Level) == 0 {
				foundLevel := checkEachUntilFound(h.Opts.LevelFields, func(field string) bool {
					if !bytes.Equal(key, []byte(field)) {
						return false
					}
					h.Level = string(val)
					return true
				})
				if foundLevel {
					continue next_kv
				}
			}

			h.Fields[string(key)] = string(val)
		}
	}
	return dec.Err() == nil
}

func (h *LogfmtHandler) setLevel(val []byte)   { h.Level = string(val) }
func (h *LogfmtHandler) setMessage(val []byte) { h.Message = string(val) }
func (h *LogfmtHandler) setTime(val []byte) (parsed bool) {
	valStr := string(val)
	if valFloat, err := strconv.ParseFloat(valStr, 64); err == nil {
		h.Time, parsed = tryParseTime(valFloat)
	} else {
		h.Time, parsed = tryParseTime(string(val))
	}
	return
}
