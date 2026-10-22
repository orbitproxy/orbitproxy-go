package orbitproxy

import (
	"crypto/ed25519"
	"net/url"
	"testing"
)

func TestMachineSignatureMatchesControlPlaneVector(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	key := ed25519.NewKeyFromSeed(seed)
	body := []byte(`{"protocol":"https","local_port":3000}`)
	got := signMachine(key, "POST", "/v1/machine-api/tunnels", url.Values{"b": {"2"}, "a": {"1"}}, body, "1760000000", "bm9uY2U", "mk_vector")
	const want = "vMv37nJ1XFGlT3ag5BRFhRO3UwoMJN/aL9QeQoNUKwEKplFtKmwkQaxFxshBs4rZZ+lDGBa3vJldUNuS3snBAA=="
	if got != want {
		t.Fatalf("signature = %s", got)
	}
}
