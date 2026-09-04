package endpoint

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"log/slog"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcpstdio"
	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

// Manager tracks endpoint runtimes and dispatches work connections.
type Manager struct {
	mu         sync.RWMutex
	byID       map[string]*Runtime
	ctx        context.Context
	sendHealth func(*wire.EndpointHealth) error
	notify     chan struct{}

	execOnce     sync.Once
	pool         *mcpstdio.Pool
	bridge       *mcpstdio.Bridge
	execLogger   *slog.Logger
	diagCallback atomic.Value // mcpstdio.DiagnosticCallback
}

// NewManager creates an empty manager.
func NewManager() *Manager {
	return &Manager{
		byID:   make(map[string]*Runtime),
		notify: make(chan struct{}, 1),
	}
}

// Notify is signaled when the endpoint set changes.
func (m *Manager) Notify() <-chan struct{} { return m.notify }

func (m *Manager) signal() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

// SetContext sets the context used for health monitors.
func (m *Manager) SetContext(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx = ctx
}

// SetSendHealth sets the health reporter used by runtimes.
func (m *Manager) SetSendHealth(fn func(*wire.EndpointHealth) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendHealth = fn
}

func (m *Manager) upsert(cfg *Config) {
	if cfg == nil || cfg.EndpointID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var rt *Runtime
	if existing, ok := m.byID[cfg.EndpointID]; ok {
		existing.Update(m.ctx, cfg, m.sendHealth)
		rt = existing
	} else {
		rt = NewRuntime(m.ctx, cfg, m.sendHealth)
		m.byID[cfg.EndpointID] = rt
	}
	m.syncExecLocked(rt, cfg)
}

func (m *Manager) delete(endpointID string) {
	if endpointID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pool != nil {
		m.pool.UnregisterEndpoint(endpointID)
	}
	if rt, ok := m.byID[endpointID]; ok {
		rt.SetBridge(nil)
		rt.Close()
		delete(m.byID, endpointID)
	}
}

// Get returns a runtime by endpoint ID.
func (m *Manager) Get(endpointID string) (*Runtime, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rt, ok := m.byID[endpointID]
	return rt, ok
}

// Len returns the number of registered endpoints.
func (m *Manager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byID)
}

// Snapshot returns a copy of current endpoint configs.
func (m *Manager) Snapshot() []*Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Config, 0, len(m.byID))
	for _, rt := range m.byID {
		if cfg := rt.Config(); cfg != nil {
			cp := *cfg
			out = append(out, &cp)
		}
	}
	return out
}

// HandleNewEndpoint upserts a runtime from a NewEndpoint message.
func (m *Manager) HandleNewEndpoint(logger *slog.Logger, in *wire.NewEndpoint) {
	if logger == nil {
		logger = sdklog.Nop()
	}
	if in == nil {
		return
	}
	if in.Error != "" {
		logger.Warn("new_endpoint rejected by edge",
			"endpoint_id", in.EndpointID,
			"proxy_id", in.ProxyID,
			"error", in.Error,
		)
		m.delete(in.EndpointID)
		m.signal()
		return
	}
	cfg := configFromNewEndpoint(in)
	m.upsert(cfg)
	m.signal()
	logger.Info("endpoint registered",
		"endpoint_id", cfg.EndpointID,
		"proxy_id", cfg.ProxyID,
		"type", cfg.ProxyType,
		"protocol", cfg.Protocol,
		"delivery", cfg.Delivery,
		"local_addr", cfg.LocalAddr,
		"health_enabled", cfg.HealthEnabled,
	)
}

// HandleCloseEndpoint removes an endpoint runtime.
func (m *Manager) HandleCloseEndpoint(logger *slog.Logger, in *wire.CloseEndpoint) {
	if logger == nil {
		logger = sdklog.Nop()
	}
	if in == nil || in.EndpointID == "" {
		return
	}
	m.delete(in.EndpointID)
	m.signal()
	logger.Info("endpoint removed",
		"endpoint_id", in.EndpointID,
		"proxy_id", in.ProxyID,
	)
}

// HandleWorkConn routes a work stream to the matching endpoint runtime.
// Returns true if the stream was retained (in-process Accept owns it).
func (m *Manager) HandleWorkConn(logger *slog.Logger, stream net.Conn, start *wire.StartWorkConn) bool {
	if logger == nil {
		logger = sdklog.Nop()
	}
	if start == nil {
		return false
	}
	if start.Error != "" {
		logger.Warn("start_work_conn rejected by edge",
			"proxy_id", start.ProxyID,
			"endpoint_id", start.EndpointID,
			"error", start.Error,
		)
		return false
	}

	rt, ok := m.Get(start.EndpointID)
	if !ok {
		logger.Warn("start_work_conn for unknown endpoint",
			"proxy_id", start.ProxyID,
			"endpoint_id", start.EndpointID,
		)
		return false
	}
	return rt.InWorkConn(logger, stream, start)
}

// ClaimListener claims the in-process net.Listener for endpointID.
func (m *Manager) ClaimListener(endpointID string) (net.Listener, error) {
	rt, ok := m.Get(endpointID)
	if !ok {
		return nil, ErrEndpointMissing
	}
	return rt.ClaimListener()
}

// FindInProcessEndpoint returns an unclaimed in-process endpoint id.
// If wantID is non-empty, only that id is considered.
func (m *Manager) FindInProcessEndpoint(wantID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, rt := range m.byID {
		if wantID != "" && id != wantID {
			continue
		}
		if rt.Delivery() != DeliveryInProcess {
			continue
		}
		rt.mu.RLock()
		claimed := rt.listener != nil
		rt.mu.RUnlock()
		if !claimed {
			return id, true
		}
	}
	return "", false
}
