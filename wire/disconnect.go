package wire

// Disconnect asks the client to stop reconnecting.
type Disconnect struct {
	Reason     string `json:"reason"`
	ReasonText string `json:"reason_text,omitempty"`
}

func (Disconnect) MsgType() MessageType { return MessageTypeDisconnect }
