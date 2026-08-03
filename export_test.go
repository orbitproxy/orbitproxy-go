package orbitproxy

import (
	"testing"

	"github.com/orbitproxy/orbitproxy-go/service"
)

// EnableInsecureEdgeTLSForTest skips edge certificate verification for tests
// that dial a local self-signed mock edge.
func EnableInsecureEdgeTLSForTest(t *testing.T) {
	service.EnableInsecureTLSForTest(t)
}
