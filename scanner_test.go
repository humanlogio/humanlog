package humanlog

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink/bufsink"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestScannerLongLine(t *testing.T) {
	data := `{"msg":"` + strings.Repeat("a", 3 /*1023*1024*/) + `"}`
	ctx := context.Background()
	src := strings.NewReader(data)

	now := time.Date(2024, 10, 11, 15, 25, 6, 0, time.UTC)
	want := []*typesv1.LogEvent{
		{
			ParsedAt: timestamppb.New(now),
			Raw:      []byte(data),
			Structured: &typesv1.StructuredLogEvent{
				Msg:       strings.Repeat("a", 3 /*1023*1024*/),
				Timestamp: timestamppb.New(time.Time{}),
			},
		},
	}

	opts := DefaultOptions()
	opts.timeNow = func() time.Time {
		return now
	}

	sink := bufsink.NewSizedBufferedSink(100, nil)
	err := Scan(ctx, src, sink, opts)
	require.NoError(t, err, "got %#v", err)

	require.Len(t, sink.Buffered, len(want))
	for i, got := range sink.Buffered {
		require.Equal(t, pjson(want[i]), pjson(got))
	}
}

func TestLargePayload(t *testing.T) {

	ctx := context.Background()
	payload := `{"msg": "hello world"}`
	payload += "\n" + `{"msg":` + strings.Repeat("a", maxBufferSize+1) + `}` // more than 1mb long json payload
	payload += "\n" + `{"msg": "안녕하세요"}`
	payload += "\n" + `{"msg":` + strings.Repeat("a", maxBufferSize*3+1) + `}` // more than 3mb long json payload

	now := time.Date(2024, 10, 11, 15, 25, 6, 0, time.UTC)
	want := []*typesv1.LogEvent{
		{
			ParsedAt: timestamppb.New(now),
			Raw:      []byte(`{"msg": "hello world"}`),
			Structured: &typesv1.StructuredLogEvent{
				Msg:       "hello world",
				Timestamp: timestamppb.New(time.Time{}),
			},
		},
		{
			ParsedAt: timestamppb.New(now),
			Raw:      []byte(`{"msg": "안녕하세요"}`),
			Structured: &typesv1.StructuredLogEvent{
				Msg:       "안녕하세요",
				Timestamp: timestamppb.New(time.Time{}),
			},
		},
	}

	src := strings.NewReader(payload)

	opts := DefaultOptions()
	opts.timeNow = func() time.Time {
		return now
	}

	sink := bufsink.NewSizedBufferedSink(100, nil)
	err := Scan(ctx, src, sink, opts)
	require.NoError(t, err)

	got := sink.Buffered
	require.Equal(t, pjsonslice(want), pjsonslice(got))
}

