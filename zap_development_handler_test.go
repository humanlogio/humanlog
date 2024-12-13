package humanlog

import (
	"testing"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
)

var logLinesByLevel = map[string][]byte{
	"DEBUG": []byte(`2021-02-05T12:41:48.053-0700    DEBUG   zapper/zapper.go:18     some message 1   {"rand_index": 1}`),
	"ERROR": []byte(`2021-02-05T12:41:49.059-0700    ERROR   zapper/zapper.go:18     some message 2   {"rand_index": 3}`),
	"FATAL": []byte(`2021-02-05T15:45:04.425-0700    FATAL   zapper/zapper.go:18     some message 5   {"rand_index": 11}`),
	"INFO":  []byte(`2021-02-05T12:41:50.064-0700    INFO    zapper/zapper.go:18     some message 3   {"rand_index": 5}`),
	"WARN":  []byte(`2021-02-05T12:41:51.069-0700    WARN    zapper/zapper.go:18     some message 4   {"rand_index": 7}`),
}

func Test_zapDevLogsPrefixRe(t *testing.T) {
	tests := []struct {
		name         string
		logLine      []byte
		wantTS       string
		wantLevel    string
		wantLocation string
		wantMessage  string
		wantJSON     string
	}{
		{
			name: "debug message",

			logLine: logLinesByLevel["DEBUG"],

			wantTS:       "2021-02-05T12:41:48.053-0700",
			wantLevel:    "DEBUG",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 1",
			wantJSON:     `{"rand_index": 1}`,
		},
		{
			name: "error message",

			logLine: logLinesByLevel["ERROR"],

			wantTS:       "2021-02-05T12:41:49.059-0700",
			wantLevel:    "ERROR",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 2",
			wantJSON:     `{"rand_index": 3}`,
		},
		{
			name:         "fatal message",
			logLine:      logLinesByLevel["FATAL"],
			wantTS:       "2021-02-05T15:45:04.425-0700",
			wantLevel:    "FATAL",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 5",
			wantJSON:     `{"rand_index": 11}`,
		},
		{
			name: "info message",

			logLine: logLinesByLevel["INFO"],

			wantTS:       "2021-02-05T12:41:50.064-0700",
			wantLevel:    "INFO",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 3",
			wantJSON:     `{"rand_index": 5}`,
		},
		{

			name: "warning message",

			logLine: logLinesByLevel["WARN"],

			wantTS:       "2021-02-05T12:41:51.069-0700",
			wantLevel:    "WARN",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 4",
			wantJSON:     `{"rand_index": 7}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matches := zapDevLogsPrefixRe.FindSubmatch(test.logLine)
			if matches != nil {
				result := make(map[string]string)
				for i, name := range zapDevLogsPrefixRe.SubexpNames() {
					if i != 0 && name != "" {
						result[name] = string(matches[i])
					}
				}

				if result["timestamp"] != test.wantTS {
					t.Errorf("want %q, got %q, want != got", test.wantTS, result["timestamp"])
				}

				if result["level"] != test.wantLevel {
					t.Errorf("want %q, got %q, want != got", test.wantLevel, result["level"])
				}

				if result["location"] != test.wantLocation {
					t.Errorf("want %q, got %q, want != got", test.wantLocation, result["location"])
				}

				if result["message"] != test.wantMessage {
					t.Errorf("want %q, got %q, want != got", test.wantMessage, result["message"])
				}

				if result["jsonbody"] != test.wantJSON {
					t.Errorf("want %q, got %q, want != got", test.wantJSON, result["jsonbody"])
				}
			} else {
				t.Errorf("regular expression did not match log line")
			}
		})
	}
}

func Test_tryZapDevPrefix(t *testing.T) {
	tests := []struct {
		name      string
		logLine   []byte
		wantMatch bool

		wantTime     time.Time
		wantLevel    string
		wantLocation string
		wantMessage  string

		wantExtraContext string
	}{
		{
			name: "no match",

			logLine: []byte("that's no good"),

			wantMatch: false,
		},
		{
			name: "debug message",

			logLine: logLinesByLevel["DEBUG"],

			wantMatch:    true,
			wantTime:     time.Date(2021, 2, 5, 12, 41, 48, 53e6, time.FixedZone("", -7*3600)),
			wantLevel:    "debug",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 1",
		},
		{
			name: "error message with caller info",

			logLine: logLinesByLevel["ERROR"],

			wantMatch:    true,
			wantTime:     time.Date(2021, 2, 5, 12, 41, 49, 59e6, time.FixedZone("", -7*3600)),
			wantLevel:    "error",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 2",
		},
		{
			name: "fatal message with caller info and exit status",

			logLine: logLinesByLevel["FATAL"],

			wantMatch:    true,
			wantTime:     time.Date(2021, 2, 5, 15, 45, 4, 425e6, time.FixedZone("", -7*3600)),
			wantLevel:    "fatal",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 5",
		},
		{
			name: "info message",

			logLine: logLinesByLevel["INFO"],

			wantMatch:    true,
			wantTime:     time.Date(2021, 2, 5, 12, 41, 50, 64e6, time.FixedZone("", -7*3600)),
			wantLevel:    "info",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 3",
		},
		{

			name: "warning message with caller info",

			logLine: logLinesByLevel["WARN"],

			wantMatch:    true,
			wantTime:     time.Date(2021, 2, 5, 12, 41, 51, 69e6, time.FixedZone("", -7*3600)),
			wantLevel:    "warn",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 4",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ev := new(typesv1.StructuredLogEvent)
			m := tryZapDevPrefix(test.logLine, ev, &JSONHandler{Opts: DefaultOptions()})

			if m != test.wantMatch {
				t.Error("expected the prefix to match, it did not")
			}
			// Short circuit - if we want failure don't assert against the handler
			if !test.wantMatch {
				return
			}

			if !test.wantTime.Equal(ev.Timestamp.AsTime()) {
				t.Errorf("want %v, got %v; want != got", test.wantTime, ev.Timestamp.AsTime())
			}
			if ev.Lvl != test.wantLevel {
				t.Errorf("want %q, got %q; want != got", test.wantLevel, ev.Lvl)
			}
			if ev.Msg != test.wantMessage {
				t.Errorf("want %q, got %q; want != got", test.wantMessage, ev.Msg)
			}

			if findFieldValue(ev, "caller") != test.wantLocation {
				t.Errorf("want %q, got %q; want != got", test.wantLocation, findFieldValue(ev, "caller"))
			}
		})
	}
}

func findFieldValue(ev *typesv1.StructuredLogEvent, field string) string {
	for _, kv := range ev.Kvs {
		if kv.Key == field {
			return kv.Value.String()
		}
	}
	return ""
}

var dcLogLinesByLevel = map[string][]byte{
	"DEBUG": []byte("2021-02-06T22:55:22.004Z\tDEBUG\tzapper/zapper.go:17\tsome message 1\t{\"rand_index\": 1}"),
	"ERROR": []byte("2021-02-06T22:55:22.008Z\tERROR\tzapper/zapper.go:17\tsome message 2\t{\"rand_index\": 2}"),
	"FATAL": []byte("2021-02-06T22:55:22.009Z\tFATAL\tzapper/zapper.go:17\tsome message 5\t{\"rand_index\": 1}"),
	"INFO":  []byte("2021-02-06T22:55:22.009Z\tINFO\tzapper/zapper.go:17\tsome message 3\t{\"rand_index\": 2}"),
	"WARN":  []byte("2021-02-06T22:55:22.009Z\tWARN\tzapper/zapper.go:17\tsome message 4\t{\"rand_index\": 4}"),
}

func Test_zapDCDevLogsPrefixRe(t *testing.T) {
	tests := []struct {
		name         string
		logLine      []byte
		wantTS       string
		wantLevel    string
		wantLocation string
		wantMessage  string
		wantJSON     string
	}{
		{
			name: "debug message",

			logLine: dcLogLinesByLevel["DEBUG"],

			wantTS:       "2021-02-06T22:55:22.004Z",
			wantLevel:    "DEBUG",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 1",
			wantJSON:     `{"rand_index": 1}`,
		},
		{
			name: "error message",

			logLine: dcLogLinesByLevel["ERROR"],

			wantTS:       "2021-02-06T22:55:22.008Z",
			wantLevel:    "ERROR",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 2",
			wantJSON:     `{"rand_index": 2}`,
		},
		{
			name: "fatal message",

			logLine: dcLogLinesByLevel["FATAL"],

			wantTS:       "2021-02-06T22:55:22.009Z",
			wantLevel:    "FATAL",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 5",
			wantJSON:     `{"rand_index": 1}`,
		},
		{
			name: "info message",

			logLine: dcLogLinesByLevel["INFO"],

			wantTS:       "2021-02-06T22:55:22.009Z",
			wantLevel:    "INFO",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 3",
			wantJSON:     `{"rand_index": 2}`,
		},
		{
			name: "warn message",

			logLine: dcLogLinesByLevel["WARN"],

			wantTS:       "2021-02-06T22:55:22.009Z",
			wantLevel:    "WARN",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 4",
			wantJSON:     `{"rand_index": 4}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matches := zapDevDCLogsPrefixRe.FindSubmatch(test.logLine)
			if matches != nil {
				result := make(map[string]string)
				for i, name := range zapDevLogsPrefixRe.SubexpNames() {
					if i != 0 && name != "" {
						result[name] = string(matches[i])
					}
				}

				if result["timestamp"] != test.wantTS {
					t.Errorf("want %q, got %q, want != got", test.wantTS, result["timestamp"])
				}

				if result["level"] != test.wantLevel {
					t.Errorf("want %q, got %q, want != got", test.wantLevel, result["level"])
				}

				if result["location"] != test.wantLocation {
					t.Errorf("want %q, got %q, want != got", test.wantLocation, result["location"])
				}

				if result["message"] != test.wantMessage {
					t.Errorf("want %q, got %q, want != got", test.wantMessage, result["message"])
				}

				if result["jsonbody"] != test.wantJSON {
					t.Errorf("want %q, got %q, want != got", test.wantJSON, result["jsonbody"])
				}
			} else {
				t.Errorf("regular expression did not match log line")
			}
		})
	}
}

