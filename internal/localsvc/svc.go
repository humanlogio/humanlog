package localsvc

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"connectrpc.com/connect"
	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	alertpb "github.com/humanlogio/api/go/svc/alert/v1/alertv1connect"
	dashboardv1 "github.com/humanlogio/api/go/svc/dashboard/v1"
	dashboardpb "github.com/humanlogio/api/go/svc/dashboard/v1/dashboardv1connect"
	igv1 "github.com/humanlogio/api/go/svc/ingest/v1"
	igsvcpb "github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	lhv1 "github.com/humanlogio/api/go/svc/localhost/v1"
	lhsvcpb "github.com/humanlogio/api/go/svc/localhost/v1/localhostv1connect"
	projectv1 "github.com/humanlogio/api/go/svc/project/v1"
	projectpb "github.com/humanlogio/api/go/svc/project/v1/projectv1connect"
	qrv1 "github.com/humanlogio/api/go/svc/query/v1"
	qrsvcpb "github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	userv1 "github.com/humanlogio/api/go/svc/user/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/localstate"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/humanlogio/humanlog/pkg/validate"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type Service struct {
	ll         *slog.Logger
	tracer     trace.Tracer
	ownVersion *typesv1.Version
	storage    localstorage.Storage
	doLogin    func(ctx context.Context, returnToURL string) error
	doLogout   func(ctx context.Context, returnToURL string) error
	doUpdate   func(ctx context.Context) error
	doRestart  func(ctx context.Context) error
	getConfig  func(ctx context.Context) (*typesv1.LocalhostConfig, error)
	setConfig  func(ctx context.Context, cfg *typesv1.LocalhostConfig) error
	whoami     func(ctx context.Context) (*userv1.WhoamiResponse, error)

	db localstate.DB
}

func New(
	ll *slog.Logger,
	ownVersion *typesv1.Version,
	storage localstorage.Storage,
	state localstate.DB,
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
		tracer:     otel.GetTracerProvider().Tracer("humanlog-localhost"),
		ownVersion: ownVersion,
		storage:    storage,
		doLogin:    doLogin,
		doLogout:   doLogout,
		doUpdate:   doUpdate,
		doRestart:  doRestart,
		getConfig:  getConfig,
		setConfig:  setConfig,
		whoami:     whoami,
		db:         state,
	}
}

var (
	_ lhsvcpb.LocalhostServiceHandler     = (*Service)(nil)
	_ igsvcpb.IngestServiceHandler        = (*Service)(nil)
	_ qrsvcpb.QueryServiceHandler         = (*Service)(nil)
	_ qrsvcpb.TraceServiceHandler         = (*Service)(nil)
	_ projectpb.ProjectServiceHandler     = (*Service)(nil)
	_ dashboardpb.DashboardServiceHandler = (*Service)(nil)
	_ alertpb.AlertServiceHandler         = (*Service)(nil)
)

func (svc *Service) AsLoggingOTLP() *LoggingOTLP { return newLoggingOTLP(svc) }
func (svc *Service) AsTracingOTLP() *TracingOTLP { return newTracingOTLP(svc) }
func (svc *Service) AsMetricsOTLP() *MetricsOTLP { return newMetricsOTLP(svc) }

func (svc *Service) Ping(ctx context.Context, req *connect.Request[lhv1.PingRequest]) (*connect.Response[lhv1.PingResponse], error) {
	res := &lhv1.PingResponse{
		ClientVersion:   svc.ownVersion,
		Architecture:    runtime.GOARCH,
		OperatingSystem: runtime.GOOS,
		Meta:            &typesv1.ResMeta{},
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

func (svc *Service) PingStream(ctx context.Context, req *connect.Request[lhv1.PingRequest], srv *connect.ServerStream[lhv1.PingResponse]) error {
	res := &lhv1.PingResponse{
		ClientVersion:   svc.ownVersion,
		Architecture:    runtime.GOARCH,
		OperatingSystem: runtime.GOOS,
		Meta:            &typesv1.ResMeta{},
	}

	whoami, err := svc.whoami(ctx)
	if err != nil {
		if cerr, ok := err.(*connect.Error); ok {
			return cerr
		}
		return connect.NewError(connect.CodeInternal, fmt.Errorf("checking logged in status: %v", err))
	}
	if whoami != nil {
		res.LoggedInUser = &lhv1.PingResponse_UserDetails{
			User:                whoami.User,
			CurrentOrganization: whoami.CurrentOrganization,
			DefaultOrganization: whoami.DefaultOrganization,
		}
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := srv.Send(res); err != nil {
				return connect.NewError(connect.CodeInternal, fmt.Errorf("can't sent to client: %v", err))
			}
		}
	}
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
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get database stats: %v", err))
	}
	out := &lhv1.GetStatsResponse{DatabaseStats: databaseStats}
	return connect.NewResponse(out), nil
}

