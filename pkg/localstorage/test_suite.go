package localstorage

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func RunTest(t *testing.T, constructor func(t *testing.T) Storage) {
	tests := []struct {
		name  string
		q     string
		limit int
		input []*typesv1.IngestedLogEvent
		want  []*typesv1.Data
	}{
		{
			name:  "nothing",
			q:     `{from==2006-01-02T15:04:06.00500000Z}`,
			limit: 4,
			input: []*typesv1.IngestedLogEvent{
				{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents(nil),
			},
		},
		{
			name:  "all",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 5,
			input: []*typesv1.IngestedLogEvent{
				{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))}},
					{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
					{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
					{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))}},
				}),
			},
		},
		{
			name:  "all",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 5,
			input: []*typesv1.IngestedLogEvent{
				{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))}},
					{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
					{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
					{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))}},
				}),
			},
		},
		{
			name:  "all with pagination",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 3,
			input: []*typesv1.IngestedLogEvent{
				{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))}},
					{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
					{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
				}),
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))}},
				}),
			},
		},
		{
			name:  "all with pagination overflow",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 2,
			input: []*typesv1.IngestedLogEvent{
				{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))}},
					{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
				}),
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
					{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))}},
				}),
				dataLogEvents(nil),
			},
		},
		{
			name:  "skip last",
			q:     `{to==2006-01-02T15:04:06.004000000Z}`,
			limit: 4,
			input: []*typesv1.IngestedLogEvent{
				{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))}},
					{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
					{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
				}),
			},
		},
		{
			name:  "skip first",
			q:     `{from==2006-01-02T15:04:06.002000000Z to==2006-01-02T15:04:06.005000000Z}`,
			limit: 4,
			input: []*typesv1.IngestedLogEvent{
				{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
					{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
					{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))}},
				}),
			},
		},
		{
			name:  "from only",
			q:     `{from==2006-01-02T15:04:06.002000000Z}`,
			limit: 4,
			input: []*typesv1.IngestedLogEvent{
				{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
					{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
					{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))}},
				}),
			},
		},
		{
			name:  "slice",
			q:     `{from==2006-01-02T15:04:06.002000000Z to==2006-01-02T15:04:06.004000000Z}`,
			limit: 4,
			input: []*typesv1.IngestedLogEvent{
				{MachineId: 1, SessionId: 2, EventId: 1, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{MachineId: 1, SessionId: 2, EventId: 4, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{MachineId: 1, SessionId: 2, EventId: 2, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))}},
					{MachineId: 1, SessionId: 2, EventId: 3, ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Structured: &typesv1.StructuredLogEvent{Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))}},
				}),
			},
		},
		{
			name:  "simple query on `lvl`",
			q:     `{from==2006-01-02T15:04:06.002000000Z to==2006-01-02T15:04:06.004000000Z} filter lvl=="error"`,
			limit: 4,
			input: []*typesv1.IngestedLogEvent{
				{
					MachineId: 1, SessionId: 2, EventId: 1,
					ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"),
					Structured: &typesv1.StructuredLogEvent{
						Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")),
						Lvl:       "error",
						Msg:       "some sort of problem",
						Kvs:       []*typesv1.KV{},
					},
				},
				{
					MachineId: 1, SessionId: 2, EventId: 2,
					ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"),
					Structured: &typesv1.StructuredLogEvent{
						Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")),
						Lvl:       "info",
						Msg:       "no problem, all is fine",
						Kvs:       []*typesv1.KV{},
					},
				},
				{
					MachineId: 1, SessionId: 2, EventId: 3,
					ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 1"),
					Structured: &typesv1.StructuredLogEvent{
						Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")),
						Lvl:       "error",
						Msg:       "some sort of problem a bit later",
						Kvs:       []*typesv1.KV{},
					},
				},
				{
					MachineId: 1, SessionId: 2, EventId: 4,
					ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 1"),
					Structured: &typesv1.StructuredLogEvent{
						Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")),
						Lvl:       "info",
						Msg:       "no problem, all is fine a bit later",
						Kvs:       []*typesv1.KV{},
					},
				},
				{
					MachineId: 1, SessionId: 2, EventId: 5,
					ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.006")), Raw: []byte("hello world 1"),
					Structured: &typesv1.StructuredLogEvent{
						Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.006")),
						Lvl:       "error",
						Msg:       "some sort of problem too late",
						Kvs:       []*typesv1.KV{},
					},
				},
				{
					MachineId: 1, SessionId: 2, EventId: 6,
					ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.006")), Raw: []byte("hello world 1"),
					Structured: &typesv1.StructuredLogEvent{
						Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.006")),
						Lvl:       "info",
						Msg:       "no problem, all is fine too late",
						Kvs:       []*typesv1.KV{},
					},
				},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.IngestedLogEvent{
					{
						MachineId: 1, SessionId: 2, EventId: 3,
						ParsedAt: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 1"),
						Structured: &typesv1.StructuredLogEvent{
							Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")),
							Lvl:       "error",
							Msg:       "some sort of problem a bit later",
							Kvs:       []*typesv1.KV{},
						},
					},
				}),
			},
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			db := constructor(t)
			defer db.Close()

			var snk sink.Sink
			defer func() {
				err := snk.Close(ctx)
				require.NoError(t, err)
			}()
			for _, ev := range tt.input {
				if snk == nil {
					var err error
					snk, _, err = db.SinkFor(ctx, ev.MachineId, ev.SessionId)
					require.NoError(t, err)
				}
				err := snk.Receive(ctx, &typesv1.LogEvent{
					ParsedAt: ev.ParsedAt, Raw: ev.Raw, Structured: ev.Structured,
				})
				require.NoError(t, err)
			}

			q, err := db.Parse(ctx, tt.q)
			require.NoError(t, err)

			var (
				got []*typesv1.Data
				c   *typesv1.Cursor
			)
			for {
				out, next, err := db.Query(ctx, q, c, int(tt.limit))
				require.NoError(t, err)
				got = append(got, out)
				c = next
				if next == nil {
					break
				}
				require.LessOrEqual(t, len(got), len(tt.want))
			}

			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				diff := cmp.Diff(tt.want[i], got[i], protocmp.Transform())
				require.Empty(t, diff)
			}
		})
	}
}

func musttime(str string) time.Time {
	t, err := time.Parse("2006-01-02T15:04:05.000", str)
	if err != nil {
		panic(err)
	}
	return t
}

func dataLogEvents(events []*typesv1.IngestedLogEvent) *typesv1.Data {
	return &typesv1.Data{
		Shape: &typesv1.Data_Tabular{
			Tabular: &typesv1.Tabular{
				Shape: &typesv1.Tabular_LogEvents{
					LogEvents: &typesv1.LogEvents{
						Events: events,
					},
				},
			},
		},
	}
}
