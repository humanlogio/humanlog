package humanlog_test

import (
	"fmt"
	"testing"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog"
	"github.com/stretchr/testify/require"
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

	opts := humanlog.DefaultOptions()

	h := humanlog.JSONHandler{Opts: opts}
	ev := new(typesv1.StructuredLogEvent)
	if !h.TryHandle(raw, ev) {
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

	opts := humanlog.DefaultOptions()
	opts.LevelFields = []string{"mylevel"}
	opts.MessageFields = []string{"mymessage"}
	opts.TimeFields = []string{"mytime"}

	h := humanlog.JSONHandler{Opts: opts}

	ev := new(typesv1.StructuredLogEvent)
	if !h.TryHandle(raw, ev) {
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

	opts := humanlog.DefaultOptions()
	opts.LevelFields = []string{"data.level"}
	opts.MessageFields = []string{"data.message"}
	opts.TimeFields = []string{"data.time"}

	h := humanlog.JSONHandler{Opts: opts}
	ev := new(typesv1.StructuredLogEvent)
	if !h.TryHandle(raw, ev) {
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

	opts := humanlog.DefaultOptions()
	opts.LevelFields = []string{"data.l2.level"}
	opts.MessageFields = []string{"data.l2.message"}
	opts.TimeFields = []string{"data.l2.time"}

	h := humanlog.JSONHandler{Opts: opts}
	ev := new(typesv1.StructuredLogEvent)
	if !h.TryHandle(raw, ev) {
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

func TestJsonHandler_TryHandle_LargeNumbers(t *testing.T) {
	h := humanlog.JSONHandler{Opts: humanlog.DefaultOptions()}
	ev := new(typesv1.StructuredLogEvent)
	raw := []byte(`{"storage":{"session.id":1730187806608637000, "some": {"float": 1.2345}}}`)
	if !h.TryHandle(raw, ev) {
		t.Fatalf("failed to handle log")
	}
	require.Equal(t, "1.2345", h.Fields["storage.some.float"])
	require.Equal(t, "1730187806608637000", h.Fields["storage.session.id"])
}

func TestJsonHandler_TryHandle_FlattendArrayFields(t *testing.T) {
	handler := humanlog.JSONHandler{Opts: humanlog.DefaultOptions()}
	ev := new(typesv1.StructuredLogEvent)
	raw := []byte(`{"peers":[{"ID":"10.244.0.126:8083","URI":"10.244.0.126:8083"},{"ID":"10.244.0.206:8083","URI":"10.244.0.206:8083"},{"ID":"10.244.1.150:8083","URI":"10.244.1.150:8083"}],"storage":{"session.id":1730187806608637000, "some": {"float": 1.2345}}}`)
	if !handler.TryHandle(raw, ev) {
		t.Fatalf("failed to handle log")
	}
	require.Equal(t, "\"10.244.0.126:8083\"", handler.Fields["peers.0.ID"])
	require.Equal(t, "\"10.244.0.126:8083\"", handler.Fields["peers.0.URI"])
	require.Equal(t, "\"10.244.0.206:8083\"", handler.Fields["peers.1.ID"])
	require.Equal(t, "\"10.244.0.206:8083\"", handler.Fields["peers.1.URI"])
	require.Equal(t, "\"10.244.1.150:8083\"", handler.Fields["peers.2.ID"])
	require.Equal(t, "\"10.244.1.150:8083\"", handler.Fields["peers.2.URI"])
}

func TestJsonHandler_TryHandle_FlattenedArrayFields_NestedArray(t *testing.T) {
	handler := humanlog.JSONHandler{Opts: humanlog.DefaultOptions()}
	ev := new(typesv1.StructuredLogEvent)
	raw := []byte(`{"peers":[[1,2,3.14],[4,50.55,[6,7]],["hello","world"],{"foo":"bar"}]}`)
	if !handler.TryHandle(raw, ev) {
		t.Fatalf("failed to handle log")
	}
	require.Equal(t, "1", handler.Fields["peers.0.0"])
	require.Equal(t, "2", handler.Fields["peers.0.1"])
	require.Equal(t, "3.14", handler.Fields["peers.0.2"])
	require.Equal(t, "4", handler.Fields["peers.1.0"])
	require.Equal(t, "50.55", handler.Fields["peers.1.1"])
	require.Equal(t, "6", handler.Fields["peers.1.2.0"])
	require.Equal(t, "7", handler.Fields["peers.1.2.1"])
	require.Equal(t, "hello", handler.Fields["peers.2.0"])
	require.Equal(t, "world", handler.Fields["peers.2.1"])
	require.Equal(t, "\"bar\"", handler.Fields["peers.3.foo"])
}
