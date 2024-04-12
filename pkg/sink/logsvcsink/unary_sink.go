package logsvcsink

import (
	"context"
	"fmt"
	"io"
	"log"
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
	_ sink.Sink = (*ConnectUnarySink)(nil)
)

type ConnectUnarySink struct {
	name         string
	eventsc      chan *typesv1.LogEvent
	dropIfFull   bool
	doneFlushing chan struct{}
}

func StartUnarySink(
	ctx context.Context,
	client ingestv1connect.IngestServiceClient,
	name string,
	machineID uint64,
	bufferSize int,
	drainBufferFor time.Duration,
	dropIfFull bool,
) *ConnectUnarySink {
	snk := &ConnectUnarySink{
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
			if err != nil {
				log.Printf("failed to send logs: %v", err)
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
	machineID uint64,
	bufferSize int,
	drainBufferFor time.Duration,
	buffered []*typesv1.LogEvent,
	sessionID uint64,
	heartbeatEvery time.Duration,
) (lastBuffer []*typesv1.LogEvent, _ uint64, _ time.Duration, _ error) {
	log.Print("contacting log ingestor")
	err := retry.Do(ctx, func(ctx context.Context) (bool, error) {
		hbRes, err := client.GetHeartbeat(ctx, connect.NewRequest(&v1.GetHeartbeatRequest{MachineId: &machineID}))
		if err != nil {
			return true, fmt.Errorf("requesting heartbeat config from ingestor: %v", err)
		}
		heartbeatEvery = hbRes.Msg.HeartbeatIn.AsDuration()
		return false, nil
	}, retry.UseCapSleep(time.Second), retry.UseLog(func(attempt float64, err error) {
		log.Printf("can't reach humanlog.io, attempt %d: %v", int(attempt), err)
	}))
	if err != nil {
		return buffered, sessionID, heartbeatEvery, fmt.Errorf("retry aborted: %w", err)
	}

	log.Print("ready to send logs")
	heartbeater := time.NewTicker(heartbeatEvery)
	defer heartbeater.Stop()
	ticker := time.NewTicker(drainBufferFor)
	ticker.Stop()
	req := connect.NewRequest(&v1.IngestRequest{
		MachineId: machineID,
	})
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
				req.Msg.Events = append(req.Msg.Events, ev)
			}
			heartbeat = false
		case <-heartbeater.C:
			heartbeat = true
			// send whatever is there
		case <-ctx.Done():
			return req.Msg.Events, sessionID, heartbeatEvery, nil
		}
		ticker.Reset(drainBufferFor)

		if !heartbeat {
			// unless we're just heartbeating,
			// try to drain the channel for 100ms
		drain_buffered_events_loop:
			for len(req.Msg.Events) < bufferSize {
				select {
				case ev, more := <-snk.eventsc:
					if !more {
						flushing = true
					}
					if ev != nil {
						req.Msg.Events = append(req.Msg.Events, ev)
					}
				case <-ticker.C:
					ticker.Stop()
					break drain_buffered_events_loop
				}
			}
			// until it's empty, then send what we have
		}
		req.Msg.MachineId = machineID
		req.Msg.SessionId = sessionID
		start := time.Now()
		res, err := client.Ingest(ctx, req)
		dur := time.Since(start)
		log.Printf("send: %s %v ms (err=%v) ev=%d", snk.name, dur.Milliseconds(), err, len(req.Msg.Events))
		if err != nil {
			return req.Msg.Events, sessionID, heartbeatEvery, err
		}
		if res.Msg.SessionId != 0 {
			sessionID = res.Msg.SessionId
		}
		if res.Msg.HeartbeatIn != nil {
			heartbeatEvery = res.Msg.HeartbeatIn.AsDuration()
		}
		req.Msg.Events = req.Msg.Events[:0:len(req.Msg.Events)]
	}
	return nil, sessionID, heartbeatEvery, io.EOF
}

func (snk *ConnectUnarySink) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
	send := proto.Clone(ev).(*typesv1.LogEvent)
	if snk.dropIfFull {
		select {
		case snk.eventsc <- send:
		case <-ctx.Done():
			return ctx.Err()
		default:
			log.Print("dropping log event, buffer full")
		}
	} else {
		select {
		case snk.eventsc <- send:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Flush can only be called once, calling it twice will panic.
func (snk *ConnectUnarySink) Flush(ctx context.Context) error {
	close(snk.eventsc)
	select {
	case <-snk.doneFlushing:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
