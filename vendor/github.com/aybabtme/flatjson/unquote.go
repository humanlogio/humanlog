package flatjson

import (
	"bytes"
	"fmt"
	"strconv"
	"unicode/utf8"
	"unsafe"
)

// lifted from https://github.com/iOliverNguyen/ujson/blob/772b59d2318e0b7d164303cf297c35067f24269e/quote.go#L35C1-L88C2

// Unquote decodes a double-quoted string key or value to retrieve the
// original string value. It will avoid allocation whenever possible.
//
// The code is inspired by strconv.Unquote, but only accepts valid json string.
func Unquote(s []byte) ([]byte, error) {
	n := len(s)
	if n < 2 {
		return nil, fmt.Errorf("invalid json string")
	}
	if s[0] != '"' || s[n-1] != '"' {
		return nil, fmt.Errorf("invalid json string")
	}
	s = s[1 : n-1]
	if bytes.IndexByte(s, '\n') != -1 {
		return nil, fmt.Errorf("invalid json string")
	}

	// avoid allocation if the string is trivial
	if bytes.IndexByte(s, '\\') == -1 {
		if utf8.Valid(s) {
			return s, nil
		}
	}

	// the following code is taken from strconv.Unquote (with modification)
	var runeTmp [utf8.UTFMax]byte
	buf := make([]byte, 0, 3*len(s)/2) // Try to avoid more allocations.
	for len(s) > 0 {
		// Convert []byte to string for satisfying UnquoteChar. We won't keep
		// the retured string, so it's safe to use unsafe here.
		c, multibyte, tail, err := strconv.UnquoteChar(unsafeBytesToString(s), '"')
		if err != nil {
			return nil, err
		}

		// UnquoteChar returns tail as the remaining unprocess string. Because
		// we are processing []byte, we use len(tail) to get the remaining bytes
		// instead.
		s = s[len(s)-len(tail):]
		if c < utf8.RuneSelf || !multibyte {
			buf = append(buf, byte(c))
		} else {
			n = utf8.EncodeRune(runeTmp[:], c)
			buf = append(buf, runeTmp[:n]...)
		}
	}
	return buf, nil
}

//go:nosplit
func unsafeBytesToString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}
