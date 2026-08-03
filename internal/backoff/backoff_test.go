package backoff

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoopRetriesUntilHandlerSucceeds(t *testing.T) {
	t.Parallel()

	policy := NewExponentialBackOff()
	policy.InitialInterval = 5 * time.Millisecond
	policy.MaxInterval = 20 * time.Millisecond

	var attempts atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := Loop(ctx, policy, func(context.Context) error {
		if attempts.Add(1) < 3 {
			return errors.New("handler unavailable")
		}
		cancel()
		return context.Canceled
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Loop returned error: %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
}

func TestLoopReturnsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Loop(ctx, NewExponentialBackOff(), func(context.Context) error {
		return errors.New("handler unavailable")
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Loop returned error: %v", err)
	}
}
