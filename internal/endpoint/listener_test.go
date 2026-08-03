package endpoint

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

func TestInProcessListenerAcceptsWorkConn(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	mgr.SetContext(context.Background())
	mgr.HandleNewEndpoint(sdklog.Nop(), &wire.NewEndpoint{
		EndpointID:          "ep_listen",
		ProxyID:             "px_1",
		ProxyType:           "basic",
		Protocol:            "https",
		LocalServicePayload: json.RawMessage(`{"delivery":"in_process"}`),
	})

	ln, err := mgr.ClaimListener("ep_listen")
	if err != nil {
		t.Fatalf("ClaimListener: %v", err)
	}
	defer ln.Close()

	client, server := net.Pipe()
	defer client.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		accepted <- c
	}()

	mgr.HandleWorkConn(sdklog.Nop(), server, &wire.StartWorkConn{
		ProxyID:    "px_1",
		EndpointID: "ep_listen",
	})

	select {
	case c := <-accepted:
		defer c.Close()
		go func() { _, _ = client.Write([]byte("hello")) }()
		buf := make([]byte, 5)
		if _, err := io.ReadFull(c, buf); err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(buf) != "hello" {
			t.Fatalf("got %q", buf)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Accept timeout")
	}
}

func TestInProcessDropsWhenNotClaimed(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	mgr.SetContext(context.Background())
	mgr.HandleNewEndpoint(sdklog.Nop(), &wire.NewEndpoint{
		EndpointID:          "ep_listen",
		ProxyID:             "px_1",
		LocalServicePayload: json.RawMessage(`{"delivery":"in_process"}`),
	})

	client, server := net.Pipe()
	defer client.Close()

	mgr.HandleWorkConn(sdklog.Nop(), server, &wire.StartWorkConn{
		EndpointID: "ep_listen",
		ProxyID:    "px_1",
	})

	_ = client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	if _, err := client.Read(buf); err == nil {
		t.Fatal("expected closed/dropped work conn")
	}
}
