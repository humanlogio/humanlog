package humanlog

import (
	"bytes"
	"time"

	"github.com/go-logfmt/logfmt"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// LogfmtHandler can handle logs emitted by logrus.TextFormatter loggers.
type LogfmtHandler struct {
	Opts *HandlerOptions

	Level   string
	Time    time.Time
	Message string
	Fields  []*typesv1.KV
}

func (h *LogfmtHandler) clear() {
	h.Level = ""
	h.Time = time.Time{}
	h.Message = ""
	h.Fields = nil
}

// CanHandle tells if this line can be handled by this handler.
func (h *LogfmtHandler) TryHandle(d []byte, out *typesv1.Log) bool {
	if !bytes.ContainsRune(d, '=') {
		return false
	}
	h.clear()
	if !h.UnmarshalLogfmt(d) {
		return false
	}
	if !h.Time.IsZero() {
		out.Timestamp = timestamppb.New(h.Time)
	}
	out.Body = h.Message
	out.SeverityText = h.Level
	out.Attributes = h.Fields

	return true
}

// HandleLogfmt sets the fields of the handler.
func (h *LogfmtHandler) UnmarshalLogfmt(data []byte) bool {
	dec := logfmt.NewDecoder(bytes.NewReader(data))
	for dec.ScanRecord() {
	next_kv:
		for dec.ScanKeyval() {
			key := string(dec.Key())
			val := string(dec.Value())
			if h.Time.IsZero() {
				foundTime := checkEachUntilFound(h.Opts.TimeFields, func(field string) bool {
					if !fieldsEqualAllString(key, field) {
						return false
					}
					time, ok := tryParseTime(val)
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
					if !fieldsEqualAllString(key, field) {
						return false
					}
					h.Message = val
					return true
				})
				if foundMessage {
					continue next_kv
				}
			}

			if len(h.Level) == 0 {
				foundLevel := checkEachUntilFound(h.Opts.LevelFields, func(field string) bool {
					if !fieldsEqualAllString(key, field) {
						return false
					}
					h.Level = val
					return true
				})
				if foundLevel {
					continue next_kv
				}
			}
			h.Fields = append(h.Fields, typesv1.KeyVal(key, typesv1.ValStr(val)))
		}
	}
	return dec.Err() == nil
}
