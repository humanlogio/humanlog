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
	eventsc      chan *typesv1.LogEvent
	dropIfFull   bool
	doneFlushing chan struct{}
}

func StartStreamSink(
	ctx context.Context,
	ll *slog.Logger,
	client ingestv1connect.IngestServiceClient,
	name string,
	machineID uint64,
	bufferSize int,
	drainBufferFor time.Duration,
	dropIfFull bool,
	notifyUnableToIngest func(err error),
) *ConnectStreamSink {

	snk := &ConnectStreamSink{
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
			buffered       []*typesv1.LogEvent
			sessionID      = uint64(time.Now().UnixNano())
			heartbeatEvery = 5 * time.Second
			err            error
		)
		for {
			startedAt := time.Now()
			buffered, sessionID, heartbeatEvery, err = snk.connectAndHandleBuffer(ctx, client, machineID, bufferSize, drainBufferFor, buffered, sessionID, heartbeatEvery)
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

func (snk *ConnectStreamSink) connectAndHandleBuffer(
	ctx context.Context,
	client ingestv1connect.IngestServiceClient,
	machineID uint64,
	bufferSize int,
	drainBufferFor time.Duration,
	buffered []*typesv1.LogEvent,
	sessionID uint64,
	heartbeatEvery time.Duration,
) (lastBuffer []*typesv1.LogEvent, _ uint64, _ time.Duration, sendErr error) {
	ll := snk.ll
	ll.DebugContext(ctx, "contacting log ingestor")
	var stream *connect.ClientStreamForClient[v1.IngestStreamRequest, v1.IngestStreamResponse]
	err := retry.Do(ctx, func(ctx context.Context) (bool, error) {

		hbRes, err := client.GetHeartbeat(ctx, connect.NewRequest(&v1.GetHeartbeatRequest{MachineId: &machineID}))
		if err != nil {
			var cerr *connect.Error
			if errors.As(err, &cerr) && cerr.Code() == connect.CodeResourceExhausted {
				return false, cerr
			}
			return true, fmt.Errorf("requesting heartbeat config from ingestor: %v", err)
		}
		heartbeatEvery = hbRes.Msg.HeartbeatIn.AsDuration()

		stream = client.IngestStream(ctx)
		firstReq := &v1.IngestStreamRequest{Events: buffered, MachineId: machineID, SessionId: sessionID}
		if err := stream.Send(firstReq); err != nil {
			return true, fmt.Errorf("creating ingestion stream: %w", err)
		}
		return false, nil
	}, retry.UseCapSleep(time.Second), retry.UseLog(func(attempt float64, err error) {
		ll.WarnContext(ctx, "can't reach ingestion service", slog.Int("attempt", int(attempt)), slog.Any("err", err))
	}))
	if err != nil {
		return buffered, sessionID, heartbeatEvery, fmt.Errorf("retry aborted: %w", err)
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

			ll.ErrorContext(ctx, "closing and receiving response for log ingestor session", slog.Any("err", err))
			return
		}
		if res.Msg == nil {
			return
		}
		if res.Msg.SessionId != 0 {
			sessionID = res.Msg.SessionId
		}
		if res.Msg.HeartbeatIn != nil {
			heartbeatEvery = res.Msg.HeartbeatIn.AsDuration()
		}
	}()

	ll.DebugContext(ctx, "ready to send logs")
	heartbeater := time.NewTicker(heartbeatEvery)
	defer heartbeater.Stop()
	ticker := time.NewTicker(drainBufferFor)
	ticker.Stop()
	req := new(v1.IngestStreamRequest)
	flushing := false
	heartbeat := false
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
			heartbeat = false
		case <-heartbeater.C:
			heartbeat = true
			// send whatever is there
		case <-ctx.Done():
			return req.Events, sessionID, heartbeatEvery, nil
		}
		ticker.Reset(drainBufferFor)

		if !heartbeat {
			// unless we're just heartbeating,
			// try to drain the channel for 100ms
		drain_buffered_events_loop:
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
		}
		start := time.Now()
		sendErr = stream.Send(req)
		dur := time.Since(start)
		ll.DebugContext(ctx, "sent logs",
			slog.String("sink", snk.name),
			slog.Int64("send_ms", dur.Milliseconds()),
			slog.Any("err", err),
			slog.Int("ev_count", len(req.Events)),
			slog.Int("buffer_size", bufferSize),
			slog.Int64("drain_for_ms", drainBufferFor.Milliseconds()),
		)
		if sendErr != nil {
			return req.Events, sessionID, heartbeatEvery, sendErr
		}
		req.Events = req.Events[:0:len(req.Events)]
	}
	return nil, sessionID, heartbeatEvery, io.EOF
}

func (snk *ConnectStreamSink) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
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
