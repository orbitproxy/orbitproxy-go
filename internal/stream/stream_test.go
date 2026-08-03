package stream

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestJoinCopiesBothDirectionsAndReturnsCounts(t *testing.T) {
	t.Parallel()

	client, edgeUser := net.Pipe()
	edgeWork, backend := net.Pipe()

	joinDone := make(chan struct{})
	var inCount, outCount int64
	go func() {
		defer close(joinDone)
		inCount, outCount, _ = Join(edgeUser, edgeWork)
	}()

	echoDone := make(chan struct{})
	go func() {
		defer close(echoDone)
		defer backend.Close()
		buf := make([]byte, 64)
		n, err := backend.Read(buf)
		if err != nil {
			return
		}
		_, _ = backend.Write(buf[:n])
	}()

	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	payload := []byte("hello-join")
	if _, err := client.Write(payload); err != nil {
		t.Fatalf("client write: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
	_ = client.Close()

	<-echoDone
	<-joinDone

	if inCount+outCount != int64(2*len(payload)) {
		t.Fatalf("in=%d out=%d, want sum %d", inCount, outCount, 2*len(payload))
	}
}

func TestWrapWorkConnPassthrough(t *testing.T) {
	t.Parallel()

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	wrapped, recycle, err := WrapWorkConn(a, WorkConnWrapOptions{})
	if err != nil {
		t.Fatalf("WrapWorkConn error: %v", err)
	}
	defer recycle()
	if wrapped != a {
		t.Fatalf("expected passthrough wrapper when options disabled")
	}
}
