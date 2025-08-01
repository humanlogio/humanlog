package localstorage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/humanlogio/api/go/svc/feature/v1/featurev1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/sink"
	otlplogssvcpb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	otlpmetricssvcpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	otlptracesvcpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

type AppCtx struct {
	EnsureLoggedIn func(ctx context.Context) error
	Features       featurev1connect.FeatureServiceClient
	Config         *config.Config
	State          *state.State
}

type StorageBuilder func(
	ctx context.Context,
	ll *slog.Logger,
	cfg map[string]interface{},
	app *AppCtx,
) (Storage, error)

var registry = make(map[string]StorageBuilder)

func RegisterStorage(name string, builder StorageBuilder) {
	_, ok := registry[name]
	if ok {
		panic(fmt.Sprintf("already used: %q", name))
	}
	registry[name] = builder
}

func Open(ctx context.Context, name string, ll *slog.Logger, cfg map[string]interface{}, app *AppCtx) (Storage, error) {
	builder, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("no storage engine with name %q", name)
	}
	return builder(ctx, ll, cfg, app)
}

type Storage interface {
	Queryable
	SinkFor(ctx context.Context, res *typesv1.Resource, scope *typesv1.Scope) (_ sink.Sink, _ error)
	Stats(ctx context.Context) (*typesv1.Val, error)
	Close() error

	OTLPLogger
	OTLPTracer
	OTLPMeter

	Alertable
}

type Alertable interface {
	AlertGetOrCreate(ctx context.Context, stackName, groupName, alertName string, create func() *typesv1.AlertState) (*typesv1.AlertState, error)
	AlertUpdateState(ctx context.Context, stackName, groupName, alertName string, state *typesv1.AlertState) error
	AlertDeleteStateNotInList(ctx context.Context, stackName, groupName string, keeplist []string) error
}

type OTLPLogger interface {
	ExportLogs(ctx context.Context, req *otlplogssvcpb.ExportLogsServiceRequest) (*otlplogssvcpb.ExportLogsServiceResponse, error)
}

type OTLPTracer interface {
	ExportTraces(ctx context.Context, req *otlptracesvcpb.ExportTraceServiceRequest) (*otlptracesvcpb.ExportTraceServiceResponse, error)
}

type OTLPMeter interface {
	ExportMetrics(ctx context.Context, req *otlpmetricssvcpb.ExportMetricsServiceRequest) (*otlpmetricssvcpb.ExportMetricsServiceResponse, error)
}

type StreamOption struct {
	BatchSize             int
	BatchTrigger          <-chan time.Time
	NotifyStreamListening func(ctx context.Context)
}

type Queryable interface {
	Parse(ctx context.Context, q string) (*typesv1.Query, error)
	Format(ctx context.Context, q *typesv1.Query) (string, error)

	Query(ctx context.Context, q *typesv1.Query, c *typesv1.Cursor, limit int) (*typesv1.Data, *typesv1.Cursor, error)
	ResolveQueryType(ctx context.Context, query *typesv1.Query) (*typesv1.DataStreamType, error)
	ListSymbols(ctx context.Context, query *typesv1.Query, c *typesv1.Cursor, limit int) ([]*typesv1.Symbol, *typesv1.Cursor, error)

	Stream(ctx context.Context, q *typesv1.Query, cb func(context.Context, *typesv1.Data) (bool, error), opts *StreamOption) error

	GetTraceByID(ctx context.Context, traceID string) (*typesv1.Trace, error)
	GetTraceBySpanID(ctx context.Context, spanID string) (*typesv1.Trace, error)
	GetSpanByID(ctx context.Context, spanID string) (*typesv1.Span, error)
}

type Symbol struct {
	Name string
	Type *typesv1.VarType
}

type Cursor interface {
	IDs() (machineID, sessionID int64)
	Next(context.Context) bool
	Event(*typesv1.LogEvent) error
	Err() error
	Close() error
}
