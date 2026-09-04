package discover

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPTransport 是基于 HTTP POST 的 MCP JSON-RPC 传输层。
// 从原 client.go 的 postJSONRPC 逻辑迁入。
type HTTPTransport struct {
	URL                   string
	SessionID             string // MCP 会话 ID，在 initialize 后设置
	RewritePlaywrightHost bool
	Client                *http.Client
}

// NewHTTPTransport 创建 HTTP 传输层。
func NewHTTPTransport(localAddr, localPath string, timeout time.Duration, rewritePlaywright bool) *HTTPTransport {
	if localPath == "" {
		localPath = "/mcp"
	}
	if !strings.HasPrefix(localPath, "/") {
		localPath = "/" + localPath
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &HTTPTransport{
		URL:                   "http://" + localAddr + localPath,
		RewritePlaywrightHost: rewritePlaywright,
		Client:                &http.Client{Timeout: timeout},
	}
}

// Send 发送 JSON-RPC 消息并返回响应体。
func (t *HTTPTransport) Send(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, bytes.NewReader(request))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if t.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.SessionID)
	}
	if t.RewritePlaywrightHost {
		rewritePlaywrightLoopbackHost(req)
	}

	resp, err := t.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	// 更新 session ID
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.SessionID = sid
	} else if sid := resp.Header.Get("mcp-session-id"); sid != "" {
		t.SessionID = sid
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	if len(raw) == 0 {
		// 通知类消息允许空 body
		return json.RawMessage("{}"), nil
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		data, err := extractSSEData(raw)
		return json.RawMessage(data), err
	}
	return json.RawMessage(raw), nil
}

// Close 释放 HTTP 传输层资源（HTTP 无状态，无需清理）。
func (t *HTTPTransport) Close() error {
	return nil
}
