package humanlog

import (
	"time"

	"github.com/kr/logfmt"
)

// Handler can recognize it's log lines, parse them and prettify them.
type Handler interface {
	CanHandle(line []byte) bool
	Prettify(skipUnchanged bool) []byte
	logfmt.Handler
}

var DefaultOptions = &HandlerOptions{
	SortLongest:    true,
	SkipUnchanged:  true,
	Truncates:      true,
	LightBg:        false,
	TruncateLength: 15,
	TimeFormat:     time.Stamp,

	TimeFields:    []string{"time", "ts", "@timestamp", "timestamp"},
	MessageFields: []string{"message", "msg"},
	LevelFields:   []string{"level", "lvl", "loglevel", "severity"},

	Palette: DefaultPalette,
}

type HandlerOptions struct {
	Skip map[string]struct{}
	Keep map[string]struct{}

	TimeFields    []string
	MessageFields []string
	LevelFields   []string

	SortLongest    bool
	SkipUnchanged  bool
	Truncates      bool
	LightBg        bool
	TruncateLength int
	TimeFormat     string
	Palette        Palette
}

func (h *HandlerOptions) shouldShowKey(key string) bool {
	if len(h.Keep) != 0 {
		if _, keep := h.Keep[key]; keep {
			return true
		}
	}
	if len(h.Skip) != 0 {
		if _, skip := h.Skip[key]; skip {
			return false
		}
	}
	return true
}

func (h *HandlerOptions) shouldShowUnchanged(key string) bool {
	if len(h.Keep) != 0 {
		if _, keep := h.Keep[key]; keep {
			return true
		}
	}
	return false
}
