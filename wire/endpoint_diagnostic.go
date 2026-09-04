package wire

// EndpointDiagnostic is a client→edge diagnostic event ('G').
type EndpointDiagnostic struct {
	EndpointID    string `json:"endpoint_id"`
	Level         string `json:"level,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
	Message       string `json:"message,omitempty"`
	StderrTail    string `json:"stderr_tail,omitempty"`
	OccurredCount int    `json:"occurred_count,omitempty"`
}

func (EndpointDiagnostic) MsgType() MessageType { return MessageTypeEndpointDiagnostic }
