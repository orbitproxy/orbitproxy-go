package gateway_ctl

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// DialEdge opens one TCP+TLS+yamux client session to edge.
type DialEdge func(ctx context.Context) (net.Conn, *yamux.Session, error)

// ConnConfig holds dial/auth parameters for one edge session.
type ConnConfig struct {
	EdgeAddr      string
	MachineKey    string
	PrivateKeyPEM string
	SoftVersion   string
	DataRoot      string
}

// SessionContext is one edge control connection.
type SessionContext struct {
	ConnConfig    ConnConfig
	Yamux         *yamux.Session
	ControlStream net.Conn
	EdgeID        string
	SessionID     string
	// DataSessions 来自 ServerHello。0 或字段缺失时不拨数据会话。
	DataSessions int
	// DialEdge 拨一条数据会话。为 nil 时不拨。
	DialEdge DialEdge
	// RedialInterval 缩短测试里的重拨等待。0 用默认 backoff。
	RedialInterval time.Duration
}

// Close tears down the control stream and yamux session.
func (session *SessionContext) Close() {
	if session.ControlStream != nil {
		_ = session.ControlStream.Close()
	}
	if session.Yamux != nil {
		_ = session.Yamux.Close()
	}
}

type sessionState struct {
	mu        sync.RWMutex
	sessionID string
}

func (state *sessionState) SetSession(sessionID string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.sessionID = sessionID
}

func (state *sessionState) SessionID() string {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.sessionID
}