func (svc *Service) Ingest(ctx context.Context, req *connect.Request[igv1.IngestRequest]) (*connect.Response[igv1.IngestResponse], error) {
	ll := svc.ll
	msg := req.Msg
	resource := msg.Resource
	scope := msg.Scope
	msg.Logs = fixEventsTimestamps(ctx, ll, msg.Logs)

	snk, err := svc.storage.SinkFor(ctx, resource, scope)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	success := false
	defer func() {
		if success {
			return
		}
		err = snk.Close(ctx)
		if err != nil {
			ll.ErrorContext(ctx, "closing sink", slog.Any("err", err))
		}
	}()

	if bsnk, ok := snk.(sink.BatchSink); ok {
		if err := bsnk.ReceiveBatch(ctx, msg.Logs); err != nil {
			ll.ErrorContext(ctx, "ingesting log batch", slog.Any("err", err))
			if cerr, ok := err.(*connect.Error); ok {
				return nil, cerr
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting log batch: %v", err))
		}
	} else {
		for _, ev := range msg.Logs {
			if err := snk.Receive(ctx, ev); err != nil {
				ll.ErrorContext(ctx, "ingesting log", slog.Any("err", err))
				if cerr, ok := err.(*connect.Error); ok {
					return nil, cerr
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting log: %v", err))
			}
		}
	}
	if err = snk.Close(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("closing sink log: %v", err))
	}
	out := &igv1.IngestResponse{}
	return connect.NewResponse(out), nil
}

func fixEventsTimestamps(ctx context.Context, ll *slog.Logger, evs []*typesv1.Log) []*typesv1.Log {
	for i, ev := range evs {
		evs[i] = fixEventTimestamps(ctx, ll, ev)
	}
	return evs
}

func fixEventTimestamps(ctx context.Context, ll *slog.Logger, ev *typesv1.Log) *typesv1.Log {
	if ev.ObservedTimestamp != nil && ev.ObservedTimestamp.Seconds < 0 {
		ev.ObservedTimestamp = timestamppb.Now()
		ll.ErrorContext(ctx, "client is sending invalid parsedat")
	}
	if ev.Timestamp != nil && ev.Timestamp.Seconds < 0 {
		ev.Timestamp = ev.ObservedTimestamp
		ll.ErrorContext(ctx, "client is sending invalid timestamp")
	}
	return ev
}

func (svc *Service) IngestStream(ctx context.Context, req *connect.ClientStream[igv1.IngestStreamRequest]) (*connect.Response[igv1.IngestStreamResponse], error) {
	ll := svc.ll

	var (
		resource *typesv1.Resource
		scope    *typesv1.Scope
	)

	// get the first message which has the metadata to start ingesting
	if !req.Receive() {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("must contain at least a first request"))
	}
	msg := req.Msg()
	resource = msg.Resource
	scope = msg.Scope
	ll.DebugContext(ctx, "receiving data from stream")
	snk, err := svc.storage.SinkFor(ctx, resource, scope)
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
		msg.Logs = fixEventsTimestamps(ctx, ll, msg.Logs)
		if err := bsnk.ReceiveBatch(ctx, msg.Logs); err != nil {
			ll.ErrorContext(ctx, "ingesting log batch", slog.Any("err", err))
			if cerr, ok := err.(*connect.Error); ok {
				return nil, cerr
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting log batch: %v", err))
		}
		// then wait for more
		for req.Receive() {
			msg := req.Msg()
			msg.Logs = fixEventsTimestamps(ctx, ll, msg.Logs)
			if err := bsnk.ReceiveBatch(ctx, msg.Logs); err != nil {
				ll.ErrorContext(ctx, "ingesting log batch", slog.Any("err", err))
				if cerr, ok := err.(*connect.Error); ok {
					return nil, cerr
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting log batch: %v", err))
			}
		}
	} else {
		// ingest the first message
		msg.Logs = fixEventsTimestamps(ctx, ll, msg.Logs)
		for _, ev := range msg.Logs {
			if err := snk.Receive(ctx, ev); err != nil {
				ll.ErrorContext(ctx, "ingesting log", slog.Any("err", err))
				if cerr, ok := err.(*connect.Error); ok {
					return nil, cerr
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting log: %v", err))
			}
		}
		// then wait for more
		for req.Receive() {
			msg := req.Msg()
			msg.Logs = fixEventsTimestamps(ctx, ll, msg.Logs)
			for _, ev := range msg.Logs {
				if ev.ObservedTimestamp != nil && ev.ObservedTimestamp.Seconds < 0 {
					ev.ObservedTimestamp = timestamppb.Now()
					ll.ErrorContext(ctx, "client is sending invalid parsedat", slog.Any("err", err))
				}
				if ev != nil && ev.Timestamp != nil && ev.Timestamp.Seconds < 0 {
					ev.Timestamp = ev.ObservedTimestamp
					ll.ErrorContext(ctx, "client is sending invalid timestamp", slog.Any("err", err))
				}
				if err := snk.Receive(ctx, ev); err != nil {
					ll.ErrorContext(ctx, "ingesting log", slog.Any("err", err))
					if cerr, ok := err.(*connect.Error); ok {
						return nil, cerr
					}
					return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting log: %v", err))
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
	res := &igv1.IngestStreamResponse{}
	return connect.NewResponse(res), nil
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
											typesv1.ExprIdentifier("_indextime"),
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

	freeform, ok := data.Shape.(*typesv1.Data_FreeForm)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expected table data, got %T", data.Shape))
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
	if req.Msg.Limit < 1 {
		req.Msg.Limit = 1000 // default is 1000
	}
	if req.Msg.Limit < 10 {
		req.Msg.Limit = 10 // minimum is 10
	}
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

func (svc *Service) GetTrace(ctx context.Context, req *connect.Request[qrv1.GetTraceRequest]) (*connect.Response[qrv1.GetTraceResponse], error) {
	ctx, span := svc.tracer.Start(ctx, "localsvc.GetTrace")
	defer span.End()
	var (
		trace *typesv1.Trace
		err   error
	)
	switch by := req.Msg.By.(type) {
	case *qrv1.GetTraceRequest_TraceId:
		span.SetAttributes(attribute.String("by.trace_id", by.TraceId))
		traceID, perr := typesv1.TraceIDFromHex(nil, by.TraceId)
		if perr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid trace ID: %v", err))
		}
		trace, err = svc.storage.GetTraceByID(ctx, traceID)
	case *qrv1.GetTraceRequest_SpanId:
		span.SetAttributes(attribute.String("by.span_id", by.SpanId))
		spanID, perr := typesv1.SpanIDFromHex(nil, by.SpanId)
		if perr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid span ID: %v", err))
		}
		trace, err = svc.storage.GetTraceBySpanID(ctx, spanID)
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("getting trace from localstorage: %v", err))
	}
	if trace == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no such trace "))
	}
	span.SetStatus(codes.Ok, "trace found")
	out := &qrv1.GetTraceResponse{Trace: trace}
	return connect.NewResponse(out), nil
}

func (svc *Service) GetSpan(ctx context.Context, req *connect.Request[qrv1.GetSpanRequest]) (*connect.Response[qrv1.GetSpanResponse], error) {
	ctx, span := svc.tracer.Start(ctx, "localsvc.GetSpan", trace.WithAttributes(
		attribute.String("span_id", req.Msg.SpanId),
	))
	defer span.End()

	spanID, err := typesv1.SpanIDFromHex(nil, req.Msg.SpanId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid span ID: %v", err))
	}

	sp, err := svc.storage.GetSpanByID(ctx, spanID)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		if cerr, ok := err.(*connect.Error); ok {
			return nil, cerr
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("getting span from localstorage: %v", err))
	}
	if sp == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no span with ID %x", req.Msg.SpanId))
	}
	span.SetStatus(codes.Ok, "span found")
	out := &qrv1.GetSpanResponse{Span: sp}
	return connect.NewResponse(out), nil
}

func (svc *Service) ListSymbols(ctx context.Context, req *connect.Request[qrv1.ListSymbolsRequest]) (*connect.Response[qrv1.ListSymbolsResponse], error) {
	if req.Msg.Limit < 1 {
		req.Msg.Limit = 1000 // default is 1000
	}
	if req.Msg.Limit < 10 {
		req.Msg.Limit = 10 // minimum is 10
	}
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

func (svc *Service) CreateProject(ctx context.Context, req *connect.Request[projectv1.CreateProjectRequest]) (*connect.Response[projectv1.CreateProjectResponse], error) {
	msg := req.Msg
	out, err := svc.db.CreateProject(ctx, msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) GetProject(ctx context.Context, req *connect.Request[projectv1.GetProjectRequest]) (*connect.Response[projectv1.GetProjectResponse], error) {
	msg := req.Msg
	out, err := svc.db.GetProject(ctx, msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) UpdateProject(ctx context.Context, req *connect.Request[projectv1.UpdateProjectRequest]) (*connect.Response[projectv1.UpdateProjectResponse], error) {
	msg := req.Msg
	out, err := svc.db.UpdateProject(ctx, msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) DeleteProject(ctx context.Context, req *connect.Request[projectv1.DeleteProjectRequest]) (*connect.Response[projectv1.DeleteProjectResponse], error) {
	msg := req.Msg
	out, err := svc.db.DeleteProject(ctx, msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) ListProject(ctx context.Context, req *connect.Request[projectv1.ListProjectRequest]) (*connect.Response[projectv1.ListProjectResponse], error) {
	msg := req.Msg
	out, err := svc.db.ListProject(ctx, msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) CreateDashboard(ctx context.Context, req *connect.Request[dashboardv1.CreateDashboardRequest]) (*connect.Response[dashboardv1.CreateDashboardResponse], error) {
	msg := req.Msg
	out, err := svc.db.CreateDashboard(ctx, msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) GetDashboard(ctx context.Context, req *connect.Request[dashboardv1.GetDashboardRequest]) (*connect.Response[dashboardv1.GetDashboardResponse], error) {
	out, err := svc.db.GetDashboard(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) UpdateDashboard(ctx context.Context, req *connect.Request[dashboardv1.UpdateDashboardRequest]) (*connect.Response[dashboardv1.UpdateDashboardResponse], error) {
	out, err := svc.db.UpdateDashboard(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) DeleteDashboard(ctx context.Context, req *connect.Request[dashboardv1.DeleteDashboardRequest]) (*connect.Response[dashboardv1.DeleteDashboardResponse], error) {
	out, err := svc.db.DeleteDashboard(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) ListDashboard(ctx context.Context, req *connect.Request[dashboardv1.ListDashboardRequest]) (*connect.Response[dashboardv1.ListDashboardResponse], error) {
	out, err := svc.db.ListDashboard(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) CreateAlertGroup(ctx context.Context, req *connect.Request[alertv1.CreateAlertGroupRequest]) (*connect.Response[alertv1.CreateAlertGroupResponse], error) {
	out, err := svc.db.CreateAlertGroup(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), err
}

func (svc *Service) GetAlertGroup(ctx context.Context, req *connect.Request[alertv1.GetAlertGroupRequest]) (*connect.Response[alertv1.GetAlertGroupResponse], error) {
	out, err := svc.db.GetAlertGroup(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), err
}
func (svc *Service) UpdateAlertGroup(ctx context.Context, req *connect.Request[alertv1.UpdateAlertGroupRequest]) (*connect.Response[alertv1.UpdateAlertGroupResponse], error) {
	out, err := svc.db.UpdateAlertGroup(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), err
}
func (svc *Service) DeleteAlertGroup(ctx context.Context, req *connect.Request[alertv1.DeleteAlertGroupRequest]) (*connect.Response[alertv1.DeleteAlertGroupResponse], error) {
	out, err := svc.db.DeleteAlertGroup(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), err
}
func (svc *Service) ListAlertGroup(ctx context.Context, req *connect.Request[alertv1.ListAlertGroupRequest]) (*connect.Response[alertv1.ListAlertGroupResponse], error) {
	out, err := svc.db.ListAlertGroup(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), err
}

func (svc *Service) CreateAlertRule(ctx context.Context, req *connect.Request[alertv1.CreateAlertRuleRequest]) (*connect.Response[alertv1.CreateAlertRuleResponse], error) {
	out, err := svc.db.CreateAlertRule(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) GetAlertRule(ctx context.Context, req *connect.Request[alertv1.GetAlertRuleRequest]) (*connect.Response[alertv1.GetAlertRuleResponse], error) {
	out, err := svc.db.GetAlertRule(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) UpdateAlertRule(ctx context.Context, req *connect.Request[alertv1.UpdateAlertRuleRequest]) (*connect.Response[alertv1.UpdateAlertRuleResponse], error) {
	out, err := svc.db.UpdateAlertRule(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) DeleteAlertRule(ctx context.Context, req *connect.Request[alertv1.DeleteAlertRuleRequest]) (*connect.Response[alertv1.DeleteAlertRuleResponse], error) {
	out, err := svc.db.DeleteAlertRule(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (svc *Service) ListAlertRule(ctx context.Context, req *connect.Request[alertv1.ListAlertRuleRequest]) (*connect.Response[alertv1.ListAlertRuleResponse], error) {
	out, err := svc.db.ListAlertRule(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}
