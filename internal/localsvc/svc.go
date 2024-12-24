package localsvc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/humanlogio/api/go/pkg/logql"
	igv1 "github.com/humanlogio/api/go/svc/ingest/v1"
	igsvcpb "github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	lhv1 "github.com/humanlogio/api/go/svc/localhost/v1"
	lhsvcpb "github.com/humanlogio/api/go/svc/localhost/v1/localhostv1connect"
	qrv1 "github.com/humanlogio/api/go/svc/query/v1"
	qrsvcpb "github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/humanlogio/humanlog/pkg/sink"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Service struct {
	ll         *slog.Logger
	state      *state.State
	ownVersion *typesv1.Version
	storage    localstorage.Storage
}

func New(ll *slog.Logger, state *state.State, ownVersion *typesv1.Version, storage localstorage.Storage) *Service {
	return &Service{ll: ll, state: state, ownVersion: ownVersion, storage: storage}
}

var (
	_ lhsvcpb.LocalhostServiceHandler = (*Service)(nil)
	_ igsvcpb.IngestServiceHandler    = (*Service)(nil)
	_ qrsvcpb.QueryServiceHandler     = (*Service)(nil)
)

func (svc *Service) Ping(ctx context.Context, req *connect.Request[lhv1.PingRequest]) (*connect.Response[lhv1.PingResponse], error) {
	res := &lhv1.PingResponse{
		ClientVersion: svc.ownVersion,
		Meta:          &typesv1.ResMeta{},
	}
	if svc.state.MachineID != nil {
		res.Meta = &typesv1.ResMeta{
			MachineId: *svc.state.MachineID,
		}
	}
	return connect.NewResponse(res), nil
}

func (svc *Service) GetHeartbeat(ctx context.Context, req *connect.Request[igv1.GetHeartbeatRequest]) (*connect.Response[igv1.GetHeartbeatResponse], error) {
	msg := req.Msg
	if msg.MachineId == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("no machine ID present, ensure you're logged in (or authorized) to obtain a machine ID"))
	}
	sessionID := int64(0)
	if msg.SessionId != nil {
		sessionID = int64(*msg.SessionId)
	}
	heartbeat, err := svc.storage.Heartbeat(ctx, int64(*msg.MachineId), sessionID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&igv1.GetHeartbeatResponse{
		HeartbeatIn: durationpb.New(heartbeat),
	}), nil
}

func (svc *Service) Ingest(ctx context.Context, req *connect.Request[igv1.IngestRequest]) (*connect.Response[igv1.IngestResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("not available on localhost"))
}

func fixEventsTimestamps(ctx context.Context, ll *slog.Logger, evs []*typesv1.LogEvent) []*typesv1.LogEvent {
	for i, ev := range evs {
		evs[i] = fixEventTimestamps(ctx, ll, ev)
	}
	return evs
}

func fixEventTimestamps(ctx context.Context, ll *slog.Logger, ev *typesv1.LogEvent) *typesv1.LogEvent {
	if ev.ParsedAt != nil && ev.ParsedAt.Seconds < 0 {
		ev.ParsedAt = timestamppb.Now()
		ll.ErrorContext(ctx, "client is sending invalid parsedat")
	}
	if ev.Structured != nil && ev.Structured.Timestamp != nil && ev.Structured.Timestamp.Seconds < 0 {
		ev.Structured.Timestamp = ev.ParsedAt
		ll.ErrorContext(ctx, "client is sending invalid timestamp")
	}
	return ev
}