func TestFlatteningNestedObjects_with_a_big_number(t *testing.T) {

	ctx := context.Background()
	payload := `{"time":"2024-10-29T16:45:54.384776+09:00","level":"DEBUG","source":{"function":"github.com/humanlogio/humanlog/internal/memstorage.(*MemStorageSink).firstMatch","file":"/Users/antoine/code/src/github.com/humanlogio/humanlog/internal/memstorage/memory.go","line":243},"msg":"first match found at index","storage":{"machine.id":5089,"session.id":1730187806608637000,"i":0}}`

	now := time.Date(2024, 11, 26, 4, 0, 0, 0, time.UTC)
	want := []*typesv1.LogEvent{
		{
			ParsedAt: timestamppb.New(now),
			Raw:      []byte(`{"time":"2024-10-29T16:45:54.384776+09:00","level":"DEBUG","source":{"function":"github.com/humanlogio/humanlog/internal/memstorage.(*MemStorageSink).firstMatch","file":"/Users/antoine/code/src/github.com/humanlogio/humanlog/internal/memstorage/memory.go","line":243},"msg":"first match found at index","storage":{"machine.id":5089,"session.id":1730187806608637000,"i":0}}`),
			Structured: &typesv1.StructuredLogEvent{
				Timestamp: timestamppb.New(time.Date(2024, 10, 29, 16, 45, 54, 384776000, time.Local)),
				Lvl:       "DEBUG",
				Msg:       "first match found at index",
				Kvs: []*typesv1.KV{
					typesv1.KeyVal("source.function", typesv1.ValStr("github.com/humanlogio/humanlog/internal/memstorage.(*MemStorageSink).firstMatch")),
					typesv1.KeyVal("source.file", typesv1.ValStr("/Users/antoine/code/src/github.com/humanlogio/humanlog/internal/memstorage/memory.go")),
					typesv1.KeyVal("source.line", typesv1.ValI64(243)),
					typesv1.KeyVal("storage.machine.id", typesv1.ValI64(5089)),
					typesv1.KeyVal("storage.session.id", typesv1.ValI64(1730187806608637000)),
					typesv1.KeyVal("storage.i", typesv1.ValI64(0)),
				},
			},
		},
	}

	src := strings.NewReader(payload)
	opts := DefaultOptions()
	opts.timeNow = func() time.Time {
		return now
	}

	sink := bufsink.NewSizedBufferedSink(100, nil)
	err := Scan(ctx, src, sink, opts)
	require.NoError(t, err)

	got := sink.Buffered
	require.Equal(t, len(want), len(got)) // assume that there's no skipped log events

	n := len(want)
	for i := 0; i < n; i++ {
		actualKvs := make(map[string]*typesv1.Val)
		for _, kv := range got[i].Structured.Kvs {
			actualKvs[kv.Key] = kv.Value
		}
		expectedKvs := make(map[string]*typesv1.Val)
		for _, kv := range want[i].Structured.Kvs {
			expectedKvs[kv.Key] = kv.Value
		}
		require.Equal(t, got[i].ParsedAt, want[i].ParsedAt)
		require.Equal(t, got[i].Raw, want[i].Raw)
		require.Equal(t, got[i].Structured.Timestamp, want[i].Structured.Timestamp)
		require.Equal(t, got[i].Structured.Msg, want[i].Structured.Msg)
		require.Equal(t, got[i].Structured.Lvl, want[i].Structured.Lvl)
		require.Equal(t, expectedKvs, actualKvs)
	}
}

func TestFlatteningNestedObjects_simple(t *testing.T) {
	ctx := context.Background()
	payload := `{"storage": {"from": "2024-10-29T05:47:00Z"}}`

	now := time.Date(2024, 11, 26, 4, 0, 0, 0, time.UTC)
	want := []*typesv1.LogEvent{
		{
			ParsedAt: timestamppb.New(now),
			Raw:      []byte(`{"storage": {"from": "2024-10-29T05:47:00Z"}}`),
			Structured: &typesv1.StructuredLogEvent{
				Timestamp: timestamppb.New(time.Time{}),
				Kvs: []*typesv1.KV{
					{
						Key:   "storage.from",
						Value: typesv1.ValTime(time.Date(2024, 10, 29, 5, 47, 0, 0, time.UTC)),
					},
				},
			},
		},
	}

	src := strings.NewReader(payload)
	opts := DefaultOptions()
	opts.timeNow = func() time.Time {
		return now
	}

	sink := bufsink.NewSizedBufferedSink(100, nil)
	err := Scan(ctx, src, sink, opts)
	require.NoError(t, err)

	got := sink.Buffered
	require.Equal(t, len(want), len(got)) // assume that there's no skipped log events

	for i, got := range sink.Buffered {
		diff := cmp.Diff(want[i], got, protocmp.Transform())
		require.Empty(t, diff)
	}
}

