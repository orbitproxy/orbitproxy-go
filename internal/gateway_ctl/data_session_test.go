package gateway_ctl

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/internal/yamuxcfg"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

func TestNoDataRunnersWhenCountIsZero(t *testing.T) {
	ctl := NewControl(context.Background(), &SessionContext{
		DataSessions: 0,
		DialEdge: func(context.Context) (net.Conn, *yamux.Session, error) {
			t.Fatal("dialed a data session for data_sessions=0")
			return nil, nil, nil
		},
	}, sdklog.Nop(), nil)
	ctl.startDataSessionRunners(context.Background())
	time.Sleep(50 * time.Millisecond)
}

func TestDataSessionRunners(t *testing.T) {
	privatePEM, publicPEM := testKeyPEM(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	seen := map[int]int{}
	inflight := map[int]int{}
	acceptedWork := make(chan struct{}, 1)

	ctl := NewControl(ctx, &SessionContext{
		SessionID:      "sess_test",
		DataSessions:   2,
		RedialInterval: 20 * time.Millisecond,
		ConnConfig: ConnConfig{
			MachineKey:    "ck_test",
			PrivateKeyPEM: privatePEM,
		},
		DialEdge: func(ctx context.Context) (net.Conn, *yamux.Session, error) {
			if yamuxcfg.New().MaxStreamWindowSize != 4*1024*1024 {
				t.Errorf("window = %d", yamuxcfg.New().MaxStreamWindowSize)
			}
			left, right := net.Pipe()
			client, err := yamux.Client(left, yamuxcfg.New())
			if err != nil {
				return nil, nil, err
			}
			server, err := yamux.Server(right, yamuxcfg.New())
			if err != nil {
				_ = client.Close()
				return nil, nil, err
			}
			go serveOneDataHello(t, ctx, server, publicPEM, &mu, seen, inflight, acceptedWork)
			return left, client, nil
		},
	}, sdklog.Nop(), nil)
	ctl.startDataSessionRunners(ctx)

	deadline := time.Now().Add(3 * time.Second)
	var zero, one int
	for time.Now().Before(deadline) {
		mu.Lock()
		zero, one = seen[0], seen[1]
		mu.Unlock()
		if zero >= 2 && one >= 1 && workAccepted(acceptedWork) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if zero < 2 || one < 1 || !workAccepted(acceptedWork) {
		t.Fatalf("index0=%d index1=%d work=%v", zero, one, workAccepted(acceptedWork))
	}
	cancel()
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	after := seen[0]
	mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	if seen[0] != after {
		t.Fatalf("redialed after cancel: %d -> %d", after, seen[0])
	}
	mu.Unlock()
}

func workAccepted(ch chan struct{}) bool {
	return len(ch) > 0
}

func serveOneDataHello(t *testing.T, ctx context.Context, server *yamux.Session, publicPEM string, mu *sync.Mutex, seen, inflight map[int]int, acceptedWork chan struct{}) {
	t.Helper()
	defer server.Close()
	stream, err := server.Accept()
	if err != nil {
		return
	}
	message, err := wire.ReadMsg(stream)
	if err != nil {
		return
	}
	hello, ok := message.(*wire.DataHello)
	if !ok {
		t.Errorf("first message %T", message)
		return
	}
	canonical := wire.DataHelloCanonicalString(hello.SessionID, hello.MachineKey, hello.Index, hello.Timestamp, hello.Nonce)
	if err := wire.VerifyClientHelloSignature(publicPEM, hello.AuthSignature, canonical); err != nil {
		t.Errorf("signature: %v", err)
	}
	mu.Lock()
	if inflight[hello.Index] != 0 {
		t.Errorf("index %d dialed in parallel", hello.Index)
	}
	inflight[hello.Index]++
	seen[hello.Index]++
	count := seen[hello.Index]
	mu.Unlock()
	if err := wire.WriteMsg(stream, wire.DataHelloAck{}); err != nil {
		return
	}
	_ = stream.Close()

	if hello.Index == 1 && count == 1 {
		work, err := server.Open()
		if err != nil {
			return
		}
		_ = wire.WriteMsg(work, wire.StartWorkConn{EndpointID: "iep_test"})
		_ = work.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 1)
		_, _ = work.Read(buf)
		select {
		case acceptedWork <- struct{}{}:
		default:
		}
		<-ctx.Done()
	}
	mu.Lock()
	inflight[hello.Index]--
	mu.Unlock()
}

func testKeyPEM(t *testing.T) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	return privatePEM, publicPEM
}
