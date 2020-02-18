package humanlog

import (
	"regexp"
)

// dcLogsPrefixRe parses out a prefix like 'web_1 | ' from docker-compose
var dcLogsPrefixRe = regexp.MustCompile("^(?:\x1b\\[\\d+m)?([a-zA-Z0-9._-]+)\\s+\\|(?:\x1b\\[0m)? (.*)$")

type handler interface {
	TryHandle([]byte) bool
	setField(key, val []byte)
}

func tryDockerComposePrefix(d []byte, nextHandler handler) bool {
	if matches := dcLogsPrefixRe.FindSubmatch(d); matches != nil {
		if nextHandler.TryHandle(matches[2]) {
			nextHandler.setField([]byte(`service`), matches[1])
			return true
		}
	}
	return false
}
