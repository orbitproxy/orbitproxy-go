package service

import (
	"testing"
	"time"
)

func TestEdgeKeepAliveIdle(t *testing.T) {
	if edgeKeepAlive().Idle != 30*time.Second || !edgeKeepAlive().Enable {
		t.Fatalf("keepalive = %+v", edgeKeepAlive())
	}
}
