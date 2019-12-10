package logfmt

import (
	"bytes"
	"unicode"
	"unicode/utf8"
)

// Visitor receives key/value pairs as they are parse, returns `false` if
// it wishes to abort the parsing.
type Visitor func(key, val []byte) (more bool)

// Parse does a best effort parsing of logfmt entries. If `allowEmptyKey`,
// it will parse ` =value` as `""=value`, where empty string is a valid key.
func Parse(data []byte, allowEmptyKey, keepGarbage bool, eachPair Visitor) bool {
	// don't try to parse logfmt if there's no `mykey=` in the
	// first few bytes
	scanAllKeyValue(data, allowEmptyKey, keepGarbage, eachPair)
	return true
}

func scanAllKeyValue(data []byte, allowEmptyKey, keepGarbage bool, eachPair Visitor) {
	i := 0
	firstKV := true
	more := true
	for i < len(data) && more {
		keyStart, keyEnd, valStart, valEnd, found := scanKeyValue(data, i, allowEmptyKey)
		if !found {
			return
		}
		if firstKV {
			firstKV = false
			if keyStart != 0 && keepGarbage {
				eachPair([]byte("garbage"), data[:keyStart])
			}
		}

		if valStart == valEnd {
			more = eachPair(data[keyStart:keyEnd], nil)
		} else if data[valStart] == '"' {
			more = eachPair(data[keyStart:keyEnd], data[valStart+1:valEnd-1])
		} else {
			more = eachPair(data[keyStart:keyEnd], data[valStart:valEnd])
		}
		i = valEnd + 1
	}

}

func scanKeyValue(data []byte, from int, allowEmptyKey bool) (keyStart, keyEnd, valStart, valEnd int, found bool) {

	keyStart, keyEnd, found = findWordFollowedBy('=', data, from, allowEmptyKey)
	if !found {
		return
	}
	valStart = keyEnd + 1
	if r, sz := utf8.DecodeRune(data[valStart:]); r == '"' {
		// find next unescaped `"`
		valEnd = findUnescaped('"', '\\', data, valStart+sz)
		found = valEnd != -1
		valEnd++
		return
	}

	nextKeyStart, _, nextFound := findWordFollowedBy('=', data, keyEnd+1, allowEmptyKey)

	if nextFound {
		valEnd = nextKeyStart - 1
	} else {
		valEnd = len(data)
	}

	return
}

func findWordFollowedBy(by rune, data []byte, from int, allowEmptyKey bool) (start int, end int, found bool) {
	i := bytes.IndexRune(data[from:], by)
	if i == -1 {
		return i, i, false
	}
	i += from
	// loop for all letters before the `by`, stop at the first space
	for j := i - 1; j >= from; j-- {
		if !utf8.RuneStart(data[j]) {
			continue
		}
		r, _ := utf8.DecodeRune(data[j:])
		if unicode.IsSpace(r) {
			j++
			return j, i, allowEmptyKey || j < i
		}
	}
	return from, i, allowEmptyKey || from < i
}

func findUnescaped(toFind, escape rune, data []byte, from int) int {
	for i := from; i < len(data); {
		r, sz := utf8.DecodeRune(data[i:])
		i += sz
		if r == escape {
			// skip next char
			_, sz = utf8.DecodeRune(data[i:])
			i += sz
		} else if r == toFind {
			return i - sz
		}
	}
	return -1
}
