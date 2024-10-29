package humanlog

import (
	"bufio"
	"bytes"
	"context"
	"io"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Scan reads JSON-structured lines from src and prettify them onto dst. If
// the lines aren't JSON-structured, it will simply write them out with no
// prettification.
func Scan(ctx context.Context, src io.Reader, sink sink.Sink, opts *HandlerOptions) error {
	in := bufio.NewScanner(src)
	in.Buffer(make([]byte, 1024*1024), 1024*1024)
	in.Split(bufio.ScanLines)

	var line uint64

	logfmtEntry := LogfmtHandler{Opts: opts}
	jsonEntry := JSONHandler{Opts: opts}

	ev := new(typesv1.LogEvent)
	data := new(typesv1.StructuredLogEvent)
	ev.Structured = data

	for in.Scan() {

		line++
		lineData := in.Bytes()

		if ev.Structured == nil {
			ev.Structured = data
		}
		data.Reset()
		ev.Raw = lineData
		ev.ParsedAt = timestamppb.New(opts.timeNow())

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
		if err := sink.Receive(ctx, ev); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}

	select {
	case <-ctx.Done():
		return nil
	default:
	}

	switch err := in.Err(); err {
	case nil, io.EOF:
		return nil
	default:
		return err
	}
}

func checkEachUntilFound(fieldList []string, found func(string) bool) bool {
	for _, field := range fieldList {
		if found(field) {
			return true
		}
	}
	return false
}
