package sinkbench

import (
	"context"
	"testing"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func RunSinkBenchmark(t *testing.B, makeSink func(t testing.TB) sink.Sink) {

	ctx := context.Background()

	sk := makeSink(t)
	err := sk.Close(ctx)
	require.Error(t, err)

	var makeBatchSink func(t testing.TB) sink.BatchSink
	if _, isBatcher := sk.(sink.BatchSink); isBatcher {
		makeBatchSink = func(t testing.TB) sink.BatchSink {
			return makeSink(t).(sink.BatchSink)
		}
	}
	_ = makeBatchSink

	t.Run("basic", func(b *testing.B) {
		sink := makeSink(t)
		defer func() {
			require.NoError(t, sink.Close(ctx))
		}()
		b.StopTimer()

		evs := makeEvents(b.N)
		n := len(evs)

		b.ResetTimer()
		b.StartTimer()
		for i := 0; i < b.N; i++ {
			_ = sink.Receive(ctx, evs[i%n])
		}
		// do the thing
		b.StopTimer()
	})

	if makeBatchSink == nil {
		return
	}

	// run batcher benchmarks
}

// makeEvents makes up to n events, if it makes sense
func makeEvents(n int) []*typesv1.LogEvent {
	if n > 10000 {
		n = 10000
	}
	out := make([]*typesv1.LogEvent, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, &typesv1.LogEvent{
			ParsedAt: timestamppb.Now(),
			// Raw:
		})
	}
	return out
}
