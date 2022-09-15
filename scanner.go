package humanlog

import (
	"bufio"
	"bytes"
	"io"
	"time"

	"github.com/humanlogio/humanlog/internal/pkg/model"
	"github.com/humanlogio/humanlog/internal/pkg/sink"
)

// Scanner reads JSON-structured lines from src and prettify them onto dst. If
// the lines aren't JSON-structured, it will simply write them out with no
// prettification.
func Scanner(src io.Reader, sink sink.Sink, opts *HandlerOptions) error {
	in := bufio.NewScanner(src)
	in.Split(bufio.ScanLines)

	var line uint64

	logfmtEntry := LogfmtHandler{Opts: opts}
	jsonEntry := JSONHandler{Opts: opts}

	ev := new(model.Event)
	data := new(model.Structured)
	ev.Structured = data

	for in.Scan() {
		line++
		lineData := in.Bytes()

		if ev.Structured == nil {
			ev.Structured = data
		}
		data.Time = time.Time{}
		data.Msg = ""
		data.Level = ""
		data.KVs = data.KVs[:0]
		ev.Raw = lineData

		// remove that pesky syslog crap
		lineData = bytes.TrimPrefix(lineData, []byte("@cee: "))
		switch {

		case jsonEntry.TryHandle(lineData, data):

		case logfmtEntry.TryHandle(lineData, data):

		case tryDockerComposePrefix(lineData, data, &jsonEntry):

		case tryDockerComposePrefix(lineData, data, &logfmtEntry):

		case tryZapDevPrefix(lineData, data, &jsonEntry):

		default:
			ev.Structured = nil
		}
		if err := sink.Receive(ev); err != nil {
			return err
		}
	}

	switch err := in.Err(); err {
	case nil, io.EOF:
		return nil
	default:
		return err
	}
}
