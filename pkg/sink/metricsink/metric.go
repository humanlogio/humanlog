package metricsink

import (
	"context"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
)

var (
	_ sink.Sink      = (*Metric)(nil)
	_ sink.BatchSink = (*BatchMetric)(nil)
	_ sink.MixedSink = (*MixedMetric)(nil)
)

func NewMetricSink(
	sink sink.Sink,
	observeReceiveCall func(*typesv1.LogEvent, time.Duration, error),
) sink.Sink {
	return &Metric{
		sink:               sink,
		observeReceiveCall: observeReceiveCall,
		timeNow:            time.Now,
	}
}

type Metric struct {
	sink               sink.Sink
	observeReceiveCall func(*typesv1.LogEvent, time.Duration, error)
	timeNow            func() time.Time
}

func (sn *Metric) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
	start := sn.timeNow()
	err := sn.sink.Receive(ctx, ev)
	end := sn.timeNow()
	sn.observeReceiveCall(ev, end.Sub(start), err)
	return err
}

func (sn *Metric) Close(ctx context.Context) error {
	return sn.sink.Close(ctx)
}

func NewBatchMetricSink(
	sink sink.BatchSink,
	observeReceiveBatchCall func([]*typesv1.LogEvent, time.Duration, error),
) sink.BatchSink {
	return &BatchMetric{
		sink:                    sink,
		observeReceiveBatchCall: observeReceiveBatchCall,
		timeNow:                 time.Now,
	}
}

type BatchMetric struct {
	sink                    sink.BatchSink
	observeReceiveBatchCall func([]*typesv1.LogEvent, time.Duration, error)
	timeNow                 func() time.Time
}

func (sn *BatchMetric) ReceiveBatch(ctx context.Context, evs []*typesv1.LogEvent) error {
	start := sn.timeNow()
	err := sn.sink.ReceiveBatch(ctx, evs)
	end := sn.timeNow()
	sn.observeReceiveBatchCall(evs, end.Sub(start), err)
	return err
}

func (sn *BatchMetric) Close(ctx context.Context) error {
	return sn.sink.Close(ctx)
}

func NewMixedMetricSink(
	sink sink.MixedSink,
	observeReceiveCall func(*typesv1.LogEvent, time.Duration, error),
	observeReceiveBatchCall func([]*typesv1.LogEvent, time.Duration, error),
) sink.BatchSink {
	return &MixedMetric{
		sink:                    sink,
		observeReceiveCall:      observeReceiveCall,
		observeReceiveBatchCall: observeReceiveBatchCall,
		timeNow:                 time.Now,
	}
}

type MixedMetric struct {
	sink                    sink.MixedSink
	observeReceiveCall      func(*typesv1.LogEvent, time.Duration, error)
	observeReceiveBatchCall func([]*typesv1.LogEvent, time.Duration, error)
	timeNow                 func() time.Time
}

func (sn *MixedMetric) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
	start := sn.timeNow()
	err := sn.sink.Receive(ctx, ev)
	end := sn.timeNow()
	sn.observeReceiveCall(ev, end.Sub(start), err)
	return err
}

func (sn *MixedMetric) ReceiveBatch(ctx context.Context, evs []*typesv1.LogEvent) error {
	start := sn.timeNow()
	err := sn.sink.ReceiveBatch(ctx, evs)
	end := sn.timeNow()
	sn.observeReceiveBatchCall(evs, end.Sub(start), err)
	return err
}

func (sn *MixedMetric) Close(ctx context.Context) error {
	return sn.sink.Close(ctx)
}
