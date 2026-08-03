package wire

// ClientHello is the first control message from machine/SDK to edge.
type ClientHello struct {
	ClientKey     string `json:"client_key"`
	SoftVersion   string `json:"soft_version"`
	Timestamp     int64  `json:"timestamp"`
	Nonce         string `json:"nonce"`
	AuthSignature string `json:"auth_signature"`
}

func (ClientHello) MsgType() MessageType { return MessageTypeClientHello }

// ServerHello acknowledges a successful ClientHello.
type ServerHello struct {
	EdgeID    string `json:"edge_id"`
	SessionID string `json:"session_id"`
}

func (ServerHello) MsgType() MessageType { return MessageTypeServerHello }
