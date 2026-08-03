package mcpdiscover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestListToolsUsesSessionAndInitializedNotification(t *testing.T) {
	var sawInitialized atomic.Bool
	var sawToolsList atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode body: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		method, _ := payload["method"].(string)
		switch method {
		case "initialize":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Mcp-Session-Id", "sess-1")
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n\n"))
		case "notifications/initialized":
			if r.Header.Get("Mcp-Session-Id") != "sess-1" {
				t.Errorf("initialized missing session header")
			}
			sawInitialized.Store(true)
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if r.Header.Get("Mcp-Session-Id") != "sess-1" {
				t.Errorf("tools/list missing session header")
			}
			if !sawInitialized.Load() {
				t.Errorf("tools/list before initialized")
			}
			sawToolsList.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"browser_close","description":"Close"}]}}`))
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	host := server.Listener.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := ListTools(ctx, listToolsParams{
		LocalAddr: host,
		LocalPath: "/",
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if !sawInitialized.Load() || !sawToolsList.Load() {
		t.Fatalf("expected initialized and tools/list calls")
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "browser_close" {
		t.Fatalf("unexpected tools: %+v", result.Tools)
	}
}
