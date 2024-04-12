package teesink

import (
	"context"
	"fmt"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
)

var _ sink.Sink = (*Tee)(nil)

func NewTeeSink(sinks ...sink.Sink) sink.Sink {
	var (
		nonbatchers []sink.Sink
		batchers    []sink.BatchSink
	)
	for _, snk := range sinks {
		if batcher, ok := snk.(sink.BatchSink); ok {
			batchers = append(batchers, batcher)
		} else {
			nonbatchers = append(nonbatchers, snk)
		}
	}
	if len(batchers) != 0 && len(nonbatchers) != 0 {
		return &MixedBatchingTee{nonbatchers: nonbatchers, batchers: batchers}
	}
	if len(batchers) != 0 {
		return &BatchingTee{batchers: batchers}
	}
	return &Tee{sinks: sinks}
}

type Tee struct {
	sinks []sink.Sink
}

func (sn *Tee) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
	for i, sinks := range sn.sinks {
		if err := sinks.Receive(ctx, ev); err != nil {
			return fmt.Errorf("tee sink %d: %w", i, err)
		}
	}
	return nil
}

func (sn *Tee) Flush(ctx context.Context) error {
	for i, sinks := range sn.sinks {
		if err := sinks.Flush(ctx); err != nil {
			return fmt.Errorf("tee sink %d: %w", i, err)
		}
	}
	return nil
}

type MixedBatchingTee struct {
	nonbatchers []sink.Sink
	batchers    []sink.BatchSink
}

func (sn *MixedBatchingTee) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
	for i, sinks := range sn.nonbatchers {
		if err := sinks.Receive(ctx, ev); err != nil {
			return fmt.Errorf("tee sink %d: %w", i, err)
		}
	}
	for i, sinks := range sn.batchers {
		if err := sinks.ReceiveBatch(ctx, []*typesv1.LogEvent{ev}); err != nil {
			return fmt.Errorf("tee sink %d: %w", i, err)
		}
	}
	return nil
}

func (sn *MixedBatchingTee) ReceiveBatch(ctx context.Context, evs []*typesv1.LogEvent) error {
	for i, sinks := range sn.nonbatchers {
		for _, ev := range evs {
			if err := sinks.Receive(ctx, ev); err != nil {
				return fmt.Errorf("tee sink %d: %w", i, err)
			}
		}
	}
	for i, sinks := range sn.batchers {
		if err := sinks.ReceiveBatch(ctx, evs); err != nil {
			return fmt.Errorf("tee sink %d: %w", i, err)
		}
	}
	return nil
}

func (sn *MixedBatchingTee) Flush(ctx context.Context) error {
	for i, sinks := range sn.nonbatchers {
		if err := sinks.Flush(ctx); err != nil {
			return fmt.Errorf("tee sink %d: %w", i, err)
		}
	}
	for i, sinks := range sn.batchers {
		if err := sinks.Flush(ctx); err != nil {
			return fmt.Errorf("tee sink %d: %w", i, err)
		}
	}
	return nil
}

type BatchingTee struct {
	batchers []sink.BatchSink
}

func (sn *BatchingTee) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
	for i, sinks := range sn.batchers {
		if err := sinks.ReceiveBatch(ctx, []*typesv1.LogEvent{ev}); err != nil {
			return fmt.Errorf("tee sink %d: %w", i, err)
		}
	}
	return nil
}

func (sn *BatchingTee) ReceiveBatch(ctx context.Context, evs []*typesv1.LogEvent) error {
	for i, sinks := range sn.batchers {
		if err := sinks.ReceiveBatch(ctx, evs); err != nil {
			return fmt.Errorf("tee sink %d: %w", i, err)
		}
	}
	return nil
}

func (sn *BatchingTee) Flush(ctx context.Context) error {
	for i, sinks := range sn.batchers {
		if err := sinks.Flush(ctx); err != nil {
			return fmt.Errorf("tee sink %d: %w", i, err)
		}
	}
	return nil
}
