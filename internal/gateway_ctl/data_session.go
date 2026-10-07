package gateway_ctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/backoff"
	"github.com/orbitproxy/orbitproxy-go/wire"
)

func (ctl *Control) startDataSessionRunners(ctx context.Context) {
	if ctl.sessionCtx == nil || ctl.sessionCtx.DataSessions <= 0 || ctl.sessionCtx.DialEdge == nil {
		return
	}
	for index := 0; index < ctl.sessionCtx.DataSessions; index++ {
		go ctl.runDataSession(ctx, index)
	}
}

func (ctl *Control) runDataSession(ctx context.Context, index int) {
	policy := backoff.NewExponentialBackOff()
	if ctl.sessionCtx != nil && ctl.sessionCtx.RedialInterval > 0 {
		policy.InitialInterval = ctl.sessionCtx.RedialInterval
		policy.MaxInterval = ctl.sessionCtx.RedialInterval
	}
	_ = backoff.Loop(ctx, policy, func(ctx context.Context) error {
		return ctl.dialDataSession(ctx, index)
	}, func(err error, wait time.Duration) {
		ctl.logger.Warn("data session ended, redialing",
			"index", index,
			"err", err,
			"retry_in", wait,
		)
	})
}

func (ctl *Control) dialDataSession(ctx context.Context, index int) error {
	raw, session, err := ctl.sessionCtx.DialEdge(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	defer func() { _ = raw.Close() }()

	stream, err := session.Open()
	if err != nil {
		return err
	}
	nonce, err := randomDataNonce()
	if err != nil {
		_ = stream.Close()
		return err
	}
	hello := wire.DataHello{
		SessionID:  ctl.sessionCtx.SessionID,
		MachineKey: ctl.sessionCtx.ConnConfig.MachineKey,
		Index:      index,
		Timestamp:  time.Now().Unix(),
		Nonce:      nonce,
	}
	signed, err := wire.SignDataHello(ctl.sessionCtx.ConnConfig.PrivateKeyPEM, hello)
	if err != nil {
		_ = stream.Close()
		return err
	}
	if err := wire.WriteMsg(stream, signed); err != nil {
		_ = stream.Close()
		return err
	}
	ack, err := wire.ReadMsg(stream)
	if err != nil {
		_ = stream.Close()
		return err
	}
	if _, ok := ack.(*wire.DataHelloAck); !ok {
		_ = stream.Close()
		return fmt.Errorf("data hello ack: got %T", ack)
	}
	if err := stream.Close(); err != nil {
		return err
	}
	ctl.acceptWorkStreams(ctx, session)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("data session closed")
}

func randomDataNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
