package gateway_ctl

import (
	"net"
	"sync"

	"github.com/hashicorp/yamux"
)

// ConnConfig holds dial/auth parameters for one edge session.
type ConnConfig struct {
	EdgeAddr      string
	ClientKey     string
	PrivateKeyPEM string
	SoftVersion   string
}

// SessionContext is one edge control connection.
type SessionContext struct {
	ConnConfig    ConnConfig
	Yamux         *yamux.Session
	ControlStream net.Conn
	EdgeID        string
	SessionID     string
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
	edgeID    string
	sessionID string
}

func (state *sessionState) SetSession(edgeID, sessionID string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.edgeID = edgeID
	state.sessionID = sessionID
}

func (state *sessionState) SessionID() string {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.sessionID
}

func (state *sessionState) EdgeID() string {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.edgeID
}
