package validate

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
)

func StreamResolveBatchSize(req int) (int, error) {
	const (
		maxBatchSize     = 1000
		defaultBatchSize = 100
	)
	if req == 0 {
		return defaultBatchSize, nil
	}
	if req > maxBatchSize {
		return 0, fmt.Errorf("invalid `max_batch_size`: must be less than %d but was %d", maxBatchSize, req)
	}
	return req, nil
}

func StreamResolveBatchTicker(d *durationpb.Duration) (<-chan time.Time, func(), error) {
	const (
		minMaxBatchFor = 16 * time.Millisecond
	)
	if d == nil || d.AsDuration() == 0 {
		return nil, func() {}, nil
	}
	req := d.AsDuration()
	if req < minMaxBatchFor {
		return nil, nil, fmt.Errorf("invalid `max_batching_for`: must be greater than %v but was %v", minMaxBatchFor, req)
	}
	ticker := time.NewTicker(req)
	return ticker.C, ticker.Stop, nil
}
