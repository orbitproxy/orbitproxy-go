package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcpstdio"
)

// Probe is an active health-check strategy (periodic Check).
// Passive signals (process death, dial failure) use Observation via the
// endpoint Runtime — still the same health model, different strategy.
// Exec/stdio MCP often has ActiveProbe=nil and relies on passive observation.
type Probe interface {
	// Check 执行一次健康检查，返回 nil 表示健康。
	Check(ctx context.Context) error
	// Name 返回探针类型名称（用于日志和上报）。
	Name() string
}

// ----------------------------------------------------------------
// TCPProbe：TCP 连接探针（forward 模式使用）
// ----------------------------------------------------------------

// TCPProbe 通过 TCP 拨号检查目标地址是否可达。
type TCPProbe struct {
	Addr string
}

func (p *TCPProbe) Check(ctx context.Context) error {
	if p.Addr == "" {
		return nil
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", p.Addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func (p *TCPProbe) Name() string { return "tcp" }

// ----------------------------------------------------------------
// McpHTTPPingProbe：向 HTTP MCP server 发送 JSON-RPC ping
// 用于 forward 模式的 MCP endpoint
// ----------------------------------------------------------------

// McpHTTPPingProbe 向 HTTP MCP server 发送 ping 请求。
type McpHTTPPingProbe struct {
	URL    string
	Client *http.Client
}

func (p *McpHTTPPingProbe) Check(ctx context.Context) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL,
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create ping request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ping dial: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ping http %d", resp.StatusCode)
	}
	return nil
}

func (p *McpHTTPPingProbe) Name() string { return "mcp_http_ping" }

// ----------------------------------------------------------------
// McpStdioPingProbe：通过 execbridge Session 发送 ping
// 用于 exec 模式的 MCP endpoint
// ----------------------------------------------------------------

// McpStdioPingProbe 通过 stdio 会话发送 JSON-RPC ping。
type McpStdioPingProbe struct {
	Pool       *mcpstdio.Pool
	EndpointID string
}

func (p *McpStdioPingProbe) Check(ctx context.Context) error {
	session, err := p.Pool.GetOrCreate(p.EndpointID, p.EndpointID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	msg := &mcpstdio.Message{
		JSONRPC: json.RawMessage(`"2.0"`),
		ID:      json.RawMessage(`999`),
		Method:  json.RawMessage(`"ping"`),
	}

	_, err = session.Call(ctx, msg)
	return err
}

func (p *McpStdioPingProbe) Name() string { return "mcp_stdio_ping" }
