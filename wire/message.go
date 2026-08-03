package wire

// MessageType is the logical control-message name.
type MessageType string

const (
	MessageTypeClientHello         MessageType = "client_hello"
	MessageTypeServerHello         MessageType = "server_hello"
	MessageTypeReqWorkConn         MessageType = "req_work_conn"
	MessageTypeNewWorkConn         MessageType = "new_work_conn"
	MessageTypeStartWorkConn       MessageType = "start_work_conn"
	MessageTypeNewEndpoint         MessageType = "new_endpoint"
	MessageTypeCloseEndpoint       MessageType = "close_endpoint"
	MessageTypeEndpointHealth      MessageType = "endpoint_health"
	MessageTypeDiscoverTools       MessageType = "discover_tools"
	MessageTypeDiscoverToolsResult MessageType = "discover_tools_result"
	MessageTypeDisconnect          MessageType = "disconnect"
)

// Wire type bytes (single-byte discriminators). Stable protocol surface.
const (
	TypeClientHello         byte = 'o'
	TypeServerHello         byte = '1'
	TypeReqWorkConn         byte = 'r'
	TypeNewWorkConn         byte = 'w'
	TypeStartWorkConn       byte = 's'
	TypeNewEndpoint         byte = '3'
	TypeCloseEndpoint       byte = 'e'
	TypeEndpointHealth      byte = 'H'
	TypeDiscoverTools       byte = 'D'
	TypeDiscoverToolsResult byte = 'F'
	TypeDisconnect          byte = 'd'
)

// Message is implemented by every control-plane payload.
type Message interface {
	MsgType() MessageType
}

var byteTypeMap = map[byte]MessageType{
	TypeClientHello:         MessageTypeClientHello,
	TypeServerHello:         MessageTypeServerHello,
	TypeReqWorkConn:         MessageTypeReqWorkConn,
	TypeNewWorkConn:         MessageTypeNewWorkConn,
	TypeStartWorkConn:       MessageTypeStartWorkConn,
	TypeNewEndpoint:         MessageTypeNewEndpoint,
	TypeCloseEndpoint:       MessageTypeCloseEndpoint,
	TypeEndpointHealth:      MessageTypeEndpointHealth,
	TypeDiscoverTools:       MessageTypeDiscoverTools,
	TypeDiscoverToolsResult: MessageTypeDiscoverToolsResult,
	TypeDisconnect:          MessageTypeDisconnect,
}

// MessageTypeFromByte maps a wire type byte to its logical name.
func MessageTypeFromByte(b byte) (MessageType, bool) {
	t, ok := byteTypeMap[b]
	return t, ok
}
