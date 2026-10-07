package discover

import (
	"context"
	"encoding/json"
)

// Transport 抽象 MCP 通信传输层。
// HTTP 和 stdio 两种模式均实现此接口，使 ListTools 的发现逻辑与传输方式解耦。
type Transport interface {
	// Send 发送一条 JSON-RPC 消息并等待响应。
	// 对于通知类消息（无 id），response 可能为 nil。
	Send(ctx context.Context, request json.RawMessage) (response json.RawMessage, err error)

	// Close 释放传输层资源。
	Close() error
}

// Handshaked 由在建立连接时就已完成 MCP initialize 握手的 Transport 实现。
// ListToolsViaTransport 对其跳过 initialize / initialized，直接 tools/list，
// 避免同一会话上重复 initialize（部分 server 会拒绝第二次握手）。
type Handshaked interface {
	// ServerInfo 返回握手时拿到的 serverInfo。
	ServerInfo() (name, version string)
}
