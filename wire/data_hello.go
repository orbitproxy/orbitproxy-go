package wire

// DataHello is the handshake on stream 0 of a data session.
// This build registers it and does not send it on the control session.
type DataHello struct {
	SessionID     string `json:"session_id"`
	MachineKey    string `json:"machine_key"`
	Index         int    `json:"index"`
	Timestamp     int64  `json:"timestamp"`
	Nonce         string `json:"nonce"`
	AuthSignature string `json:"auth_signature"`
}

func (DataHello) MsgType() MessageType { return MessageTypeDataHello }

// DataHelloAck acknowledges a DataHello. The payload is empty.
type DataHelloAck struct{}

func (DataHelloAck) MsgType() MessageType { return MessageTypeDataHelloAck }
