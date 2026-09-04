package orbitproxy_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	orbitproxy "github.com/orbitproxy/orbitproxy-go"
	"github.com/orbitproxy/orbitproxy-go/internal/utils"
	"github.com/orbitproxy/orbitproxy-go/service"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

type mockEdge struct {
	t            *testing.T
	ln           net.Listener
	publicKeyPEM string
	mu           sync.Mutex
	sessions     int

	onSession func(control net.Conn, session *yamux.Session)
}

func testEdgeTLSCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "edge-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func listenTLSEdge(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{testEdgeTLSCert(t)}})
}

func startMockEdge(t *testing.T, publicKeyPEM string, onSession func(control net.Conn, session *yamux.Session)) *mockEdge {
	t.Helper()
	orbitproxy.EnableInsecureEdgeTLSForTest(t)
	ln := listenTLSEdge(t)
	m := &mockEdge{t: t, ln: ln, publicKeyPEM: publicKeyPEM, onSession: onSession}
	go m.serve()
	return m
}

func (m *mockEdge) Addr() string { return m.ln.Addr().String() }

func (m *mockEdge) Close() { _ = m.ln.Close() }

func (m *mockEdge) serve() {
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			return
		}
		go m.handleConn(conn)
	}
}

func (m *mockEdge) handleConn(conn net.Conn) {
	session, err := yamux.Server(conn, nil)
	if err != nil {
		_ = conn.Close()
		return
	}
	control, err := session.Accept()
	if err != nil {
		_ = session.Close()
		return
	}

	msg, err := wire.ReadMsg(control)
	if err != nil {
		_ = control.Close()
		_ = session.Close()
		return
	}
	hello, ok := msg.(*wire.ClientHello)
	if !ok {
		_ = control.Close()
		_ = session.Close()
		return
	}

	canonical := wire.ClientHelloCanonicalString(
		hello.MachineKey, hello.Timestamp, hello.Nonce, hello.SoftVersion,
	)
	if err := wire.VerifyClientHelloSignature(m.publicKeyPEM, hello.AuthSignature, canonical); err != nil {
		_ = wire.WriteMsg(control, wire.Disconnect{Reason: "auth_failed", ReasonText: err.Error()})
		_ = control.Close()
		_ = session.Close()
		return
	}

	m.mu.Lock()
	m.sessions++
	m.mu.Unlock()

	if err := wire.WriteMsg(control, wire.ServerHello{
		EdgeID:    "edge_test",
		SessionID: "sess_test",
	}); err != nil {
		_ = control.Close()
		_ = session.Close()
		return
	}

	if m.onSession != nil {
		m.onSession(control, session)
		return
	}

	// Keep control open until client closes / test ends.
	_, _ = io.Copy(io.Discard, control)
	_ = control.Close()
	_ = session.Close()
}

