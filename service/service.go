package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/backoff"
	"github.com/orbitproxy/orbitproxy-go/internal/config"
	"github.com/orbitproxy/orbitproxy-go/internal/endpoint"
	"github.com/orbitproxy/orbitproxy-go/internal/gateway_ctl"
	"github.com/orbitproxy/orbitproxy-go/internal/proclife"
	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/internal/userenv"
)

const (
	firstLoginMaxBackoff = 10 * time.Second
	reconnectMaxBackoff  = 20 * time.Second
)

// Delivery values for EndpointStatus.Delivery.
const (
	DeliveryForward   = endpoint.DeliveryForward
	DeliveryInProcess = endpoint.DeliveryInProcess
)

// EndpointStatus is a snapshot of an edge-pushed endpoint.
type EndpointStatus struct {
	EndpointID    string
	ProxyID       string
	Type          string
	Protocol      string
	PubHost       string
	Delivery      string
	LocalAddr     string
	HealthEnabled bool
}

// Hooks are optional lifecycle callbacks. Nil fields are skipped.
type Hooks struct {
	OnConnected    func(sessionID string)
	OnEndpoints    func(endpoints []EndpointStatus)
	OnReconnecting func(attempt int, reason string)
	OnDisconnected func(reason string, permanent bool)
}

// Service is the SDK runtime for one virtual machine connected to edge.
// Prefer orbitproxy.Start (or Connect); New is for same-module wiring.
type Service struct {
	cfg    config.Config
	logger *slog.Logger
	hooks  Hooks

	mu  sync.RWMutex
	ctl *gateway_ctl.Control

	ctx    context.Context
	cancel context.CancelFunc

	doneCh chan struct{}
	once   sync.Once
	errMu  sync.Mutex
	err    error

	watchOnce sync.Once
	logCloser io.Closer
}

// New builds a Service. Call Run (or Start) after identity fields are filled.
func New(cfg config.Config) (*Service, error) {
	if cfg.MachineKey == "" {
		return nil, fmt.Errorf("machine_key is required")
	}
	if cfg.EdgeAddr == "" {
		return nil, fmt.Errorf("edge_addr is required")
	}
	if strings.TrimSpace(cfg.MachineCACert) == "" {
		return nil, fmt.Errorf("machine_ca_cert is required")
	}
	if cfg.PrivateKeyPEM == "" {
		return nil, fmt.Errorf("private_key_pem is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = sdklog.Nop()
	}
	return &Service{
		cfg:    cfg,
		logger: logger,
		doneCh: make(chan struct{}),
	}, nil
}

// SetHooks installs lifecycle callbacks. Call before Start.
func (svr *Service) SetHooks(h Hooks) {
	svr.hooks = h
}

// SetLogCloser registers a closer for the default file logger (owned by Start).
func (svr *Service) SetLogCloser(c io.Closer) {
	svr.logCloser = c
}

func (svr *Service) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	svr.ctx = runCtx
	svr.cancel = cancel

	proclife.RemoveStaleOldBinary()
	userenv.Capture()

	if err := svr.loginWithBackoff(firstLoginMaxBackoff, 0); err != nil {
		cancel()
		return err
	}

	svr.logger.Info("orbitproxy-go started",
		"machine_key", svr.cfg.MachineKey,
		"edge_addr", svr.cfg.EdgeAddr,
		"soft_version", svr.cfg.SoftVersion,
	)
	svr.startEndpointWatcher()
	go svr.keepGatewayConnectionAlive()
	return nil
}

// MachineKey returns the bound client key for this SDK client.
func (svr *Service) MachineKey() string { return svr.cfg.MachineKey }

// Endpoints returns endpoints from the active control session's endpoint manager.
func (svr *Service) Endpoints() []EndpointStatus {
	mgr := svr.endpointMgr()
	if mgr == nil {
		return nil
	}
	return snapshotEndpoints(mgr)
}

// Done is closed when the service permanently ends.
func (svr *Service) Done() <-chan struct{} { return svr.doneCh }

// Err returns the terminal error after Done closes.
func (svr *Service) Err() error {
	svr.errMu.Lock()
	defer svr.errMu.Unlock()
	return svr.err
}

// Close stops reconnect and tears down the control session.
func (svr *Service) Close() error {
	svr.setErr(nil)
	if svr.cancel != nil {
		svr.cancel()
	}
	svr.stopCtl()
	svr.finish()
	if svr.logCloser != nil {
		_ = svr.logCloser.Close()
		svr.logCloser = nil
	}
	return nil
}

func (svr *Service) keepGatewayConnectionAlive() {
	defer svr.finish()

	attempt := 0
	for {
		if svr.ctx.Err() != nil {
			svr.setErr(svr.ctx.Err())
			return
		}

		ctl := svr.getCtl()
		if ctl == nil {
			return
		}

		select {
		case <-svr.ctx.Done():
			svr.setErr(svr.ctx.Err())
			return
		case <-ctl.Done():
		}

		if ctl.PermanentlyDisconnected() {
			reason := ctl.DisconnectReason()
			svr.logger.Warn("edge session permanently closed, stop reconnecting",
				"machine_key", svr.cfg.MachineKey,
				"reason", reason,
			)
			svr.emitDisconnected(reason, true)
			svr.setErr(fmt.Errorf("permanently disconnected: %s", reason))
			return
		}

		if svr.ctx.Err() != nil {
			svr.setErr(svr.ctx.Err())
			return
		}

		attempt++
		reason := "control connection closed"
		svr.logger.Warn("control connection closed, reconnecting",
			"machine_key", svr.cfg.MachineKey,
			"edge_addr", svr.cfg.EdgeAddr,
		)
		svr.emitReconnecting(attempt, reason)

		if err := svr.loginWithBackoff(reconnectMaxBackoff, attempt); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				svr.setErr(err)
				return
			}
			if backoff.IsPermanent(err) {
				svr.emitDisconnected(err.Error(), true)
				svr.setErr(fmt.Errorf("permanently disconnected: %s", err))
				return
			}
			svr.emitDisconnected(err.Error(), false)
			svr.setErr(err)
			return
		}
		attempt = 0
	}
}

