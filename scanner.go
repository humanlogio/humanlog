package humanlog

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxBufferSize = 1024 * 1024

// Scan reads JSON-structured lines from src and prettify them onto dst. If
// the lines aren't JSON-structured, it will simply write them out with no
// prettification.
func Scan(ctx context.Context, src io.Reader, sink sink.Sink, opts *HandlerOptions) error {

	in := bufio.NewScanner(src)
	in.Buffer(make([]byte, 0, maxBufferSize), maxBufferSize)
	in.Split(bufio.ScanLines)

	var line uint64

	logfmtEntry := LogfmtHandler{Opts: opts}
	jsonEntry := JSONHandler{Opts: opts}

	ev := new(typesv1.LogEvent)
	data := new(typesv1.StructuredLogEvent)
	ev.Structured = data

	handlers := []func([]byte, *typesv1.StructuredLogEvent) bool{
		jsonEntry.TryHandle,
		logfmtEntry.TryHandle,
		func(lineData []byte, data *typesv1.StructuredLogEvent) bool {
			return tryDockerComposePrefix(lineData, data, &jsonEntry)
		},
		func(lineData []byte, data *typesv1.StructuredLogEvent) bool {
			return tryDockerComposePrefix(lineData, data, &logfmtEntry)
		},
		func(lineData []byte, data *typesv1.StructuredLogEvent) bool {
			return tryZapDevPrefix(lineData, data, &jsonEntry)
		},
	}

	skipNextScan := false
	for {
		if !in.Scan() {
			err := in.Err()
			if err == nil || errors.Is(err, io.EOF) {
				break
			}
			if errors.Is(err, bufio.ErrTooLong) {
				in = bufio.NewScanner(src)
				in.Buffer(make([]byte, 0, maxBufferSize), maxBufferSize)
				in.Split(bufio.ScanLines)
				skipNextScan = true
				continue
			}
			break
		}
		if skipNextScan {
			skipNextScan = false
			continue
		}

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

		handled := false
	handled_line:
		for i, tryHandler := range handlers {
			if tryHandler(lineData, data) {
				if dynamicReordering {
					handlers = moveToFront(i, handlers)
				}
				handled = true
				break handled_line
			}
		}
		if !handled {
			ev.Structured = nil
		}
		// switch {

		// case jsonEntry.TryHandle(lineData, data):

		// case logfmtEntry.TryHandle(lineData, data):

		// case tryDockerComposePrefix(lineData, data, &jsonEntry):

		// case tryDockerComposePrefix(lineData, data, &logfmtEntry):

		// case tryZapDevPrefix(lineData, data, &jsonEntry):

		// default:

		// }
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
