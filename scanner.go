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

	ev := new(typesv1.Log)

	handlers := []func([]byte, *typesv1.Log) bool{
		jsonEntry.TryHandle,
		logfmtEntry.TryHandle,
		func(lineData []byte, data *typesv1.Log) bool {
			return tryDockerComposePrefix(lineData, data, &jsonEntry)
		},
		func(lineData []byte, data *typesv1.Log) bool {
			return tryDockerComposePrefix(lineData, data, &logfmtEntry)
		},
		func(lineData []byte, data *typesv1.Log) bool {
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

		ev.Reset()
		ev.Raw = lineData
		ev.ObservedTimestamp = timestamppb.New(opts.timeNow())

		// remove that pesky syslog crap
		lineData = bytes.TrimPrefix(lineData, []byte("@cee: "))

	handled_line:
		for i, tryHandler := range handlers {
			if tryHandler(lineData, ev) {
				if dynamicReordering && i != 0 {
					handlers = moveToFront(i, handlers)
				}
				break handled_line
			}
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
	for i, field := range fieldList {
		if found(field) {
			if dynamicReordering {
				// the log stream probably will always be using this field
				moveToFront(i, fieldList)
			}
			return true
		}
	}
	return false
}
