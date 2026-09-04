package endpoint

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Delivery modes for how work connections are handed to the application.
const (
	DeliveryForward   = "forward"
	DeliveryInProcess = "in_process"
	DeliveryExec      = "exec" // stdio MCP server，由 SDK 内 execbridge 桥接
)

type localServicePayload struct {
	LocalAddr string `json:"localAddr"`
	Delivery  string `json:"delivery"`
	Command   string `json:"command"`
}

func parseLocalServicePayload(raw json.RawMessage) localServicePayload {
	var p localServicePayload
	if len(raw) == 0 {
		return p
	}
	_ = json.Unmarshal(raw, &p)
	p.LocalAddr = strings.TrimSpace(p.LocalAddr)
	p.Delivery = strings.TrimSpace(p.Delivery)
	return p
}

// LocalAddrFromPayload returns localAddr from NewEndpoint.local_service_payload.
// 与宽松的 parseLocalServicePayload 不同，这里保留解码错误用于故障定位。
func LocalAddrFromPayload(raw json.RawMessage) (string, error) {
	var p localServicePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("unmarshal local_service_payload: %w", err)
	}
	p.LocalAddr = strings.TrimSpace(p.LocalAddr)
	if p.LocalAddr == "" {
		return "", fmt.Errorf("payload.localAddr is required")
	}
	return p.LocalAddr, nil
}

// ResolveDelivery returns the delivery mode from a NewEndpoint payload.
// 判定优先级：显式 delivery > 有 command 则 exec > 有 localAddr 则 forward > 否则 in_process
func ResolveDelivery(raw json.RawMessage) string {
	p := parseLocalServicePayload(raw)
	switch strings.ToLower(p.Delivery) {
	case DeliveryExec:
		return DeliveryExec
	case DeliveryInProcess, "listen", "in-process", "inprocess":
		return DeliveryInProcess
	case DeliveryForward, "dial":
		return DeliveryForward
	}
	if p.Command != "" {
		return DeliveryExec
	}
	if p.LocalAddr != "" {
		return DeliveryForward
	}
	return DeliveryInProcess
}
