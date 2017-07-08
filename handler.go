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
	SortLongest:      true,
	SkipUnchanged:    true,
	Truncates:        true,
	LightBg:          false,
	TruncateLength:   15,
	KeyRGB:           RGB{1, 108, 89},
	ValRGB:           RGB{125, 125, 125},
	TimeFormat:       time.Stamp,
	IgnoreInterrupts: false,
}

type RGB struct{ R, G, B uint8 }

func (r *RGB) tuple() (uint8, uint8, uint8) { return r.R, r.G, r.B }

type HandlerOptions struct {
	Skip             map[string]struct{}
	Keep             map[string]struct{}
	SortLongest      bool
	SkipUnchanged    bool
	Truncates        bool
	LightBg          bool
	TruncateLength   int
	KeyRGB           RGB
	ValRGB           RGB
	TimeFormat       string
	IgnoreInterrupts bool
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

func (h *HandlerOptions) SetSkip(skip []string) {
	if h.Skip == nil {
		h.Skip = make(map[string]struct{})
	}
	for _, key := range skip {
		h.Skip[key] = struct{}{}
	}
}

func (h *HandlerOptions) SetKeep(keep []string) {
	if h.Keep == nil {
		h.Keep = make(map[string]struct{})
	}
	for _, key := range keep {
		h.Keep[key] = struct{}{}
	}
}
