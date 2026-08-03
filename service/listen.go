package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/endpoint"
)

// ListenOption configures Listen.
type ListenOption func(*listenOptions)

type listenOptions struct {
	endpointID string
}

// WithEndpointID claims a specific in-process endpoint.
func WithEndpointID(endpointID string) ListenOption {
	return func(o *listenOptions) {
		o.endpointID = endpointID
	}
}

// Listen claims an in-process endpoint and returns a net.Listener.
// Forward endpoints do not use this path — they dial localAddr automatically.
func (svr *Service) Listen(ctx context.Context, opts ...ListenOption) (net.Listener, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lo := listenOptions{}
	for _, opt := range opts {
		opt(&lo)
	}
	wantID := strings.TrimSpace(lo.endpointID)

	for {
		if mgr := svr.endpointMgr(); mgr != nil {
			if wantID != "" {
				if rt, ok := mgr.Get(wantID); ok {
					if rt.Delivery() != endpoint.DeliveryInProcess {
						return nil, fmt.Errorf("endpoint %s delivery is %q, want %q", wantID, rt.Delivery(), endpoint.DeliveryInProcess)
					}
					ln, err := mgr.ClaimListener(wantID)
					if err == nil {
						svr.logger.Info("in-process endpoint claimed",
							"client_key", svr.cfg.ClientKey,
							"endpoint_id", wantID,
						)
						return ln, nil
					}
					if errors.Is(err, endpoint.ErrListenerClaimed) {
						return nil, err
					}
					if !errors.Is(err, endpoint.ErrEndpointMissing) {
						return nil, err
					}
				}
			} else if id, ok := mgr.FindInProcessEndpoint(""); ok {
				ln, err := mgr.ClaimListener(id)
				if err == nil {
					svr.logger.Info("in-process endpoint claimed",
						"client_key", svr.cfg.ClientKey,
						"endpoint_id", id,
					)
					return ln, nil
				}
				if !errors.Is(err, endpoint.ErrListenerClaimed) && !errors.Is(err, endpoint.ErrEndpointMissing) {
					return nil, err
				}
			}

			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("wait in-process endpoint: %w", ctx.Err())
			case <-svr.ctx.Done():
				return nil, fmt.Errorf("service closed: %w", svr.ctx.Err())
			case <-mgr.Notify():
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait in-process endpoint: %w", ctx.Err())
		case <-svr.ctx.Done():
			return nil, fmt.Errorf("service closed: %w", svr.ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}
