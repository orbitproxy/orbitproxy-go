package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcpstdio"
	"github.com/orbitproxy/orbitproxy-go/internal/mcp/toolschema"
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

// ListTools 通过 HTTP 传输层发现 MCP 工具（仅 tools/list，不含 catalog 预检）。
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

	transport := NewHTTPTransport(params.LocalAddr, params.LocalPath, params.Timeout, params.RewritePlaywrightHost)
	defer transport.Close()

	return ListToolsViaTransport(ctx, transport)
}

// ListToolsViaTransport 通过 Transport 接口发现 MCP 工具。
// HTTP 和 stdio 共用此逻辑：initialize → initialized → tools/list
func ListToolsViaTransport(ctx context.Context, transport Transport) (Result, error) {
	initPayload, _ := json.Marshal(map[string]any{
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
	})

	initResp, err := transport.Send(ctx, initPayload)
	if err != nil {
		return Result{}, fmt.Errorf("initialize: %w", err)
	}

	var initEnvelope struct {
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
	if err := json.Unmarshal(initResp, &initEnvelope); err != nil {
		return Result{}, fmt.Errorf("decode initialize: %w", err)
	}
	if initEnvelope.Error != nil {
		return Result{}, fmt.Errorf("initialize error: %s", initEnvelope.Error.Message)
	}

	notifPayload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	if _, err := transport.Send(ctx, notifPayload); err != nil {
		return Result{}, fmt.Errorf("initialized notify: %w", err)
	}

	toolsPayload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	toolsResp, err := transport.Send(ctx, toolsPayload)
	if err != nil {
		return Result{}, fmt.Errorf("tools/list: %w", err)
	}
	if fixed, ok := toolschema.SanitizeToolsListJSON(toolsResp); ok {
		toolsResp = fixed
	}

	var toolsEnvelope struct {
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
	if err := json.Unmarshal(toolsResp, &toolsEnvelope); err != nil {
		return Result{}, fmt.Errorf("decode tools/list: %w", err)
	}
	if toolsEnvelope.Error != nil {
		return Result{}, fmt.Errorf("tools/list error: %s", toolsEnvelope.Error.Message)
	}

	tools := make([]Tool, 0, len(toolsEnvelope.Result.Tools))
	for _, item := range toolsEnvelope.Result.Tools {
		if item.Name == "" {
			continue
		}
		tools = append(tools, Tool{
			Name:        item.Name,
			Description: item.Description,
			InputSchema: item.InputSchema,
		})
	}

	truncated := false
	if len(tools) > maxTools {
		tools = tools[:maxTools]
		truncated = true
	}

	return Result{
		Tools:         tools,
		Truncated:     truncated,
		ServerName:    initEnvelope.Result.ServerInfo.Name,
		ServerVersion: initEnvelope.Result.ServerInfo.Version,
	}, nil
}

// ListToolsViaStdio 使用临时 stdio 会话做 tools/list（不含 catalog 预检）。
func ListToolsViaStdio(
	ctx context.Context,
	cfg mcpstdio.SpawnConfig,
	endpointID string,
	machineDir string,
	onDiag mcpstdio.DiagnosticCallback,
) (Result, error) {
	transport, err := NewStdioTransport(cfg, endpointID, machineDir, onDiag)
	if err != nil {
		return Result{}, err
	}
	defer transport.Close()

	return ListToolsViaTransport(ctx, transport)
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
// 仅清单同步，不含 catalog 预检（预检见 package preflight）。
func ListToolsFromPayload(
	ctx context.Context,
	raw json.RawMessage,
	timeoutSeconds int,
	endpointID string,
	machineDir string,
	onDiag mcpstdio.DiagnosticCallback,
) (Result, error) {
	execCfg, err := mcpstdio.ParseExecPayload(raw)
	if err == nil && execCfg != nil {
		return ListToolsViaStdio(ctx, *execCfg, endpointID, machineDir, onDiag)
	}

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
