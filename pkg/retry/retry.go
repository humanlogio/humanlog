package retry

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// Defaults for the retries.
const (
	DefaultBaseSleep = 100 * time.Millisecond
	DefaultCapSleep  = 30 * time.Second
	DefaultFactor    = 2.0
)

type OnRetry func(attempt float64, err error)

type retrier struct {
	log     OnRetry
	ctx     context.Context
	r       *rand.Rand
	sleep   time.Duration
	cap     time.Duration
	factor  float64
	sleepFn func(time.Duration) <-chan time.Time
}

func newRetrier(ctx context.Context) *retrier {
	return &retrier{
		log:     func(_ float64, _ error) {},
		ctx:     ctx,
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
		sleep:   DefaultBaseSleep,
		cap:     DefaultCapSleep,
		factor:  DefaultFactor,
		sleepFn: time.After,
	}
}

// An Option changes the default behavior of a retrier.
type Option func(*retrier)

// UseBaseSleep makes the retrier sleep at least `base`.
func UseBaseSleep(base time.Duration) Option {
	return func(r *retrier) { r.sleep = base }
}

// UseCapSleep makes the retrier sleep at most `cap`.
func UseCapSleep(cap time.Duration) Option {
	return func(r *retrier) { r.cap = cap }
}

// UseFactor makes the retrier grow its exponential backoff using factor
// as the base of the exponent.
func UseFactor(factor float64) Option {
	return func(r *retrier) { r.factor = factor }
}

// UseLog makes the retrier log retries to the given func.
func UseLog(log OnRetry) Option {
	return func(r *retrier) { r.log = log }
}

// UseRand makes the retrier use the *rand.Rand for randomization of retries.
func UseRand(rd *rand.Rand) Option {
	return func(r *retrier) { r.r = rd }
}

// useSleepFn makes the retrier use the sleep func for sleeps.
func useSleepFn(fn func(time.Duration) <-chan time.Time) Option {
	return func(r *retrier) { r.sleepFn = fn }
}

// Do an action with exponential randomized backoff.
func Do(ctx context.Context, fn func(context.Context) (bool, error), opts ...Option) error {

	retrier := newRetrier(ctx)
	for _, opt := range opts {
		opt(retrier)
	}

	var (
		baseSeconds = retrier.sleep.Seconds()
		capSecond   = retrier.cap.Seconds()
		factor      = retrier.factor
		r           = retrier.r
		sleepFn     = retrier.sleepFn
		attempt     = 0.0
	)
	retryFunc := func() bool {
		sleepSeconds := r.Float64() * math.Min(capSecond, baseSeconds*math.Pow(factor, attempt))
		sleep := time.Duration(sleepSeconds * float64(time.Second))
		select {
		case <-sleepFn(sleep):
		case <-ctx.Done():
			return false
		}
		attempt += 1.0
		return true
	}

	for {
		retry, err := fn(ctx)
		if !retry {
			return err
		}

		retrier.log(attempt, err)
		if !retryFunc() {
			return err
		}
	}
}
