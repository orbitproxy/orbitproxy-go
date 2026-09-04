package wire

// MessageType is the logical control-message name.
type MessageType string

const (
	MessageTypeClientHello         MessageType = "client_hello"
	MessageTypeServerHello         MessageType = "server_hello"
	MessageTypeStartWorkConn       MessageType = "start_work_conn"
	MessageTypeNewEndpoint         MessageType = "new_endpoint"
	MessageTypeCloseEndpoint       MessageType = "close_endpoint"
	MessageTypeEndpointHealth      MessageType = "endpoint_health"
	MessageTypeDiscoverTools       MessageType = "discover_tools"
	MessageTypeDiscoverToolsResult MessageType = "discover_tools_result"
	MessageTypeExecPreflight       MessageType = "exec_preflight"
	MessageTypeExecPreflightResult MessageType = "exec_preflight_result"
	MessageTypePreflight           MessageType = "preflight"
	MessageTypePreflightResult     MessageType = "preflight_result"
	MessageTypeEndpointDiagnostic  MessageType = "endpoint_diagnostic"
	MessageTypeDisconnect          MessageType = "disconnect"
	MessageTypeStop                MessageType = "stop"
	MessageTypeRestart             MessageType = "restart"
	MessageTypeUpdate              MessageType = "update"
	MessageTypeLifecycleResult     MessageType = "lifecycle_result"
)

// Wire type bytes (single-byte discriminators). Stable protocol surface.
const (
	TypeClientHello         byte = 'o'
	TypeServerHello         byte = '1'
	TypeStartWorkConn       byte = 's'
	TypeNewEndpoint         byte = '3'
	TypeCloseEndpoint       byte = 'e'
	TypeEndpointHealth      byte = 'H'
	TypeDiscoverTools       byte = 'D'
	TypeDiscoverToolsResult byte = 'F'
	TypeExecPreflight       byte = 'P'
	TypeExecPreflightResult byte = 'Q'
	TypePreflight           byte = 'R'
	TypePreflightResult     byte = 'S'
	TypeEndpointDiagnostic  byte = 'G'
	TypeDisconnect          byte = 'd'
	TypeStop                byte = 't'
	TypeRestart             byte = 'x'
	TypeUpdate              byte = 'u'
	TypeLifecycleResult     byte = 'L'
)

// Message is implemented by every control-plane payload.
type Message interface {
	MsgType() MessageType
}

var byteTypeMap = map[byte]MessageType{
	TypeClientHello:         MessageTypeClientHello,
	TypeServerHello:         MessageTypeServerHello,
	TypeStartWorkConn:       MessageTypeStartWorkConn,
	TypeNewEndpoint:         MessageTypeNewEndpoint,
	TypeCloseEndpoint:       MessageTypeCloseEndpoint,
	TypeEndpointHealth:      MessageTypeEndpointHealth,
	TypeDiscoverTools:       MessageTypeDiscoverTools,
	TypeDiscoverToolsResult: MessageTypeDiscoverToolsResult,
	TypeExecPreflight:       MessageTypeExecPreflight,
	TypeExecPreflightResult: MessageTypeExecPreflightResult,
	TypePreflight:           MessageTypePreflight,
	TypePreflightResult:     MessageTypePreflightResult,
	TypeEndpointDiagnostic:  MessageTypeEndpointDiagnostic,
	TypeDisconnect:          MessageTypeDisconnect,
	TypeStop:                MessageTypeStop,
	TypeRestart:             MessageTypeRestart,
	TypeUpdate:              MessageTypeUpdate,
	TypeLifecycleResult:     MessageTypeLifecycleResult,
}

// MessageTypeFromByte maps a wire type byte to its logical name.
func MessageTypeFromByte(b byte) (MessageType, bool) {
	t, ok := byteTypeMap[b]
	return t, ok
}
