package yamuxcfg

import (
	"io"
	"time"

	"github.com/hashicorp/yamux"
)

// New returns the session config for machine↔edge mux.
// Application-layer Ping/Pong is not used for liveness; yamux keepalive is primary.
func New() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 30 * time.Second
	// Default 10s is too tight on proxy/TUN paths and caused session death
	// ("keepalive failed: i/o deadline reached") under brief stalls.
	cfg.ConnectionWriteTimeout = 30 * time.Second
	// Default 256KiB is too small for typical HTTP asset responses; the sender
	// fills the window and the stream is closed before WindowUpdates catch up,
	// producing truncated bodies (ERR_INCOMPLETE_CHUNKED_ENCODING).
	cfg.MaxStreamWindowSize = 6 * 1024 * 1024
	// Join teardown can leave late WindowUpdates; those library WARNs are benign.
	cfg.LogOutput = io.Discard
	return cfg
}

// Client opens a yamux client session with New().
func Client(conn io.ReadWriteCloser) (*yamux.Session, error) {
	return yamux.Client(conn, New())
}

// Server opens a yamux server session with New().
func Server(conn io.ReadWriteCloser) (*yamux.Session, error) {
	return yamux.Server(conn, New())
}
