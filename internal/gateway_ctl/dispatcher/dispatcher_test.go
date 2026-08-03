package dispatcher

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/orbitproxy/orbitproxy-go/wire"
)

func TestDispatcherSendReceive(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	d := NewDispatcher(client)
	var got wire.Message
	var wg sync.WaitGroup
	wg.Add(1)
	d.RegisterHandler(&wire.Disconnect{}, func(m wire.Message) {
		got = m
		wg.Done()
	})
	d.Run()

	if err := d.Send(wire.ReqWorkConn{}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	go func() {
		_, _ = wire.ReadMsg(server)
		if err := wire.WriteMsg(server, wire.Disconnect{Reason: "test"}); err != nil {
			t.Errorf("WriteMsg: %v", err)
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler was not invoked")
	}

	disconnect, ok := got.(*wire.Disconnect)
	if !ok {
		t.Fatalf("got %T, want *wire.Disconnect", got)
	}
	if disconnect.Reason != "test" {
		t.Fatalf("reason = %q, want test", disconnect.Reason)
	}
}

func TestDispatcherSendAfterClose(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	d := NewDispatcher(client)
	d.Run()
	_ = server.Close()

	select {
	case <-d.Done():
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not exit after read error")
	}

	deadline := time.Now().Add(time.Second)
	for {
		err := d.Send(wire.ReqWorkConn{})
		if err == io.EOF {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Send after close = %v, want EOF", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
