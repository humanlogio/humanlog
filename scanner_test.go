package humanlog

import (
	"context"
	"strings"
	"testing"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink/bufsink"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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

func TestFlatteningNestedObjects(t *testing.T) {

	ctx := context.Background()
	payload := `{"time":"2024-10-29T16:45:54.384776+09:00","level":"DEBUG","source":{"function":"github.com/humanlogio/humanlog/internal/memstorage.(*MemStorageSink).firstMatch","file":"/Users/antoine/code/src/github.com/humanlogio/humanlog/internal/memstorage/memory.go","line":243},"msg":"first match found at index","storage":{"machine.id":5089,"session.id":1730187806608637000,"i":0}}`

	now := time.Date(2024, 11, 26, 4, 0, 0, 0, time.UTC)
	want := []*typesv1.LogEvent{
		{
			ParsedAt: timestamppb.New(now),
			Raw:      []byte(`{"time":"2024-10-29T16:45:54.384776+09:00","level":"DEBUG","source":{"function":"github.com/humanlogio/humanlog/internal/memstorage.(*MemStorageSink).firstMatch","file":"/Users/antoine/code/src/github.com/humanlogio/humanlog/internal/memstorage/memory.go","line":243},"msg":"first match found at index","storage":{"machine.id":5089,"session.id":1730187806608637000,"i":0}}`),
			Structured: &typesv1.StructuredLogEvent{
				Timestamp: timestamppb.New(time.Date(2024, 10, 29, 16, 14, 54, 384776000, time.Local)),
				Lvl:       "DEBUG",
				Msg:       "first match found at index",
				Kvs: []*typesv1.KV{
					{
						Key:   "source.function",
						Value: "github.com/humanlogio/humanlog/internal/memstorage.(*MemStorageSink).firstMatch",
					},
					{
						Key:   "source.file",
						Value: "/Users/antoine/code/src/github.com/humanlogio/humanlog/internal/memstorage/memory.go",
					},
					{
						Key:   "source.line",
						Value: "243",
					},
					{
						Key:   "storage.machine.id",
						Value: "5089",
					},
					{
						Key:   "storage.session.id",
						Value: "1730187806608637000",
					},
					{
						Key:   "storage.i",
						Value: "0",
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

	for i, got := range sink.Buffered {
		expectedKvs := make(map[string]string)
		for _, kv := range want[i].Structured.Kvs {
			expectedKvs[kv.Key] = kv.Value
		}
		actualKvs := make(map[string]string)
		for _, kv := range got.Structured.Kvs {
			actualKvs[kv.Key] = kv.Value
		}
		require.Equal(t, expectedKvs, actualKvs)
	}
}

func TestKV(t *testing.T) {
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
						Value: time.Date(2024, 10, 29, 5, 47, 0, 0, time.UTC).Format(time.RFC3339),
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

	n := len(want)
	for i := 0; i < n; i++ {
		actual := make(map[string]string)
		for _, kv := range got[i].Structured.Kvs {
			actual[kv.Key] = kv.Value
		}
		expected := make(map[string]string)
		for _, kv := range want[i].Structured.Kvs {
			expected[kv.Key] = kv.Value
		}
		require.Equal(t, expected, actual)
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
