package preflight

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/discover"
	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcpstdio"
)

// RunResult is the outcome of a full MCP preflight (includes tools for CP overwrite).
type RunResult struct {
	Tools         []discover.Tool
	Truncated     bool
	ServerName    string
	ServerVersion string
	ResolvedPath  string
}

// RunOptions configures a full preflight for one endpoint.
type RunOptions struct {
	Payload        json.RawMessage
	TimeoutSeconds int
	EndpointID     string
	MachineDir     string
	OnDiag         mcpstdio.DiagnosticCallback
	// SkipProtocol 官方路径：只跑本地环境层（CheckCommand + 包版本），不 tools/list。
	SkipProtocol bool
}

// FamilyKeyFromPayload reads family_key from endpoint local_service_payload.
func FamilyKeyFromPayload(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		FamilyKey string `json:"family_key"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return payload.FamilyKey
}

// Run executes create/sync preflight.
// Official (SkipProtocol or family_key): CheckCommand + package version only.
// Custom: CheckCommand (exec) + tools/list following nextCursor until end or limit.
func Run(ctx context.Context, opts RunOptions) (*RunResult, error) {
	if len(opts.Payload) == 0 {
		return nil, fmt.Errorf("endpoint payload is empty")
	}
	timeout := time.Duration(opts.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if deadline, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	} else {
		_ = deadline
	}

	familyKey := FamilyKeyFromPayload(opts.Payload)
	skipProtocol := opts.SkipProtocol || familyKey != ""

	execCfg, err := mcpstdio.ParseExecPayload(opts.Payload)
	if err == nil && execCfg != nil {
		cmd := CheckCommand(CommandConfig{
			Command: execCfg.Command,
			Args:    execCfg.Args,
			WorkDir: execCfg.WorkDir,
		})
		if cmd == nil || !cmd.OK {
			if cmd == nil {
				return nil, fmt.Errorf("preflight: command check returned nil")
			}
			return nil, fmt.Errorf("preflight: %s: %s", cmd.ErrorCode, cmd.ErrorMessage)
		}

		if familyKey == "mysql" && !mcpstdio.EndpointEnvFileReady(opts.MachineDir, opts.EndpointID) {
			path := mcpstdio.EndpointEnvFilePath(opts.MachineDir, opts.EndpointID)
			if path == "" {
				path = "env/<endpointId>.env"
			}
			return nil, fmt.Errorf("preflight: %s: environment variable file not found: %s", CodeEnvFileMissing, path)
		}

		if skipProtocol {
			return &RunResult{ResolvedPath: cmd.ResolvedPath}, nil
		}

		transport, err := discover.NewStdioTransport(*execCfg, opts.EndpointID, opts.MachineDir, opts.OnDiag)
		if err != nil {
			return nil, err
		}
		defer transport.Close()

		listed, err := discover.ListToolsViaTransport(ctx, transport)
		if err != nil {
			return nil, err
		}
		return &RunResult{
			Tools:         listed.Tools,
			Truncated:     listed.Truncated,
			ServerName:    listed.ServerName,
			ServerVersion: listed.ServerVersion,
			ResolvedPath:  cmd.ResolvedPath,
		}, nil
	}

	if skipProtocol {
		return &RunResult{}, nil
	}

	localAddr, localPath, transportName, err := discover.ParseLocalPayload(opts.Payload)
	if err != nil {
		return nil, err
	}
	httpTransport := discover.NewHTTPTransport(
		localAddr,
		localPath,
		timeout,
		discover.IsPlaywrightPayload(opts.Payload),
	)
	defer httpTransport.Close()
	_ = transportName

	listed, err := discover.ListToolsViaTransport(ctx, httpTransport)
	if err != nil {
		return nil, err
	}
	return &RunResult{
		Tools:         listed.Tools,
		Truncated:     listed.Truncated,
		ServerName:    listed.ServerName,
		ServerVersion: listed.ServerVersion,
	}, nil
}

// ClassifyError maps a preflight error to a stable error code for wire/CP.
func ClassifyError(err error) (code, message string) {
	if err == nil {
		return "internal", "unknown error"
	}
	message = err.Error()
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, CodeEnvFileMissing), strings.Contains(lower, "environment variable file not found"):
		return CodeEnvFileMissing, message
	case strings.Contains(lower, CodePackageVersionMismatch), strings.Contains(lower, "version mismatch"):
		return CodePackageVersionMismatch, message
	case strings.Contains(lower, "package_not_installed"), strings.Contains(lower, "not installed locally"):
		return CodePackageNotInstalled, message
	case strings.Contains(lower, "command_not_found"), strings.Contains(lower, "command not found"), strings.Contains(lower, "command is empty"):
		return CodeCommandNotFound, message
	case strings.Contains(lower, "command_not_executable"), strings.Contains(lower, "not executable"):
		return CodeCommandNotExecutable, message
	case strings.Contains(lower, "dial failed"), strings.Contains(lower, "connection refused"):
		return "dial_failed", message
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "deadline"):
		return "timeout", message
	case strings.Contains(lower, "http "), strings.Contains(lower, "tools/list"), strings.Contains(lower, "decode"):
		return "protocol_error", message
	default:
		return "internal", message
	}
}
