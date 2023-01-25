package humanlog

import (
	"context"
	"strings"
	"testing"

	"github.com/humanlogio/humanlog/internal/pkg/model"
	"github.com/humanlogio/humanlog/internal/pkg/sink/bufsink"
	"github.com/stretchr/testify/require"
)

func TestScannerLongLine(t *testing.T) {
	data := `{"msg":"` + strings.Repeat("a", 1023*1024) + `"}`
	ctx := context.Background()
	src := strings.NewReader(data)
	want := []model.Event{
		{Raw: []byte(data), Structured: &model.Structured{Msg: strings.Repeat("a", 1023*1024)}},
	}
	sink := bufsink.NewSizedBufferedSink(100, nil)
	err := Scan(ctx, src, sink, DefaultOptions())
	require.NoError(t, err, "got %#v", err)
	require.Equal(t, want, sink.Buffered)
}
