package endpoint

import (
	"log/slog"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcpstdio"
	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
)

// SetExecDiagCallback updates the diagnostic sink used by the shared exec pool.
// Call on every control session start so reconnects do not keep a dead Control callback.
func (m *Manager) SetExecDiagCallback(cb mcpstdio.DiagnosticCallback) {
	m.diagCallback.Store(cb)
}

// EnsureExecBridge initializes the shared execbridge Pool/Bridge once.
// Safe to call repeatedly; the pool is reused across edge reconnects.
func (m *Manager) EnsureExecBridge(cfg mcpstdio.PoolConfig, logger *slog.Logger) {
	m.execOnce.Do(func() {
		if logger == nil {
			logger = sdklog.Nop()
		}
		if cfg.Logger == nil {
			cfg.Logger = logger
		}
		// Always route diagnostics through the live callback (updated per Control session).
		cfg.DiagCallback = func(diag mcpstdio.Diagnostic) {
			if v := m.diagCallback.Load(); v != nil {
				if cb, ok := v.(mcpstdio.DiagnosticCallback); ok && cb != nil {
					cb(diag)
				}
			}
		}
		if cfg.PIDFile != "" {
			cleaned, errs := mcpstdio.CleanOrphans(cfg.PIDFile)
			if cleaned > 0 {
				logger.Info("cleaned orphan execbridge processes", "count", cleaned)
			}
			for _, err := range errs {
				logger.Warn("orphan cleanup error", "err", err)
			}
		}
		pool := mcpstdio.NewPool(cfg)
		bridge := mcpstdio.NewBridge(pool, logger)
		m.mu.Lock()
		m.pool = pool
		m.bridge = bridge
		m.execLogger = logger
		m.mu.Unlock()
	})
}

// ShutdownExec closes all exec sessions. Call on machine shutdown.
func (m *Manager) ShutdownExec() {
	m.mu.Lock()
	pool := m.pool
	m.mu.Unlock()
	if pool != nil {
		pool.CloseAll()
	}
}

func (m *Manager) syncExecLocked(rt *Runtime, cfg *Config) {
	if m.pool == nil || m.bridge == nil || cfg == nil || rt == nil {
		return
	}
	if cfg.Delivery != DeliveryExec {
		m.pool.UnregisterEndpoint(cfg.EndpointID)
		rt.SetBridge(nil)
		return
	}
	spawnCfg, err := mcpstdio.ParseExecPayload(cfg.LocalServicePayload)
	if err != nil {
		if m.execLogger != nil {
			m.execLogger.Warn("exec payload parse failed",
				"endpoint_id", cfg.EndpointID,
				"err", err,
			)
		}
		m.pool.UnregisterEndpoint(cfg.EndpointID)
		rt.SetBridge(nil)
		return
	}
	m.pool.RegisterEndpoint(cfg.EndpointID, mcpstdio.SessionConfig{
		SpawnConfig: *spawnCfg,
		EndpointID:  cfg.EndpointID,
	})
	rt.SetBridge(m.bridge)
}
