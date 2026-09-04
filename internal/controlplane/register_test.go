package controlplane

import (
	"strings"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testCACert = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"

func TestRegisterSuccessEnvelope(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/machines/register" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.AuthToken != "tok_test" || req.MachineKey != "ck_test" || req.PublicKey == "" || req.Version != "0.1.0" {
			t.Fatalf("bad request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"edge": map[string]any{
					"addr":   "edge.example.com:443",
					"caCert": testCACert,
				},
			},
		})
	}))
	defer srv.Close()

	result, err := Register(context.Background(), RegisterOptions{
		APIURL:     srv.URL,
		AuthToken:  "tok_test",
		MachineKey:  "ck_test",
		PublicKey:  "-----BEGIN PUBLIC KEY-----\nMCowBQYDK2VwAyEA\n-----END PUBLIC KEY-----",
		HTTPClient: srv.Client(),
		Version:    "0.1.0",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.EdgeAddr != "edge.example.com:443" {
		t.Fatalf("result = %+v", result)
	}
	if strings.TrimSpace(result.CACert) != strings.TrimSpace(testCACert) {
		t.Fatalf("CACert = %q", result.CACert)
	}
}

func TestRegisterFlatEdgeAddr(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"edge_addr": "edge.example.com:443",
				"caCert":    testCACert,
			},
		})
	}))
	defer srv.Close()

	result, err := Register(context.Background(), RegisterOptions{
		APIURL:     srv.URL,
		AuthToken:  "tok_test",
		MachineKey:  "ck_test",
		PublicKey:  "pk",
		HTTPClient: srv.Client(),
		Version:    "1.2.3",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.EdgeAddr != "edge.example.com:443" {
		t.Fatalf("EdgeAddr = %q", result.EdgeAddr)
	}
	if strings.TrimSpace(result.CACert) != strings.TrimSpace(testCACert) {
		t.Fatalf("CACert = %q", result.CACert)
	}
}

func TestRegisterHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid authtoken"}`))
	}))
	defer srv.Close()

	_, err := Register(context.Background(), RegisterOptions{
		APIURL:     srv.URL,
		AuthToken:  "bad",
		MachineKey:  "ck_test",
		PublicKey:  "pk",
		HTTPClient: srv.Client(),
		Version:    "1.2.3",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterRequiresFields(t *testing.T) {
	t.Parallel()
	if _, err := Register(context.Background(), RegisterOptions{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := Register(context.Background(), RegisterOptions{
		APIURL:    "http://example",
		AuthToken: "tok",
	}); err == nil {
		t.Fatal("expected MachineKey required")
	}
	if _, err := Register(context.Background(), RegisterOptions{
		APIURL:     "http://example",
		AuthToken:  "tok",
		MachineKey: "ck",
		PublicKey:  "pk",
	}); err == nil {
		t.Fatal("expected version required")
	}
}
