package humanlog_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/aybabtme/humanlog"
)

func TestJSONHandler_UnmarshalJSON_ParsesFields(t *testing.T) {
	msg := `The service is running on port 8080.`
	level := `info`
	timeFormat := time.RFC3339Nano
	tm, err := time.Parse(timeFormat, "2012-11-01T22:08:41+00:00")
	if err != nil {
		t.Fatalf("failed to parse time: %v", err)
	}

	raw := []byte(fmt.Sprintf(`{ "message": %q, "level": %q, "time": %q }`, msg, level, tm))

	opts := *humanlog.DefaultOptions

	h := humanlog.JSONHandler{Opts: &opts}

	if !h.TryHandle(raw) {
		t.Fatalf("failed to parse log level")
	}

	if h.Level != level {
		t.Fatalf("not equal: expected %q, got %q", level, h.Level)
	}

	if h.Message != msg {
		t.Fatalf("not equal: expected %q, got %q", msg, h.Message)
	}

	if !h.Time.Equal(tm) {
		t.Fatalf("not equal: expected %q, got %q", tm, h.Time)
	}
}

func TestJSONHandler_UnmarshalJSON_ParsesCustomFields(t *testing.T) {
	msg := `The service is running on port 8080.`
	level := `info`
	timeFormat := time.RFC3339Nano
	tm, err := time.Parse(timeFormat, "2012-11-01T22:08:41+00:00")
	if err != nil {
		t.Fatalf("failed to parse time: %v", err)
	}

	raw := []byte(fmt.Sprintf(`{ "mymessage": %q, "mylevel": %q, "mytime": %q }`, msg, level, tm))

	opts := *humanlog.DefaultOptions
	opts.LevelFields = []string{"mylevel"}
	opts.MessageFields = []string{"mymessage"}
	opts.TimeFields = []string{"mytime"}

	h := humanlog.JSONHandler{Opts: &opts}

	if !h.TryHandle(raw) {
		t.Fatalf("failed to parse log level")
	}

	if h.Level != level {
		t.Fatalf("not equal: expected %q, got %q", level, h.Level)
	}

	if h.Message != msg {
		t.Fatalf("not equal: expected %q, got %q", msg, h.Message)
	}

	if !h.Time.Equal(tm) {
		t.Fatalf("not equal: expected %q, got %q", tm, h.Time)
	}
}
func TestJSONHandler_UnmarshalJSON_ParsesCustomNestedFields(t *testing.T) {
	msg := `The service is running on port 8080.`
	level := `info`
	timeFormat := time.RFC3339Nano
	tm, err := time.Parse(timeFormat, "2012-11-01T22:08:41+00:00")
	if err != nil {
		t.Fatalf("failed to parse time: %v", err)
	}

	raw := []byte(fmt.Sprintf(`{ "data": { "message": %q, "level": %q, "time": %q }}`, msg, level, tm))

	opts := *humanlog.DefaultOptions
	opts.LevelFields = []string{"data.level"}
	opts.MessageFields = []string{"data.message"}
	opts.TimeFields = []string{"data.time"}

	h := humanlog.JSONHandler{Opts: &opts}

	if !h.TryHandle(raw) {
		t.Fatalf("failed to handle log")
	}

	if h.Level != level {
		t.Fatalf("not equal: expected %q, got %q", level, h.Level)
	}

	if h.Message != msg {
		t.Fatalf("not equal: expected %q, got %q", msg, h.Message)
	}

	if !h.Time.Equal(tm) {
		t.Fatalf("not equal: expected %q, got %q", tm, h.Time)
	}
}

func TestJSONHandler_UnmarshalJSON_ParsesCustomMultiNestedFields(t *testing.T) {
	msg := `The service is running on port 8080.`
	level := `info`
	timeFormat := time.RFC3339Nano
	tm, err := time.Parse(timeFormat, "2012-11-01T22:08:41+00:00")
	if err != nil {
		t.Fatalf("failed to parse time: %v", err)
	}

	raw := []byte(fmt.Sprintf(`{
	  "data": {
	    "l2": {
	      "message": %q,
	      "level": %q,
	      "time": %q
	    }
	  }
	}`, msg, level, tm))

	opts := *humanlog.DefaultOptions
	opts.LevelFields = []string{"data.l2.level"}
	opts.MessageFields = []string{"data.l2.message"}
	opts.TimeFields = []string{"data.l2.time"}

	h := humanlog.JSONHandler{Opts: &opts}

	if !h.TryHandle(raw) {
		t.Fatalf("failed to handle log")
	}

	if h.Level != level {
		t.Fatalf("not equal: expected %q, got %q", level, h.Level)
	}

	if h.Message != msg {
		t.Fatalf("not equal: expected %q, got %q", msg, h.Message)
	}

	if !h.Time.Equal(tm) {
		t.Fatalf("not equal: expected %q, got %q", tm, h.Time)
	}
}
