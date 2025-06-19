package humanlog

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
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

	opts := DefaultOptions()

	h := JSONHandler{Opts: opts}
	ev := new(typesv1.Log)
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

	opts := DefaultOptions()
	opts.LevelFields = []string{"mylevel"}
	opts.MessageFields = []string{"mymessage"}
	opts.TimeFields = []string{"mytime"}

	h := JSONHandler{Opts: opts}

	ev := new(typesv1.Log)
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

	opts := DefaultOptions()
	opts.LevelFields = []string{"data.level"}
	opts.MessageFields = []string{"data.message"}
	opts.TimeFields = []string{"data.time"}

	h := JSONHandler{Opts: opts}
	ev := new(typesv1.Log)
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

	opts := DefaultOptions()
	opts.LevelFields = []string{"data.l2.level"}
	opts.MessageFields = []string{"data.l2.message"}
	opts.TimeFields = []string{"data.l2.time"}

	h := JSONHandler{Opts: opts}
	ev := new(typesv1.Log)
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
	h := JSONHandler{Opts: DefaultOptions()}
	ev := new(typesv1.Log)
	raw := []byte(`{"storage":{"session.id":1730187806608637000, "some": {"float": 1.2345}}}`)
	if !h.TryHandle(raw, ev) {
		t.Fatalf("failed to handle log")
	}
	require.Equal(t, 1.2345, findField(h, "storage.some.float").GetF64())
	require.Equal(t, int64(1730187806608637000), findField(h, "storage.session.id").GetI64())
}

func TestJsonHandler_TryHandle_FlattendArrayFields(t *testing.T) {
	handler := JSONHandler{Opts: DefaultOptions()}
	ev := new(typesv1.Log)
	raw := []byte(`{"peers":[{"ID":"10.244.0.126:8083","URI":"10.244.0.126:8083"},{"ID":"10.244.0.206:8083","URI":"10.244.0.206:8083"},{"ID":"10.244.1.150:8083","URI":"10.244.1.150:8083"}],"storage":{"session.id":1730187806608637000, "some": {"float": 1.2345}}}`)
	if !handler.TryHandle(raw, ev) {
		t.Fatalf("failed to handle log")
	}
	require.Equal(t, "10.244.0.126:8083", findField(handler, "peers.0.ID").GetStr())
	require.Equal(t, "10.244.0.126:8083", findField(handler, "peers.0.URI").GetStr())
	require.Equal(t, "10.244.0.206:8083", findField(handler, "peers.1.ID").GetStr())
	require.Equal(t, "10.244.0.206:8083", findField(handler, "peers.1.URI").GetStr())
	require.Equal(t, "10.244.1.150:8083", findField(handler, "peers.2.ID").GetStr())
	require.Equal(t, "10.244.1.150:8083", findField(handler, "peers.2.URI").GetStr())
}

func TestJsonHandler_TryHandle_FlattenedArrayFields_NestedArray(t *testing.T) {
	handler := JSONHandler{Opts: DefaultOptions()}
	ev := new(typesv1.Log)
	raw := []byte(`{"peers":[[1,2,3.14],[4,50.55,[6,7]],["hello","world"],{"foo":"bar"}]}`)
	if !handler.TryHandle(raw, ev) {
		t.Fatalf("failed to handle log")
	}
	require.Equal(t, int64(1), findField(handler, "peers.0.0").GetI64())
	require.Equal(t, int64(2), findField(handler, "peers.0.1").GetI64())
	require.Equal(t, float64(3.14), findField(handler, "peers.0.2").GetF64())
	require.Equal(t, int64(4), findField(handler, "peers.1.0").GetI64())
	require.Equal(t, float64(50.55), findField(handler, "peers.1.1").GetF64())
	require.Equal(t, int64(6), findField(handler, "peers.1.2.0").GetI64())
	require.Equal(t, int64(7), findField(handler, "peers.1.2.1").GetI64())
	require.Equal(t, "hello", findField(handler, "peers.2.0").GetStr())
	require.Equal(t, "world", findField(handler, "peers.2.1").GetStr())
	require.Equal(t, "bar", findField(handler, "peers.3.foo").GetStr())
}

func findField(handler JSONHandler, field string) *typesv1.Val {
	for _, kv := range handler.Fields {
		if kv.Key == field {
			return kv.Value
		}
	}
	return nil
}

func TestParseAsctimeFields(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want *timestamppb.Timestamp
	}{
		{
			name: "asctime",
			raw:  []byte(`{"asctime": ["12-05-05 22:11:08,506248"]}`),
			want: timestamppb.New(time.Date(2012, 5, 5, 22, 11, 8, 506248000, time.UTC)),
		},
		{
			name: "time",
			raw:  []byte(`{"time": "12-05-05 22:11:08,506248"}`),
			want: timestamppb.New(time.Date(2012, 5, 5, 22, 11, 8, 506248000, time.UTC)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := DefaultOptions()
			h := JSONHandler{Opts: opts}
			ev := new(typesv1.Log)
			if !h.TryHandle(test.raw, ev) {
				t.Fatalf("failed to handle log")
			}
			// timezone should be identified before parsing... we can't just treat as UTC
			got := ev.Timestamp
			require.Empty(t, cmp.Diff(test.want, got, protocmp.Transform()))
		})
	}
}

func TestParseKvTime(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want *timestamppb.Timestamp
	}{
		{
			name: "ts1",
			raw:  []byte(`{"ts1": "2012-05-05T22:11:08.506248Z"}`),
			want: timestamppb.New(time.Date(2012, 5, 5, 22, 11, 8, 506248000, time.UTC)),
		},
		{
			name: "ts2",
			raw:  []byte(`{"ts2": "2012-05-05 22:11:08,506248"}`),
			want: timestamppb.New(time.Date(2012, 5, 5, 22, 11, 8, 506248000, time.UTC)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := DefaultOptions()
			opts.DetectTimestamp = true
			h := JSONHandler{Opts: opts}
			ev := new(typesv1.Log)
			if !h.TryHandle(test.raw, ev) {
				t.Fatalf("failed to handle log")
			}
			got := ev.Attributes[0].Value.GetTs()
			require.Empty(t, cmp.Diff(test.want, got, protocmp.Transform()))
		})
	}
}
