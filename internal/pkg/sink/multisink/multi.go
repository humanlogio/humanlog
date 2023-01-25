package multisink

import (
	"context"

	"github.com/humanlogio/humanlog/internal/pkg/model"
	"github.com/humanlogio/humanlog/internal/pkg/sink"
)

func SequentialSink(sinks ...sink.Sink) sink.Sink {
	return &sequential{sinks: sinks}
}

type sequential struct {
	sinks []sink.Sink
}

func (sn *sequential) Receive(ctx context.Context, ev *model.Event) error {
	for _, sk := range sn.sinks {
		if err := sk.Receive(ctx, ev); err != nil {
			return err
		}
	}
	return nil
}
