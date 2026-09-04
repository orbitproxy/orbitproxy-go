package wire

// Stop asks the process to exit and stop reconnecting.
type Stop struct {
	RequestID  string `json:"request_id"`
	Reason     string `json:"reason,omitempty"`
	ReasonText string `json:"reason_text,omitempty"`
}

func (Stop) MsgType() MessageType { return MessageTypeStop }

// Restart asks the process to replace itself in place.
type Restart struct {
	RequestID string `json:"request_id"`
}

func (Restart) MsgType() MessageType { return MessageTypeRestart }

// Update asks the process to install a new binary and restart.
type Update struct {
	RequestID   string `json:"request_id"`
	DownloadURL string `json:"download_url"`
	Version     string `json:"version,omitempty"`
	Artifact    string `json:"artifact,omitempty"`
}

func (Update) MsgType() MessageType { return MessageTypeUpdate }

// LifecycleResult acks stop/restart/update.
type LifecycleResult struct {
	RequestID    string `json:"request_id"`
	Status       string `json:"status"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	Version      string `json:"version,omitempty"`
}

func (LifecycleResult) MsgType() MessageType { return MessageTypeLifecycleResult }
