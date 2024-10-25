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
	_ sink.Sink = (*ConnectBidiStreamSink)(nil)
)

type ConnectBidiStreamSink struct {
	ll           *slog.Logger
	name         string
	eventsc      chan *typesv1.LogEvent
	dropIfFull   bool
	doneFlushing chan struct{}
}

func StartBidiStreamSink(
	ctx context.Context,
	ll *slog.Logger,
	client ingestv1connect.IngestServiceClient,
	name string,
	machineID uint64,
	bufferSize int,
	drainBufferFor time.Duration,
	dropIfFull bool,
	notifyUnableToIngest func(err error),
) *ConnectBidiStreamSink {
	snk := &ConnectBidiStreamSink{
		ll: ll.With(
			slog.String("sink", name),
			slog.Uint64("machine_id", machineID),
		),
		name:         name,
		eventsc:      make(chan *typesv1.LogEvent, bufferSize),
		dropIfFull:   dropIfFull,
		doneFlushing: make(chan struct{}),
	}

	go func() {
		var (
			buffered        []*typesv1.LogEvent
			resumeSessionID uint64
			err             error
		)
		for {
			startedAt := time.Now()
			buffered, resumeSessionID, err = snk.connectAndHandleBuffer(ctx, client, machineID, bufferSize, drainBufferFor, buffered, resumeSessionID)
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
				ll.ErrorContext(ctx, "failed to send logs", slog.Any("err", err))
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

func (snk *ConnectBidiStreamSink) connectAndHandleBuffer(
	ctx context.Context,
	client ingestv1connect.IngestServiceClient,
	machineID uint64,
	bufferSize int,
	drainBufferFor time.Duration,
	buffered []*typesv1.LogEvent,
	resumeSessionID uint64,
) (lastBuffer []*typesv1.LogEvent, _ uint64, _ error) {
	ll := snk.ll
	ll.DebugContext(ctx, "contacting log ingestor")
	var stream *connect.BidiStreamForClient[v1.IngestBidiStreamRequest, v1.IngestBidiStreamResponse]
	err := retry.Do(ctx, func(ctx context.Context) (bool, error) {
		stream = client.IngestBidiStream(ctx)
		firstReq := &v1.IngestBidiStreamRequest{Events: buffered, MachineId: machineID, ResumeSessionId: resumeSessionID}
		if err := stream.Send(firstReq); err != nil {
			var cerr *connect.Error
			if errors.As(err, &cerr) && cerr.Code() == connect.CodeResourceExhausted {
				return false, cerr
			}
			return true, fmt.Errorf("creating ingestion stream: %w", err)
		}
		return false, nil
	}, retry.UseCapSleep(time.Second), retry.UseLog(func(attempt float64, err error) {
		ll.WarnContext(ctx, "can't reach ingestion service", slog.Int("attempt", int(attempt)), slog.Any("err", err))
	}))
	if err != nil {
		return buffered, resumeSessionID, fmt.Errorf("retry aborted: %w", err)
	}

	ll.DebugContext(ctx, "receiving log ingestor session")
	res, err := stream.Receive()
	if err != nil {
		// nothing is buffered
		return nil, resumeSessionID, fmt.Errorf("waiting for ingestion stream session ID: %w", err)
	}
	defer func() {
		_ = stream.CloseRequest()
		_ = stream.CloseResponse()
	}()

	ll.DebugContext(ctx, "ready to send logs")
	resumeSessionID = res.SessionId
	ticker := time.NewTicker(drainBufferFor)
	ticker.Stop()
	req := new(v1.IngestBidiStreamRequest)
	flushing := false
	for !flushing {
		// wait for any event to come
		select {
		case ev, more := <-snk.eventsc:
			if !more {
				flushing = true
			}
			if ev != nil {
				req.Events = append(req.Events, ev)
			}
		case <-ctx.Done():
			return req.Events, resumeSessionID, nil
		}
		ticker.Reset(drainBufferFor)

	drain_buffered_events_loop:
		// try to drain the channel for 100ms
		for len(req.Events) < bufferSize {
			select {
			case ev, more := <-snk.eventsc:
				if !more {
					flushing = true
				}
				if ev != nil {
					req.Events = append(req.Events, ev)
				}
			case <-ticker.C:
				ticker.Stop()
				break drain_buffered_events_loop
			}
		}
		// until it's empty, then send what we have
		start := time.Now()
		err := stream.Send(req)
		dur := time.Since(start)
		ll.DebugContext(ctx, "sent logs",
			slog.String("sink", snk.name),
			slog.Int64("send_ms", dur.Milliseconds()),
			slog.Any("err", err),
			slog.Int("ev_count", len(req.Events)),
			slog.Int("buffer_size", bufferSize),
			slog.Int64("drain_for_ms", drainBufferFor.Milliseconds()),
		)
		if err != nil {
			return req.Events, resumeSessionID, err
		}
		req.Events = req.Events[:0:len(req.Events)]
	}
	return nil, resumeSessionID, io.EOF
}

func (snk *ConnectBidiStreamSink) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
	send := proto.Clone(ev).(*typesv1.LogEvent)
	if snk.dropIfFull {
		select {
		case snk.eventsc <- send:
		case <-ctx.Done():
			return ctx.Err()
		default:
			snk.ll.WarnContext(ctx, "dropping log event, buffer full!")
		}
	} else {
		select {
		case snk.eventsc <- send:
		case <-ctx.Done():
			return ctx.Err()
		default:
			// would have blocked~
			snk.ll.WarnContext(ctx, "blocking on log event, buffer full!")
			select {
			case snk.eventsc <- send:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

// Flush can only be called once, calling it twice will panic.
func (snk *ConnectBidiStreamSink) Flush(ctx context.Context) error {
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
