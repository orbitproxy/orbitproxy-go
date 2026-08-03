package endpoint

import (
	"encoding/json"
	"testing"

	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

func TestManagerUpsertAndDelete(t *testing.T) {
	t.Parallel()

	logger := sdklog.Nop()
	mgr := NewManager()
	mgr.HandleNewEndpoint(logger, &wire.NewEndpoint{
		EndpointID:          "ep-1",
		ProxyID:             "px-1",
		ProxyType:           "basic",
		Protocol:            "https",
		LocalServicePayload: json.RawMessage(`{"localAddr":"127.0.0.1:8080"}`),
	})

	if mgr.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", mgr.Len())
	}
	rt, ok := mgr.Get("ep-1")
	if !ok {
		t.Fatal("Get(ep-1) not found")
	}
	cfg := rt.Config()
	if cfg == nil || string(cfg.LocalServicePayload) != `{"localAddr":"127.0.0.1:8080"}` {
		t.Fatalf("Config() = %+v", cfg)
	}

	mgr.HandleCloseEndpoint(logger, &wire.CloseEndpoint{EndpointID: "ep-1", ProxyID: "px-1"})
	if mgr.Len() != 0 {
		t.Fatalf("Len() after delete = %d, want 0", mgr.Len())
	}
}

func TestManagerRejectsNewWithError(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	mgr.HandleNewEndpoint(sdklog.Nop(), &wire.NewEndpoint{
		EndpointID: "ep-1",
		ProxyID:    "px-1",
		Error:      "disabled",
	})
	if mgr.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", mgr.Len())
	}
}

func TestRuntimeUpdatePreservesInstance(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	mgr.HandleNewEndpoint(sdklog.Nop(), &wire.NewEndpoint{
		EndpointID:          "ep-1",
		ProxyID:             "px-1",
		ProxyType:           "basic",
		LocalServicePayload: json.RawMessage(`{"localAddr":"127.0.0.1:8080"}`),
	})
	rt1, _ := mgr.Get("ep-1")

	mgr.HandleNewEndpoint(sdklog.Nop(), &wire.NewEndpoint{
		EndpointID:          "ep-1",
		ProxyID:             "px-1",
		ProxyType:           "basic",
		LocalServicePayload: json.RawMessage(`{"localAddr":"127.0.0.1:9090"}`),
	})
	rt2, _ := mgr.Get("ep-1")

	if rt1 != rt2 {
		t.Fatal("upsert should update existing runtime in place")
	}
	if string(rt2.Config().LocalServicePayload) != `{"localAddr":"127.0.0.1:9090"}` {
		t.Fatalf("LocalServicePayload = %q", string(rt2.Config().LocalServicePayload))
	}
}
