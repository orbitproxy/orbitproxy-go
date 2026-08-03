package health

import (
	"context"
	"net"
	"time"
)

// Monitor performs periodic TCP dial health checks.
type Monitor struct {
	interval       time.Duration
	timeout        time.Duration
	maxFailedTimes int
	addr           string

	failedTimes    uint64
	statusOK       bool
	statusNormalFn func()
	statusFailedFn func()

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMonitor creates a TCP health monitor.
func NewMonitor(
	ctx context.Context,
	intervalSec, timeoutSec, maxFailed int,
	addr string,
	statusNormalFn func(),
	statusFailedFn func(),
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
		addr:           addr,
		statusOK:       false,
		statusNormalFn: statusNormalFn,
		statusFailedFn: statusFailedFn,
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
		err := monitor.doTCPCheck(doCtx)

		select {
		case <-monitor.ctx.Done():
			cancel()
			return
		default:
			cancel()
		}

		if err == nil {
			if !monitor.statusOK && monitor.statusNormalFn != nil {
				monitor.statusOK = true
				monitor.failedTimes = 0
				monitor.statusNormalFn()
			}
		} else {
			monitor.failedTimes++
			if monitor.statusOK && int(monitor.failedTimes) >= monitor.maxFailedTimes && monitor.statusFailedFn != nil {
				monitor.statusOK = false
				monitor.statusFailedFn()
			}
		}

		select {
		case <-monitor.ctx.Done():
			return
		case <-time.After(monitor.interval):
		}
	}
}

func (monitor *Monitor) doTCPCheck(ctx context.Context) error {
	if monitor.addr == "" {
		return nil
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", monitor.addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}
