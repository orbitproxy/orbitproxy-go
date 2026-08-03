package endpoint

import (
	"context"
	"net"
	"sync"
	"time"

	"log/slog"

	"github.com/orbitproxy/orbitproxy-go/internal/health"
	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

// Runtime is the local runtime for one endpoint.
type Runtime struct {
	mu sync.RWMutex

	ctx        context.Context
	cfg        *Config
	listener   *chanListener
	monitor    *health.Monitor
	sendHealth func(*wire.EndpointHealth) error
}

// NewRuntime creates a runtime and optionally starts health monitoring.
func NewRuntime(ctx context.Context, cfg *Config, sendHealth func(*wire.EndpointHealth) error) *Runtime {
	rt := &Runtime{
		ctx:        ctx,
		cfg:        cfg,
		sendHealth: sendHealth,
	}
	rt.restartMonitorLocked()
	return rt
}

// Config returns the current endpoint config snapshot.
func (rt *Runtime) Config() *Config {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.cfg
}

// Delivery returns the endpoint delivery mode.
func (rt *Runtime) Delivery() string {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	if rt.cfg == nil {
		return ""
	}
	return rt.cfg.Delivery
}

// Update replaces the endpoint config in place.
func (rt *Runtime) Update(ctx context.Context, cfg *Config, sendHealth func(*wire.EndpointHealth) error) {
	if cfg == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.ctx = ctx
	rt.cfg = cfg
	if sendHealth != nil {
		rt.sendHealth = sendHealth
	}
	rt.restartMonitorLocked()
}

// ClaimListener creates (or returns) the in-process net.Listener for this endpoint.
func (rt *Runtime) ClaimListener() (net.Listener, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cfg == nil {
		return nil, errNotReady
	}
	if rt.cfg.Delivery != DeliveryInProcess {
		return nil, errNotInProcess
	}
	if rt.listener != nil {
		return nil, errListenerClaimed
	}
	rt.listener = newChanListener(rt.cfg.EndpointID)
	return rt.listener, nil
}

// Close stops health monitoring and closes any claimed listener.
func (rt *Runtime) Close() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.stopMonitorLocked()
	if rt.listener != nil {
		_ = rt.listener.Close()
		rt.listener = nil
	}
}

// ReportUnhealthy pushes an unhealthy EndpointHealth if enabled.
func (rt *Runtime) ReportUnhealthy(reason string) {
	rt.mu.RLock()
	cfg := rt.cfg
	sendHealth := rt.sendHealth
	rt.mu.RUnlock()

	if cfg == nil || !cfg.HealthEnabled || sendHealth == nil {
		return
	}
	_ = sendHealth(&wire.EndpointHealth{
		EndpointID: cfg.EndpointID,
		ProxyID:    cfg.ProxyID,
		Healthy:    false,
		Reason:     reason,
		Ts:         time.Now().Unix(),
	})
}

func (rt *Runtime) restartMonitorLocked() {
	rt.stopMonitorLocked()
	if rt.cfg == nil || !rt.cfg.HealthEnabled || rt.ctx == nil {
		return
	}
	if rt.cfg.Delivery == DeliveryInProcess || rt.cfg.LocalAddr == "" {
		return
	}

	cfg := rt.cfg
	sendHealth := rt.sendHealth
	localAddr := cfg.LocalAddr
	rt.monitor = health.NewMonitor(
		rt.ctx,
		cfg.HealthIntervalSeconds,
		cfg.HealthTimeoutSeconds,
		cfg.HealthMaxFailed,
		localAddr,
		func() {
			if sendHealth == nil {
				return
			}
			_ = sendHealth(&wire.EndpointHealth{
				EndpointID: cfg.EndpointID,
				ProxyID:    cfg.ProxyID,
				Healthy:    true,
				Ts:         time.Now().Unix(),
			})
		},
		func() {
			if sendHealth == nil {
				return
			}
			_ = sendHealth(&wire.EndpointHealth{
				EndpointID: cfg.EndpointID,
				ProxyID:    cfg.ProxyID,
				Healthy:    false,
				Reason:     "health check failed",
				Ts:         time.Now().Unix(),
			})
		},
	)
	rt.monitor.Start()
}

func (rt *Runtime) stopMonitorLocked() {
	if rt.monitor != nil {
		rt.monitor.Stop()
		rt.monitor = nil
	}
}

// InWorkConn dispatches a work connection by delivery mode.
// Returns true when the stream is retained for in-process Accept.
func (rt *Runtime) InWorkConn(logger *slog.Logger, stream net.Conn, start *wire.StartWorkConn) bool {
	if logger == nil {
		logger = sdklog.Nop()
	}
	rt.mu.RLock()
	cfg := rt.cfg
	listener := rt.listener
	rt.mu.RUnlock()

	if cfg == nil {
		logger.Warn("endpoint runtime not ready",
			"proxy_id", start.ProxyID,
			"endpoint_id", start.EndpointID,
		)
		return false
	}
	if start.EndpointID != "" && start.EndpointID != cfg.EndpointID {
		logger.Warn("start_work_conn endpoint mismatch",
			"proxy_id", start.ProxyID,
			"endpoint_id", start.EndpointID,
			"config_endpoint_id", cfg.EndpointID,
		)
		return false
	}

	if cfg.Delivery == DeliveryInProcess {
		if listener == nil {
			logger.Warn("in-process endpoint not claimed via Listen, dropping work conn",
				"proxy_id", start.ProxyID,
				"endpoint_id", start.EndpointID,
			)
			return false
		}
		if !listener.Offer(stream) {
			logger.Warn("in-process Accept too slow, dropping work conn",
				"proxy_id", start.ProxyID,
				"endpoint_id", start.EndpointID,
			)
			return false
		}
		return true
	}

	TCPJoinHandler{}.InWorkConn(logger, stream, start, rt)
	return false
}
