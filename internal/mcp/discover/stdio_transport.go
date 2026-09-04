package discover

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcpstdio"
)

// StdioTransport 通过 execbridge Session 与 stdio MCP server 通信。
// 用于 exec 模式的工具发现——发现完成后关闭会话（临时会话，不并入 pool）。
type StdioTransport struct {
	session *mcpstdio.Session
}

// NewStdioTransport 创建一个临时的 stdio 传输层。
// 启动子进程并完成 MCP 握手。调用方必须在使用完毕后 Close。
// onDiag 可选：Session 握手失败/异常退出时回调。
func NewStdioTransport(cfg mcpstdio.SpawnConfig, endpointID, machineDir string, onDiag mcpstdio.DiagnosticCallback) (*StdioTransport, error) {
	sessionCfg := mcpstdio.SessionConfig{
		SpawnConfig:  cfg,
		EndpointID:   endpointID,
		MachineDir:   machineDir,
		DiagCallback: onDiag,
	}

	session, err := mcpstdio.NewSession(sessionCfg)
	if err != nil {
		return nil, fmt.Errorf("stdio transport: %w", err)
	}
	return &StdioTransport{session: session}, nil
}

// Send 通过 stdio 管道发送 JSON-RPC 消息并等待响应。
func (t *StdioTransport) Send(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
	msg := &mcpstdio.Message{}
	if err := json.Unmarshal(request, msg); err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}

	// 通知类消息（无 id）——发完即走
	if msg.IsNotification() {
		if err := t.session.SendNotification(msg); err != nil {
			return nil, err
		}
		return json.RawMessage("{}"), nil
	}

	// 请求类消息——调用并等待响应
	resp, err := t.session.Call(ctx, msg)
	if err != nil {
		return nil, err
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal response: %w", err)
	}
	return json.RawMessage(respBytes), nil
}

// Close 关闭临时会话，终止子进程。
func (t *StdioTransport) Close() error {
	if t.session != nil {
		return t.session.Close()
	}
	return nil
}