func (svr *Service) loginWithBackoff(maxInterval time.Duration, reconnectAttempt int) error {
	policy := backoff.NewExponentialBackOff()
	policy.MaxInterval = maxInterval
	attempt := reconnectAttempt

	return backoff.Loop(svr.ctx, policy, func(ctx context.Context) error {
		if err := svr.login(ctx); err != nil {
			svr.logger.Warn("connect to edge error",
				"machine_key", svr.cfg.MachineKey,
				"edge_addr", svr.cfg.EdgeAddr,
				"err", err,
			)
			return err
		}
		backoff.Reset(policy)
		svr.logger.Info("login to edge success",
			"machine_key", svr.cfg.MachineKey,
			"edge_addr", svr.cfg.EdgeAddr,
		)
		return nil
	}, func(err error, wait time.Duration) {
		if svr.ctx.Err() != nil {
			return
		}
		attempt++
		svr.logger.Warn("connect to edge failed, retrying",
			"machine_key", svr.cfg.MachineKey,
			"edge_addr", svr.cfg.EdgeAddr,
			"err", err,
			"retry_in", wait,
		)
		if reconnectAttempt > 0 || attempt > 1 {
			svr.emitReconnecting(attempt, err.Error())
		}
	})
}

func (svr *Service) login(ctx context.Context) error {
	sessionCtx, err := svr.dialGateway(ctx)
	if err != nil {
		return err
	}

	// Reuse endpoint manager across reconnects; first login lets Control create one.
	var mgr *endpoint.Manager
	if prev := svr.getCtl(); prev != nil {
		mgr = prev.EndpointManager()
	}

	ctl := gateway_ctl.NewControl(svr.ctx, sessionCtx, svr.logger, mgr)
	ctl.Run()

	svr.mu.Lock()
	if svr.ctl != nil {
		svr.ctl.Close()
	}
	svr.ctl = ctl
	svr.mu.Unlock()

	svr.logger.Info("edge session established",
		"machine_key", svr.cfg.MachineKey,
		"edge_id", sessionCtx.EdgeID,
		"session_id", sessionCtx.SessionID,
	)
	svr.emitConnected(sessionCtx.SessionID)
	svr.emitEndpointsSnapshot()
	return nil
}

func (svr *Service) startEndpointWatcher() {
	svr.watchOnce.Do(func() {
		go svr.watchEndpoints()
	})
}

func (svr *Service) watchEndpoints() {
	var lastNotify <-chan struct{}
	for {
		if svr.ctx.Err() != nil {
			return
		}
		mgr := svr.endpointMgr()
		if mgr == nil {
			select {
			case <-svr.ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		if lastNotify != mgr.Notify() {
			lastNotify = mgr.Notify()
			svr.emitEndpointsSnapshot()
		}
		select {
		case <-svr.ctx.Done():
			return
		case <-lastNotify:
			svr.emitEndpointsSnapshot()
		}
	}
}

func (svr *Service) emitConnected(sessionID string) {
	if svr.hooks.OnConnected != nil {
		svr.hooks.OnConnected(sessionID)
	}
}

func (svr *Service) emitReconnecting(attempt int, reason string) {
	if svr.hooks.OnReconnecting != nil {
		svr.hooks.OnReconnecting(attempt, reason)
	}
}

func (svr *Service) emitDisconnected(reason string, permanent bool) {
	if svr.hooks.OnDisconnected != nil {
		svr.hooks.OnDisconnected(reason, permanent)
	}
}

func (svr *Service) emitEndpointsSnapshot() {
	if svr.hooks.OnEndpoints == nil {
		return
	}
	mgr := svr.endpointMgr()
	if mgr == nil {
		svr.hooks.OnEndpoints(nil)
		return
	}
	svr.hooks.OnEndpoints(snapshotEndpoints(mgr))
}

func snapshotEndpoints(mgr *endpoint.Manager) []EndpointStatus {
	snap := mgr.Snapshot()
	out := make([]EndpointStatus, 0, len(snap))
	for _, b := range snap {
		if b == nil {
			continue
		}
		out = append(out, EndpointStatus{
			EndpointID:    b.EndpointID,
			ProxyID:       b.ProxyID,
			Type:          b.ProxyType,
			Protocol:      b.Protocol,
			PubHost:       b.PubHost,
			Delivery:      b.Delivery,
			LocalAddr:     b.LocalAddr,
			HealthEnabled: b.HealthEnabled,
		})
	}
	return out
}

func (svr *Service) getCtl() *gateway_ctl.Control {
	svr.mu.RLock()
	defer svr.mu.RUnlock()
	return svr.ctl
}

func (svr *Service) endpointMgr() *endpoint.Manager {
	ctl := svr.getCtl()
	if ctl == nil {
		return nil
	}
	return ctl.EndpointManager()
}

func (svr *Service) stopCtl() {
	svr.mu.Lock()
	defer svr.mu.Unlock()
	if svr.ctl != nil {
		if mgr := svr.ctl.EndpointManager(); mgr != nil {
			mgr.ShutdownExec()
		}
		svr.ctl.Close()
		svr.ctl = nil
	}
}

func (svr *Service) finish() {
	svr.once.Do(func() { close(svr.doneCh) })
}

func (svr *Service) setErr(err error) {
	svr.errMu.Lock()
	defer svr.errMu.Unlock()
	if svr.err == nil {
		svr.err = err
	}
}
