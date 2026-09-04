package mcpstdio

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"log/slog"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/toolschema"
)

// Bridge 是 exec 模式的 HTTP ⇄ stdio JSON-RPC 桥接层。
// 每个 work conn 被视为一个独立的 HTTP 请求/响应对。
type Bridge struct {
	pool   *Pool
	logger *slog.Logger
}

// NewBridge 创建 bridge 实例。
func NewBridge(pool *Pool, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{pool: pool, logger: logger}
}

// HandleWorkConn 处理一个来自 Edge 的 work conn。
// work conn 承载完整的 HTTP 请求；bridge 解析请求体中的 JSON-RPC，
// 获取/创建 stdio 会话，转发给子进程，将响应写回 work conn。
func (b *Bridge) HandleWorkConn(conn net.Conn, endpointID string) {
	defer conn.Close()

	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		b.logger.Warn("read http request from work conn failed",
			"endpoint_id", endpointID,
			"err", err,
		)
		writeHTTPError(conn, http.StatusBadRequest, CodeInternal, "failed to read HTTP request")
		return
	}
	defer req.Body.Close()

	sessionKey := deriveSessionKey(endpointID)

	body, err := io.ReadAll(io.LimitReader(req.Body, 2<<20))
	if err != nil {
		writeHTTPError(conn, http.StatusBadRequest, CodeInternal, "failed to read request body")
		return
	}
	if len(body) == 0 {
		writeHTTPError(conn, http.StatusBadRequest, CodeInternal, "empty request body")
		return
	}

	msg := &Message{}
	if err := json.Unmarshal(body, msg); err != nil {
		writeHTTPError(conn, http.StatusBadRequest, CodeInternal, "invalid JSON-RPC message")
		return
	}

	session, err := b.pool.GetOrCreate(sessionKey, endpointID)
	if err != nil {
		b.logger.Warn("get or create session failed",
			"endpoint_id", endpointID,
			"session_key", sessionKey,
			"err", err,
		)
		code := http.StatusBadGateway
		errCode := CodeSpawnFailed
		if isLimitError(err) {
			code = http.StatusServiceUnavailable
			errCode = CodeConcurrencyLimit
		} else if isExitedOnStartError(err) {
			errCode = CodeExitedOnStart
		}
		writeHTTPError(conn, code, errCode, err.Error())
		return
	}

	method := msg.MethodString()

	// Session 已完成 MCP 握手：对客户端 initialize 合成应答，禁止二次转发给子进程。
	if method == "initialize" && msg.IsRequest() {
		writeSynthesizedInitialize(conn, msg, session)
		return
	}
	if method == "notifications/initialized" && msg.IsNotification() {
		writeHTTPResponse(conn, http.StatusAccepted, []byte("{}"))
		return
	}

	// 通知类消息（无 id）——发完即走，不等响应
	if msg.IsNotification() {
		if err := session.SendNotification(msg); err != nil {
			if isDeadSessionError(err) {
				b.pool.Invalidate(sessionKey)
			}
			writeHTTPError(conn, http.StatusBadGateway, CodeExitedAtRuntime, err.Error())
			return
		}
		writeHTTPResponse(conn, http.StatusAccepted, []byte("{}"))
		return
	}

	// 请求类消息——调用子进程并等待响应
	resp, err := session.Call(req.Context(), msg)
	if err != nil {
		b.logger.Warn("session call failed",
			"endpoint_id", endpointID,
			"method", method,
			"err", err,
		)
		statusCode := http.StatusBadGateway
		errCode := CodeInternal
		if isTimeoutError(err) {
			statusCode = http.StatusGatewayTimeout
		}
		if isDeadSessionError(err) {
			errCode = CodeExitedAtRuntime
			b.pool.Invalidate(sessionKey)
		}
		writeHTTPError(conn, statusCode, errCode, err.Error())
		return
	}

	respBody, err := json.Marshal(resp)
	if err != nil {
		writeHTTPError(conn, http.StatusInternalServerError, CodeInternal, "failed to marshal response")
		return
	}
	// 不看 method 字符串：凡是 result.tools 形状的 JSON-RPC 都洗。
	if fixed, ok := toolschema.SanitizeToolsListJSON(respBody); ok {
		respBody = fixed
	}
	writeHTTPResponse(conn, http.StatusOK, respBody)
}

func writeSynthesizedInitialize(conn net.Conn, req *Message, session *Session) {
	result := session.CachedInitializeResult()
	if len(result) == 0 {
		name, version := session.ServerInfo()
		if name == "" {
			name = "orbitproxy-exec"
		}
		if version == "" {
			version = "1.0.0"
		}
		raw, _ := json.Marshal(map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"serverInfo": map[string]any{
				"name":    name,
				"version": version,
			},
		})
		result = raw
	}
	out := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(req.ID),
		"result":  json.RawMessage(result),
	}
	body, _ := json.Marshal(out)
	writeHTTPResponse(conn, http.StatusOK, body)
}

// deriveSessionKey 返回会话键。
// exec 模式下每个 endpoint 共享一个 stdio 子进程——sessionKey = endpointID。
func deriveSessionKey(endpointID string) string {
	return endpointID
}

// ----------------------------------------------------------------
// HTTP 响应写入
// ----------------------------------------------------------------

func writeHTTPResponse(conn net.Conn, statusCode int, body []byte) {
	resp := &http.Response{
		StatusCode:    statusCode,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        make(http.Header),
		ContentLength: int64(len(body)),
		Body:          io.NopCloser(bytes.NewReader(body)),
	}
	resp.Header.Set("Content-Type", "application/json")
	_ = resp.Write(conn)
}

func writeHTTPError(conn net.Conn, statusCode int, errCode, errMsg string) {
	errBody := map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    -32000,
			"message": errMsg,
			"data": map[string]string{
				"error_code": errCode,
			},
		},
	}
	body, _ := json.Marshal(errBody)
	writeHTTPResponse(conn, statusCode, body)
}

func isLimitError(err error) bool {
	return strings.Contains(err.Error(), "limit reached")
}

func isTimeoutError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline")
}

func isDeadSessionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "subprocess exited") ||
		strings.Contains(msg, "subprocess already exited") ||
		strings.Contains(msg, "write stdin:") ||
		strings.Contains(msg, "exit_code=")
}

func isExitedOnStartError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "shortly after MCP handshake")
}

// ----------------------------------------------------------------
// Payload 解析
// ----------------------------------------------------------------

// ParseExecPayload 从 endpoint 的 local_service_payload 解析 exec 配置。
func ParseExecPayload(raw json.RawMessage) (*SpawnConfig, error) {
	var payload struct {
		Delivery       string   `json:"delivery"`
		Command        string   `json:"command"`
		Args           []string `json:"args"`
		WorkDir        string   `json:"workDir"`
		EnvPassthrough []string `json:"envPassthrough"`
		CatalogKey     string   `json:"catalogKey"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode exec payload: %w", err)
	}
	if payload.Command == "" {
		return nil, fmt.Errorf("exec payload: command is required")
	}
	return &SpawnConfig{
		Command:        payload.Command,
		Args:           applyFilesystemOpenRoot(payload.CatalogKey, payload.Args),
		WorkDir:        payload.WorkDir,
		EnvPassthrough: payload.EnvPassthrough,
	}, nil
}
