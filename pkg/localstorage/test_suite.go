package localstorage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.32.0"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func RunTest(t *testing.T, constructor func(t *testing.T, timeNow func() time.Time, newUlid func() string) Storage) {
	tests := []struct {
		name       string
		q          string
		limit      int
		inputRes   *typesv1.Resource
		inputScope *typesv1.Scope
		input      []*typesv1.Log
		want       []*typesv1.Data
	}{
		{
			name:  "nothing",
			q:     `{from==2006-01-02T15:04:06.00500000Z}`,
			limit: 4,
			inputRes: typesv1.NewResource("", []*typesv1.KV{
				typesv1.FromOTELAttribute(semconv.ServiceName("my service")),
			}),
			inputScope: &typesv1.Scope{Name: "gotest"},
			input: []*typesv1.Log{
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents(nil),
			},
		},
		{
			name:  "all",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 5,
			inputRes: typesv1.NewResource("", []*typesv1.KV{
				typesv1.FromOTELAttribute(semconv.ServiceName("my service")),
			}),
			inputScope: &typesv1.Scope{Name: "gotest"},
			input: []*typesv1.Log{
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.Log{
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))},
				}),
			},
		},
		{
			name:  "all",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 5,
			inputRes: typesv1.NewResource("", []*typesv1.KV{
				typesv1.FromOTELAttribute(semconv.ServiceName("my service")),
			}),
			inputScope: &typesv1.Scope{Name: "gotest"},
			input: []*typesv1.Log{
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.Log{
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))},
				}),
			},
		},
		{
			name:  "all with pagination",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 3,
			inputRes: typesv1.NewResource("", []*typesv1.KV{
				typesv1.FromOTELAttribute(semconv.ServiceName("my service")),
			}),
			inputScope: &typesv1.Scope{Name: "gotest"},
			input: []*typesv1.Log{
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.Log{
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))},
				}),
				dataLogEvents([]*typesv1.Log{
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))},
				}),
			},
		},
		{
			name:  "all with pagination overflow",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 2,
			inputRes: typesv1.NewResource("", []*typesv1.KV{
				typesv1.FromOTELAttribute(semconv.ServiceName("my service")),
			}),
			inputScope: &typesv1.Scope{Name: "gotest"},
			input: []*typesv1.Log{
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.Log{
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))},
				}),
				dataLogEvents([]*typesv1.Log{
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))},
				}),
				dataLogEvents(nil),
			},
		},
		{
			name:  "skip last",
			q:     `{to==2006-01-02T15:04:06.004000000Z}`,
			limit: 4,
			inputRes: typesv1.NewResource("", []*typesv1.KV{
				typesv1.FromOTELAttribute(semconv.ServiceName("my service")),
			}),
			inputScope: &typesv1.Scope{Name: "gotest"},
			input: []*typesv1.Log{
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.Log{
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))},
				}),
			},
		},
		{
			name:  "skip first",
			q:     `{from==2006-01-02T15:04:06.002000000Z to==2006-01-02T15:04:06.005000000Z}`,
			limit: 4,
			inputRes: typesv1.NewResource("", []*typesv1.KV{
				typesv1.FromOTELAttribute(semconv.ServiceName("my service")),
			}),
			inputScope: &typesv1.Scope{Name: "gotest"},
			input: []*typesv1.Log{
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.Log{
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))},
				}),
			},
		},
		{
			name:  "from only",
			q:     `{from==2006-01-02T15:04:06.002000000Z}`,
			limit: 4,
			inputRes: typesv1.NewResource("", []*typesv1.KV{
				typesv1.FromOTELAttribute(semconv.ServiceName("my service")),
			}),
			inputScope: &typesv1.Scope{Name: "gotest"},
			input: []*typesv1.Log{
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.Log{
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004"))},
				}),
			},
		},
		{
			name:  "slice",
			q:     `{from==2006-01-02T15:04:06.002000000Z to==2006-01-02T15:04:06.004000000Z}`,
			limit: 4,
			inputRes: typesv1.NewResource("", []*typesv1.KV{
				typesv1.FromOTELAttribute(semconv.ServiceName("my service")),
			}),
			inputScope: &typesv1.Scope{Name: "gotest"},
			input: []*typesv1.Log{
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3")},
				{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.004")), Raw: []byte("hello world 4")},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.Log{
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002")), Raw: []byte("hello world 2"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.002"))},
					{ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 3"), Timestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003"))},
				}),
			},
		},
		{
			name:  "simple query on `lvl`",
			q:     `{from==2006-01-02T15:04:06.002000000Z to==2006-01-02T15:04:06.004000000Z} filter lvl=="error"`,
			limit: 4,
			inputRes: typesv1.NewResource("", []*typesv1.KV{
				typesv1.FromOTELAttribute(semconv.ServiceName("my service")),
			}),
			inputScope: &typesv1.Scope{Name: "gotest"},
			input: []*typesv1.Log{
				{

					ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"),
					Timestamp:    timestamppb.New(musttime("2006-01-02T15:04:06.001")),
					SeverityText: "error",
					Body:         "some sort of problem",
					Attributes:   []*typesv1.KV{},
				},
				{

					ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.001")), Raw: []byte("hello world 1"),
					Timestamp:    timestamppb.New(musttime("2006-01-02T15:04:06.001")),
					SeverityText: "info",
					Body:         "no problem, all is fine",
					Attributes:   []*typesv1.KV{},
				},
				{

					ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 1"),
					Timestamp:    timestamppb.New(musttime("2006-01-02T15:04:06.003")),
					SeverityText: "error",
					Body:         "some sort of problem a bit later",
					Attributes:   []*typesv1.KV{},
				},
				{

					ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 1"),
					Timestamp:    timestamppb.New(musttime("2006-01-02T15:04:06.003")),
					SeverityText: "info",
					Body:         "no problem, all is fine a bit later",
					Attributes:   []*typesv1.KV{},
				},
				{

					ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.006")), Raw: []byte("hello world 1"),
					Timestamp:    timestamppb.New(musttime("2006-01-02T15:04:06.006")),
					SeverityText: "error",
					Body:         "some sort of problem too late",
					Attributes:   []*typesv1.KV{},
				},
				{

					ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.006")), Raw: []byte("hello world 1"),
					Timestamp:    timestamppb.New(musttime("2006-01-02T15:04:06.006")),
					SeverityText: "info",
					Body:         "no problem, all is fine too late",
					Attributes:   []*typesv1.KV{},
				},
			},
			want: []*typesv1.Data{
				dataLogEvents([]*typesv1.Log{
					{

						ObservedTimestamp: timestamppb.New(musttime("2006-01-02T15:04:06.003")), Raw: []byte("hello world 1"),
						Timestamp:    timestamppb.New(musttime("2006-01-02T15:04:06.003")),
						SeverityText: "error",
						Body:         "some sort of problem a bit later",
						Attributes:   []*typesv1.KV{},
					},
				}),
			},
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			now := time.Date(2025, 3, 10, 20, 55, 20, 0, time.UTC)
			timeNow := func() time.Time {
				return now
			}
			i := 0
			newUlid := func() string {
				i++
				return fmt.Sprintf("ulid-%d", i)
			}

			db := constructor(t, timeNow, newUlid)
			defer db.Close()

			var snk sink.Sink
			defer func() {
				err := snk.Close(ctx)
				require.NoError(t, err)
			}()
			for _, ev := range tt.input {
				if snk == nil {
					var err error
					snk, err = db.SinkFor(ctx, tt.inputRes, tt.inputScope)
					require.NoError(t, err)
				}
				err := snk.Receive(ctx, ev)
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

func dataLogEvents(events []*typesv1.Log) *typesv1.Data {
	return &typesv1.Data{
		Shape: &typesv1.Data_Logs{
			Logs: &typesv1.Logs{Logs: events},
		},
	}
}
