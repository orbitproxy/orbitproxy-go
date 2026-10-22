package orbitproxy

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
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

func TestDeletePolicyReturnsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/machine-api/tunnels/prx_1/policies/basic_auth/tprg_1" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"rule_group_id": "tprg_1", "deleted": true}})
	}))
	defer server.Close()
	client, err := NewMachineClient(MachineClientOptions{APIURL: server.URL, MachineKey: "mk_test", PrivateKeyPEM: testPrivateKeyPEM(t)})
	if err != nil {
		t.Fatal(err)
	}
	data, err := client.DeletePolicy(t.Context(), "prx_1", "basic_auth", "tprg_1")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Deleted bool `json:"deleted"`
	}
	if err := json.Unmarshal(data, &body); err != nil || !body.Deleted {
		t.Fatalf("body = %s", data)
	}
}

func testPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}
