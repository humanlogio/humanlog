package humanlog

import (
	"log"
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

var _ = HandlerOptionsFrom(config.DefaultConfig) // ensure it's valid

func HandlerOptionsFrom(cfg config.Config) *HandlerOptions {
	opts := DefaultOptions
	if cfg.Skip != nil {
		opts.Skip = sliceToSet(cfg.Skip)
	}
	if cfg.Keep != nil {
		opts.Keep = sliceToSet(cfg.Keep)
	}
	if cfg.TimeFields != nil {
		opts.TimeFields = *cfg.TimeFields
	}
	if cfg.MessageFields != nil {
		opts.MessageFields = *cfg.MessageFields
	}
	if cfg.LevelFields != nil {
		opts.LevelFields = *cfg.LevelFields
	}
	if cfg.SortLongest != nil {
		opts.SortLongest = *cfg.SortLongest
	}
	if cfg.SkipUnchanged != nil {
		opts.SkipUnchanged = *cfg.SkipUnchanged
	}
	if cfg.Truncates != nil {
		opts.Truncates = *cfg.Truncates
	}
	if cfg.LightBg != nil {
		opts.LightBg = *cfg.LightBg
	}
	if cfg.TruncateLength != nil {
		opts.TruncateLength = *cfg.TruncateLength
	}
	if cfg.TimeFormat != nil {
		opts.TimeFormat = *cfg.TimeFormat
	}
	if cfg.Palette != nil {
		pl, err := PaletteFrom(*cfg.Palette)
		if err != nil {
			log.Printf("invalid palette, using default one: %v", err)
		} else {
			opts.Palette = *pl
		}
	}
	return opts
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

func sliceToSet(arr *[]string) map[string]struct{} {
	if arr == nil {
		return nil
	}
	out := make(map[string]struct{})
	for _, key := range *arr {
		out[key] = struct{}{}
	}
	return out
}
