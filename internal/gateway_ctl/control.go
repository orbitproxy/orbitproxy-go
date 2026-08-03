package gateway_ctl

import (
	"context"
	"sync"
	"sync/atomic"

	"log/slog"

	"github.com/orbitproxy/orbitproxy-go/internal/endpoint"
	"github.com/orbitproxy/orbitproxy-go/internal/gateway_ctl/dispatcher"
	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

// Control is one edge control connection session.
type Control struct {
	ctx        context.Context
	sessionCtx *SessionContext
	session    *sessionState
	logger     *slog.Logger

	endpointMgr *endpoint.Manager

	msgDispatcher *dispatcher.Dispatcher

	doneCh chan struct{}
	once   sync.Once

	permanentDisconnect atomic.Bool
	disconnectReason    atomic.Value // string
}

// NewControl creates a Control for an already-dialed session (ServerHello already consumed).
func NewControl(ctx context.Context, sessionCtx *SessionContext, logger *slog.Logger, endpointMgr *endpoint.Manager) *Control {
	if logger == nil {
		logger = sdklog.Nop()
	}
	if endpointMgr == nil {
		endpointMgr = endpoint.NewManager()
	}
	ctl := &Control{
		ctx:           ctx,
		sessionCtx:    sessionCtx,
		session:       &sessionState{},
		logger:        logger,
		endpointMgr:    endpointMgr,
		msgDispatcher: dispatcher.NewDispatcher(sessionCtx.ControlStream),
		doneCh:        make(chan struct{}),
	}
	return ctl
}

// Run starts the control workers.
func (ctl *Control) Run() {
	go ctl.worker()
}

func (ctl *Control) worker() {
	defer ctl.finish()

	ctl.endpointMgr.SetContext(ctl.ctx)
	ctl.endpointMgr.SetSendHealth(func(h *wire.EndpointHealth) error {
		return ctl.msgDispatcher.Send(*h)
	})
	ctl.registerMsgHandlers()
	go ctl.msgDispatcher.Run()

	<-ctl.msgDispatcher.Done()
	if ctl.ctx.Err() == nil {
		ctl.logger.Debug("control message dispatcher exited")
	}
	ctl.closeSession()
}

// Done is closed when the control session ends.
func (ctl *Control) Done() <-chan struct{} {
	return ctl.doneCh
}

// Close closes the underlying session.
func (ctl *Control) Close() {
	ctl.closeSession()
}

func (ctl *Control) finish() {
	ctl.once.Do(func() { close(ctl.doneCh) })
}

func (ctl *Control) closeSession() {
	ctl.sessionCtx.Close()
}

// PermanentlyDisconnected reports whether edge asked us to stop reconnecting.
func (ctl *Control) PermanentlyDisconnected() bool {
	return ctl.permanentDisconnect.Load()
}

// DisconnectReason returns the permanent disconnect reason if any.
func (ctl *Control) DisconnectReason() string {
	reason, _ := ctl.disconnectReason.Load().(string)
	return reason
}

func (ctl *Control) markPermanentDisconnect(reason string) {
	ctl.permanentDisconnect.Store(true)
	ctl.disconnectReason.Store(reason)
}

// EndpointManager returns the endpoint manager.
func (ctl *Control) EndpointManager() *endpoint.Manager {
	return ctl.endpointMgr
}
