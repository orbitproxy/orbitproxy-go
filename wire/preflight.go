package wire

import "encoding/json"

// Preflight asks the client to run full MCP preflight for an endpoint ('R').
// Unlike ExecPreflight (command-only), this runs tools/list + catalog checks
// and returns tools for control-plane overwrite.
type Preflight struct {
	RequestID      string `json:"request_id"`
	EndpointID     string `json:"endpoint_id"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

func (Preflight) MsgType() MessageType { return MessageTypePreflight }

// PreflightResult is the client→edge full preflight reply ('S').
type PreflightResult struct {
	RequestID     string           `json:"request_id"`
	EndpointID    string           `json:"endpoint_id,omitempty"`
	Status        string           `json:"status"`
	ErrorCode     string           `json:"error_code,omitempty"`
	ErrorMessage  string           `json:"error_message,omitempty"`
	Truncated     bool             `json:"truncated"`
	ServerName    string           `json:"server_name,omitempty"`
	ServerVersion string           `json:"server_version,omitempty"`
	ResolvedPath  string           `json:"resolved_path,omitempty"`
	Tools         []DiscoveredTool `json:"tools,omitempty"`
	// Raw payload passthrough for forward-compatible fields.
	Extra json.RawMessage `json:"extra,omitempty"`
}

func (PreflightResult) MsgType() MessageType { return MessageTypePreflightResult }
