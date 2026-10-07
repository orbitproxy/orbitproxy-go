package preflight

import (
	"context"
	"encoding/json"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/discover"
	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcperr"
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
	// SkipProtocol 只跑本地环境层（CheckCommand + 包是否已安装 + 运行前提），不拉进程、不 tools/list。
	// 由调用方显式指定；预检不按 connector 家族推导。
	SkipProtocol bool
}

// Run executes create/sync preflight.
//
// exec 型：CheckCommand → 运行前提（环境文件）→ spawn + 握手 + 存活窗 → tools/list（跟随 nextCursor）。
// forward 型：HTTP 连接 → 握手 → tools/list。
// 每一阶段失败都返回 *mcperr.Error，调用方用 ClassifyError 取稳定错误码。
func Run(ctx context.Context, opts RunOptions) (*RunResult, error) {
	if len(opts.Payload) == 0 {
		return nil, mcperr.New(mcperr.StageCommand, mcperr.CodeInternal, "endpoint payload is empty")
	}
	timeout := time.Duration(opts.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	execCfg, err := mcpstdio.ParseExecPayload(opts.Payload)
	if err == nil && execCfg != nil {
		return runExec(ctx, opts, *execCfg)
	}

	if opts.SkipProtocol {
		return &RunResult{}, nil
	}

	localAddr, localPath, _, err := discover.ParseLocalPayload(opts.Payload)
	if err != nil {
		return nil, mcperr.Wrap(mcperr.StageDial, mcperr.CodeInternal, "", err)
	}
	httpTransport := discover.NewHTTPTransport(
		localAddr,
		localPath,
		timeout,
		discover.IsPlaywrightPayload(opts.Payload),
	)
	defer httpTransport.Close()

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

func runExec(ctx context.Context, opts RunOptions, execCfg mcpstdio.SpawnConfig) (*RunResult, error) {
	cmd := CheckCommand(CommandConfig{
		Command: execCfg.Command,
		Args:    execCfg.Args,
		WorkDir: execCfg.WorkDir,
	})
	if cmd == nil {
		return nil, mcperr.New(mcperr.StageCommand, mcperr.CodeInternal, "command check returned nil")
	}
	if !cmd.OK {
		return nil, cmd.Err()
	}

	if execCfg.RequiresEnvFile && !mcpstdio.EndpointEnvFileReady(opts.MachineDir, opts.EndpointID) {
		return nil, mcpstdio.EnvFileMissingError(opts.MachineDir, opts.EndpointID)
	}

	if opts.SkipProtocol {
		return &RunResult{ResolvedPath: cmd.ResolvedPath}, nil
	}

	transport, err := discover.NewStdioTransport(execCfg, opts.EndpointID, opts.MachineDir, opts.OnDiag)
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

// ClassifyError maps a preflight error to a stable error code for wire/CP.
// 委托 mcperr.Classify：按类型取码，不做文本匹配。
func ClassifyError(err error) (code, message string) {
	return mcperr.Classify(err)
}
