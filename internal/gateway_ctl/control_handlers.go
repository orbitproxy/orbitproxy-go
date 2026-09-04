package gateway_ctl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/gateway_ctl/dispatcher"
	"github.com/orbitproxy/orbitproxy-go/internal/health"
	"github.com/orbitproxy/orbitproxy-go/internal/mcp/discover"
	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcpstdio"
	"github.com/orbitproxy/orbitproxy-go/internal/mcp/preflight"
	"github.com/orbitproxy/orbitproxy-go/internal/proclife"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

func (ctl *Control) registerMsgHandlers() {
	// ServerHello is consumed during dialGateway. Seed session here.
	if ctl.sessionCtx.SessionID != "" {
		ctl.session.SetSession(ctl.sessionCtx.SessionID)
	}

	ctl.msgDispatcher.RegisterHandler(&wire.ServerHello{}, ctl.wrapHandler(ctl.handleServerHello))
	ctl.msgDispatcher.RegisterHandler(&wire.NewEndpoint{}, ctl.wrapHandler(ctl.handleNewEndpoint))
	ctl.msgDispatcher.RegisterHandler(&wire.CloseEndpoint{}, ctl.wrapHandler(ctl.handleCloseEndpoint))
	ctl.msgDispatcher.RegisterHandler(&wire.DiscoverTools{}, dispatcher.AsyncHandler(ctl.handleDiscoverTools))
	ctl.msgDispatcher.RegisterHandler(&wire.Preflight{}, dispatcher.AsyncHandler(ctl.handlePreflight))
	ctl.msgDispatcher.RegisterHandler(&wire.ExecPreflight{}, dispatcher.AsyncHandler(ctl.handleExecPreflight))
	ctl.msgDispatcher.RegisterHandler(&wire.Disconnect{}, ctl.wrapHandler(ctl.handleDisconnect))
	ctl.msgDispatcher.RegisterHandler(&wire.Stop{}, dispatcher.AsyncHandler(ctl.handleStop))
	ctl.msgDispatcher.RegisterHandler(&wire.Restart{}, dispatcher.AsyncHandler(ctl.handleRestart))
	ctl.msgDispatcher.RegisterHandler(&wire.Update{}, dispatcher.AsyncHandler(ctl.handleUpdate))
	ctl.msgDispatcher.SetDefaultHandler(func(m wire.Message) {
		ctl.logger.Debug("ignored edge control message", "type", string(m.MsgType()))
	})
}

func (ctl *Control) handleServerHello(m wire.Message) error {
	hello, ok := m.(*wire.ServerHello)
	if !ok {
		return fmt.Errorf("unexpected server hello type %T", m)
	}
	ctl.session.SetSession(hello.SessionID)
	ctl.logger.Info("edge session established",
		"machine_key", ctl.sessionCtx.ConnConfig.MachineKey,
		"edge_id", hello.EdgeID,
		"session_id", hello.SessionID,
	)
	return nil
}

func (ctl *Control) handleNewEndpoint(m wire.Message) error {
	in, ok := m.(*wire.NewEndpoint)
	if !ok {
		return nil
	}
	ctl.endpointMgr.HandleNewEndpoint(ctl.logger, in)
	if in.Error == "" && in.DiscoverTools != nil {
		opts := *in.DiscoverTools
		if strings.TrimSpace(opts.RequestID) == "" {
			ctl.logger.Warn("new_endpoint.discover_tools missing request_id",
				"endpoint_id", in.EndpointID,
			)
			return nil
		}
		go ctl.runDiscoverTools(&wire.DiscoverTools{
			RequestID:      opts.RequestID,
			EndpointID:     in.EndpointID,
			TimeoutSeconds: opts.TimeoutSeconds,
		})
	}
	return nil
}

func (ctl *Control) handleCloseEndpoint(m wire.Message) error {
	in, ok := m.(*wire.CloseEndpoint)
	if !ok {
		return nil
	}
	ctl.endpointMgr.HandleCloseEndpoint(ctl.logger, in)
	return nil
}

func (ctl *Control) handleDiscoverTools(m wire.Message) {
	in, ok := m.(*wire.DiscoverTools)
	if !ok || in == nil {
		return
	}
	ctl.runDiscoverTools(in)
}

