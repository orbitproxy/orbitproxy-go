package orbitproxy_test

import (
	"testing"

	orbitproxy "github.com/orbitproxy/orbitproxy-go"
)

func TestVersionFromBuildInfo(t *testing.T) {
	t.Parallel()

	v := orbitproxy.Version()
	if v == "" {
		t.Fatal("Version() returned empty")
	}
	// In-module tests usually resolve to "(devel)" or "devel".
	t.Logf("Version() = %q", v)
}
