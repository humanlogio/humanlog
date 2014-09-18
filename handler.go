package humanlog

import (
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
	TruncateLength: 15,
	KeyRGB:         RGB{1, 108, 89},
	ValRGB:         RGB{125, 125, 125},
}

type RGB struct{ R, G, B uint8 }

func (r *RGB) tuple() (uint8, uint8, uint8) { return r.R, r.G, r.B }

type HandlerOptions struct {
	Skip           map[string]struct{}
	Keep           map[string]struct{}
	SortLongest    bool
	SkipUnchanged  bool
	Truncates      bool
	TruncateLength int
	KeyRGB         RGB
	ValRGB         RGB
}

func (h *HandlerOptions) setup() {
	h.Skip = make(map[string]struct{})
	h.Keep = make(map[string]struct{})
}

func (h *HandlerOptions) shouldShowKey(key string) bool {
	if len(h.Keep) != 0 {
		_, keep := h.Keep[key]
		return keep
	}
	if len(h.Skip) != 0 {
		_, skip := h.Skip[key]
		return !skip
	}
	return true
}

func (h *HandlerOptions) SetSkip(skip []string) {
	if len(h.Keep) != 0 || h.Skip == nil {
		h.setup()
	}
	for _, key := range skip {
		h.Skip[key] = struct{}{}
	}
}

func (h *HandlerOptions) SetKeep(keep []string) {
	if len(h.Skip) != 0 || h.Keep == nil {
		h.setup()
	}
	for _, key := range keep {
		h.Keep[key] = struct{}{}
	}
}
