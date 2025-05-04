package localsvc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	igv1 "github.com/humanlogio/api/go/svc/ingest/v1"
	igsvcpb "github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	lhv1 "github.com/humanlogio/api/go/svc/localhost/v1"
	lhsvcpb "github.com/humanlogio/api/go/svc/localhost/v1/localhostv1connect"
	qrv1 "github.com/humanlogio/api/go/svc/query/v1"
	qrsvcpb "github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/humanlogio/humanlog/pkg/validate"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Service struct {
	ll         *slog.Logger
	state      *state.State
	ownVersion *typesv1.Version
	storage    localstorage.Storage
	doLogin    func(ctx context.Context, returnToURL string) error
	doLogout   func(ctx context.Context, returnToURL string) error
	doUpdate   func(ctx context.Context) error
	doRestart  func(ctx context.Context) error
	getConfig  func(ctx context.Context) (*typesv1.LocalhostConfig, error)
	setConfig  func(ctx context.Context, cfg *typesv1.LocalhostConfig) error
	whoami     func(ctx context.Context) (*userv1.WhoamiResponse, error)
}

func New(
	ll *slog.Logger,
	state *state.State,
	ownVersion *typesv1.Version,
	storage localstorage.Storage,
	doLogin func(ctx context.Context, returnToURL string) error,
	doLogout func(ctx context.Context, returnToURL string) error,
	doUpdate func(ctx context.Context) error,
	doRestart func(ctx context.Context) error,
	getConfig func(ctx context.Context) (*typesv1.LocalhostConfig, error),
	setConfig func(ctx context.Context, cfg *typesv1.LocalhostConfig) error,
	whoami func(ctx context.Context) (*userv1.WhoamiResponse, error),
) *Service {
	return &Service{
		ll:         ll,
		state:      state,
		ownVersion: ownVersion,
		storage:    storage,
		doLogin:    doLogin,
		doLogout:   doLogout,
		doUpdate:   doUpdate,
		doRestart:  doRestart,
		getConfig:  getConfig,
		setConfig:  setConfig,
		whoami:     whoami,
	}
}

var (
	_ lhsvcpb.LocalhostServiceHandler = (*Service)(nil)
	_ igsvcpb.IngestServiceHandler    = (*Service)(nil)
	_ qrsvcpb.QueryServiceHandler     = (*Service)(nil)
)

func (svc *Service) AsLoggingOTLP() *LoggingOTLP { return newLoggingOTLP(svc) }
func (svc *Service) AsTracingOTLP() *TracingOTLP { return newTracingOTLP(svc) }
func (svc *Service) AsMetricsOTLP() *MetricsOTLP { return newMetricsOTLP(svc) }

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
	whoami, err := svc.whoami(ctx)
	if err != nil {
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("checking logged in status: %v", err))
	}
	if whoami != nil {
		res.LoggedInUser = &lhv1.PingResponse_UserDetails{
			User:                whoami.User,
			CurrentOrganization: whoami.CurrentOrganization,
			DefaultOrganization: whoami.DefaultOrganization,
		}
	}

	return connect.NewResponse(res), nil
}

func (svc *Service) DoLogin(ctx context.Context, req *connect.Request[lhv1.DoLoginRequest]) (*connect.Response[lhv1.DoLoginResponse], error) {
	err := svc.doLogin(ctx, req.Msg.ReturnToURL)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to login: %v", err))
	}
	out := &lhv1.DoLoginResponse{}
	return connect.NewResponse(out), nil
}

func (svc *Service) DoLogout(ctx context.Context, req *connect.Request[lhv1.DoLogoutRequest]) (*connect.Response[lhv1.DoLogoutResponse], error) {
	err := svc.doLogout(ctx, req.Msg.ReturnToURL)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to logout: %v", err))
	}
	out := &lhv1.DoLogoutResponse{}
	return connect.NewResponse(out), nil
}

func (svc *Service) DoUpdate(ctx context.Context, req *connect.Request[lhv1.DoUpdateRequest]) (*connect.Response[lhv1.DoUpdateResponse], error) {
	err := svc.doUpdate(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update: %v", err))
	}
	out := &lhv1.DoUpdateResponse{}
	return connect.NewResponse(out), nil
}

func (svc *Service) DoRestart(ctx context.Context, req *connect.Request[lhv1.DoRestartRequest]) (*connect.Response[lhv1.DoRestartResponse], error) {
	err := svc.doRestart(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restart: %v", err))
	}
	out := &lhv1.DoRestartResponse{}
	return connect.NewResponse(out), nil
}

