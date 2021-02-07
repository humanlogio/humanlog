package humanlog

import (
	"reflect"
	"testing"
	"time"
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
			wantLevel:    "debug",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 1",
		},
		{
			name: "error message with caller info",

			logLine: logLinesByLevel["ERROR"],

			wantMatch:    true,
			wantLevel:    "error",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 2",
		},
		{
			name: "fatal message with caller info and exit status",

			logLine: logLinesByLevel["FATAL"],

			wantMatch: true,

			wantLevel:    "fatal",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 5",
		},
		{
			name: "info message",

			logLine: logLinesByLevel["INFO"],

			wantMatch:    true,
			wantLevel:    "info",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 3",
		},
		{

			name: "warning message with caller info",

			logLine: logLinesByLevel["WARN"],

			wantMatch:    true,
			wantLevel:    "warn",
			wantLocation: "zapper/zapper.go:18",
			wantMessage:  "some message 4",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := &JSONHandler{}
			m := tryZapDevPrefix(test.logLine, h)

			if m != test.wantMatch {
				t.Error("expected the prefix to match, it did not")
			}
			// Short circuit - if we want failure don't assert against the handler
			if !test.wantMatch {
				return
			}

			if reflect.DeepEqual(time.Time{}, h.Time) {
				t.Errorf("want a parsed time, got empty time; want != got")
			}
			if h.Level != test.wantLevel {
				t.Errorf("want %q, got %q; want != got", test.wantLevel, h.Level)
			}
			if h.Message != test.wantMessage {
				t.Errorf("want %q, got %q; want != got", test.wantMessage, h.Message)
			}
			if h.Fields["caller"] != test.wantLocation {
				t.Errorf("want %q, got %q; want != got", test.wantLocation, h.Fields["caller"])
			}
		})
	}
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
			wantLevel:    "debug",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 1",
		},
		{
			name: "error message with caller info",

			logLine: dcLogLinesByLevel["ERROR"],

			wantMatch:    true,
			wantLevel:    "error",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 2",
		},
		{
			name: "fatal message with caller info and exit status",

			logLine: dcLogLinesByLevel["FATAL"],

			wantMatch: true,

			wantLevel:    "fatal",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 5",
		},
		{
			name: "info message",

			logLine: dcLogLinesByLevel["INFO"],

			wantMatch:    true,
			wantLevel:    "info",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 3",
		},
		{

			name: "warning message with caller info",

			logLine: dcLogLinesByLevel["WARN"],

			wantMatch:    true,
			wantLevel:    "warn",
			wantLocation: "zapper/zapper.go:17",
			wantMessage:  "some message 4",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := &JSONHandler{}
			m := tryZapDevDCPrefix(test.logLine, h)

			if m != test.wantMatch {
				t.Error("expected the prefix to match, it did not")
			}
			// Short circuit - if we want failure don't assert against the handler
			if !test.wantMatch {
				return
			}

			if reflect.DeepEqual(time.Time{}, h.Time) {
				t.Errorf("want a parsed time, got empty time; want != got")
			}
			if h.Level != test.wantLevel {
				t.Errorf("want %q, got %q; want != got", test.wantLevel, h.Level)
			}
			if h.Message != test.wantMessage {
				t.Errorf("want %q, got %q; want != got", test.wantMessage, h.Message)
			}
			if h.Fields["caller"] != test.wantLocation {
				t.Errorf("want %q, got %q; want != got", test.wantLocation, h.Fields["caller"])
			}
		})
	}
}