func (ctl *Control) runDiscoverTools(in *wire.DiscoverTools) {
	requestID := strings.TrimSpace(in.RequestID)
	endpointID := strings.TrimSpace(in.EndpointID)
	if requestID == "" || endpointID == "" {
		ctl.logger.Warn("discover_tools missing request_id or endpoint_id",
			"request_id", requestID,
			"endpoint_id", endpointID,
		)
		return
	}

	result := wire.DiscoverToolsResult{
		RequestID:  requestID,
		EndpointID: endpointID,
		Status:     "succeeded",
	}

	rt, ok := ctl.endpointMgr.Get(endpointID)
	if !ok || rt.Config() == nil || len(rt.Config().LocalServicePayload) == 0 {
		result.Status = "failed"
		result.ErrorCode = "endpoint_not_found"
		result.ErrorMessage = "endpoint is not installed"
		if sendErr := ctl.msgDispatcher.Send(result); sendErr != nil {
			ctl.logger.Warn("send discover_tools_result failed",
				"request_id", requestID,
				"err", sendErr,
			)
		}
		return
	}

	timeoutSeconds := in.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	ctx, cancel := context.WithTimeout(ctl.ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	discovered, err := discover.ListToolsFromPayload(
		ctx,
		rt.Config().LocalServicePayload,
		timeoutSeconds,
		endpointID,
		ctl.machineDir(),
		ctl.reportEndpointDiagnostic,
	)
	if err != nil {
		result.Status = "failed"
		result.ErrorCode, result.ErrorMessage = classifyDiscoverError(err)
		ctl.logger.Warn("discover tools failed",
			"request_id", requestID,
			"endpoint_id", endpointID,
			"err", err,
		)
	} else {
		result.Truncated = discovered.Truncated
		result.ServerName = discovered.ServerName
		result.ServerVersion = discovered.ServerVersion
		result.Tools = make([]wire.DiscoveredTool, 0, len(discovered.Tools))
		for _, tool := range discovered.Tools {
			result.Tools = append(result.Tools, wire.DiscoveredTool{
				Name:            tool.Name,
				Description:     tool.Description,
				InputSchemaJSON: tool.InputSchema,
			})
		}
	}

	if sendErr := ctl.msgDispatcher.Send(result); sendErr != nil {
		ctl.logger.Warn("send discover_tools_result failed",
			"request_id", requestID,
			"err", sendErr,
		)
	}
}

func classifyDiscoverError(err error) (code, message string) {
	return preflight.ClassifyError(err)
}

func (ctl *Control) handlePreflight(m wire.Message) {
	in, ok := m.(*wire.Preflight)
	if !ok || in == nil {
		return
	}
	ctl.runPreflight(in)
}

func (ctl *Control) runPreflight(in *wire.Preflight) {
	requestID := strings.TrimSpace(in.RequestID)
	endpointID := strings.TrimSpace(in.EndpointID)
	if requestID == "" || endpointID == "" {
		ctl.logger.Warn("preflight missing request_id or endpoint_id",
			"request_id", requestID,
			"endpoint_id", endpointID,
		)
		return
	}

	result := wire.PreflightResult{
		RequestID:  requestID,
		EndpointID: endpointID,
		Status:     "succeeded",
	}

	rt, ok := ctl.endpointMgr.Get(endpointID)
	if !ok || rt.Config() == nil || len(rt.Config().LocalServicePayload) == 0 {
		result.Status = "failed"
		result.ErrorCode = "endpoint_not_found"
		result.ErrorMessage = "endpoint is not installed"
		if sendErr := ctl.msgDispatcher.Send(result); sendErr != nil {
			ctl.logger.Warn("send preflight_result failed",
				"request_id", requestID,
				"err", sendErr,
			)
		}
		return
	}

	timeoutSeconds := in.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	ctx, cancel := context.WithTimeout(ctl.ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	out, err := preflight.Run(ctx, preflight.RunOptions{
		Payload:        rt.Config().LocalServicePayload,
		TimeoutSeconds: timeoutSeconds,
		EndpointID:     endpointID,
		MachineDir:     ctl.machineDir(),
		OnDiag:         ctl.reportEndpointDiagnostic,
	})
	if err != nil {
		result.Status = "failed"
		result.ErrorCode, result.ErrorMessage = preflight.ClassifyError(err)
		ctl.logger.Warn("preflight failed",
			"request_id", requestID,
			"endpoint_id", endpointID,
			"err", err,
		)
	} else {
		result.Truncated = out.Truncated
		result.ServerName = out.ServerName
		result.ServerVersion = out.ServerVersion
		result.ResolvedPath = out.ResolvedPath
		result.Tools = make([]wire.DiscoveredTool, 0, len(out.Tools))
		for _, tool := range out.Tools {
			result.Tools = append(result.Tools, wire.DiscoveredTool{
				Name:            tool.Name,
				Description:     tool.Description,
				InputSchemaJSON: tool.InputSchema,
			})
		}
	}

	if sendErr := ctl.msgDispatcher.Send(result); sendErr != nil {
		ctl.logger.Warn("send preflight_result failed",
			"request_id", requestID,
			"err", sendErr,
		)
	}
}

// reportEndpointDiagnostic handles a passive health observation from mcpstdio
// (process exit / handshake failure). Primary effect: mark endpoint unhealthy.
// EndpointDiagnostic is still sent as optional evidence for richer stderr/exit details.
func (ctl *Control) reportEndpointDiagnostic(diag mcpstdio.Diagnostic) {
	if ctl == nil {
		return
	}

	obs := health.Observation{
		Healthy:    false,
		Code:       diag.Code,
		Message:    diag.Message,
		ExitCode:   diag.ExitCode,
		StderrTail: string(diag.StderrTail),
		Source:     "process",
		ObservedAt: diag.OccurredAt,
	}
	if obs.ObservedAt.IsZero() {
		obs.ObservedAt = time.Now()
	}
	if rt, ok := ctl.endpointMgr.Get(diag.EndpointID); ok && rt != nil {
		rt.MarkUnhealthy(obs)
	}

	if ctl.msgDispatcher == nil {
		return
	}
	msg := wire.EndpointDiagnostic{
		EndpointID:    diag.EndpointID,
		Level:         "error",
		ErrorCode:     diag.Code,
		Message:       diag.Message,
		StderrTail:    string(diag.StderrTail),
		OccurredCount: diag.OccurCount,
	}
	if err := ctl.msgDispatcher.Send(msg); err != nil {
		ctl.logger.Warn("send endpoint_diagnostic failed",
			"endpoint_id", diag.EndpointID,
			"error_code", diag.Code,
			"err", err,
		)
	}
}

func (ctl *Control) handleExecPreflight(m wire.Message) {
	in, ok := m.(*wire.ExecPreflight)
	if !ok || in == nil {
		return
	}

	requestID := strings.TrimSpace(in.RequestID)
	endpointID := strings.TrimSpace(in.EndpointID)
	if requestID == "" || endpointID == "" {
		ctl.logger.Warn("exec_preflight missing request_id or endpoint_id",
			"request_id", requestID,
			"endpoint_id", endpointID,
		)
		return
	}

	pf := preflight.CheckCommand(preflight.CommandConfig{
		Command: in.Command,
		Args:    in.Args,
		WorkDir: in.WorkDir,
	})
	if pf == nil {
		pf = &preflight.CommandResult{
			ErrorCode:    mcpstdio.CodeInternal,
			ErrorMessage: "preflight returned nil",
		}
	}

	result := wire.ExecPreflightResult{
		RequestID:    requestID,
		EndpointID:   endpointID,
		ResolvedPath: pf.ResolvedPath,
	}
	if pf.OK {
		result.Status = "succeeded"
		result.CommandFound = true
		result.CommandExecutable = true
		result.WorkdirExists = true
	} else {
		result.Status = "failed"
		result.ErrorCode = pf.ErrorCode
		result.ErrorMessage = pf.ErrorMessage
		switch pf.ErrorCode {
		case preflight.CodeCommandNotFound:
			// all false
		case preflight.CodePackageNotInstalled:
			result.CommandFound = true
			result.CommandExecutable = true
		case preflight.CodeCommandNotExecutable:
			result.CommandFound = true
		case preflight.CodeSpawnFailed:
			result.CommandFound = true
			result.CommandExecutable = true
		default:
			if pf.ResolvedPath != "" {
				result.CommandFound = true
				result.CommandExecutable = true
			}
		}
	}

	if sendErr := ctl.msgDispatcher.Send(result); sendErr != nil {
		ctl.logger.Warn("send exec_preflight_result failed",
			"request_id", requestID,
			"err", sendErr,
		)
	}
}

func (ctl *Control) handleDisconnect(m wire.Message) error {
	in, ok := m.(*wire.Disconnect)
	if !ok {
		return nil
	}
	reason := in.Reason
	if in.ReasonText != "" {
		reason = in.ReasonText
	}
	if reason == "" {
		reason = "edge disconnected session"
	}
	ctl.markPermanentDisconnect(reason)
	ctl.logger.Warn("edge requested permanent disconnect",
		"machine_key", ctl.sessionCtx.ConnConfig.MachineKey,
		"reason", in.Reason,
		"reason_text", in.ReasonText,
	)
	ctl.closeSession()
	return nil
}

func (ctl *Control) handleStop(m wire.Message) {
	in, ok := m.(*wire.Stop)
	if !ok || in == nil {
		return
	}
	ctl.ackLifecycle(in.RequestID, "succeeded", "", "", "")
	time.Sleep(300 * time.Millisecond)
	reason := in.Reason
	if reason == "" {
		reason = "stopped"
	}
	ctl.markPermanentDisconnect(reason)
	if in.ReasonText != "" {
		ctl.logger.Info("edge requested stop", "reason", reason, "reason_text", in.ReasonText)
	} else {
		ctl.logger.Info("edge requested stop", "reason", reason)
	}
	ctl.closeSession()
}

func (ctl *Control) handleRestart(m wire.Message) {
	in, ok := m.(*wire.Restart)
	if !ok || in == nil {
		return
	}
	ctl.ackLifecycle(in.RequestID, "succeeded", "", "", "")
	time.Sleep(300 * time.Millisecond)
	ctl.logger.Info("edge requested restart")
	if err := proclife.Restart(); err != nil {
		ctl.logger.Error("restart failed", "err", err)
	}
}

func (ctl *Control) handleUpdate(m wire.Message) {
	in, ok := m.(*wire.Update)
	if !ok || in == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctl.ctx, 2*time.Minute)
	defer cancel()
	if err := proclife.InstallFromURL(ctx, in.DownloadURL); err != nil {
		ctl.logger.Error("update failed", "err", err)
		ctl.ackLifecycle(in.RequestID, "failed", "internal", err.Error(), "")
		return
	}
	ctl.ackLifecycle(in.RequestID, "succeeded", "", "", in.Version)
	time.Sleep(300 * time.Millisecond)
	ctl.logger.Info("update installed, restarting", "version", in.Version, "artifact", in.Artifact)
	if err := proclife.Restart(); err != nil {
		ctl.logger.Error("restart after update failed", "err", err)
	}
}

func (ctl *Control) ackLifecycle(requestID, status, errorCode, errorMessage, version string) {
	if strings.TrimSpace(requestID) == "" {
		return
	}
	if err := ctl.msgDispatcher.Send(wire.LifecycleResult{
		RequestID:    requestID,
		Status:       status,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
		Version:      version,
	}); err != nil {
		ctl.logger.Warn("send lifecycle result failed",
			"request_id", requestID,
			"err", err,
		)
	}
}

// acceptWorkStreams 接收 Edge 主动打开的 yamux work stream，读取 StartWorkConn 后路由到对应 endpoint。
func (ctl *Control) acceptWorkStreams(ctx context.Context) {
	yamuxSession := ctl.sessionCtx.Yamux
	if yamuxSession == nil {
		return
	}
	for {
		stream, err := yamuxSession.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			ctl.logger.Debug("accept yamux work stream ended", "err", err)
			return
		}
		go func() {
			msg, err := wire.ReadMsg(stream)
			if err != nil {
				_ = stream.Close()
				ctl.logger.Warn("read work stream message failed",
					"stage", "start_work_recv",
					"err", err,
				)
				return
			}
			start, ok := msg.(*wire.StartWorkConn)
			if !ok {
				_ = stream.Close()
				ctl.logger.Warn("work stream 首条消息不是 start_work_conn",
					"stage", "start_work_recv",
					"type", string(msg.MsgType()),
				)
				return
			}
			// Info：证明 Edge 的 StartWorkConn 已到达本机（与 Edge stage 对表用）。
			ctl.logger.Info("start_work_conn received",
				"stage", "start_work_recv",
				"proxy_id", start.ProxyID,
				"endpoint_id", start.EndpointID,
			)
			if !ctl.endpointMgr.HandleWorkConn(ctl.logger, stream, start) {
				_ = stream.Close()
			}
		}()
	}
}

func (ctl *Control) wrapHandler(handler func(wire.Message) error) func(wire.Message) {
	return func(m wire.Message) {
		if err := handler(m); err != nil {
			ctl.logger.Warn("handle edge msg failed, skipping",
				"machine_key", ctl.sessionCtx.ConnConfig.MachineKey,
				"type", string(m.MsgType()),
				"err", err,
			)
		}
	}
}
