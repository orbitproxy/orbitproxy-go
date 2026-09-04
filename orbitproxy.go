package orbitproxy

import (
	"context"
	"net/http"

	"github.com/orbitproxy/orbitproxy-go/service"
)

// Options configures Connect (SDK Register + Start convenience).
type Options struct {
	// AuthToken is required: account authtoken (same as machine CLI).
	AuthToken string
	// MachineKey is required: rotatable key that maps to one machine row.
	// SDK always restores via MachineKey; it never registers a new machine.
	MachineKey string
	// APIURL is required: control-plane base URL for /v1/machines/register.
	APIURL string

	// Logger configures default dual-channel logging or a custom slog sink.
	Logger LoggerOptions

	HTTPClient *http.Client // optional; used only for register HTTP, not edge traffic
	Version    string       // semver; empty falls back to Version() and still must be semver

	// Lifecycle hooks (same as StartOptions).
	OnConnected    func(sessionID string)
	OnEndpoints    func(endpoints []service.EndpointStatus)
	OnReconnecting func(attempt int, reason string)
	OnDisconnected func(reason string, permanent bool)
}

// Connect is Register (SDK restore) + Start.
// CLI should use its own register, then call Start with the Identity it built.
func Connect(ctx context.Context, opts Options) (*service.Service, error) {
	id, err := Register(ctx, RegisterOptions{
		AuthToken:  opts.AuthToken,
		MachineKey: opts.MachineKey,
		APIURL:     opts.APIURL,
		HTTPClient: opts.HTTPClient,
		Version:    opts.Version,
	})
	if err != nil {
		return nil, err
	}
	return Start(ctx, *id, StartOptions{
		Logger:         opts.Logger,
		OnConnected:    opts.OnConnected,
		OnEndpoints:    opts.OnEndpoints,
		OnReconnecting: opts.OnReconnecting,
		OnDisconnected: opts.OnDisconnected,
	})
}
