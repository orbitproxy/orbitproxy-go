package wire

import "encoding/json"

// DiscoverTools asks the client to run tools/list against an already-installed
// endpoint ('D'). Create-sync uses NewEndpoint.DiscoverTools instead.
type DiscoverTools struct {
	RequestID      string `json:"request_id"`
	EndpointID     string `json:"endpoint_id"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

func (DiscoverTools) MsgType() MessageType { return MessageTypeDiscoverTools }

// DiscoveredTool is one tools/list entry.
type DiscoveredTool struct {
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	InputSchemaJSON json.RawMessage `json:"input_schema_json,omitempty"`
}

// DiscoverToolsResult is the client→edge discover reply ('F').
type DiscoverToolsResult struct {
	RequestID     string           `json:"request_id"`
	EndpointID    string           `json:"endpoint_id,omitempty"`
	Status        string           `json:"status"`
	ErrorCode     string           `json:"error_code,omitempty"`
	ErrorMessage  string           `json:"error_message,omitempty"`
	Truncated     bool             `json:"truncated"`
	ServerName    string           `json:"server_name,omitempty"`
	ServerVersion string           `json:"server_version,omitempty"`
	Tools         []DiscoveredTool `json:"tools,omitempty"`
}

func (DiscoverToolsResult) MsgType() MessageType { return MessageTypeDiscoverToolsResult }
