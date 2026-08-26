package gateway_ctl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/gateway_ctl/dispatcher"
	"github.com/orbitproxy/orbitproxy-go/internal/mcpdiscover"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

func (ctl *Control) registerMsgHandlers() {
	// ServerHello is consumed during dialGateway. Seed session here.
	if ctl.sessionCtx.SessionID != "" {
		ctl.session.SetSession(ctl.sessionCtx.EdgeID, ctl.sessionCtx.SessionID)
	}

	ctl.msgDispatcher.RegisterHandler(&wire.ServerHello{}, ctl.wrapHandler(ctl.handleServerHello))
	ctl.msgDispatcher.RegisterHandler(&wire.ReqWorkConn{}, dispatcher.AsyncHandler(ctl.handleReqWorkConn))
	ctl.msgDispatcher.RegisterHandler(&wire.NewEndpoint{}, ctl.wrapHandler(ctl.handleNewEndpoint))
	ctl.msgDispatcher.RegisterHandler(&wire.CloseEndpoint{}, ctl.wrapHandler(ctl.handleCloseEndpoint))
	ctl.msgDispatcher.RegisterHandler(&wire.DiscoverTools{}, dispatcher.AsyncHandler(ctl.handleDiscoverTools))
	ctl.msgDispatcher.RegisterHandler(&wire.Disconnect{}, ctl.wrapHandler(ctl.handleDisconnect))
	ctl.msgDispatcher.SetDefaultHandler(func(m wire.Message) {
		ctl.logger.Debug("ignored edge control message", "type", string(m.MsgType()))
	})
}

func (ctl *Control) handleServerHello(m wire.Message) error {
	hello, ok := m.(*wire.ServerHello)
	if !ok {
		return fmt.Errorf("unexpected server hello type %T", m)
	}
	ctl.session.SetSession(hello.EdgeID, hello.SessionID)
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

	discovered, err := mcpdiscover.ListToolsFromPayload(ctx, rt.Config().LocalServicePayload, timeoutSeconds)
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
	if err == nil {
		return "internal", "unknown error"
	}
	message = err.Error()
	lower := strings.ToLower(message)
	switch {
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

func (ctl *Control) handleReqWorkConn(m wire.Message) {
	_ = m

	sessionID := ctl.session.SessionID()
	if sessionID == "" {
		ctl.logger.Warn("req_work_conn before server hello, ignoring",
			"machine_key", ctl.sessionCtx.ConnConfig.MachineKey,
		)
		return
	}

	yamuxSession := ctl.sessionCtx.Yamux
	if yamuxSession == nil {
		ctl.logger.Warn("yamux session unavailable",
			"machine_key", ctl.sessionCtx.ConnConfig.MachineKey,
		)
		return
	}

	stream, err := yamuxSession.Open()
	if err != nil {
		ctl.logger.Warn("open yamux work stream failed",
			"machine_key", ctl.sessionCtx.ConnConfig.MachineKey,
			"err", err,
		)
		return
	}

	newWork := wire.NewWorkConn{
		SessionID: sessionID,
	}
	if err := wire.WriteMsg(stream, newWork); err != nil {
		_ = stream.Close()
		ctl.logger.Warn("send new_work_conn failed",
			"machine_key", ctl.sessionCtx.ConnConfig.MachineKey,
			"err", err,
		)
		return
	}

	in, err := wire.ReadMsg(stream)
	if err != nil {
		_ = stream.Close()
		ctl.logger.Debug("read start_work_conn failed",
			"machine_key", ctl.sessionCtx.ConnConfig.MachineKey,
			"err", err,
		)
		return
	}
	start, ok := in.(*wire.StartWorkConn)
	if !ok {
		_ = stream.Close()
		ctl.logger.Debug("work stream first message is not start_work_conn",
			"machine_key", ctl.sessionCtx.ConnConfig.MachineKey,
			"type", string(in.MsgType()),
		)
		return
	}

	// In-process Listen retains the stream for Accept(); forward mode closes it in Join.
	if !ctl.endpointMgr.HandleWorkConn(ctl.logger, stream, start) {
		_ = stream.Close()
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
