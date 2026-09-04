package wire

// StartWorkConn 由 Edge 在新开的 yamux work stream 上发送，告知 Machine 要路由到哪个 endpoint。
type StartWorkConn struct {
	ProxyID      string `json:"proxy_id"`
	EndpointID   string `json:"endpoint_id"`
	RouteVersion string `json:"route_version"`
	SourceAddr   string `json:"source_addr"`
	Error        string `json:"error,omitempty"`
}

func (StartWorkConn) MsgType() MessageType { return MessageTypeStartWorkConn }
