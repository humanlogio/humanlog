package logsvcsink

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/humanlogio/api/go/svc/ingest/v1"
	"github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
	"google.golang.org/protobuf/proto"
)

var (
	_ sink.Sink = (*ConnectUnarySink)(nil)
)

type ConnectUnarySink struct {
	ll           *slog.Logger
	name         string
	eventsc      chan *typesv1.Log
	dropIfFull   bool
	doneFlushing chan struct{}
}

func StartUnarySink(
	ctx context.Context,
	ll *slog.Logger,
	client ingestv1connect.IngestServiceClient,
	name string,
	resource *typesv1.Resource,
	scope *typesv1.Scope,
	bufferSize int,
	drainBufferFor time.Duration,
	dropIfFull bool,
	notifyUnableToIngest func(err error),
) *ConnectUnarySink {
	snk := &ConnectUnarySink{
		ll:           ll.With(slog.String("sink", name)),
		name:         name,
		eventsc:      make(chan *typesv1.Log, bufferSize),
		dropIfFull:   dropIfFull,
		doneFlushing: make(chan struct{}),
	}

	go func() {
		var (
			buffered []*typesv1.Log
			err      error
		)
		for {
			startedAt := time.Now()
			buffered, err = snk.connectAndHandleBuffer(ctx, client, resource, scope, bufferSize, drainBufferFor, buffered)
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

func (snk *ConnectUnarySink) connectAndHandleBuffer(
	ctx context.Context,
	client ingestv1connect.IngestServiceClient,
	resource *typesv1.Resource,
	scope *typesv1.Scope,
	bufferSize int,
	drainBufferFor time.Duration,
	buffered []*typesv1.Log,
) (lastBuffer []*typesv1.Log, _ error) {
	ll := snk.ll
	ll.DebugContext(ctx, "contacting log ingestor")

	ll.DebugContext(ctx, "ready to send logs")
	req := connect.NewRequest(&v1.IngestRequest{
		Resource: resource,
		Scope:    scope,
	})
	ticker := time.NewTicker(drainBufferFor)
	ticker.Stop()
	flushing := false
	for !flushing {
		// wait for any event to come
		select {
		case ev, more := <-snk.eventsc:
			if !more {
				flushing = true
			}
			if ev != nil {
				req.Msg.Logs = append(req.Msg.Logs, ev)
			}
		case <-ctx.Done():
			return req.Msg.Logs, nil
		}
		ticker.Reset(drainBufferFor)

		// try to drain the channel for 100ms
	drain_buffered_events_loop:
		for len(req.Msg.Logs) < bufferSize {
			select {
			case ev, more := <-snk.eventsc:
				if !more {
					flushing = true
				}
				if ev != nil {
					req.Msg.Logs = append(req.Msg.Logs, ev)
				}
			case <-ticker.C:
				ticker.Stop()
				break drain_buffered_events_loop
			}
		}
		// until it's empty, then send what we have
		start := time.Now()
		_, err := client.Ingest(ctx, req)
		dur := time.Since(start)
		ll.DebugContext(ctx, "sent logs",
			slog.String("sink", snk.name),
			slog.Int64("send_ms", dur.Milliseconds()),
			slog.Any("err", err),
			slog.Int("ev_count", len(req.Msg.Logs)),
			slog.Int("buffer_size", bufferSize),
			slog.Int64("drain_for_ms", drainBufferFor.Milliseconds()),
		)
		if err != nil {
			return req.Msg.Logs, err
		}

		req.Msg.Logs = req.Msg.Logs[:0:len(req.Msg.Logs)]
	}
	return nil, io.EOF
}

func (snk *ConnectUnarySink) Receive(ctx context.Context, ev *typesv1.Log) error {
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
func (snk *ConnectUnarySink) Close(ctx context.Context) error {
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
