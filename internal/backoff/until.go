package backoff

import (
	"math/rand/v2"
	"time"
)

// BackoffFunc adapts a function to BackoffManager.
type BackoffFunc func(previousDuration time.Duration, previousConditionError bool) time.Duration

func (f BackoffFunc) Backoff(previousDuration time.Duration, previousConditionError bool) time.Duration {
	return f(previousDuration, previousConditionError)
}

// BackoffManager computes the next wait duration.
type BackoffManager interface {
	Backoff(previousDuration time.Duration, previousConditionError bool) time.Duration
}

// FastBackoffOptions configures periodic backoff with optional fast retries.
type FastBackoffOptions struct {
	Duration           time.Duration
	Factor             float64
	Jitter             float64
	MaxDuration        time.Duration
	InitDurationIfFail time.Duration

	FastRetryCount  int
	FastRetryDelay  time.Duration
	FastRetryJitter float64
	FastRetryWindow time.Duration
}

type fastBackoffImpl struct {
	options FastBackoffOptions
	clock   clock

	lastCalledTime      time.Time
	consecutiveErrCount int

	fastRetryCutoffTime     time.Time
	countsInFastRetryWindow int
}

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// NewFastBackoffManager creates a periodic backoff scheduler.
func NewFastBackoffManager(options FastBackoffOptions) BackoffManager {
	return newFastBackoffManagerWithClock(options, realClock{})
}

func newFastBackoffManagerWithClock(options FastBackoffOptions, clk clock) BackoffManager {
	if clk == nil {
		clk = realClock{}
	}
	return &fastBackoffImpl{
		options:                 options,
		clock:                   clk,
		countsInFastRetryWindow: 1,
	}
}

func (f *fastBackoffImpl) Backoff(previousDuration time.Duration, previousConditionError bool) time.Duration {
	if f.lastCalledTime.IsZero() {
		f.lastCalledTime = f.clock.Now()
		return f.options.Duration
	}
	now := f.clock.Now()
	f.lastCalledTime = now

	if previousConditionError {
		f.consecutiveErrCount++
	} else {
		f.consecutiveErrCount = 0
	}

	if f.options.FastRetryCount > 0 && previousConditionError {
		f.countsInFastRetryWindow++
		if f.countsInFastRetryWindow <= f.options.FastRetryCount {
			return Jitter(f.options.FastRetryDelay, f.options.FastRetryJitter)
		}
		if now.After(f.fastRetryCutoffTime) {
			f.fastRetryCutoffTime = now.Add(f.options.FastRetryWindow)
			f.countsInFastRetryWindow = 0
		}
	}

	if previousConditionError {
		var duration time.Duration
		if f.consecutiveErrCount == 1 {
			duration = emptyOr(f.options.InitDurationIfFail, previousDuration)
		} else {
			duration = previousDuration
		}

		duration = emptyOr(duration, time.Second)
		if f.options.Factor != 0 {
			duration = time.Duration(float64(duration) * f.options.Factor)
		}
		if f.options.Jitter > 0 {
			duration = Jitter(duration, f.options.Jitter)
		}
		if f.options.MaxDuration > 0 && duration > f.options.MaxDuration {
			duration = f.options.MaxDuration
		}
		return duration
	}
	return f.options.Duration
}

// BackoffUntil runs f until it returns done or stopCh closes.
func BackoffUntil(f func() (bool, error), manager BackoffManager, sliding bool, stopCh <-chan struct{}) {
	var delay time.Duration
	previousError := false

	ticker := time.NewTicker(manager.Backoff(delay, previousError))
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		if !sliding {
			delay = manager.Backoff(delay, previousError)
		}

		done, err := f()
		if done {
			return
		}
		previousError = err != nil

		if sliding {
			delay = manager.Backoff(delay, previousError)
		}

		ticker.Reset(delay)
		select {
		case <-stopCh:
			return
		case <-ticker.C:
		}
	}
}

// Jitter adds random jitter to duration.
func Jitter(duration time.Duration, maxFactor float64) time.Duration {
	if maxFactor <= 0.0 {
		maxFactor = 1.0
	}
	return duration + time.Duration(rand.Float64()*maxFactor*float64(duration))
}

// Until runs f every period until stopCh closes.
func Until(f func(), period time.Duration, stopCh <-chan struct{}) {
	ff := func() (bool, error) {
		f()
		return false, nil
	}
	BackoffUntil(ff, BackoffFunc(func(time.Duration, bool) time.Duration {
		return period
	}), true, stopCh)
}

func emptyOr(value, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	return value
}
