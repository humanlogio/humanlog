package retry

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetrySuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Date(2024, time.October, 25, 13, 40, 37, 0, time.UTC)
	start := now
	r := rand.New(rand.NewSource(now.UnixNano()))
	tch := make(chan time.Time, 10)
	tch <- now

	called := 0
	err := Do(ctx,
		func(ctx context.Context) (bool, error) {
			called++
			return false, nil
		},
		UseRand(r),
		useSleepFn(func(d time.Duration) <-chan time.Time {
			t.Logf("sleeping for %v", d)
			now = now.Add(d)
			go func() {
				select {
				case tch <- now:
				case <-ctx.Done():
					panic("so sloooow")
				}
			}()
			return tch
		}),
	)
	require.NoError(t, err)
	require.Equal(t, 1, called)
	require.Equal(t, start, now)
}

func TestRetryFailOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Date(2024, time.October, 25, 13, 40, 37, 0, time.UTC)
	start := now
	r := rand.New(rand.NewSource(now.UnixNano()))
	tch := make(chan time.Time, 1)
	tch <- now

	called := 0
	err := Do(ctx,
		func(ctx context.Context) (bool, error) {
			called++
			return called != 2, nil
		},
		UseRand(r),
		useSleepFn(func(d time.Duration) <-chan time.Time {
			t.Logf("sleeping for %v", d)
			now = now.Add(d)
			go func() {
				select {
				case tch <- now:
				case <-ctx.Done():
					panic("so sloooow")
				}
			}()
			return tch
		}),
	)
	require.NoError(t, err)
	require.Equal(t, 2, called)
	require.Equal(t, start.Add(61015267), now)
}
