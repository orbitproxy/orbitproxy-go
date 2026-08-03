package orbitproxy

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/orbitproxy/orbitproxy-go/service"
)

// Options configures Connect (SDK Register + Start convenience).
type Options struct {
	// AuthToken is required: account authtoken (same as machine CLI).
	AuthToken string
	// ClientKey is required: rotatable key that maps to one machine row.
	// SDK always restores via ClientKey; it never registers a new machine.
	ClientKey string
	// APIURL is required: control-plane base URL for /v1/machines/register.
	APIURL string

	// Logger receives SDK log messages via standard log/slog.Logger.
	// Nil discards all messages.
	Logger     *slog.Logger
	HTTPClient *http.Client // optional; used only for register HTTP, not edge traffic
}

// Connect is Register (SDK restore) + Start.
// CLI should use its own register, then call Start with the Identity it built.
func Connect(ctx context.Context, opts Options) (*service.Service, error) {
	id, err := Register(ctx, RegisterOptions{
		AuthToken:  opts.AuthToken,
		ClientKey:  opts.ClientKey,
		APIURL:     opts.APIURL,
		HTTPClient: opts.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	return Start(ctx, *id, StartOptions{Logger: opts.Logger})
}
