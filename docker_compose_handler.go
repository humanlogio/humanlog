package humanlog

import (
	"regexp"

	"github.com/humanlogio/humanlog/internal/pkg/model"
)

// dcLogsPrefixRe parses out a prefix like 'web_1 | ' from docker-compose
// The regex exists of five parts:
// 1. An optional color terminal escape sequence
// 2. The name of the service
// 3. Any number of spaces, and a pipe symbol
// 4. An optional color reset escape sequence
// 5. The rest of the line
var dcLogsPrefixRe = regexp.MustCompile("^(?:\x1b\\[\\d+m)?(?P<service_name>[a-zA-Z0-9._-]+)\\s+\\|(?:\x1b\\[0m)? (?P<rest_of_line>.*)$")

type handler interface {
	TryHandle([]byte, *model.Structured) bool
}

func tryDockerComposePrefix(d []byte, ev *model.Structured, nextHandler handler) bool {
	matches := dcLogsPrefixRe.FindSubmatch(d)
	if matches != nil {
		if nextHandler.TryHandle(matches[2], ev) {
			ev.KVs = append(ev.KVs, model.KV{
				Key: "service", Value: string(matches[1]),
			})
			return true
		}
		// The Zap Development handler is only built for `JSONHandler`s so
		// short-circuit calls for LogFmtHandlers
		switch h := nextHandler.(type) {
		case *JSONHandler:
			if tryZapDevDCPrefix(matches[2], ev, h) {
				ev.KVs = append(ev.KVs, model.KV{
					Key: "service", Value: string(matches[1]),
				})
				return true
			}
		}
	}
	return false
}
