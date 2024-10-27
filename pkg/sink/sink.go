package sink

import (
	"context"

	typesv1 "github.com/humanlogio/api/go/types/v1"
)

type Sink interface {
	Receive(ctx context.Context, ev *typesv1.LogEvent) error
	Close(ctx context.Context) error
}

type BatchSink interface {
	ReceiveBatch(ctx context.Context, evs []*typesv1.LogEvent) error
	Close(ctx context.Context) error
}
