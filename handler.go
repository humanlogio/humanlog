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
	ColorFlag:      ColorModeAuto,
	LightBg:        false,
	TruncateLength: 15,
	TimeFormat:     time.Stamp,

	TimeFields:    []string{"time", "ts", "@timestamp", "timestamp"},
	MessageFields: []string{"message", "msg"},
	LevelFields:   []string{"level", "lvl", "loglevel", "severity"},

	palette: DefaultPalette,
}

type HandlerOptions struct {
	Skip map[string]struct{} `json:"skip"`
	Keep map[string]struct{} `json:"keep"`

	TimeFields    []string `json:"time_fields"`
	MessageFields []string `json:"message_fields"`
	LevelFields   []string `json:"level_fields"`

	SortLongest    bool      `json:"sort_longest"`
	SkipUnchanged  bool      `json:"skip_unchanged"`
	Truncates      bool      `json:"truncates"`
	LightBg        bool      `json:"light_bg"`
	ColorFlag      ColorMode `json:"color_mode"`
	TruncateLength int       `json:"truncate_length"`
	TimeFormat     string    `json:"time_format"`

	Palette *TextPalette `json:"palette"`
	// once compiled
	palette *Palette
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
