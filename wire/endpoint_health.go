package wire

// EndpointHealth reports local backend health to edge.
// Active probe failures and passive observations (process death, dial errors)
// both surface here — one health model.
type EndpointHealth struct {
	EndpointID string `json:"endpoint_id"`
	ProxyID    string `json:"proxy_id"`
	Healthy    bool   `json:"healthy"`
	Reason     string `json:"reason,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Ts         int64  `json:"ts"`
}

func (EndpointHealth) MsgType() MessageType { return MessageTypeEndpointHealth }
