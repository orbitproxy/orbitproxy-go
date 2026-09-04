package endpoint

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcpstdio"
	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

func TestManagerWiresExecBridge(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	mgr.SetContext(context.Background())
	mgr.EnsureExecBridge(mcpstdio.DefaultPoolConfig(), sdklog.Nop())
	defer mgr.ShutdownExec()

	mgr.HandleNewEndpoint(sdklog.Nop(), &wire.NewEndpoint{
		EndpointID:          "ep-exec",
		ProxyID:             "px-1",
		ProxyType:           "mcp",
		Protocol:            "https",
		LocalServicePayload: json.RawMessage(`{"delivery":"exec","command":"echo","args":["hi"]}`),
	})

	rt, ok := mgr.Get("ep-exec")
	if !ok {
		t.Fatal("endpoint missing")
	}
	if rt.Delivery() != DeliveryExec {
		t.Fatalf("delivery = %q, want exec", rt.Delivery())
	}
	rt.mu.RLock()
	bridge := rt.bridge
	rt.mu.RUnlock()
	if bridge == nil {
		t.Fatal("expected runtime bridge to be set for exec endpoint")
	}

	mgr.HandleCloseEndpoint(sdklog.Nop(), &wire.CloseEndpoint{EndpointID: "ep-exec", ProxyID: "px-1"})
	if mgr.Len() != 0 {
		t.Fatalf("Len() = %d after close", mgr.Len())
	}
}

func TestManagerExecWorkConnRoundTrip(t *testing.T) {
	t.Parallel()

	script := `#!/usr/bin/env python3
import sys, json
def reply(msg, result=None):
    out = {"jsonrpc":"2.0","id": msg.get("id"), "result": result if result is not None else {}}
    sys.stdout.write(json.dumps(out) + "\n")
    sys.stdout.flush()
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    msg = json.loads(line)
    method = msg.get("method")
    if method == "initialize":
        reply(msg, {"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"t","version":"0"}})
    elif method == "notifications/initialized":
        continue
    elif method == "tools/list":
        reply(msg, {"tools":[{"name":"ping_tool","description":"x","inputSchema":{"type":"object"}}]})
    elif method == "ping":
        reply(msg, {})
    else:
        reply(msg, {})
`

	dir := t.TempDir()
	path := dir + "/mcp_server.py"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	mgr.SetContext(context.Background())
	mgr.EnsureExecBridge(mcpstdio.DefaultPoolConfig(), sdklog.Nop())
	defer mgr.ShutdownExec()

	payload, _ := json.Marshal(map[string]any{
		"delivery": "exec",
		"command":  "python3",
		"args":     []string{path},
	})
	mgr.HandleNewEndpoint(sdklog.Nop(), &wire.NewEndpoint{
		EndpointID:          "ep-py",
		ProxyID:             "px-1",
		ProxyType:           "mcp",
		Protocol:            "https",
		LocalServicePayload: payload,
	})

	client, server := net.Pipe()
	defer client.Close()

	go func() {
		_ = mgr.HandleWorkConn(sdklog.Nop(), server, &wire.StartWorkConn{
			EndpointID: "ep-py",
			ProxyID:    "px-1",
		})
	}()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req := fmt.Sprintf(
		"POST /mcp HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		len(body), body,
	)
	if _, err := client.Write([]byte(req)); err != nil {
		t.Fatalf("write request: %v", err)
	}
	_ = client.SetReadDeadline(time.Now().Add(8 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result, _ := raw["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %#v", raw)
	}
}
