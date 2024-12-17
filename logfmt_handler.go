package humanlog

import (
	"bytes"
	"time"

	"github.com/go-logfmt/logfmt"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// LogfmtHandler can handle logs emmited by logrus.TextFormatter loggers.
type LogfmtHandler struct {
	Opts *HandlerOptions

	Level   string
	Time    time.Time
	Message string
	Fields  map[string]*typesv1.Val
}

func (h *LogfmtHandler) clear() {
	h.Level = ""
	h.Time = time.Time{}
	h.Message = ""
	h.Fields = make(map[string]*typesv1.Val)
}

// CanHandle tells if this line can be handled by this handler.
func (h *LogfmtHandler) TryHandle(d []byte, out *typesv1.StructuredLogEvent) bool {
	if !bytes.ContainsRune(d, '=') {
		return false
	}
	h.clear()
	if !h.UnmarshalLogfmt(d) {
		return false
	}
	out.Timestamp = timestamppb.New(h.Time)
	out.Msg = h.Message
	out.Lvl = h.Level
	for k, v := range h.Fields {
		out.Kvs = append(out.Kvs, &typesv1.KV{Key: k, Value: v})
	}
	return true
}

// HandleLogfmt sets the fields of the handler.
func (h *LogfmtHandler) UnmarshalLogfmt(data []byte) bool {
	if h.Fields == nil {
		h.Fields = make(map[string]*typesv1.Val)
	}
	dec := logfmt.NewDecoder(bytes.NewReader(data))
	for dec.ScanRecord() {
	next_kv:
		for dec.ScanKeyval() {
			key := dec.Key()
			val := dec.Value()
			if h.Time.IsZero() {
				foundTime := checkEachUntilFound(h.Opts.TimeFields, func(field string) bool {
					if !bytes.Equal(key, []byte(field)) {
						return false
					}
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

			h.Fields[string(key)] = typesv1.ValStr(string(val))
		}
	}
	return dec.Err() == nil
}
