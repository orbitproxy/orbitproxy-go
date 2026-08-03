package backoff

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v5"
)

const (
	DefaultInitialInterval = time.Second
	DefaultMaxInterval     = 60 * time.Second
)

// NewExponentialBackOff returns a policy with SDK defaults.
func NewExponentialBackOff() *backoff.ExponentialBackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = DefaultInitialInterval
	b.MaxInterval = DefaultMaxInterval
	b.Multiplier = 2
	return b
}

// OnErrorFunc is invoked between retries.
type OnErrorFunc func(err error, wait time.Duration)

// Loop retries handler until it succeeds or ctx is cancelled.
func Loop(
	ctx context.Context,
	policy *backoff.ExponentialBackOff,
	handler func(context.Context) error,
	onError OnErrorFunc,
) error {
	if policy == nil {
		policy = NewExponentialBackOff()
	}

	for {
		err := handler(ctx)
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		wait := policy.NextBackOff()
		if onError != nil {
			onError(err, wait)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// Reset resets the exponential policy.
func Reset(policy *backoff.ExponentialBackOff) {
	if policy != nil {
		policy.Reset()
	}
}
