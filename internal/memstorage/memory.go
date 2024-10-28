package memstorage

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/teivah/broadcast"
	"google.golang.org/protobuf/proto"
)

var (
	_ localstorage.Queryable = (*MemStorage)(nil)
	_ localstorage.Storage   = (*MemStorage)(nil)
	_ sink.Sink              = (*MemStorageSink)(nil)
	_ sink.BatchSink         = (*MemStorageSink)(nil)
)

func init() {
	localstorage.RegisterStorage("basic", func(ctx context.Context, ll *slog.Logger, cfg map[string]interface{}) (localstorage.Storage, error) {
		return NewMemStorage(ll), nil
	})
}

type MemStorage struct {
	ll        *slog.Logger
	heartbeat time.Duration
	sinksMu   sync.Mutex
	sinks     []*MemStorageSink

	newSinkRelay *broadcast.Relay[*MemStorageSink]
}

func NewMemStorage(ll *slog.Logger) *MemStorage {
	return &MemStorage{
		ll:           ll,
		heartbeat:    time.Hour,
		newSinkRelay: broadcast.NewRelay[*MemStorageSink](),
	}
}

type SummarizedEvents struct {
	BucketWidth time.Duration
	Buckets     []struct {
		Time       time.Time
		EventCount int
	}
}

func (str *MemStorage) Close() error { return nil }

func (str *MemStorage) Query(ctx context.Context, q *typesv1.LogQuery) (<-chan localstorage.Cursor, error) {
	if q.To != nil && q.From.AsTime().After(q.To.AsTime()) {
		return nil, fmt.Errorf("invalid query, `to` is before `from`")
	}

	str.sinksMu.Lock()
	defer str.sinksMu.Unlock()

	var cursors []localstorage.Cursor
	for _, snk := range str.sinks {
		if idx, ok, err := snk.firstMatch(ctx, q); err != nil {
			return nil, err
		} else if ok || q.To == nil {
			ll := snk.queryLogger(q)
			ll.DebugContext(ctx, "sink is relevant for query")
			cursors = append(cursors, newMemSinkCursor(ll, q, idx, idx, true, snk))
		}
	}
	newCursors := make(chan localstorage.Cursor, len(cursors))
	for _, cursor := range cursors {
		newCursors <- cursor
	}
	if q.To == nil {
		l := str.newSinkRelay.Listener(1)

		go func() {
			defer close(newCursors)
			defer l.Close()
			for {
				select {
				case <-ctx.Done():
					return
				case newSink := <-l.Ch():
					ll := newSink.queryLogger(q)
					ll.DebugContext(ctx, "a new sink appeared")
					newCursor := newMemSinkCursor(ll, q, 0, 0, true, newSink)
					select {
					case <-ctx.Done():
						return
					case newCursors <- newCursor:
					}
				}
			}
		}()
	} else {
		close(newCursors)
	}
	return newCursors, nil
}

func (str *MemStorage) Heartbeat(ctx context.Context, machineID, sessionID int64) (time.Duration, error) {
	return str.heartbeat, nil
}

func (str *MemStorage) SinkFor(ctx context.Context, machineID, sessionID int64) (sink.Sink, time.Duration, error) {
	str.sinksMu.Lock()
	defer str.sinksMu.Unlock()

	id := SinkID{machineID: machineID, sessionID: sessionID}
	loc, ok := slices.BinarySearchFunc(str.sinks, id, func(mss *MemStorageSink, si SinkID) int {
		return mss.id.cmp(si)
	})
	if ok {
		return str.sinks[loc], str.heartbeat, nil
	}
	ll := str.ll.With(
		slog.Int64("machine.id", machineID),
		slog.Int64("session.id", sessionID),
	)
	newsink := newMemStorageSink(ll, id)
	str.sinks = slices.Insert(str.sinks, loc, newsink)
	str.newSinkRelay.Broadcast(newsink)

	return newsink, str.heartbeat, nil
}

type MemSinkCursor struct {
	ll *slog.Logger
	q  *typesv1.LogQuery

	cur  int
	next int
	more bool

	sink     *MemStorageSink
	listener *broadcast.Listener[struct{}]
	err      error
}

func newMemSinkCursor(
	ll *slog.Logger,
	q *typesv1.LogQuery,
	cur int,
	next int,
	more bool,
	sink *MemStorageSink,
) *MemSinkCursor {
	var listener *broadcast.Listener[struct{}]
	if !sink.closed && q.To == nil {
		listener = sink.relay.Listener(1)
	}
	return &MemSinkCursor{ll: ll, q: q, cur: cur, next: next, more: more, sink: sink, listener: listener}
}

func (crs *MemSinkCursor) IDs() (machineID, sessionID int64) {
	return crs.sink.id.machineID, crs.sink.id.sessionID
}

func (crs *MemSinkCursor) Next(ctx context.Context) bool {
	hasCurrent := crs.cur >= 0 && crs.more
	if !hasCurrent {
		return false
	}
	crs.cur = crs.next
	crs.next = crs.next + 1
	crs.next, crs.more, crs.err = crs.sink.nextMatch(ctx, crs.ll, crs.q, crs.next, crs.listener)
	return hasCurrent && crs.err == nil
}

func (crs *MemSinkCursor) Event(ev *typesv1.LogEvent) error {
	orig := crs.sink.evs[crs.cur]
	ev.ParsedAt = orig.ParsedAt
	ev.Raw = orig.Raw
	ev.Structured = orig.Structured
	return nil
}

func (crs *MemSinkCursor) Err() error {
	return crs.err
}

