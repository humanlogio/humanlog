package localsvc

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"connectrpc.com/connect"
	igv1 "github.com/humanlogio/api/go/svc/ingest/v1"
	igsvcpb "github.com/humanlogio/api/go/svc/ingest/v1/ingestv1connect"
	lhv1 "github.com/humanlogio/api/go/svc/localhost/v1"
	lhsvcpb "github.com/humanlogio/api/go/svc/localhost/v1/localhostv1connect"
	qrv1 "github.com/humanlogio/api/go/svc/query/v1"
	qrsvcpb "github.com/humanlogio/api/go/svc/query/v1/queryv1connect"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/state"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/humanlogio/humanlog/pkg/sink"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Service struct {
	ll         *slog.Logger
	state      *state.State
	ownVersion *typesv1.Version
	storage    localstorage.Storage
}

func New(ll *slog.Logger, state *state.State, ownVersion *typesv1.Version, storage localstorage.Storage) *Service {
	return &Service{ll: ll, state: state, ownVersion: ownVersion, storage: storage}
}

var (
	_ lhsvcpb.LocalhostServiceHandler = (*Service)(nil)
	_ igsvcpb.IngestServiceHandler    = (*Service)(nil)
	_ qrsvcpb.QueryServiceHandler     = (*Service)(nil)
)

func (svc *Service) Ping(ctx context.Context, req *connect.Request[lhv1.PingRequest]) (*connect.Response[lhv1.PingResponse], error) {
	res := &lhv1.PingResponse{
		ClientVersion: svc.ownVersion,
		Meta:          &typesv1.ResMeta{},
	}
	if svc.state.MachineID != nil {
		res.Meta = &typesv1.ResMeta{
			MachineId: *svc.state.MachineID,
		}
	}
	return connect.NewResponse(res), nil
}

func (svc *Service) GetHeartbeat(ctx context.Context, req *connect.Request[igv1.GetHeartbeatRequest]) (*connect.Response[igv1.GetHeartbeatResponse], error) {
	msg := req.Msg
	if msg.MachineId == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("no machine ID present, ensure you're logged in (or authorized) to obtain a machine ID"))
	}
	sessionID := int64(0)
	if msg.SessionId != nil {
		sessionID = int64(*msg.SessionId)
	}
	heartbeat, err := svc.storage.Heartbeat(ctx, int64(*msg.MachineId), sessionID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&igv1.GetHeartbeatResponse{
		HeartbeatIn: durationpb.New(heartbeat),
	}), nil
}

func (svc *Service) Ingest(ctx context.Context, req *connect.Request[igv1.IngestRequest]) (*connect.Response[igv1.IngestResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("not available on localhost"))
}

func fixEventsTimestamps(ctx context.Context, ll *slog.Logger, evs []*typesv1.LogEvent) []*typesv1.LogEvent {
	for i, ev := range evs {
		evs[i] = fixEventTimestamps(ctx, ll, ev)
	}
	return evs
}

func fixEventTimestamps(ctx context.Context, ll *slog.Logger, ev *typesv1.LogEvent) *typesv1.LogEvent {
	if ev.ParsedAt != nil && ev.ParsedAt.Seconds < 0 {
		ev.ParsedAt = timestamppb.Now()
		ll.ErrorContext(ctx, "client is sending invalid parsedat")
	}
	if ev.Structured != nil && ev.Structured.Timestamp != nil && ev.Structured.Timestamp.Seconds < 0 {
		ev.Structured.Timestamp = ev.ParsedAt
		ll.ErrorContext(ctx, "client is sending invalid timestamp")
	}
	return ev
}

