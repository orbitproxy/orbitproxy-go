package preflight

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeTransport struct {
	sendFn func(ctx context.Context, request json.RawMessage) (json.RawMessage, error)
	calls  []string
}

func (f *fakeTransport) Send(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
	var payload map[string]any
	_ = json.Unmarshal(request, &payload)
	if method, ok := payload["method"].(string); ok {
		f.calls = append(f.calls, method)
	}
	if f.sendFn != nil {
		return f.sendFn(ctx, request)
	}
	return json.RawMessage(`{"jsonrpc":"2.0","id":3,"result":{}}`), nil
}

func TestCatalogKeyFromPayload(t *testing.T) {
	if got := CatalogKeyFromPayload(json.RawMessage(`{"catalogKey":"MySQL"}`)); got != "mysql" {
		t.Fatalf("got %q, want mysql", got)
	}
	if got := CatalogKeyFromPayload(json.RawMessage(`{}`)); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestRegistryRunNoopWhenMissing(t *testing.T) {
	reg := NewRegistry()
	ft := &fakeTransport{}
	if err := reg.Run(context.Background(), "filesystem", ft, nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(ft.calls) != 0 {
		t.Fatalf("expected no calls, got %v", ft.calls)
	}
}

func TestRegistryDispatchesMysql(t *testing.T) {
	reg := NewRegistry()
	reg.Register(mysqlCatalogCheck{})
	ft := &fakeTransport{
		sendFn: func(_ context.Context, request json.RawMessage) (json.RawMessage, error) {
			var payload map[string]any
			if err := json.Unmarshal(request, &payload); err != nil {
				t.Fatalf("decode: %v", err)
			}
			params, _ := payload["params"].(map[string]any)
			if params["name"] != "mysql_query" {
				t.Fatalf("unexpected tool: %v", params["name"])
			}
			args, _ := params["arguments"].(map[string]any)
			if args["sql"] != "SELECT 1" {
				t.Fatalf("unexpected sql: %v", args["sql"])
			}
			return json.RawMessage(`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"1"}]}}`), nil
		},
	}
	if err := reg.Run(context.Background(), "mysql", ft, []Tool{{Name: "mysql_query"}}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(ft.calls) != 1 || ft.calls[0] != "tools/call" {
		t.Fatalf("unexpected calls: %v", ft.calls)
	}
}

func TestMysqlCatalogCheckFailsOnIsError(t *testing.T) {
	ft := &fakeTransport{
		sendFn: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"jsonrpc":"2.0","id":3,"result":{"isError":true,"content":[{"type":"text","text":"access denied"}]}}`), nil
		},
	}
	err := mysqlCatalogCheck{}.Check(context.Background(), ft, []Tool{{Name: "mysql_query"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestRunCatalogDefaultMysql(t *testing.T) {
	ft := &fakeTransport{
		sendFn: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"ok"}]}}`), nil
		},
	}
	if err := RunCatalog(context.Background(), "mysql", ft, []Tool{{Name: "mysql_query"}}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}
