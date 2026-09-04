package orbitproxy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/orbitproxy/orbitproxy-go/internal/controlplane"
	"github.com/orbitproxy/orbitproxy-go/internal/utils"
)

// RegisterOptions configures the SDK restore register path.
// SDK does not create machines; CLI has its own register.
type RegisterOptions struct {
	// AuthToken is required: account authtoken.
	AuthToken string
	// MachineKey is required: restores the machine mapped by this key.
	MachineKey string
	// APIURL is required: control-plane base URL for /v1/machines/register.
	APIURL string

	Hostname   string // optional; defaults to os.Hostname()
	Version    string // semver; empty falls back to Version() and still must be semver
	HTTPClient *http.Client
}

// Register restores via AuthToken + MachineKey and returns Identity for Start.
// It does not dial edge. Create / persist / machine id are CLI concerns.
func Register(ctx context.Context, opts RegisterOptions) (*Identity, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	authToken := strings.TrimSpace(opts.AuthToken)
	machineKey := strings.TrimSpace(opts.MachineKey)
	apiURL := strings.TrimSpace(opts.APIURL)
	if authToken == "" {
		return nil, fmt.Errorf("AuthToken is required")
	}
	if machineKey == "" {
		return nil, fmt.Errorf("MachineKey is required")
	}
	if apiURL == "" {
		return nil, fmt.Errorf("APIURL is required")
	}

	pubPEM, privPEM, err := utils.GenerateEd25519KeyPairPEM()
	if err != nil {
		return nil, err
	}

	hostname := strings.TrimSpace(opts.Hostname)
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = Version()
	}
	if !isClientSemver(version) {
		return nil, fmt.Errorf("version must be semver, got %q; pass RegisterOptions.Version or depend on a tagged module", version)
	}

	reg, err := controlplane.Register(ctx, controlplane.RegisterOptions{
		APIURL:     apiURL,
		AuthToken:  authToken,
		MachineKey:  machineKey,
		PublicKey:  pubPEM,
		Hostname:   hostname,
		Version:    version,
		HTTPClient: opts.HTTPClient,
	})
	if err != nil {
		return nil, fmt.Errorf("register machine: %w", err)
	}

	return &Identity{
		MachineKey:     machineKey,
		EdgeAddr:      reg.EdgeAddr,
		MachineCACert: reg.CACert,
		PrivateKeyPEM: privPEM,
	}, nil
}
