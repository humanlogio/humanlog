package logsvcsink

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/humanlogio/api/go/svc/ingest/v1"
	"github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/retry"
	"github.com/humanlogio/humanlog/pkg/sink"
	"google.golang.org/protobuf/proto"
)

var (
	_ sink.Sink = (*ConnectStreamSink)(nil)
)

type ConnectStreamSink struct {
	ll           *slog.Logger
	name         string
	eventsc      chan *typesv1.Log
	dropIfFull   bool
	doneFlushing chan struct{}
}

func StartStreamSink(
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
) *ConnectStreamSink {

	snk := &ConnectStreamSink{
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

func (snk *ConnectStreamSink) connectAndHandleBuffer(
	ctx context.Context,
	client ingestv1connect.IngestServiceClient,
	resource *typesv1.Resource,
	scope *typesv1.Scope,
	bufferSize int,
	drainBufferFor time.Duration,
	buffered []*typesv1.Log,
) (lastBuffer []*typesv1.Log, sendErr error) {
	ll := snk.ll
	ll.DebugContext(ctx, "contacting log ingestor")
	var stream *connect.ClientStreamForClient[v1.IngestStreamRequest, v1.IngestStreamResponse]
	err := retry.Do(ctx, func(ctx context.Context) (bool, error) {

		stream = client.IngestStream(ctx)
		firstReq := &v1.IngestStreamRequest{Logs: buffered, Resource: resource, Scope: scope}
		if err := stream.Send(firstReq); err != nil {
			return true, fmt.Errorf("creating ingestion stream: %w", err)
		}
		return false, nil
	}, retry.UseCapSleep(time.Second), retry.UseLog(func(attempt float64, err error) {
		ll.DebugContext(ctx, "can't reach ingestion service", slog.Int("attempt", int(attempt)), slog.Any("err", err))
	}))
	if err != nil {
		return buffered, fmt.Errorf("retry aborted: %w", err)
	}

	defer func() {
		res, err := stream.CloseAndReceive()
		if err != nil {
			var cerr *connect.Error
			if errors.Is(sendErr, io.EOF) && errors.As(err, &cerr) && cerr.Code() == connect.CodeResourceExhausted {
				sendErr = cerr
				ll.ErrorContext(ctx, "no active plan, can't ingest logs", slog.Any("err", err))
				return
			}
			if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				ll.ErrorContext(ctx, "closing and receiving response for log ingestor session", slog.Any("err", err))
			}
			return
		}
		if res.Msg == nil {
			return
		}
	}()

	ll.DebugContext(ctx, "ready to send logs")
	ticker := time.NewTicker(drainBufferFor)
	ticker.Stop()
	req := new(v1.IngestStreamRequest)
	flushing := false
	for !flushing {
		// wait for any event to come
		select {
		case ev, more := <-snk.eventsc:
			if !more {
				ll.DebugContext(ctx, "no more events coming, flushing buffer (while waiting)")
				flushing = true
			}
			if ev != nil {
				req.Logs = append(req.Logs, ev)
			}
			// send whatever is there
		case <-ctx.Done():
			return req.Logs, nil
		}
		ticker.Reset(drainBufferFor)

		// try to drain the channel for 100ms
		ll.DebugContext(ctx, "draining for a bit before sending", slog.Duration("drain_for", drainBufferFor))
	drain_buffered_events_loop:
		for len(req.Logs) < bufferSize {
			select {
			case ev, more := <-snk.eventsc:
				if ev != nil {
					req.Logs = append(req.Logs, ev)
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
		sendErr = stream.Send(req)
		dur := time.Since(start)
		ll.DebugContext(ctx, "sent logs",
			slog.String("sink", snk.name),
			slog.Int64("send_ms", dur.Milliseconds()),
			slog.Any("err", err),
			slog.Int("ev_count", len(req.Logs)),
			slog.Int("buffer_size", bufferSize),
			slog.Int64("drain_for_ms", drainBufferFor.Milliseconds()),
		)
		if sendErr != nil {
			return req.Logs, sendErr
		}
		req.Logs = req.Logs[:0:len(req.Logs)]
	}
	return nil, io.EOF
}

func (snk *ConnectStreamSink) Receive(ctx context.Context, ev *typesv1.Log) error {
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
func (snk *ConnectStreamSink) Close(ctx context.Context) error {
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
