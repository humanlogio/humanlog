package humanlog

import (
	"time"

	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/kr/logfmt"
)

// Handler can recognize it's log lines, parse them and prettify them.
type Handler interface {
	CanHandle(line []byte) bool
	Prettify(skipUnchanged bool) []byte
	logfmt.Handler
}

var DefaultOptions = func() *HandlerOptions {
	opts := &HandlerOptions{
		TimeFields:    []string{"time", "ts", "@timestamp", "timestamp", "Timestamp", "asctime"},
		MessageFields: []string{"message", "msg", "Body"},
		LevelFields:   []string{"level", "lvl", "loglevel", "severity", "SeverityText"},
		timeNow:       time.Now,
	}
	return opts
}

type HandlerOptions struct {
	TimeFields    []string
	MessageFields []string
	LevelFields   []string

	timeNow func() time.Time
}

var _ = HandlerOptionsFrom(config.DefaultConfig) // ensure it's valid

func HandlerOptionsFrom(cfg config.Config) *HandlerOptions {
	opts := DefaultOptions()
	if cfg.TimeFields != nil {
		opts.TimeFields = appendUnique(opts.TimeFields, *cfg.TimeFields)
	}
	if cfg.MessageFields != nil {
		opts.MessageFields = appendUnique(opts.MessageFields, *cfg.MessageFields)
	}
	if cfg.LevelFields != nil {
		opts.LevelFields = appendUnique(opts.LevelFields, *cfg.LevelFields)
	}
	return opts
}

func appendUnique(a []string, b []string) []string {
	// init with `len(b)` because usually `a` will be
	// nil at first, but `b` wont be
	seen := make(map[string]struct{}, len(b))
	out := make([]string, 0, len(b))
	for _, aa := range a {
		if _, ok := seen[aa]; ok {
			continue
		}
		seen[aa] = struct{}{}
		out = append(out, aa)
	}
	for _, bb := range b {
		if _, ok := seen[bb]; ok {
			continue
		}
		seen[bb] = struct{}{}
		out = append(out, bb)
	}
	return out
}
