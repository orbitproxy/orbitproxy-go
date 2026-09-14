package discover

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

func TestListToolsFollowsNextCursor(t *testing.T) {
	var pages []string
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
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"custom","version":"1"}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			params, _ := payload["params"].(map[string]any)
			cursor, _ := params["cursor"].(string)
			pages = append(pages, cursor)
			w.Header().Set("Content-Type", "application/json")
			if cursor == "" {
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"tool_a"}],"nextCursor":"page-2"}}`))
				return
			}
			if cursor != "page-2" {
				t.Errorf("unexpected cursor %q", cursor)
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"tool_b"}]}}`))
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := ListTools(ctx, listToolsParams{
		LocalAddr: server.Listener.Addr().String(),
		LocalPath: "/",
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(pages) != 2 || pages[0] != "" || pages[1] != "page-2" {
		t.Fatalf("pages = %v", pages)
	}
	if len(result.Tools) != 2 || result.Tools[0].Name != "tool_a" || result.Tools[1].Name != "tool_b" {
		t.Fatalf("unexpected tools: %+v", result.Tools)
	}
	if result.Truncated {
		t.Fatal("did not expect truncation")
	}
}
