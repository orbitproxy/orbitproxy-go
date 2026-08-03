package mcpdiscover

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

const maxTools = 100

// Tool is one MCP tools/list entry.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Result is the outcome of a local tools/list.
type Result struct {
	Tools         []Tool
	Truncated     bool
	ServerName    string
	ServerVersion string
}

type listToolsParams struct {
	LocalAddr             string
	LocalPath             string
	Transport             string
	Timeout               time.Duration
	RewritePlaywrightHost bool
}

func ListTools(ctx context.Context, params listToolsParams) (Result, error) {
	if params.LocalAddr == "" {
		return Result{}, fmt.Errorf("localAddr is required")
	}
	if params.LocalPath == "" {
		params.LocalPath = "/mcp"
	}
	if !strings.HasPrefix(params.LocalPath, "/") {
		params.LocalPath = "/" + params.LocalPath
	}
	if params.Timeout <= 0 {
		params.Timeout = 30 * time.Second
	}

	url := "http://" + params.LocalAddr + params.LocalPath
	client := &http.Client{Timeout: params.Timeout}

	sessionID, serverName, serverVersion, err := initialize(ctx, client, url, params.RewritePlaywrightHost)
	if err != nil {
		return Result{}, err
	}
	if err := notifyInitialized(ctx, client, url, sessionID, params.RewritePlaywrightHost); err != nil {
		return Result{}, err
	}
	tools, err := toolsList(ctx, client, url, sessionID, params.RewritePlaywrightHost)
	if err != nil {
		return Result{}, err
	}
	truncated := false
	if len(tools) > maxTools {
		tools = tools[:maxTools]
		truncated = true
	}
	_ = params.Transport
	return Result{
		Tools:         tools,
		Truncated:     truncated,
		ServerName:    serverName,
		ServerVersion: serverVersion,
	}, nil
}

func initialize(ctx context.Context, client *http.Client, url string, rewritePlaywrightHost bool) (sessionID, serverName, serverVersion string, err error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "orbitproxy-go",
				"version": "1.0.0",
			},
		},
	}
	raw, headers, err := postJSONRPC(ctx, client, url, "", payload, false, rewritePlaywrightHost)
	if err != nil {
		return "", "", "", err
	}
	var envelope struct {
		Result struct {
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", "", "", fmt.Errorf("decode initialize: %w", err)
	}
	if envelope.Error != nil {
		return "", "", "", fmt.Errorf("initialize error: %s", envelope.Error.Message)
	}
	sessionID = headers.Get("Mcp-Session-Id")
	if sessionID == "" {
		sessionID = headers.Get("mcp-session-id")
	}
	return sessionID, envelope.Result.ServerInfo.Name, envelope.Result.ServerInfo.Version, nil
}

func notifyInitialized(ctx context.Context, client *http.Client, url, sessionID string, rewritePlaywrightHost bool) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	_, _, err := postJSONRPC(ctx, client, url, sessionID, payload, true, rewritePlaywrightHost)
	return err
}

func toolsList(ctx context.Context, client *http.Client, url, sessionID string, rewritePlaywrightHost bool) ([]Tool, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	raw, _, err := postJSONRPC(ctx, client, url, sessionID, payload, false, rewritePlaywrightHost)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode tools/list: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("tools/list error: %s", envelope.Error.Message)
	}
	out := make([]Tool, 0, len(envelope.Result.Tools))
	for _, item := range envelope.Result.Tools {
		if item.Name == "" {
			continue
		}
		out = append(out, Tool{
			Name:        item.Name,
			Description: item.Description,
			InputSchema: item.InputSchema,
		})
	}
	return out, nil
}

func postJSONRPC(
	ctx context.Context,
	client *http.Client,
	url string,
	sessionID string,
	payload map[string]any,
	allowEmptyBody bool,
	rewritePlaywrightHost bool,
) ([]byte, http.Header, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	if rewritePlaywrightHost {
		rewritePlaywrightLoopbackHost(req)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("dial failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	if len(raw) == 0 {
		if allowEmptyBody {
			return []byte("{}"), resp.Header, nil
		}
		return nil, resp.Header, fmt.Errorf("empty response body")
	}
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		data, err := extractSSEData(raw)
		return data, resp.Header, err
	}
	return raw, resp.Header, nil
}

func extractSSEData(raw []byte) ([]byte, error) {
	lines := strings.Split(string(raw), "\n")
	var dataLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(dataLines) == 0 {
		return nil, fmt.Errorf("empty sse data")
	}
	return []byte(dataLines[len(dataLines)-1]), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ParseLocalPayload reads discovery fields from endpoint local_service_payload.
func ParseLocalPayload(raw json.RawMessage) (localAddr, localPath, transport string, err error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", "", "", fmt.Errorf("decode local payload: %w", err)
	}
	localAddr, _ = payload["localAddr"].(string)
	localPath, _ = payload["localPath"].(string)
	transport, _ = payload["transport"].(string)
	if localAddr == "" {
		return "", "", "", fmt.Errorf("localAddr is required")
	}
	return localAddr, localPath, transport, nil
}

// ListToolsFromPayload runs tools/list using endpoint local_service_payload.
func ListToolsFromPayload(ctx context.Context, raw json.RawMessage, timeoutSeconds int) (Result, error) {
	localAddr, localPath, transport, err := ParseLocalPayload(raw)
	if err != nil {
		return Result{}, err
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	return ListTools(ctx, listToolsParams{
		LocalAddr:             localAddr,
		LocalPath:             localPath,
		Transport:             transport,
		Timeout:               timeout,
		RewritePlaywrightHost: IsPlaywrightPayload(raw),
	})
}
