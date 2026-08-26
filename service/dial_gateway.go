package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/gateway_ctl"
	"github.com/orbitproxy/orbitproxy-go/internal/yamuxcfg"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

const defaultHelloTimeout = 15 * time.Second

// dialGateway dials edge: TCP → TLS → yamux → signed ClientHello → ServerHello.
func (svr *Service) dialGateway(ctx context.Context) (*gateway_ctl.SessionContext, error) {
	dialer := &net.Dialer{
		// TCP probes help NAT/proxy paths stay alive.
		KeepAliveConfig: net.KeepAliveConfig{
			Enable: true,
			Idle:   30 * time.Second,
		},
	}
	rawConn, err := dialer.DialContext(ctx, "tcp", svr.cfg.EdgeAddr)
	if err != nil {
		return nil, fmt.Errorf("dial edge %s: %w", svr.cfg.EdgeAddr, err)
	}

	host, _, err := net.SplitHostPort(svr.cfg.EdgeAddr)
	if err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("parse edge_addr %q: %w", svr.cfg.EdgeAddr, err)
	}
	tlsCfg := &tls.Config{
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureSkipVerifyEnabled(),
	}
	if !tlsCfg.InsecureSkipVerify {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(strings.TrimSpace(svr.cfg.MachineCACert))) {
			_ = rawConn.Close()
			return nil, fmt.Errorf("invalid machine CA certificate")
		}
		tlsCfg.RootCAs = pool
	}
	tlsConn := tls.Client(rawConn, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("tls handshake with edge: %w", err)
	}

	yamuxSession, err := yamuxcfg.Client(tlsConn)
	if err != nil {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("open yamux session: %w", err)
	}

	controlStream, err := yamuxSession.Open()
	if err != nil {
		_ = yamuxSession.Close()
		return nil, fmt.Errorf("open yamux control stream: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = controlStream.Close()
			_ = yamuxSession.Close()
		}
	}()

	hello, err := wire.NewClientHello(svr.cfg.MachineKey, svr.cfg.SoftVersion)
	if err != nil {
		return nil, err
	}
	hello, err = wire.SignClientHello(svr.cfg.PrivateKeyPEM, hello)
	if err != nil {
		return nil, err
	}
	if err := wire.WriteMsg(controlStream, hello); err != nil {
		return nil, fmt.Errorf("send client hello: %w", err)
	}

	svr.logger.Info("edge connected, waiting for server hello",
		"machine_key", svr.cfg.MachineKey,
		"edge_addr", svr.cfg.EdgeAddr,
	)

	helloResp, err := waitServerHello(ctx, controlStream)
	if err != nil {
		return nil, err
	}

	success = true
	return &gateway_ctl.SessionContext{
		ConnConfig: gateway_ctl.ConnConfig{
			EdgeAddr:      svr.cfg.EdgeAddr,
			MachineKey:     svr.cfg.MachineKey,
			PrivateKeyPEM: svr.cfg.PrivateKeyPEM,
			SoftVersion:   svr.cfg.SoftVersion,
		},
		Yamux:         yamuxSession,
		ControlStream: controlStream,
		EdgeID:        helloResp.EdgeID,
		SessionID:     helloResp.SessionID,
	}, nil
}

func waitServerHello(ctx context.Context, controlStream net.Conn) (*wire.ServerHello, error) {
	deadline := defaultHelloTimeout
	if d, ok := ctx.Deadline(); ok {
		if remaining := time.Until(d); remaining > 0 && remaining < deadline {
			deadline = remaining
		}
	}
	_ = controlStream.SetReadDeadline(time.Now().Add(deadline))
	defer func() { _ = controlStream.SetReadDeadline(time.Time{}) }()

	type result struct {
		msg wire.Message
		err error
	}
	ch := make(chan result, 1)
	go func() {
		m, err := wire.ReadMsg(controlStream)
		ch <- result{m, err}
	}()

	select {
	case <-ctx.Done():
		_ = controlStream.Close()
		return nil, fmt.Errorf("wait server hello: %w", ctx.Err())
	case res := <-ch:
		if res.err != nil {
			return nil, fmt.Errorf("read server hello: %w", res.err)
		}
		switch m := res.msg.(type) {
		case *wire.ServerHello:
			if m.SessionID == "" {
				return nil, fmt.Errorf("server hello missing session_id")
			}
			return m, nil
		case *wire.Disconnect:
			reason := m.Reason
			if m.ReasonText != "" {
				reason = m.ReasonText
			}
			if reason == "" {
				reason = "edge disconnected"
			}
			return nil, fmt.Errorf("edge rejected session: %s", reason)
		default:
			return nil, fmt.Errorf("unexpected first control message %T", res.msg)
		}
	}
}