func startRegisterServer(t *testing.T, edgeAddr string, capturePub *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/machines/register" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			AuthToken string `json:"authtoken"`
			MachineKey string `json:"machineKey"`
			PublicKey string `json:"publicKey"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if body.AuthToken == "" || body.MachineKey == "" {
			http.Error(w, "authtoken and machineKey required", 400)
			return
		}
		if capturePub != nil {
			*capturePub = body.PublicKey
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"edge": map[string]any{"addr": edgeAddr, "caCert": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"},
			},
		})
	}))
}

func testConnectOpts(api *httptest.Server) orbitproxy.Options {
	return orbitproxy.Options{
		AuthToken:  "tok_test",
		MachineKey:  "ck_test",
		APIURL:     api.URL,
		HTTPClient: api.Client(),
		Version:    "1.2.3",
	}
}

func TestConnectFailFastBadKey(t *testing.T) {
	t.Parallel()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`invalid sdk key`))
	}))
	defer api.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := testConnectOpts(api)
	opts.AuthToken = "tok_bad"
	_, err := orbitproxy.Connect(ctx, opts)
	if err == nil {
		t.Fatal("expected register failure")
	}
}

func TestConnectAuthRejectRetriesUntilContextDone(t *testing.T) {
	t.Parallel()

	// Edge verifies against a different key than the client signs with.
	// Start retries with backoff; Connect only fails when ctx ends.
	wrongPub, _, err := utils.GenerateEd25519KeyPairPEM()
	if err != nil {
		t.Fatal(err)
	}
	edge := startMockEdge(t, wrongPub, nil)
	defer edge.Close()

	var clientPub string
	api := startRegisterServer(t, edge.Addr(), &clientPub)
	defer api.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = orbitproxy.Connect(ctx, testConnectOpts(api))
	if err == nil {
		t.Fatal("expected connect failure after context deadline")
	}
}

func TestConnectSuccessEndpointsAndClose(t *testing.T) {
	t.Parallel()

	var publicKeyPEM string
	ready := make(chan struct{})
	var once sync.Once

	// Start edge with a placeholder; swap key after register by using a channel.
	// Simpler: register server stores pub, edge accepts any valid signature by
	// verifying with the captured public key from register.
	orbitproxy.EnableInsecureEdgeTLSForTest(t)
	edgeLn := listenTLSEdge(t)
	defer edgeLn.Close()

	pubCh := make(chan string, 1)
	go func() {
		for {
			conn, err := edgeLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				session, err := yamux.Server(conn, nil)
				if err != nil {
					_ = conn.Close()
					return
				}
				control, err := session.Accept()
				if err != nil {
					_ = session.Close()
					return
				}
				msg, err := wire.ReadMsg(control)
				if err != nil {
					return
				}
				hello := msg.(*wire.ClientHello)
				pub := <-pubCh
				canonical := wire.ClientHelloCanonicalString(
					hello.MachineKey, hello.Timestamp, hello.Nonce, hello.SoftVersion,
				)
				if err := wire.VerifyClientHelloSignature(pub, hello.AuthSignature, canonical); err != nil {
					_ = wire.WriteMsg(control, wire.Disconnect{Reason: "auth_failed"})
					return
				}
				_ = wire.WriteMsg(control, wire.ServerHello{EdgeID: "edge_1", SessionID: "sess_1"})
				once.Do(func() { close(ready) })
				_, _ = io.Copy(io.Discard, control)
			}(conn)
		}
	}()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PublicKey string `json:"publicKey"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		publicKeyPEM = body.PublicKey
		pubCh <- publicKeyPEM
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"edge": map[string]any{"addr": edgeLn.Addr().String(), "caCert": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"},
			},
		})
	}))
	defer api.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc, err := orbitproxy.Connect(ctx, testConnectOpts(api))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer svc.Close()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("edge did not accept session")
	}

	if svc.MachineKey() != "ck_test" {
		t.Fatalf("MachineKey = %q", svc.MachineKey())
	}
	// Endpoints come from edge NewEndpoint, not register.
	if len(svc.Endpoints()) != 0 {
		t.Fatalf("Endpoints = %+v, want empty before NewEndpoint", svc.Endpoints())
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-svc.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed")
	}
}

