package wire

import "encoding/json"

// DiscoverToolsOptions asks the client to run tools/list after NewEndpoint install
// and reply with DiscoverToolsResult (same shape as a standalone DiscoverTools).
type DiscoverToolsOptions struct {
	RequestID      string `json:"request_id"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// NewEndpoint notifies the client that an endpoint backend should start.
// Optional DiscoverTools runs tools/list after install and sends DiscoverToolsResult.
type NewEndpoint struct {
	EndpointID            string                `json:"endpoint_id"`
	ProxyID               string                `json:"proxy_id"`
	ProxyType             string                `json:"proxy_type"`
	Protocol              string                `json:"protocol"`
	LocalServicePayload   json.RawMessage       `json:"local_service_payload,omitempty"`
	HealthEnabled         bool                  `json:"health_enabled"`
	HealthIntervalSeconds int                   `json:"health_interval_seconds"`
	HealthTimeoutSeconds  int                   `json:"health_timeout_seconds"`
	HealthMaxFailed       int                   `json:"health_max_failed"`
	DiscoverTools         *DiscoverToolsOptions `json:"discover_tools,omitempty"`
	Error                 string                `json:"error,omitempty"`
}

func (NewEndpoint) MsgType() MessageType { return MessageTypeNewEndpoint }

// CloseEndpoint notifies the client to tear down an endpoint backend.
type CloseEndpoint struct {
	EndpointID string `json:"endpoint_id"`
	ProxyID    string `json:"proxy_id,omitempty"`
}

func (CloseEndpoint) MsgType() MessageType { return MessageTypeCloseEndpoint }
