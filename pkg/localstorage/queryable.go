package localstorage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
)

type StorageBuilder func(ctx context.Context, ll *slog.Logger, cfg map[string]interface{}) (Storage, error)

var registry = make(map[string]StorageBuilder)

func RegisterStorage(name string, builder StorageBuilder) {
	_, ok := registry[name]
	if ok {
		panic(fmt.Sprintf("already used: %q", name))
	}
	registry[name] = builder
}

func Open(ctx context.Context, name string, ll *slog.Logger, cfg map[string]interface{}) (Storage, error) {
	builder, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("no storage engine with name %q", name)
	}
	return builder(ctx, ll, cfg)
}

type Storage interface {
	Queryable
	SinkFor(ctx context.Context, machineID, sessionID int64) (_ sink.Sink, heartbeatIn time.Duration, _ error)
	Heartbeat(ctx context.Context, machineID, sessionID int64) (time.Duration, error)
}

type Queryable interface {
	Query(context.Context, *typesv1.LogQuery) (<-chan Cursor, error)
}

type Cursor interface {
	IDs() (machineID, sessionID int64)
	Next(context.Context) bool
	Event(*typesv1.LogEvent) error
	Err() error
	Close() error
}
