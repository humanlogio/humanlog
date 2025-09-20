package otlpink

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
	collogpb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	otellogpb "go.opentelemetry.io/proto/otlp/logs/v1"
	otlpresource "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

var (
	_ sink.Sink = (*OTLPSink)(nil)
)

type OTLPSink struct {
	ll           *slog.Logger
	name         string
	eventsc      chan *typesv1.Log
	dropIfFull   bool
	doneFlushing chan struct{}
}

func StartOTLPSink(
	ctx context.Context,
	ll *slog.Logger,
	client collogpb.LogsServiceClient,
	name string,
	resource *typesv1.Resource,
	scope *typesv1.Scope,
	bufferSize int,
	drainBufferFor time.Duration,
	dropIfFull bool,
	notifyUnableToIngest func(err error),
) *OTLPSink {
	snk := &OTLPSink{
		ll:           ll.With(slog.String("sink", name)),
		name:         name,
		eventsc:      make(chan *typesv1.Log, bufferSize),
		dropIfFull:   dropIfFull,
		doneFlushing: make(chan struct{}),
	}

	var (
		otelresource = &otlpresource.Resource{
			Attributes: typesv1.ToOTLPKVs(resource.Attributes),
		}
		otelscope = &otlpcommon.InstrumentationScope{
			Name:       scope.Name,
			Version:    scope.Version,
			Attributes: typesv1.ToOTLPKVs(scope.Attributes),
		}
	)

	go func() {
		var (
			buffered []*otellogpb.LogRecord
			err      error
		)
		for {
			startedAt := time.Now()
			buffered, err = snk.connectAndHandleBuffer(ctx, client, otelresource, otelscope, bufferSize, drainBufferFor, buffered)
			if err == io.EOF {
				close(snk.doneFlushing)
				return
			}
			var cerr *connect.Error
			if errors.As(err, &cerr) && cerr.Code() == connect.CodeResourceExhausted {
				close(snk.doneFlushing)
				notifyUnableToIngest(err)
				return
			}
			if err != nil {
				ll.DebugContext(ctx, "failed to send logs", slog.Any("err", err))
			}
			if time.Since(startedAt) < time.Second {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			} else {
				select {
				case <-ctx.Done():
					return
				default:
				}
			}

		}
	}()

	return snk
}

func internalToOTLPLog(in *typesv1.Log) *otellogpb.LogRecord {
	out := &otellogpb.LogRecord{
		TimeUnixNano:           uint64(in.Timestamp.AsTime().UnixNano()),
		ObservedTimeUnixNano:   uint64(in.ObservedTimestamp.AsTime().UnixNano()),
		SeverityNumber:         otellogpb.SeverityNumber(in.SeverityNumber),
		SeverityText:           in.SeverityText,
		Body:                   &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: in.Body}},
		Attributes:             typesv1.ToOTLPKVs(in.Attributes),
		DroppedAttributesCount: 0,
		Flags:                  in.TraceFlags,
	}
	if in.TraceId != nil {
		out.TraceId = in.TraceId.Raw
	}
	if in.SpanId != nil {
		out.SpanId = in.SpanId.Raw
	}
	return out
}

func (snk *OTLPSink) connectAndHandleBuffer(
	ctx context.Context,
	client collogpb.LogsServiceClient,
	resource *otlpresource.Resource,
	scope *otlpcommon.InstrumentationScope,
	bufferSize int,
	drainBufferFor time.Duration,
	buffered []*otellogpb.LogRecord,
) (lastBuffer []*otellogpb.LogRecord, _ error) {
	ll := snk.ll
	ll.DebugContext(ctx, "contacting log ingestor")

	ticker := time.NewTicker(drainBufferFor)
	ticker.Stop()
	flushing := false
	for !flushing {
		if len(buffered) == 0 {
			// wait for any event to come
			select {
			case ev, more := <-snk.eventsc:
				if !more {
					ll.DebugContext(ctx, "no more events coming, flushing buffer (while waiting)")
					flushing = true
				}
				if ev != nil {
					buffered = append(buffered, internalToOTLPLog(ev))
				}
			case <-ctx.Done():
				return buffered, nil
			}
		}
		ticker.Reset(drainBufferFor)

		// try to drain the channel for 100ms
		ll.DebugContext(ctx, "draining for a bit before sending", slog.Duration("drain_for", drainBufferFor))
	drain_buffered_events_loop:
		for len(buffered) < bufferSize {
			select {
			case ev, more := <-snk.eventsc:
				if ev != nil {
					buffered = append(buffered, internalToOTLPLog(ev))
				}
				if !more {
					ll.DebugContext(ctx, "no more events coming, flushing buffer (while draining)")
					flushing = true
					break drain_buffered_events_loop
				}
			case <-ticker.C:
				ticker.Stop()
				ll.DebugContext(ctx, "done draining")
				break drain_buffered_events_loop
			}
		}
		// until it's empty, then send what we have
		start := time.Now()
		res, err := client.Export(ctx, &collogpb.ExportLogsServiceRequest{ResourceLogs: []*otellogpb.ResourceLogs{{
			Resource: resource,
			ScopeLogs: []*otellogpb.ScopeLogs{{
				Scope:      scope,
				LogRecords: buffered,
			}},
		}}})
		if err != nil {
			return buffered, err
		}
		dur := time.Since(start)
		args := []any{
			slog.String("sink", snk.name),
			slog.Int64("send_ms", dur.Milliseconds()),
			slog.Any("err", err),
			slog.Int("ev_count", len(buffered)),
			slog.Int("buffer_size", bufferSize),
			slog.Int64("drain_for_ms", drainBufferFor.Milliseconds()),
		}
		if res != nil && res.PartialSuccess != nil {
			args = append(args,
				slog.Int64("rejected_count", res.PartialSuccess.RejectedLogRecords),
				slog.String("rejected_error_message", res.PartialSuccess.ErrorMessage),
			)
		}
		ll.DebugContext(ctx, "sent logs", args...)

		buffered = buffered[:0:len(buffered)]
	}
	return nil, io.EOF
}

func (snk *OTLPSink) Receive(ctx context.Context, ev *typesv1.Log) error {
	send := proto.Clone(ev).(*typesv1.Log)
	if snk.dropIfFull {
		select {
		case snk.eventsc <- send:
		case <-ctx.Done():
			return nil
		default:
			snk.ll.WarnContext(ctx, "dropping log event, buffer full!")
		}
	} else {
		select {
		case snk.eventsc <- send:
		case <-ctx.Done():
			return nil
		default:
			// would have blocked~
			snk.ll.WarnContext(ctx, "blocking on log event, buffer full!")
			select {
			case snk.eventsc <- send:
			case <-ctx.Done():
				return nil
			}
		}
	}
	return nil
}

// Close can only be called once, calling it twice will panic.
func (snk *OTLPSink) Close(ctx context.Context) error {
	close(snk.eventsc)
	snk.ll.DebugContext(ctx, "starting to flush")
	select {
	case <-snk.doneFlushing:
		snk.ll.DebugContext(ctx, "done flushing")
	case <-ctx.Done():
		snk.ll.DebugContext(ctx, "unable to finish flushing")
		return ctx.Err()
	}
	return nil
}