func Test_tryZapDevDCPrefix(t *testing.T) {
	tests := []struct {
		name      string
		logLine   []byte
		wantMatch bool

		wantTime     time.Time
		wantLevel    string
		wantLocation string
		wantMessage  string

		wantExtraContext string
	}{
		{
			name: "no match",

			logLine: []byte("that's no good"),

			wantMatch: false,
		},
		{
			name: "debug message",

			logLine: dcLogLinesByLevel["DEBUG"],

			wantMatch:    true,
			wantTime:     time.Date(2021, 2, 6, 22, 55, 22, 4e6, time.UTC),
			wantLevel:    "debug",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 1",
		},
		{
			name: "error message with caller info",

			logLine: dcLogLinesByLevel["ERROR"],

			wantMatch:    true,
			wantTime:     time.Date(2021, 2, 6, 22, 55, 22, 8e6, time.UTC),
			wantLevel:    "error",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 2",
		},
		{
			name: "fatal message with caller info and exit status",

			logLine: dcLogLinesByLevel["FATAL"],

			wantMatch: true,

			wantTime:     time.Date(2021, 2, 6, 22, 55, 22, 9e6, time.UTC),
			wantLevel:    "fatal",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 5",
		},
		{
			name: "info message",

			logLine: dcLogLinesByLevel["INFO"],

			wantMatch:    true,
			wantTime:     time.Date(2021, 2, 6, 22, 55, 22, 9e6, time.UTC),
			wantLevel:    "info",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 3",
		},
		{

			name: "warning message with caller info",

			logLine: dcLogLinesByLevel["WARN"],

			wantMatch:    true,
			wantTime:     time.Date(2021, 2, 6, 22, 55, 22, 9e6, time.UTC),
			wantLevel:    "warn",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 4",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ev := new(typesv1.StructuredLogEvent)
			m := tryZapDevDCPrefix(test.logLine, ev, &JSONHandler{Opts: DefaultOptions()})

			if m != test.wantMatch {
				t.Error("expected the prefix to match, it did not")
			}
			// Short circuit - if we want failure don't assert against the handler
			if !test.wantMatch {
				return
			}

			if !test.wantTime.Equal(ev.Timestamp.AsTime()) {
				t.Errorf("want %v, got %v; want != got", test.wantTime, ev.Timestamp.AsTime())
			}
			if ev.Lvl != test.wantLevel {
				t.Errorf("want %q, got %q; want != got", test.wantLevel, ev.Lvl)
			}
			if ev.Msg != test.wantMessage {
				t.Errorf("want %q, got %q; want != got", test.wantMessage, ev.Msg)
			}

			if findFieldValue(ev, "caller") != test.wantLocation {
				t.Errorf("want %q, got %q; want != got", test.wantLocation, findFieldValue(ev, "caller"))
			}
		})
	}
}
