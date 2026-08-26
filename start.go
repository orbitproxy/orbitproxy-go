package orbitproxy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/orbitproxy/orbitproxy-go/internal/config"
	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/service"
)

// StartOptions configures the shared Start runtime.
type StartOptions struct {
	// Logger receives SDK log messages. Nil discards all messages.
	Logger *slog.Logger
	// SoftVersion is sent in ClientHello. Empty defaults to Version()
	// (SDK module version from build info). CLI should set this via ldflags.
	SoftVersion string

	// OnConnected is called after each successful edge session establish
	// (first login and every reconnect). sessionID is from ServerHello.
	OnConnected func(sessionID string)
	// OnEndpoints is called whenever the endpoint set changes (add/update/remove).
	OnEndpoints func(endpoints []service.EndpointStatus)
	// OnReconnecting is called when the control connection drops and a reconnect
	// attempt is about to begin. attempt is 1-based for the current outage.
	OnReconnecting func(attempt int, reason string)
	// OnDisconnected is called when the service stops reconnecting.
	// permanent is true for edge Disconnect (e.g. machine_deleted).
	OnDisconnected func(reason string, permanent bool)
}

// Start dials edge with Identity and returns a running *service.Service.
// Shared by SDK (after its Register) and CLI (after CLI's own register).
// First login retries with backoff until success or ctx ends.
func Start(ctx context.Context, id Identity, opts StartOptions) (*service.Service, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	machineKey := strings.TrimSpace(id.MachineKey)
	edgeAddr := strings.TrimSpace(id.EdgeAddr)
	machineCACert := strings.TrimSpace(id.MachineCACert)
	privPEM := strings.TrimSpace(id.PrivateKeyPEM)
	if machineKey == "" {
		return nil, fmt.Errorf("Identity.MachineKey is required")
	}
	if edgeAddr == "" {
		return nil, fmt.Errorf("Identity.EdgeAddr is required")
	}
	if machineCACert == "" {
		return nil, fmt.Errorf("Identity.MachineCACert is required")
	}
	if privPEM == "" {
		return nil, fmt.Errorf("Identity.PrivateKeyPEM is required")
	}

	logger := opts.Logger
	if logger == nil {
		logger = sdklog.Nop()
	}
	softVersion := strings.TrimSpace(opts.SoftVersion)
	if softVersion == "" {
		softVersion = Version()
	}

	svc, err := service.New(config.Config{
		MachineKey:     machineKey,
		EdgeAddr:      edgeAddr,
		MachineCACert: machineCACert,
		PrivateKeyPEM: privPEM,
		SoftVersion:   softVersion,
		Logger:        logger,
	})
	if err != nil {
		return nil, err
	}
	svc.SetHooks(service.Hooks{
		OnConnected:    opts.OnConnected,
		OnEndpoints:    opts.OnEndpoints,
		OnReconnecting: opts.OnReconnecting,
		OnDisconnected: opts.OnDisconnected,
	})
	if err := svc.Start(ctx); err != nil {
		_ = svc.Close()
		return nil, err
	}
	return svc, nil
}
