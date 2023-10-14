package bufsink

import (
	"context"

	"github.com/humanlogio/humanlog/internal/pkg/model"
	"github.com/humanlogio/humanlog/sink"
)

type SizedBuffer struct {
	Buffered []model.Event
	flush    sink.BatchSink
}

var _ sink.Sink = (*SizedBuffer)(nil)

func NewSizedBufferedSink(size int, flush sink.BatchSink) *SizedBuffer {
	return &SizedBuffer{
		Buffered: make([]model.Event, 0, size),
		flush:    flush,
	}
}

func (sn *SizedBuffer) Receive(ctx context.Context, ev *model.Event) error {
	sn.Buffered = append(sn.Buffered, *ev)
	if len(sn.Buffered) == cap(sn.Buffered) {
		if err := sn.flush.ReceiveBatch(ctx, sn.Buffered); err != nil {
			sn.Buffered = sn.Buffered[:len(sn.Buffered)-1]
			return err
		}
		sn.Buffered = sn.Buffered[:0]
	}
	return nil
}