func (svc *Service) IngestStream(ctx context.Context, req *connect.ClientStream[igv1.IngestStreamRequest]) (*connect.Response[igv1.IngestStreamResponse], error) {
	ll := svc.ll

	var (
		machineID int64
		sessionID int64
	)

	// get the first message which has the metadata to start ingesting
	if !req.Receive() {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("must contain at least a first request"))
	}
	msg := req.Msg()
	machineID = int64(msg.MachineId)
	sessionID = int64(msg.SessionId)
	if sessionID == 0 {
		sessionID = time.Now().UnixNano()
	}
	if machineID == 0 && svc.state.MachineID != nil {
		machineID = int64(*svc.state.MachineID)
	}
	ll = ll.With(
		slog.Int64("machine_id", machineID),
		slog.Int64("session_id", sessionID),
	)
	ll.DebugContext(ctx, "receiving data from stream")
	snk, heartbeatIn, err := svc.storage.SinkFor(ctx, machineID, sessionID)
	if err != nil {
		ll.ErrorContext(ctx, "obtaining sink for stream", slog.Any("err", err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("obtaining sink for stream: %v", err))
	}
	defer func() {
		if ferr := snk.Close(ctx); ferr != nil {
			if err == nil {
				err = ferr
			} else {
				ll.ErrorContext(ctx, "erroneous exit and also failed to flush", slog.Any("err", err))
			}
		}
	}()

	if bsnk, ok := snk.(sink.BatchSink); ok {
		// ingest the first message
		msg.Events = fixEventsTimestamps(ctx, ll, msg.Events)
		if err := bsnk.ReceiveBatch(ctx, msg.Events); err != nil {
			ll.ErrorContext(ctx, "ingesting event batch", slog.Any("err", err))
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event batch: %v", err))
		}
		// then wait for more
		for req.Receive() {
			msg := req.Msg()
			msg.Events = fixEventsTimestamps(ctx, ll, msg.Events)
			if err := bsnk.ReceiveBatch(ctx, msg.Events); err != nil {
				ll.ErrorContext(ctx, "ingesting event batch", slog.Any("err", err))
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event batch: %v", err))
			}
		}
	} else {
		// ingest the first message
		msg.Events = fixEventsTimestamps(ctx, ll, msg.Events)
		for _, ev := range msg.Events {
			if err := snk.Receive(ctx, ev); err != nil {
				ll.ErrorContext(ctx, "ingesting event", slog.Any("err", err))
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event: %v", err))
			}
		}
		// then wait for more
		for req.Receive() {
			msg := req.Msg()
			msg.Events = fixEventsTimestamps(ctx, ll, msg.Events)
			for _, ev := range msg.Events {
				if ev.ParsedAt != nil && ev.ParsedAt.Seconds < 0 {
					ev.ParsedAt = timestamppb.Now()
					ll.ErrorContext(ctx, "client is sending invalid parsedat", slog.Any("err", err))
				}
				if ev.Structured != nil && ev.Structured.Timestamp != nil && ev.Structured.Timestamp.Seconds < 0 {
					ev.Structured.Timestamp = ev.ParsedAt
					ll.ErrorContext(ctx, "client is sending invalid timestamp", slog.Any("err", err))
				}
				if err := snk.Receive(ctx, ev); err != nil {
					ll.ErrorContext(ctx, "ingesting event", slog.Any("err", err))
					return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting event: %v", err))
				}
			}
		}
	}
	if err := req.Err(); err != nil {
		ll.ErrorContext(ctx, "ingesting localhost stream", slog.Any("err", err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ingesting localhost stream: %v", err))
	}
	res := &igv1.IngestStreamResponse{
		SessionId:   uint64(sessionID),
		HeartbeatIn: durationpb.New(heartbeatIn),
	}
	return connect.NewResponse(res), nil
}

func (svc *Service) IngestBidiStream(ctx context.Context, req *connect.BidiStream[igv1.IngestBidiStreamRequest, igv1.IngestBidiStreamResponse]) error {
	return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("not available on localhost"))
}

func (svc *Service) SummarizeEvents(ctx context.Context, req *connect.Request[qrv1.SummarizeEventsRequest]) (*connect.Response[qrv1.SummarizeEventsResponse], error) {
	if req.Msg.From == nil {
		req.Msg.From = timestamppb.New(time.Now().Add(-time.Minute))
	}
	if req.Msg.To == nil {
		req.Msg.To = timestamppb.Now()
	}
	if req.Msg.BucketCount < 1 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("bucket count must be greater than 1"))
	}
	ll := svc.ll.With(
		slog.Time("from", req.Msg.From.AsTime()),
		slog.Time("to", req.Msg.From.AsTime()),
		slog.Int("bucket_count", int(req.Msg.BucketCount)),
		slog.Int("environment_id", int(req.Msg.EnvironmentId)),
	)

	cursors, err := svc.storage.Query(ctx, &typesv1.LogQuery{
		From: req.Msg.From,
		To:   req.Msg.To,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("summarizing local storage: %v", err))
	}
	ll = ll.With(slog.Int("cursor_len", len(cursors)))
	ll.DebugContext(ctx, "queried, got cursors")

	from := req.Msg.From.AsTime()
	to := req.Msg.To.AsTime()
	width := to.Sub(from) / time.Duration(req.Msg.BucketCount)

	type bucket struct {
		ts    time.Time
		count int
	}
	var buckets []bucket
	for now := from; now.Before(to) || now.Equal(to); now = now.Add(width) {
		buckets = append(buckets, bucket{ts: now})
	}
	ll = ll.With(slog.Duration("width", width))

	for cursor := range cursors {
		for cursor.Next(ctx) {
			ev := new(typesv1.LogEvent)
			if err := cursor.Event(ev); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("scanning events: %v", err))
			}
			ts := ev.ParsedAt.AsTime().Truncate(width)
			loc, _ := slices.BinarySearchFunc(buckets, ts, func(a bucket, t time.Time) int {
				return a.ts.Compare(t)
			})
			buckets[loc].count++
		}
		if err := cursor.Err(); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("counting summary: %v", err))
		}
		if err := cursor.Close(); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("closing cursor: %v", err))
		}
	}
	ll.DebugContext(ctx, "iterated all cursors")
	out := &qrv1.SummarizeEventsResponse{
		BucketWidth: durationpb.New(width),
	}
	for _, bucket := range buckets {
		if bucket.count == 0 {
			continue
		}
		out.Buckets = append(out.Buckets, &qrv1.SummarizeEventsResponse_Bucket{
			Ts:         timestamppb.New(bucket.ts),
			EventCount: uint64(bucket.count),
		})
	}
	ll.DebugContext(ctx, "non-zero buckets filled", slog.Int("buckets_len", len(out.Buckets)))
	return connect.NewResponse(out), nil
}
func (svc *Service) WatchQuery(ctx context.Context, req *connect.Request[qrv1.WatchQueryRequest], stream *connect.ServerStream[qrv1.WatchQueryResponse]) error {
	query := req.Msg.GetQuery()

	ll := svc.ll.With(
		slog.String("query.query", query.Query.String()),
	)
	if query.From != nil {
		ll = ll.With(slog.String("query.from", query.From.AsTime().Format(time.RFC3339Nano)))
	}
	if query.To != nil {
		ll = ll.With(slog.String("query.to", query.To.AsTime().Format(time.RFC3339Nano)))
	}

	// ticker := time.NewTicker(time.Second)
	// defer ticker.Stop()
	// for i := 0; ; i++ {
	// 	select {
	// 	case now := <-ticker.C:

	// 		err := stream.Send(&qrv1.WatchQueryResponse{
	// 			Events: []*typesv1.LogEventGroup{
	// 				{
	// 					MachineId: 1,
	// 					SessionId: 1,
	// 					Logs: []*typesv1.LogEvent{
	// 						{ParsedAt: timestamppb.New(now), Raw: []byte(fmt.Sprintf("it's now %d o-clock", now.Hour()))},
	// 					},
	// 				},
	// 			},
	// 		})
	// 		ll.DebugContext(ctx, "send a message group", slog.Any("err", err))

	// 	case <-ctx.Done():
	// 		return nil
	// 	}
	// }

	ll.DebugContext(ctx, "running query through storage")
	cursors, err := svc.storage.Query(ctx, req.Msg.Query)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("querying local storage: %v", err))
	}
	ll.DebugContext(ctx, "received cursors", slog.Int("cursor_len", len(cursors)))

	legc := make(chan *typesv1.LogEventGroup)

	iterateCursor := func(ctx context.Context, cursor localstorage.Cursor) error {
		defer func() {
			_ = cursor.Close()
		}()
		var (
			lastSend             = time.Now()
			machineID, sessionID = cursor.IDs()
			evs                  []*typesv1.LogEvent
		)
		ll := ll.With(
			slog.Int64("machine_id", machineID),
			slog.Int64("session_id", sessionID),
		)
		for cursor.Next(ctx) {
			ev := new(typesv1.LogEvent)
			if err := cursor.Event(ev); err != nil {
				return err
			}
			evs = append(evs, ev)
			now := time.Now()
			if now.Sub(lastSend) > 100*time.Millisecond {
				ll.DebugContext(ctx, "cursor batch sent", slog.Int("batch_len", len(evs)))
				lastSend = now
				select {
				case legc <- &typesv1.LogEventGroup{
					MachineId: machineID,
					SessionId: sessionID,
					Logs:      evs,
				}:
				case <-ctx.Done():
					return nil
				}
				evs = evs[:0]
			}
		}
		if err := cursor.Err(); err != nil {
			ll.ErrorContext(ctx, "failed to advance query cursor", slog.Any("err", err))
		}
		ll.DebugContext(ctx, "cursor done, sending last batch", slog.Int("batch_len", len(evs)))
		select {
		case <-ctx.Done():
			return nil
		default:

		}
		select {
		case legc <- &typesv1.LogEventGroup{
			MachineId: machineID,
			SessionId: sessionID,
			Logs:      evs,
		}:
		case <-ctx.Done():
		}
		return nil
	}

	allCursorsStarted := make(chan struct{})

	cursorCtx, cancelCursors := context.WithCancel(ctx)
	defer cancelCursors()
	eg, cursorCtx := errgroup.WithContext(cursorCtx)
	go func() {
		defer close(allCursorsStarted)
		for {
			select {
			case <-ctx.Done():
				return
			case cursor, more := <-cursors:
				if !more {
					return
				}
				eg.Go(func() error { return iterateCursor(cursorCtx, cursor) })
			}
		}
	}()

	doneSending := make(chan struct{})
	go func() {
		defer func() {
			close(doneSending)
			cancelCursors()
		}()
		var (
			sender = time.NewTicker(100 * time.Millisecond)
			legs   []*typesv1.LogEventGroup
		)
		defer sender.Stop()

		ll.DebugContext(ctx, "accumulator: starting accumulation loop")
		defer func() {
			ll.DebugContext(ctx, "accumulator: done accumulating")
			if len(legs) > 0 {
				err = stream.Send(&qrv1.WatchQueryResponse{
					Events: legs,
				})
				if err != nil {
					ll.ErrorContext(ctx, "accumulator: failed to send response", slog.Any("err", err))
					return
				}
			}
		}()
	wait_for_more_leg:
		for {
			select {
			case <-ctx.Done():
				return
			case leg, more := <-legc:
				if !more {
					break wait_for_more_leg
				}
				// try to append to an existing LEG first
				ll.DebugContext(ctx, "accumulator: received cursor batch", slog.Int("leg_count", len(legs)))
				for _, eleg := range legs {
					if eleg != nil && leg != nil && eleg.MachineId == leg.MachineId &&
						eleg.SessionId == leg.SessionId {
						ll.DebugContext(ctx, "accumulator: appending to existing log event group")
						eleg.Logs = append(eleg.Logs, leg.Logs...)
						continue wait_for_more_leg
					}
				}
				// didn't have an existing LEG for it, add it
				if leg != nil {
					ll.DebugContext(ctx, "accumulator: creating new log event group")
					legs = append(legs, leg)
				}
			case <-sender.C:
				if len(legs) < 1 {
					continue
				}
				ll.DebugContext(ctx, "accumulator: sending watch query response")
				err := stream.Send(&qrv1.WatchQueryResponse{
					Events: legs,
				})
				legs = legs[:0]
				if err != nil {
					ll.ErrorContext(ctx, "accumulator: failed to send response", slog.Any("err", err))
					return
				}
			}
		}
	}()

	ll.DebugContext(ctx, "accumulator: streaming started")
	select {
	case <-allCursorsStarted:
		err = eg.Wait()
	case <-ctx.Done():
	}
	ll.DebugContext(ctx, "accumulator: all data consumed, finishing")
	close(legc)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("streaming localhost log for query: %v", err))
	}
	select {
	case <-ctx.Done():
	case <-doneSending:
	}
	ll.DebugContext(ctx, "accumulator: finished")
	return nil
}