func TestFlatteningNestedObjects_with_arrays(t *testing.T) {
	payload := `{"time":"2024-12-05T06:40:35.247902137Z","level":"DEBUG","source":{"function":"main.realMain.func5.1","file":"github.com/humanlogio/apisvc/cmd/apisvc/server_cmd.go","line":407},"msg":"galaxycache peers updated","selfURI":"10.244.0.126:8083","peers":[{"ID":"10.244.0.126:8083","URI":"10.244.0.126:8083"},{"ID":"10.244.0.206:8083","URI":"10.244.0.206:8083"},{"ID":"10.244.1.150:8083","URI":"10.244.1.150:8083"}]}`

	now := time.Date(2024, 12, 9, 0, 0, 0, 0, time.UTC)
	want := []*typesv1.LogEvent{
		{
			ParsedAt: timestamppb.New(now),
			Raw:      []byte(`{"time":"2024-12-05T06:40:35.247902137Z","level":"DEBUG","source":{"function":"main.realMain.func5.1","file":"github.com/humanlogio/apisvc/cmd/apisvc/server_cmd.go","line":407},"msg":"galaxycache peers updated","selfURI":"10.244.0.126:8083","peers":[{"ID":"10.244.0.126:8083","URI":"10.244.0.126:8083"},{"ID":"10.244.0.206:8083","URI":"10.244.0.206:8083"},{"ID":"10.244.1.150:8083","URI":"10.244.1.150:8083"}]}`),
			Structured: &typesv1.StructuredLogEvent{
				Timestamp: timestamppb.New(time.Date(2024, 12, 5, 6, 40, 35, 247902137, time.UTC)),
				Lvl:       "DEBUG",
				Msg:       "galaxycache peers updated",
				Kvs: []*typesv1.KV{
					{
						Key:   "selfURI",
						Value: typesv1.ValStr("10.244.0.126:8083"),
					},
					{
						Key:   "source.function",
						Value: typesv1.ValStr("main.realMain.func5.1"),
					},
					{
						Key:   "source.file",
						Value: typesv1.ValStr("github.com/humanlogio/apisvc/cmd/apisvc/server_cmd.go"),
					},
					{
						Key:   "source.line",
						Value: typesv1.ValI64(407),
					},
					{
						Key:   "peers.0.ID",
						Value: typesv1.ValStr("10.244.0.126:8083"),
					},
					{
						Key:   "peers.0.URI",
						Value: typesv1.ValStr("10.244.0.126:8083"),
					},
					{
						Key:   "peers.1.ID",
						Value: typesv1.ValStr("10.244.0.206:8083"),
					},
					{
						Key:   "peers.1.URI",
						Value: typesv1.ValStr("10.244.0.206:8083"),
					},
					{
						Key:   "peers.2.ID",
						Value: typesv1.ValStr("10.244.1.150:8083"),
					},
					{
						Key:   "peers.2.URI",
						Value: typesv1.ValStr("10.244.1.150:8083"),
					},
				},
			},
		},
	}

	src := strings.NewReader(payload)
	opts := DefaultOptions()
	opts.timeNow = func() time.Time {
		return now
	}

	sink := bufsink.NewSizedBufferedSink(100, nil)
	ctx := context.Background()
	err := Scan(ctx, src, sink, opts)
	require.NoError(t, err)

	got := sink.Buffered
	require.Equal(t, len(want), len(got)) // assume that there's no skipped log events

	n := len(want)
	for i := 0; i < n; i++ {
		actualKvs := make(map[string]*typesv1.Val)
		for _, kv := range got[i].Structured.Kvs {
			actualKvs[kv.Key] = kv.Value
		}
		expectedKvs := make(map[string]*typesv1.Val)
		for _, kv := range want[i].Structured.Kvs {
			expectedKvs[kv.Key] = kv.Value
		}
		require.Equal(t, got[i].ParsedAt, want[i].ParsedAt)
		require.Equal(t, got[i].Raw, want[i].Raw)
		require.Equal(t, got[i].Structured.Timestamp, want[i].Structured.Timestamp)
		require.Equal(t, got[i].Structured.Msg, want[i].Structured.Msg)
		require.Equal(t, got[i].Structured.Lvl, want[i].Structured.Lvl)
		require.Equal(t, expectedKvs, actualKvs)
	}
}

