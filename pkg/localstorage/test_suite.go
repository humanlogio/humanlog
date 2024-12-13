package localstorage

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func RunTest(t *testing.T, constructor func(t *testing.T) Storage) {
	tests := []struct {
		name    string
		q       *typesv1.LogQuery
		waitFor time.Duration
		input   []*typesv1.LogEventGroup
		want    []*typesv1.LogEventGroup
	}{
		{
			name: "nothing",
			q: &typesv1.LogQuery{
				To: timestamppb.New(musttime("2006-01-02T15:04:06.000")),
			},
			input: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
					},
				},
			},
			want: []*typesv1.LogEventGroup{},
		},
		{
			name: "all",
			q: &typesv1.LogQuery{
				To: timestamppb.New(musttime("2006-01-02T15:04:06.005")),
			},
			input: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
					},
				},
			},
			want: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))}},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))}},
					},
				},
			},
		},
		{
			name: "skip last",
			q: &typesv1.LogQuery{
				To: timestamppb.New(musttime("2006-01-02T15:04:06.004")),
			},
			input: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
					},
				},
			},
			want: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))}},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
					},
				},
			},
		},
		{
			name: "skip first",
			q: &typesv1.LogQuery{
				From: timestamppb.New(musttime("2006-01-02T15:04:06.002")),
				To:   timestamppb.New(musttime("2006-01-02T15:04:06.005")),
			},
			input: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
					},
				},
			},
			want: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))}},
					},
				},
			},
		},
		{
			name: "from only",
			q: &typesv1.LogQuery{
				From: timestamppb.New(musttime("2006-01-02T15:04:06.002")),
			},
			waitFor: time.Second,
			input: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
					},
				},
			},
			want: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))}},
					},
				},
			},
		},
		{
			name: "slice",
			q: &typesv1.LogQuery{
				From: timestamppb.New(musttime("2006-01-02T15:04:06.002")),
				To:   timestamppb.New(musttime("2006-01-02T15:04:06.004")),
			},
			input: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
					},
				},
			},
			want: []*typesv1.LogEventGroup{
				{
					MachineId: 1, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
					},
				},
			},
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			db := constructor(t)
			defer db.Close()

			for _, leg := range tt.input {
				snk, _, err := db.SinkFor(ctx, leg.MachineId, leg.SessionId)
				require.NoError(t, err)
				for _, ev := range leg.Logs {
					err = snk.Receive(ctx, ev)
					require.NoError(t, err)
				}
				err = snk.Close(ctx)
				require.NoError(t, err)
			}

			queryctx := ctx
			if tt.waitFor != 0 {
				var cancel context.CancelFunc
				queryctx, cancel = context.WithTimeout(ctx, tt.waitFor)
				defer cancel()
			}
			now := time.Now()
			cursors, err := db.Query(queryctx, tt.q)
			require.NoError(t, err)
			got := drainCursors(t, queryctx, cursors)

			if tt.waitFor != 0 {
				queriedFor := time.Since(now)
				require.InDelta(t, tt.waitFor.Milliseconds(), queriedFor.Milliseconds(), 30)
			}

			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				diff := cmp.Diff(tt.want[i], got[i], protocmp.Transform())
				require.Empty(t, diff)
			}
		})
	}
}

func drainCursors(t *testing.T, ctx context.Context, cursors <-chan Cursor) []*typesv1.LogEventGroup {
	out := make([]*typesv1.LogEventGroup, 0, len(cursors))
	for cursor := range cursors {
		mid, sid := cursor.IDs()
		leg := &typesv1.LogEventGroup{
			MachineId: mid, SessionId: sid,
		}
		for cursor.Next(ctx) {
			ev := new(typesv1.LogEvent)
			err := cursor.Event(ev)
			require.NoError(t, err)
			leg.Logs = append(leg.Logs, ev)
		}
		require.NoError(t, cursor.Err())
		out = append(out, leg)
	}
	return out
}

func musttime(str string) time.Time {
	t, err := time.Parse("2006-01-02T15:04:05.000", str)
	if err != nil {
		panic(err)
	}
	return t
}
