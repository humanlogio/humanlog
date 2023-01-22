package sink

import (
	"context"

	"github.com/humanlogio/humanlog/internal/pkg/model"
)

type Sink interface {
	Receive(ctx context.Context, ev *model.Event) error
}

type BatchSink interface {
	ReceiveBatch(ctx context.Context, evs []model.Event) error
}
