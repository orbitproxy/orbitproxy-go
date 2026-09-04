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
