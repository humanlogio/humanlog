package bufsink

import (
	"context"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/sink"
	"google.golang.org/protobuf/proto"
)

type SizedBuffer struct {
	size     int
	Buffered []*typesv1.Log
	flush    sink.BatchSink
}

var _ sink.Sink = (*SizedBuffer)(nil)

func NewSizedBufferedSink(size int, flush sink.BatchSink) *SizedBuffer {
	return &SizedBuffer{
		size:     size,
		Buffered: make([]*typesv1.Log, 0, size),
		flush:    flush,
	}
}

func (sn *SizedBuffer) Close(ctx context.Context) error {
	return nil
}

func (sn *SizedBuffer) Receive(ctx context.Context, ev *typesv1.Log) error {
	cev := proto.Clone(ev).(*typesv1.Log)
	sn.Buffered = append(sn.Buffered, cev)
	if len(sn.Buffered) == sn.size {
		if err := sn.flush.ReceiveBatch(ctx, sn.Buffered); err != nil {
			sn.Buffered = sn.Buffered[:len(sn.Buffered)-1]
			return err
		}
		sn.Buffered = sn.Buffered[:0:sn.size]
	}
	return nil
}