func (crs *MemSinkCursor) Close() error {
	if crs.listener != nil {
		crs.listener.Close()
	}
	return nil
}

type SinkID struct {
	machineID int64
	sessionID int64
}

func (sid SinkID) cmp(other SinkID) int {
	mid := sid.machineID - other.machineID
	if mid != 0 {
		return int(mid)
	}
	return int(sid.sessionID - other.sessionID)
}

type MemStorageSink struct {
	ll  *slog.Logger
	mu  sync.RWMutex
	id  SinkID
	evs []*typesv1.LogEvent

	closed bool
	relay  *broadcast.Relay[struct{}]
}

func newMemStorageSink(ll *slog.Logger, id SinkID) *MemStorageSink {
	return &MemStorageSink{ll: ll, id: id, relay: broadcast.NewRelay[struct{}]()}
}

func (snk *MemStorageSink) queryLogger(q *typesv1.LogQuery) *slog.Logger {
	ll := snk.ll.With(
		slog.Bool("sink.closed", snk.closed),
		slog.String("query", q.Query),
	)
	if q.From != nil {
		ll = ll.With(slog.Time("from", q.From.AsTime()))
	}
	if q.To != nil {
		ll = ll.With(slog.Time("to", q.To.AsTime()))
	}
	return ll
}

func (snk *MemStorageSink) firstMatch(ctx context.Context, q *typesv1.LogQuery) (index int, ok bool, err error) {
	snk.mu.RLock()
	defer snk.mu.RUnlock()

	for i, ev := range snk.evs {
		if eventMatches(q, ev) {
			snk.ll.DebugContext(ctx, "first match found at index", slog.Int("i", i))
			return i, true, nil
		}
	}
	return len(snk.evs), false, nil
}

func (snk *MemStorageSink) nextMatch(
	ctx context.Context,
	ll *slog.Logger,
	q *typesv1.LogQuery,
	fromIndex int,
	listener *broadcast.Listener[struct{}],
) (index int, ok bool, err error) {
restartMatch:
	snk.mu.RLock()
	unlocked := false
	defer func() {
		if !unlocked {
			snk.mu.RUnlock()
		}
	}()
	shouldWaitForMore := !snk.closed && listener != nil && q.To == nil
	if len(snk.evs) < fromIndex {
		ll.DebugContext(ctx, "reached end of buffer")
		if shouldWaitForMore {
			// sink is still receiving data and query is unbound
			// unlock the sink so more logs can be received,
			// then restart the next match process
			snk.mu.RUnlock()
			unlocked = true
			ll.DebugContext(ctx, "waiting for more data")
			select {
			case <-listener.Ch():
				ll.DebugContext(ctx, "more data received, rematching")
				goto restartMatch
			case <-ctx.Done():
				return -1, false, ctx.Err()
			}
		}
		return -1, false, nil
	}
	ll.DebugContext(ctx, "searching buffer for next match")
	for i, ev := range snk.evs[fromIndex:] {
		if eventMatches(q, ev) {
			return fromIndex + i, true, nil
		}
	}
	if !shouldWaitForMore {
		return -1, false, nil
	}
	// sink is still receiving data and query is unbound
	// unlock the sink so more logs can be received,
	// then restart the next match process
	snk.mu.RUnlock()
	unlocked = true
	ll.DebugContext(ctx, "waiting for more data")
	select {
	case <-listener.Ch():
		ll.DebugContext(ctx, "more data received, rematching")
		goto restartMatch
	case <-ctx.Done():
		return -1, false, ctx.Err()
	}
}

func eventMatches(q *typesv1.LogQuery, ev *typesv1.LogEvent) bool {
	ts := ev.ParsedAt.AsTime()
	from := q.From.AsTime()
	atOrAfter := ts.Equal(from) || ts.After(from)

	to := q.To
	before := to == nil || ts.Before(to.AsTime())

	// TODO(antoine): match more stuff on the query

	return atOrAfter && before
}

var q = struct{}{}

func (snk *MemStorageSink) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
	snk.mu.Lock()
	defer snk.mu.Unlock()
	if snk.closed {
		return fmt.Errorf("sink is closed")
	}
	snk.receive(ev)
	snk.relay.Broadcast(q)
	return nil
}

func (snk *MemStorageSink) ReceiveBatch(ctx context.Context, evs []*typesv1.LogEvent) error {
	snk.mu.Lock()
	defer snk.mu.Unlock()
	if snk.closed {
		return fmt.Errorf("sink is closed")
	}
	for _, ev := range evs {
		snk.receive(ev)
	}
	snk.relay.Broadcast(q)
	return nil
}

func (snk *MemStorageSink) receive(ev *typesv1.LogEvent) {
	ev = proto.Clone(ev).(*typesv1.LogEvent)
	if len(snk.evs) == 0 {
		snk.evs = append(snk.evs, ev)
		return
	}
	lastEv := snk.evs[len(snk.evs)-1]
	if (lastEv.ParsedAt.Seconds > ev.ParsedAt.Seconds) ||
		(lastEv.ParsedAt.Seconds == ev.ParsedAt.Seconds && lastEv.ParsedAt.Nanos > ev.ParsedAt.Nanos) {
		panic(fmt.Sprintf("out of order inserts within same sink?\nlastEv:\n\t%#v\nev:\n\t%#v",
			lastEv.ParsedAt,
			ev.ParsedAt,
		))
	}
	snk.evs = append(snk.evs, ev)

}

func (snk *MemStorageSink) Close(ctx context.Context) error {
	snk.mu.Lock()
	defer snk.mu.Unlock()
	snk.closed = true
	return nil
}
