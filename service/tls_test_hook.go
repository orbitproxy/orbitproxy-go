package service

import (
	"sync"
	"testing"
)

var (
	insecureMu   sync.Mutex
	insecureRefs int
)

// EnableInsecureTLSForTest skips edge cert verification while t is running.
// Used by SDK integration tests against a local self-signed mock edge.
func EnableInsecureTLSForTest(t *testing.T) {
	t.Helper()
	insecureMu.Lock()
	insecureRefs++
	insecureMu.Unlock()
	t.Cleanup(func() {
		insecureMu.Lock()
		insecureRefs--
		insecureMu.Unlock()
	})
}

func insecureSkipVerifyEnabled() bool {
	insecureMu.Lock()
	defer insecureMu.Unlock()
	return insecureRefs > 0
}
