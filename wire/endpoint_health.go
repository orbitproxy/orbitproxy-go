package wire

// EndpointHealth reports local backend health to edge.
type EndpointHealth struct {
	EndpointID string `json:"endpoint_id"`
	ProxyID    string `json:"proxy_id"`
	Healthy    bool   `json:"healthy"`
	Reason     string `json:"reason,omitempty"`
	Ts         int64  `json:"ts"`
}

func (EndpointHealth) MsgType() MessageType { return MessageTypeEndpointHealth }
