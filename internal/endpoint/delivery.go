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
)

type localServicePayload struct {
	LocalAddr string `json:"localAddr"`
	Delivery  string `json:"delivery"`
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
func LocalAddrFromPayload(raw json.RawMessage) (string, error) {
	p := parseLocalServicePayload(raw)
	if p.LocalAddr == "" {
		return "", fmt.Errorf("localAddr missing")
	}
	return p.LocalAddr, nil
}

// ResolveDelivery returns forward or in_process from a NewEndpoint payload.
func ResolveDelivery(raw json.RawMessage) string {
	p := parseLocalServicePayload(raw)
	switch strings.ToLower(p.Delivery) {
	case DeliveryInProcess, "listen", "in-process", "inprocess":
		return DeliveryInProcess
	case DeliveryForward, "dial":
		return DeliveryForward
	}
	if p.LocalAddr != "" {
		return DeliveryForward
	}
	// No localAddr and no explicit delivery: treat as in-process so Listen
	// endpoints can omit localAddr entirely.
	return DeliveryInProcess
}