func (svc *Service) IngestStream(ctx context.Context, req *connect.ClientStream[igv1.IngestStreamRequest]) (*connect.Response[igv1.IngestStreamResponse], error) {
	ll := svc.ll

	var (
		machineID int64
		sessionID int64
	)

	// get the first message which has the metadata to start ingesting
	if !req.Receive() {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("must contain at least a first request"))
	}
	msg := req.Msg()
	machineID = int64(msg.MachineId)
	sessionID = int64(msg.SessionId)
	if sessionID == 0 {
		sessionID = time.Now().UnixNano()
	}
	if machineID == 0 && svc.state.MachineID != nil {
		machineID = int64(*svc.state.MachineID)
	}
	ll = ll.With(
		slog.Int64("machine_id", machineID),
		slog.Int64("session_id", sessionID),
	)
	ll.DebugContext(ctx, "receiving data from stream")
	snk, heartbeatIn, err := svc.storage.SinkFor(ctx, machineID, sessionID)
	if err != nil {
		ll.ErrorContext(ctx, "obtaining sink for stream", slog.Any("err", err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("obtaining sink for stream: %v", err))
	}
	defer func() {
		if ferr := snk.Close(ctx); ferr != nil {
			if err == nil {
				err = ferr
			} else {
				ll.ErrorContext(ctx, "erroneous exit and also failed to flush", slog.Any("err", err))
			}
		}
	}()

	if bsnk, ok := snk.(sink.BatchSink); ok {
		// ingest the first message
		msg.Events = fixEventsTimestamps(ctx, ll, msg.Events)
		if err := bsnk.ReceiveBatch(ctx, msg.Events); err != nil {
			ll.ErrorContext(ctx, "ingesting event batch", slog.Any("err", err))
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event batch: %v", err))
		}
		// then wait for more
		for req.Receive() {
			msg := req.Msg()
			msg.Events = fixEventsTimestamps(ctx, ll, msg.Events)
			if err := bsnk.ReceiveBatch(ctx, msg.Events); err != nil {
				ll.ErrorContext(ctx, "ingesting event batch", slog.Any("err", err))
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event batch: %v", err))
			}
		}
	} else {
		// ingest the first message
		msg.Events = fixEventsTimestamps(ctx, ll, msg.Events)
		for _, ev := range msg.Events {
			if err := snk.Receive(ctx, ev); err != nil {
				ll.ErrorContext(ctx, "ingesting event", slog.Any("err", err))
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event: %v", err))
			}
		}
		// then wait for more
		for req.Receive() {
			msg := req.Msg()
			msg.Events = fixEventsTimestamps(ctx, ll, msg.Events)
			for _, ev := range msg.Events {
				if ev.ParsedAt != nil && ev.ParsedAt.Seconds < 0 {
					ev.ParsedAt = timestamppb.Now()
					ll.ErrorContext(ctx, "client is sending invalid parsedat", slog.Any("err", err))
				}
				if ev.Structured != nil && ev.Structured.Timestamp != nil && ev.Structured.Timestamp.Seconds < 0 {
					ev.Structured.Timestamp = ev.ParsedAt
					ll.ErrorContext(ctx, "client is sending invalid timestamp", slog.Any("err", err))
				}
				if err := snk.Receive(ctx, ev); err != nil {
					ll.ErrorContext(ctx, "ingesting event", slog.Any("err", err))
					return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event: %v", err))
				}
			}
		}
	}
	if err := req.Err(); err != nil {
		ll.ErrorContext(ctx, "ingesting localhost stream", slog.Any("err", err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting localhost stream: %v", err))
	}
	res := &igv1.IngestStreamResponse{
		SessionId:   uint64(sessionID),
		HeartbeatIn: durationpb.New(heartbeatIn),
	}
	return connect.NewResponse(res), nil
}

func (svc *Service) IngestBidiStream(ctx context.Context, req *connect.BidiStream[igv1.IngestBidiStreamRequest, igv1.IngestBidiStreamResponse]) error {
	return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("not available on localhost"))
}

func (svc *Service) SummarizeEvents(ctx context.Context, req *connect.Request[qrv1.SummarizeEventsRequest]) (*connect.Response[qrv1.SummarizeEventsResponse], error) {
	if req.Msg.From == nil {
		req.Msg.From = timestamppb.New(time.Now().Add(-time.Minute))
	}
	if req.Msg.To == nil {
		req.Msg.To = timestamppb.Now()
	}
	if req.Msg.BucketCount < 1 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("bucket count must be greater than 1"))
	}
	ll := svc.ll.With(
		slog.Time("from", req.Msg.From.AsTime()),
		slog.Time("to", req.Msg.From.AsTime()),
		slog.Int("bucket_count", int(req.Msg.BucketCount)),
		slog.Int("environment_id", int(req.Msg.EnvironmentId)),
	)

	data, _, err := svc.storage.Query(ctx, &typesv1.LogQuery{
		Timerange: &typesv1.Timerange{
			From: typesv1.ExprLiteral(typesv1.ValTimestamp(req.Msg.From)),
			To:   typesv1.ExprLiteral(typesv1.ValTimestamp(req.Msg.To)),
		},
		Query: &typesv1.Statements{
			Statements: []*typesv1.Statement{
				{
					Stmt: &typesv1.Statement_Summarize{
						Summarize: &typesv1.SummarizeOperator{
							AggregateFunction: &typesv1.FuncCall{Name: "count"},
							By: &typesv1.SummarizeOperator_ByOperator{Scalars: []*typesv1.Expr{
								typesv1.ExprIdentifier("ts"),
							}},
						},
					},
				},
			},
		},
	}, nil, int(req.Msg.BucketCount))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("summarizing local storage: %v", err))
	}
	ll.DebugContext(ctx, "queried")

	shape, ok := data.Shape.(*typesv1.Data_ScalarTimeseries)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expected scalar timeseries, got %T", data.Shape))
	}
	sts := shape.ScalarTimeseries

	out := &qrv1.SummarizeEventsResponse{}
	for _, bucket := range sts.Scalars {
		v, ok := bucket.Scalar.Kind.(*typesv1.Scalar_I64)
		if !ok {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expected timeseries of i64, got %T", bucket.Scalar.Kind))
		}
		out.Buckets = append(out.Buckets, &qrv1.SummarizeEventsResponse_Bucket{
			Ts:         bucket.Ts,
			EventCount: uint64(v.I64),
		})
	}
	ll.DebugContext(ctx, "non-zero buckets filled", slog.Int("buckets_len", len(out.Buckets)))
	return connect.NewResponse(out), nil
}

func (svc *Service) WatchQuery(ctx context.Context, req *connect.Request[qrv1.WatchQueryRequest], stream *connect.ServerStream[qrv1.WatchQueryResponse]) error {
	return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("watching queries is going away for now"))
}

func (svc *Service) Parse(ctx context.Context, req *connect.Request[qrv1.ParseRequest]) (*connect.Response[qrv1.ParseResponse], error) {
	query := req.Msg.GetQuery()
	q, err := logql.Parse(query)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("parsing `query`: %v", err))
	}
	out := &qrv1.ParseResponse{Query: q}
	return connect.NewResponse(out), nil
}

func (svc *Service) Query(ctx context.Context, req *connect.Request[qrv1.QueryRequest]) (*connect.Response[qrv1.QueryResponse], error) {
	query := req.Msg.GetQuery()
	if query == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("required: `query`"))
	}
	data, cursor, err := svc.storage.Query(ctx, query, req.Msg.Cursor, int(req.Msg.Limit))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("querying local storage: %v", err))
	}
	out := &qrv1.QueryResponse{
		Next: cursor,
		Data: data,
	}
	return connect.NewResponse(out), nil
}
