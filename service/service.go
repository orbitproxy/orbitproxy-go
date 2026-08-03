package service

import (
	"strings"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"log/slog"

	"github.com/orbitproxy/orbitproxy-go/internal/endpoint"
	"github.com/orbitproxy/orbitproxy-go/internal/backoff"
	"github.com/orbitproxy/orbitproxy-go/internal/config"
	"github.com/orbitproxy/orbitproxy-go/internal/gateway_ctl"
	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
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
	EndpointID string
	ProxyID    string
	Type       string
	Protocol   string
	Delivery   string
	LocalAddr  string
}

// Service is the SDK runtime for one virtual machine connected to edge.
// Prefer orbitproxy.Start (or Connect); New is for same-module wiring.
type Service struct {
	cfg    config.Config
	logger *slog.Logger

	mu  sync.RWMutex
	ctl *gateway_ctl.Control

	ctx    context.Context
	cancel context.CancelFunc

	doneCh chan struct{}
	once   sync.Once
	errMu  sync.Mutex
	err    error
}

// New builds a Service. Call Run (or Start) after identity fields are filled.
func New(cfg config.Config) (*Service, error) {
	if cfg.ClientKey == "" {
		return nil, fmt.Errorf("client_key is required")
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

func (svr *Service) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	svr.ctx = runCtx
	svr.cancel = cancel

	if err := svr.loginWithBackoff(firstLoginMaxBackoff); err != nil {
		cancel()
		return err
	}

	svr.logger.Info("orbitproxy-go started",
		 "client_key", svr.cfg.ClientKey,
		"edge_addr", svr.cfg.EdgeAddr,
		"soft_version", svr.cfg.SoftVersion,
	)
	go svr.keepGatewayConnectionAlive()
	return nil
}

// ClientKey returns the bound client key for this SDK client.
func (svr *Service) ClientKey() string { return svr.cfg.ClientKey }

// Endpoints returns endpoints from the active control session's endpoint manager.
func (svr *Service) Endpoints() []EndpointStatus {
	mgr := svr.endpointMgr()
	if mgr == nil {
		return nil
	}
	snap := mgr.Snapshot()
	out := make([]EndpointStatus, 0, len(snap))
	for _, b := range snap {
		if b == nil {
			continue
		}
		out = append(out, EndpointStatus{
			EndpointID: b.EndpointID,
			ProxyID:    b.ProxyID,
			Type:       b.ProxyType,
			Protocol:   b.Protocol,
			Delivery:   b.Delivery,
			LocalAddr:  b.LocalAddr,
		})
	}
	return out
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
	return nil
}

func (svr *Service) keepGatewayConnectionAlive() {
	defer svr.finish()

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
				 "client_key", svr.cfg.ClientKey,
				"reason", reason,
			)
			svr.setErr(fmt.Errorf("permanently disconnected: %s", reason))
			return
		}

		if svr.ctx.Err() != nil {
			svr.setErr(svr.ctx.Err())
			return
		}

		svr.logger.Warn("control connection closed, reconnecting",
			 "client_key", svr.cfg.ClientKey,
			"edge_addr", svr.cfg.EdgeAddr,
		)

		if err := svr.loginWithBackoff(reconnectMaxBackoff); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				svr.setErr(err)
				return
			}
			svr.setErr(err)
			return
		}
	}
}

func (svr *Service) loginWithBackoff(maxInterval time.Duration) error {
	policy := backoff.NewExponentialBackOff()
	policy.MaxInterval = maxInterval

	return backoff.Loop(svr.ctx, policy, func(ctx context.Context) error {
		if err := svr.login(ctx); err != nil {
			svr.logger.Warn("connect to edge error",
				 "client_key", svr.cfg.ClientKey,
				"edge_addr", svr.cfg.EdgeAddr,
				"err", err,
			)
			return err
		}
		backoff.Reset(policy)
		svr.logger.Info("login to edge success",
			 "client_key", svr.cfg.ClientKey,
			"edge_addr", svr.cfg.EdgeAddr,
		)
		return nil
	}, func(err error, wait time.Duration) {
		if svr.ctx.Err() != nil {
			return
		}
		svr.logger.Warn("connect to edge failed, retrying",
			 "client_key", svr.cfg.ClientKey,
			"edge_addr", svr.cfg.EdgeAddr,
			"err", err,
			"retry_in", wait,
		)
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
		 "client_key", svr.cfg.ClientKey,
		"edge_id", sessionCtx.EdgeID,
		"session_id", sessionCtx.SessionID,
	)
	return nil
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
