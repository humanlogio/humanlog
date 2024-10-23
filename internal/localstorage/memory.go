package localstorage

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
	"google.golang.org/protobuf/proto"
)

var (
	_ Queryable      = (*MemStorage)(nil)
	_ Storage        = (*MemStorage)(nil)
	_ sink.Sink      = (*MemStorageSink)(nil)
	_ sink.BatchSink = (*MemStorageSink)(nil)
)

type MemStorage struct {
	heartbeat time.Duration
	sinksMu   sync.Mutex
	sinks     []*MemStorageSink
}

func NewMemStorage() *MemStorage {
	return &MemStorage{heartbeat: time.Hour}
}

type SummarizedEvents struct {
	BucketWidth time.Duration
	Buckets     []struct {
		Time       time.Time
		EventCount int
	}
}

func (str *MemStorage) Query(ctx context.Context, q *typesv1.LogQuery) ([]Cursor, error) {
	if q.To != nil && q.From.AsTime().After(q.To.AsTime()) {
		return nil, fmt.Errorf("invalid query, `to` is before `from`")
	}

	str.sinksMu.Lock()
	defer str.sinksMu.Unlock()
	var cursors []Cursor
	for _, snk := range str.sinks {
		if idx, ok, err := snk.firstMatch(ctx, q); err != nil {
			return nil, err
		} else if ok {
			cursors = append(cursors, &MemSinkCursor{q: q, cur: idx, next: idx, more: true, sink: snk})
		}
	}
	return cursors, nil
}

func (str *MemStorage) Heartbeat(ctx context.Context, machineID, sessionID int64) (time.Duration, error) {
	return str.heartbeat, nil
}

func (str *MemStorage) SinkFor(machineID, sessionID int64) (sink.Sink, time.Duration, error) {
	str.sinksMu.Lock()
	defer str.sinksMu.Unlock()
	id := SinkID{machineID: machineID, sessionID: sessionID}
	loc, ok := slices.BinarySearchFunc(str.sinks, id, func(mss *MemStorageSink, si SinkID) int {
		return mss.id.cmp(si)
	})
	if ok {
		return str.sinks[loc], time.Hour, nil
	}
	newsink := &MemStorageSink{id: id}
	str.sinks = slices.Insert(str.sinks, loc, newsink)
	return newsink, str.heartbeat, nil
}

type MemSinkCursor struct {
	q *typesv1.LogQuery

	cur  int
	next int
	more bool

	sink *MemStorageSink
	err  error
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
	crs.next, crs.more, crs.err = crs.sink.nextMatch(ctx, crs.q, crs.next)
	return hasCurrent && crs.err == nil
}

func (crs *MemSinkCursor) Event() *typesv1.LogEvent {
	return crs.sink.evs[crs.cur]
}

func (crs *MemSinkCursor) Err() error {
	return crs.err
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
	mu  sync.RWMutex
	id  SinkID
	evs []*typesv1.LogEvent
}

func (snk *MemStorageSink) firstMatch(_ context.Context, q *typesv1.LogQuery) (index int, ok bool, err error) {
	snk.mu.RLock()
	defer snk.mu.RUnlock()

	for i, ev := range snk.evs {
		if eventMatches(q, ev) {
			return i, true, nil
		}
	}
	return -1, false, nil
}

func (snk *MemStorageSink) nextMatch(_ context.Context, q *typesv1.LogQuery, fromIndex int) (index int, ok bool, err error) {
	snk.mu.RLock()
	defer snk.mu.RUnlock()
	if len(snk.evs) < fromIndex {
		return -1, false, nil
	}
	for i, ev := range snk.evs[fromIndex:] {
		if eventMatches(q, ev) {
			return fromIndex + i, true, nil
		}
	}
	return -1, false, nil
}

func eventMatches(q *typesv1.LogQuery, ev *typesv1.LogEvent) bool {
	ts := ev.ParsedAt.AsTime()
	from := q.From.AsTime()
	to := q.To.AsTime()
	atOrAfter := ts.Equal(from) || ts.After(from)
	before := ts.Before(to)

	// TODO(antoine): match more stuff on the query

	return atOrAfter && before
}

func (snk *MemStorageSink) Receive(ctx context.Context, ev *typesv1.LogEvent) error {
	snk.mu.Lock()
	defer snk.mu.Unlock()
	snk.receive(ev)
	return nil
}

func (snk *MemStorageSink) ReceiveBatch(ctx context.Context, evs []*typesv1.LogEvent) error {
	snk.mu.Lock()
	defer snk.mu.Unlock()
	for _, ev := range evs {
		snk.receive(ev)
	}
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

func (snk *MemStorageSink) Flush(ctx context.Context) error { return nil }
