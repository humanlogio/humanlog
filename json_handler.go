package humanlog

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aybabtme/flatjson"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// JSONHandler can handle logs emitted by logrus.TextFormatter loggers.
type JSONHandler struct {
	Opts *HandlerOptions

	Level   string
	Time    time.Time
	Message string
	Fields  []*typesv1.KV
}

// searchJSON searches a document for a key using the found func to determine if the value is accepted.
// kvs is the deserialized json document.
// fieldList is a list of field names that should be searched. Sub-documents can be searched by using the dot (.). For example, to search {"data"{"message": "<this field>"}} the item would be data.message
func searchJSON(kvs map[string]interface{}, fieldList []string, found func(key string, value interface{}) bool) bool {
	for i, field := range fieldList {
		splits := strings.SplitN(field, ".", 2)
		if len(splits) > 1 {
			name, fieldKey := splits[0], splits[1]
			val, ok := kvs[name]
			if !ok {
				// the key does not exist in the document
				continue
			}
			if m, ok := val.(map[string]interface{}); ok {
				// its value is JSON and was unmarshaled to map[string]interface{} so search the sub document
				return searchJSON(m, []string{fieldKey}, found)
			}
		} else {
			// this is not a sub-document search, so search the root
			for k, v := range kvs {
				if fieldsEqualAllString(field, k) && found(k, v) {
					if dynamicReordering {
						// the log stream probably will always be using this field
						moveToFront(i, fieldList)
					}
					return true
				}
			}
		}
	}
	return false
}

func (h *JSONHandler) clear() {
	h.Level = ""
	h.Time = time.Time{}
	h.Message = ""
	h.Fields = nil
}

// TryHandle tells if this line was handled by this handler.
func (h *JSONHandler) TryHandle(d []byte, out *typesv1.StructuredLogEvent) bool {
	h.clear()
	if !h.UnmarshalJSON(d) {
		return false
	}
	out.Timestamp = timestamppb.New(h.Time)
	out.Msg = h.Message
	out.Lvl = h.Level
	out.Kvs = h.Fields
	return true
}

func deleteJSONKey(key string, jsonData map[string]interface{}) {
	if _, ok := jsonData[key]; ok {
		// found the key at the root
		delete(jsonData, key)
		return
	}

	splits := strings.SplitN(key, ".", 2)
	if len(splits) < 2 {
		// invalid selector
		return
	}
	k, v := splits[0], splits[1]
	ifce, ok := jsonData[k]
	if !ok {
		return // the key doesn't exist
	}
	if m, ok := ifce.(map[string]interface{}); ok {
		deleteJSONKey(v, m)
	}
}

func getFlattenedFields(v map[string]interface{}) map[string]*typesv1.Val {
	extValues := make(map[string]*typesv1.Val)
	for key, nestedVal := range v {
		switch valTyped := nestedVal.(type) {
		case json.Number:
			if z, err := valTyped.Int64(); err == nil {
				extValues[key] = typesv1.ValI64(z)
				continue
			}
			if f, err := valTyped.Float64(); err == nil {
				extValues[key] = typesv1.ValF64(f)
				continue
			}
			extValues[key] = typesv1.ValStr(valTyped.String())
		case string:
			extValues[key] = typesv1.ValStr(valTyped)
		case bool:
			extValues[key] = typesv1.ValBool(valTyped)
		case []interface{}:
			flattenedArrayFields := getFlattenedArrayFields(valTyped)
			for k, v := range flattenedArrayFields {
				extValues[key+"."+k] = v
			}
		case map[string]interface{}:
			flattenedFields := getFlattenedFields(valTyped)
			for keyNested, valStr := range flattenedFields {
				extValues[key+"."+keyNested] = valStr
			}
		default:
			extValues[key] = typesv1.ValStr(fmt.Sprintf("%v", valTyped))
		}
	}
	return extValues
}

func getFlattenedArrayFields(data []interface{}) map[string]*typesv1.Val {
	flattened := make(map[string]*typesv1.Val)
	for i, v := range data {
		switch vt := v.(type) {
		case json.Number:
			if z, err := vt.Int64(); err == nil {
				flattened[strconv.Itoa(i)] = typesv1.ValI64(z)
			} else if f, err := vt.Float64(); err == nil {
				flattened[strconv.Itoa(i)] = typesv1.ValF64(f)
			} else {
				flattened[strconv.Itoa(i)] = typesv1.ValStr(vt.String())
			}
		case string:
			flattened[strconv.Itoa(i)] = typesv1.ValStr(vt)
		case bool:
			flattened[strconv.Itoa(i)] = typesv1.ValBool(vt)
		case []interface{}:
			flattenedArrayFields := getFlattenedArrayFields(vt)
			for k, v := range flattenedArrayFields {
				flattened[fmt.Sprintf("%d.%s", i, k)] = v
			}
		case map[string]interface{}:
			flattenedFields := getFlattenedFields(vt)
			for k, v := range flattenedFields {
				flattened[fmt.Sprintf("%d.%s", i, k)] = v
			}
		default:
			flattened[strconv.Itoa(i)] = typesv1.ValStr(fmt.Sprintf("%v", vt))
		}
	}
	return flattened
}

