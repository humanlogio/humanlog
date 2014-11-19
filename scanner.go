package humanlog

import (
	"bufio"
	"io"

	"github.com/aybabtme/humanlog/parser/logfmt"
)

var (
	eol = [...]byte{'\n'}
)

// Scanner reads logfmt'd lines from src and prettify them onto dst.
// If the lines aren't logfmt, it will simply write them out with no
// prettification.
func Scanner(src io.Reader, dst io.Writer, opts *HandlerOptions) error {
	in := bufio.NewScanner(src)
	in.Split(bufio.ScanLines)

	var line uint64

	var lastLogrus bool

	logrusEntry := LogrusHandler{Opts: opts}

	for in.Scan() {
		line++
		lineData := in.Bytes()
		switch {

		case logrusEntry.CanHandle(lineData) && logfmt.Parse(lineData, true, true, logrusEntry.visit):
			dst.Write(logrusEntry.Prettify(opts.SkipUnchanged && lastLogrus))
			lastLogrus = true

		default:
			lastLogrus = false
			dst.Write(lineData)
		}
		dst.Write(eol[:])

	}

	switch err := in.Err(); err {
	case nil, io.EOF:
		return nil
	default:
		return err
	}
}
