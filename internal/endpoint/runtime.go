package endpoint

import (
	"context"
	"net"
	"sync"
	"time"

	"log/slog"

	"github.com/orbitproxy/orbitproxy-go/internal/health"
	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcpstdio"
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
	bridge     *mcpstdio.Bridge // exec 模式的桥接层，由外部注入
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

// SetBridge 注入 exec 模式的桥接层。
func (rt *Runtime) SetBridge(b *mcpstdio.Bridge) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.bridge = b
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

// ReportUnhealthy is a convenience wrapper for dial/passive failures with only a reason string.
func (rt *Runtime) ReportUnhealthy(reason string) {
	rt.MarkUnhealthy(health.Unhealthy("dial_failed", reason, "dial"))
}

// MarkUnhealthy applies a health observation (active probe or passive event) and reports EndpointHealth.
// Exec MCP process death and forward dial failures both land here — one health model.
func (rt *Runtime) MarkUnhealthy(obs health.Observation) {
	obs.Healthy = false
	if obs.ObservedAt.IsZero() {
		obs.ObservedAt = time.Now()
	}
	rt.reportHealth(obs)
}

// MarkHealthy reports the endpoint as healthy.
func (rt *Runtime) MarkHealthy(source string) {
	rt.reportHealth(health.HealthyObs(source))
}

func (rt *Runtime) reportHealth(obs health.Observation) {
	rt.mu.RLock()
	cfg := rt.cfg
	sendHealth := rt.sendHealth
	rt.mu.RUnlock()

	if cfg == nil || sendHealth == nil {
		return
	}
	// Active probe / dial updates respect HealthEnabled.
	// Passive process observations always report unhealthy: for exec MCP that
	// IS the health strategy (no ActiveProbe), even when the UI toggle is off.
	switch obs.Source {
	case "probe", "dial":
		if !cfg.HealthEnabled {
			return
		}
	default:
		if obs.Healthy && !cfg.HealthEnabled {
			return
		}
	}
	msg := &wire.EndpointHealth{
		EndpointID: cfg.EndpointID,
		ProxyID:    cfg.ProxyID,
		Healthy:    obs.Healthy,
		Ts:         obs.ObservedAt.Unix(),
	}
	if !obs.Healthy {
		msg.Reason = obs.ReasonText()
		msg.ErrorCode = obs.Code
	}
	_ = sendHealth(msg)
}

func (rt *Runtime) restartMonitorLocked() {
	rt.stopMonitorLocked()
	if rt.cfg == nil || !rt.cfg.HealthEnabled || rt.ctx == nil {
		return
	}

	// in_process 模式不做外部健康检查
	if rt.cfg.Delivery == DeliveryInProcess {
		return
	}

	// Active probe is optional. Exec MCP often has nil probe and relies on
	// passive MarkUnhealthy from process observation.
	probe := rt.selectProbeLocked()
	if probe == nil {
		return
	}

	cfg := rt.cfg
	rt.monitor = health.NewMonitor(
		rt.ctx,
		cfg.HealthIntervalSeconds,
		cfg.HealthTimeoutSeconds,
		cfg.HealthMaxFailed,
		probe,
		func() {
			rt.MarkHealthy("probe")
		},
		func(obs health.Observation) {
			rt.MarkUnhealthy(obs)
		},
	)
	rt.monitor.Start()
}

// selectProbeLocked picks an active health probe for this delivery mode.
// Caller holds rt.mu write lock. Nil means passive-only health.
func (rt *Runtime) selectProbeLocked() health.Probe {
	switch rt.cfg.Delivery {
	case DeliveryExec:
		// exec/stdio：被动监听子进程即可，不强制 ActiveProbe。
		return nil
	default:
		if rt.cfg.LocalAddr == "" {
			return nil
		}
		return &health.TCPProbe{Addr: rt.cfg.LocalAddr}
	}
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

	// exec 模式：通过 bridge 桥接到 stdio MCP server 子进程
	if cfg.Delivery == DeliveryExec {
		rt.mu.RLock()
		bridge := rt.bridge
		rt.mu.RUnlock()
		if bridge == nil {
			logger.Warn("exec bridge not initialized, dropping work conn",
				"proxy_id", start.ProxyID,
				"endpoint_id", start.EndpointID,
			)
			return false
		}
		bridge.HandleWorkConn(stream, cfg.EndpointID)
		return false
	}

	TCPJoinHandler{}.InWorkConn(logger, stream, start, rt)
	return false
}