func TestFlatteningNestedObjects_with_nested_arrays(t *testing.T) {
	payload := `{"time":"2024-12-05T06:40:35.247902137Z","level":"DEBUG","msg":"galaxycache peers updated","peers":[[1,2,3],[4,5,6],[{"ID":"10.244.0.126:8083","URI":"10.244.0.126:8083"},{"ID":"10.244.0.206:8083","URI":"10.244.0.206:8083"},{"ID":"10.244.1.150:8083","URI":"10.244.1.150:8083"}]]}`

	now := time.Date(2024, 12, 9, 0, 0, 0, 0, time.UTC)
	want := []*typesv1.LogEvent{
		{
			ParsedAt: timestamppb.New(now),
			Raw:      []byte(`{"time":"2024-12-05T06:40:35.247902137Z","level":"DEBUG","msg":"galaxycache peers updated","peers":[[1,2,3],[4,5,6],[{"ID":"10.244.0.126:8083","URI":"10.244.0.126:8083"},{"ID":"10.244.0.206:8083","URI":"10.244.0.206:8083"},{"ID":"10.244.1.150:8083","URI":"10.244.1.150:8083"}]]}`),
			Structured: &typesv1.StructuredLogEvent{
				Timestamp: timestamppb.New(time.Date(2024, 12, 5, 6, 40, 35, 247902137, time.UTC)),
				Lvl:       "DEBUG",
				Msg:       "galaxycache peers updated",
				Kvs: []*typesv1.KV{
					{
						Key:   "peers.0.0",
						Value: typesv1.ValI64(1),
					},
					{
						Key:   "peers.0.1",
						Value: typesv1.ValI64(2),
					},
					{
						Key:   "peers.0.2",
						Value: typesv1.ValI64(3),
					},
					{
						Key:   "peers.1.0",
						Value: typesv1.ValI64(4),
					},
					{
						Key:   "peers.1.1",
						Value: typesv1.ValI64(5),
					},
					{
						Key:   "peers.1.2",
						Value: typesv1.ValI64(6),
					},
					{
						Key:   "peers.2.0.ID",
						Value: typesv1.ValStr("10.244.0.126:8083"),
					},
					{
						Key:   "peers.2.0.URI",
						Value: typesv1.ValStr("10.244.0.126:8083"),
					},
					{
						Key:   "peers.2.1.ID",
						Value: typesv1.ValStr("10.244.0.206:8083"),
					},
					{
						Key:   "peers.2.1.URI",
						Value: typesv1.ValStr("10.244.0.206:8083"),
					},
					{
						Key:   "peers.2.2.ID",
						Value: typesv1.ValStr("10.244.1.150:8083"),
					},
					{
						Key:   "peers.2.2.URI",
						Value: typesv1.ValStr("10.244.1.150:8083"),
					},
				},
			},
		},
	}

	src := strings.NewReader(payload)
	opts := DefaultOptions()
	opts.timeNow = func() time.Time {
		return now
	}

	sink := bufsink.NewSizedBufferedSink(100, nil)
	ctx := context.Background()
	err := Scan(ctx, src, sink, opts)
	require.NoError(t, err)

	got := sink.Buffered
	require.Equal(t, len(want), len(got)) // assume that there's no skipped log events

	n := len(want)
	for i := 0; i < n; i++ {
		actualKvs := make(map[string]*typesv1.Val)
		for _, kv := range got[i].Structured.Kvs {
			actualKvs[kv.Key] = kv.Value
		}
		expectedKvs := make(map[string]*typesv1.Val)
		for _, kv := range want[i].Structured.Kvs {
			expectedKvs[kv.Key] = kv.Value
		}
		require.Equal(t, got[i].ParsedAt, want[i].ParsedAt)
		require.Equal(t, got[i].Raw, want[i].Raw)
		require.Equal(t, got[i].Structured.Timestamp, want[i].Structured.Timestamp)
		require.Equal(t, got[i].Structured.Msg, want[i].Structured.Msg)
		require.Equal(t, got[i].Structured.Lvl, want[i].Structured.Lvl)
		require.Equal(t, expectedKvs, actualKvs)
	}
}

func pjsonslice[E proto.Message](m []E) string {
	sb := strings.Builder{}
	for _, e := range m {
		sb.WriteString(pjson(e))
		sb.WriteRune('\n')
	}
	return sb.String()
}

func pjson(m proto.Message) string {
	o, err := protojson.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(o)
}