// UnmarshalJSON sets the fields of the handler.
func (h *JSONHandler) UnmarshalJSON(data []byte) bool {

	var (
		hasFoundTimestamp = false
		hasFoundLevel     = false
		hasFoundMsg       = false
	)

	_, ok, err := flatjson.ScanObject(data, 0, &flatjson.Callbacks{
		MaxDepth: 99,
		OnFloat: func(prefixes flatjson.Prefixes, val flatjson.Float) {
			key := keyFor(data, prefixes, val.Name)
			if !hasFoundTimestamp {
				hasFoundTimestamp = checkEachUntilFound(h.Opts.TimeFields, func(s string) bool {
					if !fieldsEqualAllString(s, key) {
						return false
					}
					h.Time = parseTimeFloat64(val.Value)
					return true
				})
				if hasFoundTimestamp {
					return
				}
			}
			if !hasFoundLevel {
				hasFoundLevel = checkEachUntilFound(h.Opts.LevelFields, func(s string) bool {
					if !fieldsEqualAllString(s, key) {
						return false
					}
					h.Level = convertBunyanLogLevelF64(val.Value)
					return true
				})
				if hasFoundLevel {
					return
				}
			}
			h.Fields = append(h.Fields, typesv1.KeyVal(key, typesv1.ValF64(val.Value)))
		},
		OnInteger: func(prefixes flatjson.Prefixes, val flatjson.Integer) {
			key := keyFor(data, prefixes, val.Name)
			if !hasFoundTimestamp {
				hasFoundTimestamp = checkEachUntilFound(h.Opts.TimeFields, func(s string) bool {
					if !fieldsEqualAllString(s, key) {
						return false
					}

					h.Time = parseTimeInt64(val.Value)
					return true
				})
				if hasFoundTimestamp {
					return
				}
			}
			if !hasFoundLevel {
				hasFoundLevel = checkEachUntilFound(h.Opts.LevelFields, func(s string) bool {
					if !fieldsEqualAllString(s, key) {
						return false
					}

					h.Level = convertBunyanLogLevelI64(val.Value)
					return true
				})
				if hasFoundLevel {
					return
				}
			}
			h.Fields = append(h.Fields, typesv1.KeyVal(key, typesv1.ValI64(val.Value)))
		},
		OnString: func(prefixes flatjson.Prefixes, val flatjson.String) {
			key := keyFor(data, prefixes, val.Name)
			value, _ := strconv.Unquote(val.Value.String(data))
			if !hasFoundTimestamp {
				if val.Name.IsArrayIndex() && val.Name.Index() == 0 {
					// it might be a weird timestamp in an array (`asctime`)
				}

				hasFoundTimestamp = checkEachUntilFound(h.Opts.TimeFields, func(s string) bool {
					// HACK: `asctime` is a weird format...
					if s == "asctime" && len(prefixes) == 1 && val.Name.IsArrayIndex() && val.Name.Index() == 0 {
						// it might be a weird timestamp in an array (`asctime`)
						// in this case, we look at the name of the key before the value
						key = prefixes.AsString(data)
					}
					if !fieldsEqualAllString(s, key) {
						return false
					}
					h.Time, hasFoundTimestamp = tryParseTimeString(value)
					return hasFoundTimestamp
				})
				if hasFoundTimestamp {
					return
				}
			}
			if !hasFoundLevel {
				hasFoundLevel = checkEachUntilFound(h.Opts.LevelFields, func(s string) bool {
					if !fieldsEqualAllString(s, key) {
						return false
					}
					h.Level = value
					return true
				})
				if hasFoundLevel {
					return
				}
			}
			if !hasFoundMsg {
				hasFoundMsg = checkEachUntilFound(h.Opts.MessageFields, func(s string) bool {
					if !fieldsEqualAllString(s, key) {
						return false
					}
					h.Message = value
					return true
				})
				if hasFoundMsg {
					return
				}
			}
			if h.Opts.DetectTimestamp {
				tryParseTime := func(value string) (time.Time, bool) {
					for _, layout := range TimeFormats {
						ts, err := time.Parse(layout, value)
						if err != nil {
							continue
						}
						return ts, true
					}
					return time.Time{}, false
				}
				ts, ok := tryParseTime(value)
				if ok {
					h.Fields = append(h.Fields, typesv1.KeyVal(key, typesv1.ValTime(ts)))
					return
				}
			}
			h.Fields = append(h.Fields, typesv1.KeyVal(key, typesv1.ValStr(value)))
		},
		OnBoolean: func(prefixes flatjson.Prefixes, val flatjson.Bool) {
			key := keyFor(data, prefixes, val.Name)
			h.Fields = append(h.Fields, typesv1.KeyVal(key, typesv1.ValBool(val.Value)))
		},
		OnNull: func(prefixes flatjson.Prefixes, val flatjson.Null) {
			key := keyFor(data, prefixes, val.Name)
			h.Fields = append(h.Fields, typesv1.KeyVal(key, typesv1.ValNull()))
		},
	})
	if err != nil {
		return false
	}
	return ok
}

func keyFor(data []byte, prefixes flatjson.Prefixes, pfx flatjson.Prefix) string {
	if len(prefixes) == 0 {
		return flatjson.Prefixes{pfx}.AsString(data)
	}
	return append(prefixes, pfx).AsString(data)
}

// convertBunyanLogLevel returns a human readable log level given a numerical bunyan level
// https://github.com/trentm/node-bunyan#levels
func convertBunyanLogLevelF64(level float64) string {
	switch level {
	case 10:
		return "trace"
	case 20:
		return "debug"
	case 30:
		return "info"
	case 40:
		return "warn"
	case 50:
		return "error"
	case 60:
		return "fatal"
	default:
		return "???"
	}
}

// convertBunyanLogLevel returns a human readable log level given a numerical bunyan level
// https://github.com/trentm/node-bunyan#levels
func convertBunyanLogLevelI64(level int64) string {
	switch level {
	case 10:
		return "trace"
	case 20:
		return "debug"
	case 30:
		return "info"
	case 40:
		return "warn"
	case 50:
		return "error"
	case 60:
		return "fatal"
	default:
		return "???"
	}
}
