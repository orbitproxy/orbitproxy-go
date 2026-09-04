package health

import (
	"context"
	"time"
)

// Monitor runs optional ActiveProbe checks on an interval.
// Passive observations are applied by the endpoint Runtime via MarkUnhealthy /
// MarkHealthy — they do not need a Monitor, but when a Monitor exists it shares
// the same health callbacks.
type Monitor struct {
	interval       time.Duration
	timeout        time.Duration
	maxFailedTimes int
	probe          Probe

	failedTimes uint64
	statusOK    bool

	// Called when active probe transitions to healthy / unhealthy.
	onHealthy   func()
	onUnhealthy func(obs Observation)

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMonitor creates a probe-based health monitor.
// probe must be non-nil.
func NewMonitor(
	ctx context.Context,
	intervalSec, timeoutSec, maxFailed int,
	probe Probe,
	onHealthy func(),
	onUnhealthy func(obs Observation),
) *Monitor {
	if intervalSec <= 0 {
		intervalSec = 10
	}
	if timeoutSec <= 0 {
		timeoutSec = 3
	}
	if maxFailed <= 0 {
		maxFailed = 1
	}
	newctx, cancel := context.WithCancel(ctx)

	return &Monitor{
		interval:       time.Duration(intervalSec) * time.Second,
		timeout:        time.Duration(timeoutSec) * time.Second,
		maxFailedTimes: maxFailed,
		probe:          probe,
		statusOK:       false,
		onHealthy:      onHealthy,
		onUnhealthy:    onUnhealthy,
		ctx:            newctx,
		cancel:         cancel,
	}
}

// Start begins the check loop.
func (monitor *Monitor) Start() {
	go monitor.checkWorker()
}

// Stop cancels the check loop.
func (monitor *Monitor) Stop() {
	monitor.cancel()
}

func (monitor *Monitor) checkWorker() {
	for {
		doCtx, cancel := context.WithDeadline(monitor.ctx, time.Now().Add(monitor.timeout))
		err := monitor.probe.Check(doCtx)

		select {
		case <-monitor.ctx.Done():
			cancel()
			return
		default:
			cancel()
		}

		if err == nil {
			if !monitor.statusOK && monitor.onHealthy != nil {
				monitor.statusOK = true
				monitor.failedTimes = 0
				monitor.onHealthy()
			}
		} else {
			monitor.failedTimes++
			if monitor.statusOK && int(monitor.failedTimes) >= monitor.maxFailedTimes && monitor.onUnhealthy != nil {
				monitor.statusOK = false
				probeName := "probe"
				if monitor.probe != nil {
					probeName = monitor.probe.Name()
				}
				monitor.onUnhealthy(Observation{
					Healthy:    false,
					Code:       "probe_failed",
					Message:    err.Error(),
					Source:     probeName,
					ObservedAt: time.Now(),
				})
			}
		}

		select {
		case <-monitor.ctx.Done():
			return
		case <-time.After(monitor.interval):
		}
	}
}
