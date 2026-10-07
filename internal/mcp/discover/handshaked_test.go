package discover

import (
	"context"
	"encoding/json"
	"testing"
)

// scriptedTransport 记录收到的 method，并对 tools/list 返回固定结果。
type scriptedTransport struct {
	methods []string
}

func (t *scriptedTransport) Send(_ context.Context, request json.RawMessage) (json.RawMessage, error) {
	var msg struct {
		ID     *int   `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(request, &msg); err != nil {
		return nil, err
	}
	t.methods = append(t.methods, msg.Method)
	switch msg.Method {
	case "initialize":
		return json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"http-srv","version":"2"}}}`), nil
	case "notifications/initialized":
		return json.RawMessage(`{}`), nil
	case "tools/list":
		return json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"echo"}]}}`), nil
	}
	return nil, nil
}

func (t *scriptedTransport) Close() error { return nil }

// handshakedTransport 模拟会话创建时已完成握手的 Transport（如 stdio Session）。
type handshakedTransport struct {
	scriptedTransport
}

func (t *handshakedTransport) ServerInfo() (string, string) { return "stdio-srv", "1" }

func TestListToolsViaTransportSkipsInitializeWhenHandshaked(t *testing.T) {
	transport := &handshakedTransport{}
	result, err := ListToolsViaTransport(context.Background(), transport)
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.methods) != 1 || transport.methods[0] != "tools/list" {
		t.Fatalf("handshaked transport must only see tools/list, got %v", transport.methods)
	}
	if result.ServerName != "stdio-srv" || result.ServerVersion != "1" {
		t.Fatalf("server info must come from the transport: %+v", result)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", result.Tools)
	}
}

func TestListToolsViaTransportHandshakesPlainTransport(t *testing.T) {
	transport := &scriptedTransport{}
	result, err := ListToolsViaTransport(context.Background(), transport)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"initialize", "notifications/initialized", "tools/list"}
	if len(transport.methods) != len(want) {
		t.Fatalf("methods = %v, want %v", transport.methods, want)
	}
	for i := range want {
		if transport.methods[i] != want[i] {
			t.Fatalf("methods = %v, want %v", transport.methods, want)
		}
	}
	if result.ServerName != "http-srv" || result.ServerVersion != "2" {
		t.Fatalf("server info must come from initialize: %+v", result)
	}
}

// 编译期保证 StdioTransport 实现 Handshaked。
var _ Handshaked = (*StdioTransport)(nil)
