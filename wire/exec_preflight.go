package wire

// ExecPreflight asks the client to run command preflight for an endpoint ('P').
type ExecPreflight struct {
	RequestID  string   `json:"request_id"`
	EndpointID string   `json:"endpoint_id"`
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	WorkDir    string   `json:"work_dir,omitempty"`
}

func (ExecPreflight) MsgType() MessageType { return MessageTypeExecPreflight }

// ExecPreflightResult is the client→edge preflight reply ('Q').
type ExecPreflightResult struct {
	RequestID         string `json:"request_id"`
	EndpointID        string `json:"endpoint_id,omitempty"`
	Status            string `json:"status"`
	ErrorCode         string `json:"error_code,omitempty"`
	ErrorMessage      string `json:"error_message,omitempty"`
	CommandFound      bool   `json:"command_found"`
	CommandExecutable bool   `json:"command_executable"`
	WorkdirExists     bool   `json:"workdir_exists"`
	ResolvedPath      string `json:"resolved_path,omitempty"`
}

func (ExecPreflightResult) MsgType() MessageType { return MessageTypeExecPreflightResult }
