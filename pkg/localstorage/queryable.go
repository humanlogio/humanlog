package localstorage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/humanlogio/api/go/svc/feature/v1/featurev1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
)

type AppCtx struct {
	EnsureLoggedIn func(ctx context.Context) error
	Features       featurev1connect.FeatureServiceClient
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
	SinkFor(ctx context.Context, machineID, sessionID int64) (_ sink.Sink, heartbeatIn time.Duration, _ error)
	Heartbeat(ctx context.Context, machineID, sessionID int64) (time.Duration, error)
	Close() error
}

type Queryable interface {
	WatchLogQuery(context.Context, *typesv1.LogQuery) (<-chan Cursor, error)

	Query(ctx context.Context, q *typesv1.LogQuery, c *typesv1.Cursor, limit int) (*typesv1.Data, *typesv1.Cursor, error)
	ResolveQueryType(ctx context.Context, query *typesv1.LogQuery) (*typesv1.DataStreamType, error)
	ListSymbols(ctx context.Context, c *typesv1.Cursor, limit int) ([]*typesv1.Symbol, *typesv1.Cursor, error)
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
