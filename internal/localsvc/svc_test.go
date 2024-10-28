package localsvc

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	qrv1 "github.com/humanlogio/api/go/svc/query/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/memstorage"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSummarize(t *testing.T) {

	tests := []struct {
		name  string
		req   *qrv1.SummarizeEventsRequest
		input []*typesv1.LogEventGroup
		want  *qrv1.SummarizeEventsResponse
	}{
		{
			name: "all",
			req: &qrv1.SummarizeEventsRequest{
				From:        timestamppb.New(musttime("2006-01-02T15:04:06.001")),
				To:          timestamppb.New(musttime("2006-01-02T15:04:06.005")),
				BucketCount: 100,
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
			want: &qrv1.SummarizeEventsResponse{
				BucketWidth: durationpb.New(40 * time.Microsecond),
				Buckets: []*qrv1.SummarizeEventsResponse_Bucket{
					{Ts: timestamppb.New(musttime("2006-01-02T15:04:06.001")), EventCount: 1},
					{Ts: timestamppb.New(musttime("2006-01-02T15:04:06.002")), EventCount: 1},
					{Ts: timestamppb.New(musttime("2006-01-02T15:04:06.003")), EventCount: 1},
					{Ts: timestamppb.New(musttime("2006-01-02T15:04:06.004")), EventCount: 1},
				},
			},
		},
		{
			name: "one hour long, all data in 1 bucket",
			req: &qrv1.SummarizeEventsRequest{
				From:        timestamppb.New(musttime("2006-01-02T14:04:06.005")),
				To:          timestamppb.New(musttime("2006-01-02T15:04:06.005")),
				BucketCount: 60,
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
			want: &qrv1.SummarizeEventsResponse{
				BucketWidth: durationpb.New(60 * time.Second),
				Buckets: []*qrv1.SummarizeEventsResponse_Bucket{
					{Ts: timestamppb.New(musttime("2006-01-02T15:04:06.005")), EventCount: 4},
				},
			},
		},
		{
			name: "one hour long, two session data in 2 bucket",
			req: &qrv1.SummarizeEventsRequest{
				From:        timestamppb.New(musttime("2006-01-02T14:04:06.005")),
				To:          timestamppb.New(musttime("2006-01-02T15:04:06.005")),
				BucketCount: 60,
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
				{
					MachineId: 4, SessionId: 2,
					Logs: []*typesv1.LogEvent{
						{ParsedAt: timestamppb.New(musttime("2006-01-02T14:45:06.001")), Raw: []byte("hello world 1")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T14:45:06.002")), Raw: []byte("hello world 2")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T14:45:06.003")), Raw: []byte("hello world 3")},
						{ParsedAt: timestamppb.New(musttime("2006-01-02T14:45:06.004")), Raw: []byte("hello world 4")},
					},
				},
			},
			want: &qrv1.SummarizeEventsResponse{
				BucketWidth: durationpb.New(60 * time.Second),
				Buckets: []*qrv1.SummarizeEventsResponse_Bucket{
					{Ts: timestamppb.New(musttime("2006-01-02T14:45:06.005")), EventCount: 4},
					{Ts: timestamppb.New(musttime("2006-01-02T15:04:06.005")), EventCount: 4},
				},
			},
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			ll := slog.New(slog.NewTextHandler(os.Stderr, nil))
			mem := memstorage.NewMemStorage(ll)

			for _, leg := range tt.input {
				snk, _, err := mem.SinkFor(ctx, leg.MachineId, leg.SessionId)
				require.NoError(t, err)
				for _, ev := range leg.Logs {
					err = snk.Receive(ctx, ev)
					require.NoError(t, err)
				}
				err = snk.Close(ctx)
				require.NoError(t, err)
			}

			svc := New(ll, nil, nil, mem)
			got, err := svc.SummarizeEvents(ctx, connect.NewRequest(tt.req))
			require.NoError(t, err)
			require.Equal(t, protojson.Format(tt.want), protojson.Format(got.Msg))

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