func TestConnectWorkConnForward(t *testing.T) {
	t.Parallel()

	localLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer localLn.Close()

	go func() {
		for {
			c, err := localLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				_, _ = c.Write(append([]byte("echo:"), buf[:n]...))
			}(c)
		}
	}()

	pubCh := make(chan string, 1)
	orbitproxy.EnableInsecureEdgeTLSForTest(t)
	edgeLn := listenTLSEdge(t)
	defer edgeLn.Close()

	workDone := make(chan string, 1)
	go func() {
		conn, err := edgeLn.Accept()
		if err != nil {
			return
		}
		session, err := yamux.Server(conn, nil)
		if err != nil {
			return
		}
		control, err := session.Accept()
		if err != nil {
			return
		}
		msg, _ := wire.ReadMsg(control)
		hello := msg.(*wire.ClientHello)
		pub := <-pubCh
		canonical := wire.ClientHelloCanonicalString(
			hello.MachineKey, hello.Timestamp, hello.Nonce, hello.SoftVersion,
		)
		if err := wire.VerifyClientHelloSignature(pub, hello.AuthSignature, canonical); err != nil {
			t.Errorf("verify: %v", err)
			return
		}
		_ = wire.WriteMsg(control, wire.ServerHello{EdgeID: "edge_1", SessionID: "sess_1"})

		_ = wire.WriteMsg(control, wire.NewEndpoint{
			EndpointID:          "ep_1",
			ProxyID:             "px_1",
			ProxyType:           "basic",
			Protocol:            "https",
			LocalServicePayload: json.RawMessage(`{"localAddr":"` + localLn.Addr().String() + `"}`),
		})
		time.Sleep(50 * time.Millisecond)

		// Edge 直接打开 yamux stream 并发送 StartWorkConn
		work, err := session.Open()
		if err != nil {
			t.Errorf("open work stream: %v", err)
			return
		}
		_ = wire.WriteMsg(work, wire.StartWorkConn{
			ProxyID:    "px_1",
			EndpointID: "ep_1",
			SourceAddr: "1.2.3.4:5",
		})

		_, _ = work.Write([]byte("ping"))
		buf := make([]byte, 64)
		n, err := work.Read(buf)
		if err != nil {
			t.Errorf("read work: %v", err)
			return
		}
		workDone <- string(buf[:n])
		_ = work.Close()
		_, _ = io.Copy(io.Discard, control)
	}()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PublicKey string `json:"publicKey"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		pubCh <- body.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"edge": map[string]any{"addr": edgeLn.Addr().String(), "caCert": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"},
			},
		})
	}))
	defer api.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc, err := orbitproxy.Connect(ctx, testConnectOpts(api))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer svc.Close()

	select {
	case got := <-workDone:
		if got != "echo:ping" {
			t.Fatalf("forwarded = %q, want echo:ping", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("work conn timeout")
	}

	eps := svc.Endpoints()
	if len(eps) != 1 || eps[0].EndpointID != "ep_1" || eps[0].LocalAddr != localLn.Addr().String() {
		t.Fatalf("Endpoints after NewEndpoint = %+v", eps)
	}
	if eps[0].Delivery != service.DeliveryForward {
		t.Fatalf("Delivery = %q, want forward", eps[0].Delivery)
	}
}

func TestConnectListenMode(t *testing.T) {
	t.Parallel()

	pubCh := make(chan string, 1)
	orbitproxy.EnableInsecureEdgeTLSForTest(t)
	edgeLn := listenTLSEdge(t)
	defer edgeLn.Close()

	workDone := make(chan string, 1)
	go func() {
		conn, err := edgeLn.Accept()
		if err != nil {
			return
		}
		session, err := yamux.Server(conn, nil)
		if err != nil {
			return
		}
		control, err := session.Accept()
		if err != nil {
			return
		}
		msg, _ := wire.ReadMsg(control)
		hello := msg.(*wire.ClientHello)
		pub := <-pubCh
		canonical := wire.ClientHelloCanonicalString(
			hello.MachineKey, hello.Timestamp, hello.Nonce, hello.SoftVersion,
		)
		if err := wire.VerifyClientHelloSignature(pub, hello.AuthSignature, canonical); err != nil {
			t.Errorf("verify: %v", err)
			return
		}
		_ = wire.WriteMsg(control, wire.ServerHello{EdgeID: "edge_1", SessionID: "sess_1"})
		_ = wire.WriteMsg(control, wire.NewEndpoint{
			EndpointID:          "ep_listen",
			ProxyID:             "px_1",
			ProxyType:           "basic",
			Protocol:            "https",
			LocalServicePayload: json.RawMessage(`{"delivery":"in_process"}`),
		})
		time.Sleep(500 * time.Millisecond)

		// Edge 直接打开 yamux stream 并发送 StartWorkConn
		work, err := session.Open()
		if err != nil {
			t.Errorf("open work stream: %v", err)
			return
		}
		_ = wire.WriteMsg(work, wire.StartWorkConn{
			ProxyID:    "px_1",
			EndpointID: "ep_listen",
		})
		_, _ = work.Write([]byte("via-listen"))
		buf := make([]byte, 64)
		n, err := work.Read(buf)
		if err != nil {
			t.Errorf("read work: %v", err)
			return
		}
		workDone <- string(buf[:n])
		_ = work.Close()
		_, _ = io.Copy(io.Discard, control)
	}()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PublicKey string `json:"publicKey"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		pubCh <- body.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"edge": map[string]any{"addr": edgeLn.Addr().String(), "caCert": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"},
			},
		})
	}))
	defer api.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc, err := orbitproxy.Connect(ctx, testConnectOpts(api))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer svc.Close()

	listenCtx, listenCancel := context.WithTimeout(ctx, 3*time.Second)
	defer listenCancel()
	ln, err := svc.Listen(listenCtx, service.WithEndpointID("ep_listen"))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		n, err := c.Read(buf)
		if err != nil {
			t.Errorf("handler read: %v", err)
			return
		}
		_, _ = c.Write(append([]byte("ok:"), buf[:n]...))
	}()

	select {
	case got := <-workDone:
		if got != "ok:via-listen" {
			t.Fatalf("got %q, want ok:via-listen", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listen work conn timeout")
	}

	eps := svc.Endpoints()
	if len(eps) != 1 || eps[0].Delivery != service.DeliveryInProcess {
		t.Fatalf("Endpoints = %+v", eps)
	}
}

func TestOptionsValidation(t *testing.T) {
	t.Parallel()
	_, err := orbitproxy.Connect(context.Background(), orbitproxy.Options{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterAndStart(t *testing.T) {
	t.Parallel()

	pubCh := make(chan string, 1)
	orbitproxy.EnableInsecureEdgeTLSForTest(t)
	edgeLn := listenTLSEdge(t)
	defer edgeLn.Close()

	ready := make(chan struct{})
	var once sync.Once
	go func() {
		conn, err := edgeLn.Accept()
		if err != nil {
			return
		}
		session, err := yamux.Server(conn, nil)
		if err != nil {
			return
		}
		control, err := session.Accept()
		if err != nil {
			return
		}
		msg, err := wire.ReadMsg(control)
		if err != nil {
			return
		}
		hello := msg.(*wire.ClientHello)
		pub := <-pubCh
		canonical := wire.ClientHelloCanonicalString(
			hello.MachineKey, hello.Timestamp, hello.Nonce, hello.SoftVersion,
		)
		if err := wire.VerifyClientHelloSignature(pub, hello.AuthSignature, canonical); err != nil {
			t.Errorf("verify: %v", err)
			return
		}
		_ = wire.WriteMsg(control, wire.ServerHello{EdgeID: "edge_1", SessionID: "sess_1"})
		once.Do(func() { close(ready) })
		_, _ = io.Copy(io.Discard, control)
	}()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MachineKey string `json:"machineKey"`
			PublicKey string `json:"publicKey"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.MachineKey != "ck_test" {
			t.Errorf("machineKey = %q", body.MachineKey)
		}
		pubCh <- body.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"edge": map[string]any{"addr": edgeLn.Addr().String(), "caCert": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"},
			},
		})
	}))
	defer api.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	id, err := orbitproxy.Register(ctx, orbitproxy.RegisterOptions{
		AuthToken:  "tok_test",
		MachineKey:  "ck_test",
		APIURL:     api.URL,
		HTTPClient: api.Client(),
		Version:    "1.2.3",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if id.MachineKey != "ck_test" || id.EdgeAddr == "" || id.PrivateKeyPEM == "" {
		t.Fatalf("identity = %+v", id)
	}

	svc, err := orbitproxy.Start(ctx, *id, orbitproxy.StartOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Close()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("edge did not accept session")
	}
	if svc.MachineKey() != "ck_test" {
		t.Fatalf("MachineKey = %q", svc.MachineKey())
	}
}

func TestStartWithCLIIdentity(t *testing.T) {
	t.Parallel()

	pub, priv, err := utils.GenerateEd25519KeyPairPEM()
	if err != nil {
		t.Fatal(err)
	}
	edge := startMockEdge(t, pub, nil)
	defer edge.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// CLI path: own register already done; Start with assembled Identity.
	svc, err := orbitproxy.Start(ctx, orbitproxy.Identity{
		MachineKey:     "ck_cli",
		EdgeAddr:      edge.Addr(),
		MachineCACert: "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
		PrivateKeyPEM: priv,
	}, orbitproxy.StartOptions{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Close()
	if svc.MachineKey() != "ck_cli" {
		t.Fatalf("MachineKey = %q", svc.MachineKey())
	}
}

func TestStartRequiresIdentity(t *testing.T) {
	t.Parallel()
	_, err := orbitproxy.Start(context.Background(), orbitproxy.Identity{}, orbitproxy.StartOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestConnectDiscoverToolsOnNewEndpoint(t *testing.T) {
	t.Parallel()

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		switch payload["method"] {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "sess-mcp")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"mock","version":"1"}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"echo","description":"Echo"}]}}`))
		default:
			http.Error(w, "bad method", 400)
		}
	}))
	defer mcp.Close()

	pubCh := make(chan string, 1)
	resultCh := make(chan *wire.DiscoverToolsResult, 1)
	orbitproxy.EnableInsecureEdgeTLSForTest(t)
	edgeLn := listenTLSEdge(t)
	defer edgeLn.Close()

	go func() {
		conn, err := edgeLn.Accept()
		if err != nil {
			return
		}
		session, err := yamux.Server(conn, nil)
		if err != nil {
			return
		}
		control, err := session.Accept()
		if err != nil {
			return
		}
		msg, _ := wire.ReadMsg(control)
		hello := msg.(*wire.ClientHello)
		pub := <-pubCh
		canonical := wire.ClientHelloCanonicalString(
			hello.MachineKey, hello.Timestamp, hello.Nonce, hello.SoftVersion,
		)
		if err := wire.VerifyClientHelloSignature(pub, hello.AuthSignature, canonical); err != nil {
			t.Errorf("verify: %v", err)
			return
		}
		_ = wire.WriteMsg(control, wire.ServerHello{EdgeID: "edge_1", SessionID: "sess_1"})
		_ = wire.WriteMsg(control, wire.NewEndpoint{
			EndpointID:          "ep_mcp",
			ProxyID:             "px_mcp",
			ProxyType:           "mcp",
			Protocol:            "https",
			LocalServicePayload: json.RawMessage(`{"localAddr":"` + mcp.Listener.Addr().String() + `","localPath":"/"}`),
			DiscoverTools: &wire.DiscoverToolsOptions{
				RequestID: "mer_1",
			},
		})
		for {
			in, err := wire.ReadMsg(control)
			if err != nil {
				return
			}
			if res, ok := in.(*wire.DiscoverToolsResult); ok {
				resultCh <- res
				return
			}
		}
	}()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PublicKey string `json:"publicKey"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		pubCh <- body.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"edge": map[string]any{"addr": edgeLn.Addr().String(), "caCert": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"},
			},
		})
	}))
	defer api.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc, err := orbitproxy.Connect(ctx, testConnectOpts(api))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer svc.Close()

	select {
	case res := <-resultCh:
		if res.RequestID != "mer_1" || res.Status != "succeeded" {
			t.Fatalf("response = %+v", res)
		}
		if len(res.Tools) != 1 || res.Tools[0].Name != "echo" {
			t.Fatalf("tools = %+v", res.Tools)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("discover_tools_result timeout")
	}
}

func TestConnectDiscoverToolsStandalone(t *testing.T) {
	t.Parallel()

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		switch payload["method"] {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "sess-mcp")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"mock","version":"1"}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping"}]}}`))
		default:
			http.Error(w, "bad method", 400)
		}
	}))
	defer mcp.Close()

	pubCh := make(chan string, 1)
	resultCh := make(chan *wire.DiscoverToolsResult, 1)
	orbitproxy.EnableInsecureEdgeTLSForTest(t)
	edgeLn := listenTLSEdge(t)
	defer edgeLn.Close()

	go func() {
		conn, err := edgeLn.Accept()
		if err != nil {
			return
		}
		session, err := yamux.Server(conn, nil)
		if err != nil {
			return
		}
		control, err := session.Accept()
		if err != nil {
			return
		}
		msg, _ := wire.ReadMsg(control)
		hello := msg.(*wire.ClientHello)
		pub := <-pubCh
		canonical := wire.ClientHelloCanonicalString(
			hello.MachineKey, hello.Timestamp, hello.Nonce, hello.SoftVersion,
		)
		if err := wire.VerifyClientHelloSignature(pub, hello.AuthSignature, canonical); err != nil {
			t.Errorf("verify: %v", err)
			return
		}
		_ = wire.WriteMsg(control, wire.ServerHello{EdgeID: "edge_1", SessionID: "sess_1"})
		_ = wire.WriteMsg(control, wire.NewEndpoint{
			EndpointID:          "ep_mcp",
			ProxyID:             "px_mcp",
			ProxyType:           "mcp",
			Protocol:            "https",
			LocalServicePayload: json.RawMessage(`{"localAddr":"` + mcp.Listener.Addr().String() + `","localPath":"/"}`),
		})
		_ = wire.WriteMsg(control, wire.DiscoverTools{
			RequestID:  "mer_standalone",
			EndpointID: "ep_mcp",
		})
		for {
			in, err := wire.ReadMsg(control)
			if err != nil {
				return
			}
			if res, ok := in.(*wire.DiscoverToolsResult); ok {
				resultCh <- res
				return
			}
		}
	}()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PublicKey string `json:"publicKey"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		pubCh <- body.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"edge": map[string]any{"addr": edgeLn.Addr().String(), "caCert": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"},
			},
		})
	}))
	defer api.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc, err := orbitproxy.Connect(ctx, testConnectOpts(api))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer svc.Close()

	select {
	case res := <-resultCh:
		if res.RequestID != "mer_standalone" || res.Status != "succeeded" {
			t.Fatalf("response = %+v", res)
		}
		if len(res.Tools) != 1 || res.Tools[0].Name != "ping" {
			t.Fatalf("tools = %+v", res.Tools)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("discover_tools_result timeout")
	}
}
