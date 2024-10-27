package localstorage

import (
	"context"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
)

type Storage interface {
	Queryable
	SinkFor(machineID, sessionID int64) (_ sink.Sink, heartbeatIn time.Duration, _ error)
	Heartbeat(ctx context.Context, machineID, sessionID int64) (time.Duration, error)
}

type Queryable interface {
	Query(context.Context, *typesv1.LogQuery) (<-chan Cursor, error)
}

type Cursor interface {
	IDs() (machineID, sessionID int64)
	Next(context.Context) bool
	Event() *typesv1.LogEvent
	Err() error
	Close() error
}
