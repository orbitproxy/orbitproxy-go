package wire

// ReqWorkConn asks the client to open a new yamux work stream.
type ReqWorkConn struct{}

func (ReqWorkConn) MsgType() MessageType { return MessageTypeReqWorkConn }

// NewWorkConn is sent on a freshly opened work stream.
type NewWorkConn struct {
	SessionID string `json:"session_id"`
}

func (NewWorkConn) MsgType() MessageType { return MessageTypeNewWorkConn }

// StartWorkConn tells the client which endpoint to dial locally.
type StartWorkConn struct {
	ProxyID      string `json:"proxy_id"`
	EndpointID   string `json:"endpoint_id"`
	RouteVersion string `json:"route_version"`
	SourceAddr   string `json:"source_addr"`
	Error        string `json:"error,omitempty"`
}

func (StartWorkConn) MsgType() MessageType { return MessageTypeStartWorkConn }