func (svc *Service) GetConfig(ctx context.Context, req *connect.Request[lhv1.GetConfigRequest]) (*connect.Response[lhv1.GetConfigResponse], error) {
	cfg, err := svc.getConfig(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get config: %v", err))
	}
	out := &lhv1.GetConfigResponse{Config: cfg}
	return connect.NewResponse(out), nil
}

func (svc *Service) SetConfig(ctx context.Context, req *connect.Request[lhv1.SetConfigRequest]) (*connect.Response[lhv1.SetConfigResponse], error) {
	if err := svc.setConfig(ctx, req.Msg.Config); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set config: %v", err))
	}
	out := &lhv1.SetConfigResponse{}
	return connect.NewResponse(out), nil
}

func (svc *Service) GetStats(ctx context.Context, req *connect.Request[lhv1.GetStatsRequest]) (*connect.Response[lhv1.GetStatsResponse], error) {
	databaseStats, err := svc.storage.Stats(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set database stats: %v", err))
	}
	out := &lhv1.GetStatsResponse{DatabaseStats: databaseStats}
	return connect.NewResponse(out), nil
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
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
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
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
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
			if cerr, ok := err.(*connect.Error); ok {
				return nil, cerr
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event batch: %v", err))
		}
		// then wait for more
		for req.Receive() {
			msg := req.Msg()
			msg.Events = fixEventsTimestamps(ctx, ll, msg.Events)
			if err := bsnk.ReceiveBatch(ctx, msg.Events); err != nil {
				ll.ErrorContext(ctx, "ingesting event batch", slog.Any("err", err))
				if cerr, ok := err.(*connect.Error); ok {
					return nil, cerr
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event batch: %v", err))
			}
		}
	} else {
		// ingest the first message
		msg.Events = fixEventsTimestamps(ctx, ll, msg.Events)
		for _, ev := range msg.Events {
			if err := snk.Receive(ctx, ev); err != nil {
				ll.ErrorContext(ctx, "ingesting event", slog.Any("err", err))
				if cerr, ok := err.(*connect.Error); ok {
					return nil, cerr
				}
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
					if cerr, ok := err.(*connect.Error); ok {
						return nil, cerr
					}
					return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event: %v", err))
				}
			}
		}
	}
	if err := req.Err(); err != nil {
		ll.ErrorContext(ctx, "ingesting localhost stream", slog.Any("err", err))
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
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

	period := req.Msg.To.AsTime().Sub(req.Msg.From.AsTime())
	bucketWidth := period / time.Duration(req.Msg.BucketCount)

	data, _, err := svc.storage.Query(ctx, &typesv1.Query{
		Timerange: &typesv1.Timerange{
			From: typesv1.ExprLiteral(typesv1.ValTimestamp(req.Msg.From)),
			To:   typesv1.ExprLiteral(typesv1.ValTimestamp(req.Msg.To)),
		},
		Query: &typesv1.Statements{
			Statements: []*typesv1.Statement{
				{
					Stmt: &typesv1.Statement_Summarize{
						Summarize: &typesv1.SummarizeOperator{
							Parameters: &typesv1.SummarizeOperator_Parameters{
								Parameters: []*typesv1.SummarizeOperator_Parameter{
									{AggregateFunction: &typesv1.FuncCall{Name: "count"}},
								},
							},
							ByGroupExpressions: &typesv1.SummarizeOperator_ByGroupExpressions{
								Groups: []*typesv1.SummarizeOperator_ByGroupExpression{
									{
										Scalar: typesv1.ExprFuncCall("bin",
											typesv1.ExprIdentifier("ts"),
											typesv1.ExprLiteral(typesv1.ValDuration(bucketWidth)),
										),
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil, int(req.Msg.BucketCount))
	if err != nil {
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("summarizing local storage: %v", err))
	}
	ll.DebugContext(ctx, "queried")

	shape, ok := data.Shape.(*typesv1.Data_Tabular)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expected tabular data, got %T", data.Shape))
	}
	freeform, ok := shape.Tabular.Shape.(*typesv1.Tabular_FreeForm)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expected a freeform's table, got %T", shape.Tabular.Shape))
	}
	table := freeform.FreeForm
	header := table.Type
	if len(header.Columns) != 2 {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expected 2 columns in table, got %d", len(header.Columns)))
	}
	if sc := header.Columns[0].Type.GetScalar(); sc != typesv1.ScalarType_ts {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expected 1st column to be a timestamp, got %v", header.Columns[0].Type))
	}
	if sc := header.Columns[1].Type.GetScalar(); sc != typesv1.ScalarType_i64 {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expected 2nd column to be an i64, got %v", header.Columns[1].Type))
	}

	out := &qrv1.SummarizeEventsResponse{}
	for _, row := range table.Rows {
		ts := row.Items[0].GetTs()
		count := row.Items[1].GetI64()
		out.Buckets = append(out.Buckets, &qrv1.SummarizeEventsResponse_Bucket{
			Ts:         ts,
			EventCount: uint64(count),
		})
	}
	ll.DebugContext(ctx, "non-zero buckets filled", slog.Int("buckets_len", len(out.Buckets)))
	return connect.NewResponse(out), nil
}

func (svc *Service) Parse(ctx context.Context, req *connect.Request[qrv1.ParseRequest]) (*connect.Response[qrv1.ParseResponse], error) {
	query := req.Msg.GetQuery()

	q, err := svc.storage.Parse(ctx, query)
	if err != nil {
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("parsing query: %v", err))
	}
	dst, err := svc.storage.ResolveQueryType(ctx, q)
	if err != nil {
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("resolving query's data type: %v", err))
	}
	out := &qrv1.ParseResponse{Query: q, DataType: dst}
	return connect.NewResponse(out), nil
}

func (svc *Service) Format(ctx context.Context, req *connect.Request[qrv1.FormatRequest]) (*connect.Response[qrv1.FormatResponse], error) {
	query := req.Msg.GetQuery()

	var parsed *typesv1.Query
	switch q := query.(type) {
	case *qrv1.FormatRequest_Parsed:
		parsed = q.Parsed
	case *qrv1.FormatRequest_Raw:
		v, err := svc.storage.Parse(ctx, q.Raw)
		if err != nil {
			if cerr, ok := err.(*connect.Error); ok {
				return nil, cerr
			}
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("parsing query: %v", err))
		}
		parsed = v

	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unsupported query option %T", q))
	}

	formatted, err := svc.storage.Format(ctx, parsed)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("formatting query: %v", err))
	}

	out := &qrv1.FormatResponse{Formatted: formatted}
	return connect.NewResponse(out), nil
}

func (svc *Service) Query(ctx context.Context, req *connect.Request[qrv1.QueryRequest]) (*connect.Response[qrv1.QueryResponse], error) {
	query := req.Msg.GetQuery()
	if query == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("required: `query`"))
	}
	data, cursor, err := svc.storage.Query(ctx, query, req.Msg.Cursor, int(req.Msg.Limit))
	if err != nil {
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("querying local storage: %v", err))
	}
	out := &qrv1.QueryResponse{
		Next: cursor,
		Data: data,
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) Stream(ctx context.Context, req *connect.Request[qrv1.StreamRequest], srv *connect.ServerStream[qrv1.StreamResponse]) error {
	query := req.Msg.GetQuery()
	if query == nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("required: `query`"))
	}

	batchSize, err := validate.StreamResolveBatchSize(int(req.Msg.MaxBatchSize))
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	ticker, doneTicker, err := validate.StreamResolveBatchTicker(req.Msg.MaxBatchingFor)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	defer doneTicker()

	opts := &localstorage.StreamOption{
		BatchSize:    batchSize,
		BatchTrigger: ticker,
	}

	err = svc.storage.Stream(ctx, query, func(ctx context.Context, d *typesv1.Data) (bool, error) {
		return true, srv.Send(&qrv1.StreamResponse{Data: d})
	}, opts)
	if err != nil {
		if cerr, ok := err.(*connect.Error); ok {
			return cerr
		}
		return connect.NewError(connect.CodeInternal, fmt.Errorf("streaming from local storage: %v", err))
	}
	return nil
}

func (svc *Service) ListSymbols(ctx context.Context, req *connect.Request[qrv1.ListSymbolsRequest]) (*connect.Response[qrv1.ListSymbolsResponse], error) {
	symbols, next, err := svc.storage.ListSymbols(ctx, nil, req.Msg.Cursor, int(req.Msg.Limit))
	if err != nil {
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("querying local storage: %v", err))
	}
	out := &qrv1.ListSymbolsResponse{
		Next:  next,
		Items: make([]*qrv1.ListSymbolsResponse_ListItem, 0, len(symbols)),
	}
	for _, sym := range symbols {
		out.Items = append(out.Items, &qrv1.ListSymbolsResponse_ListItem{
			Symbol: sym,
		})
	}
	return connect.NewResponse(out), nil
}
