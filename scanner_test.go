package humanlog

import (
	"context"
	"strings"
	"testing"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink/bufsink"
	"github.com/stretchr/testify/require"
)

func TestScannerLongLine(t *testing.T) {
	data := `{"msg":"` + strings.Repeat("a", 1023*1024) + `"}`
	ctx := context.Background()
	src := strings.NewReader(data)
	want := []*typesv1.LogEvent{
		{Raw: []byte(data), Structured: &typesv1.StructuredLogEvent{Msg: strings.Repeat("a", 1023*1024)}},
	}
	sink := bufsink.NewSizedBufferedSink(100, nil)
	err := Scan(ctx, src, sink, DefaultOptions())
	require.NoError(t, err, "got %#v", err)
	require.Equal(t, want, sink.Buffered)
}
