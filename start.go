package orbitproxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/orbitproxy/orbitproxy-go/internal/config"
	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
	"github.com/orbitproxy/orbitproxy-go/internal/userenv"
	"github.com/orbitproxy/orbitproxy-go/service"
)

// StartOptions configures the shared Start runtime.
type StartOptions struct {
	// Logger configures default dual-channel logging or a custom slog sink.
	// Zero value enables defaults under ~/.orbitproxy/<machineKey>/logs/.
	Logger LoggerOptions
	// SoftVersion is sent in ClientHello. Empty defaults to Version()
	// (SDK module version from build info). CLI should set this via ldflags.
	SoftVersion string
	// DataRoot is the client --workdir (default ~/.orbitproxy).
	// Env files live under <DataRoot>/<machineKey>/env/.
	DataRoot string

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

	userenv.EnsureHome()
	userenv.Capture()

	logger, logCloser := resolveLogger(machineKey, opts.Logger)
	softVersion := strings.TrimSpace(opts.SoftVersion)
	if softVersion == "" {
		softVersion = Version()
	}

	svc, err := service.New(config.Config{
		MachineKey:    machineKey,
		EdgeAddr:      edgeAddr,
		MachineCACert: machineCACert,
		PrivateKeyPEM: privPEM,
		SoftVersion:   softVersion,
		DataRoot:      strings.TrimSpace(opts.DataRoot),
		Logger:        logger,
	})
	if err != nil {
		if logCloser != nil {
			_ = logCloser.Close()
		}
		return nil, err
	}
	if logCloser != nil {
		svc.SetLogCloser(logCloser)
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

func resolveLogger(machineKey string, opts LoggerOptions) (*slog.Logger, io.Closer) {
	if opts.Slog != nil {
		return opts.Slog, nil
	}
	if sdklog.Disabled() {
		return sdklog.Nop(), nil
	}
	cfg := sdklog.FileConfig{
		Dir:        strings.TrimSpace(opts.Dir),
		MachineKey: machineKey,
		MaxSizeMB:  opts.MaxSizeMB,
		MaxAgeDays: opts.MaxAgeDays,
		MaxBackups: opts.MaxBackups,
	}
	if opts.MaxSizeMB > 0 || opts.MaxAgeDays > 0 || opts.MaxBackups > 0 || opts.Compress {
		cfg.Compress = opts.Compress
		cfg.CompressSet = true
	}
	logger, path, closer, err := sdklog.OpenDefault(cfg)
	if err != nil {
		fallback := sdklog.ConsoleOnly()
		fallback.Warn("default file logger unavailable; using stderr only", "err", err)
		return fallback, nil
	}
	logger.Info("logging to file",
		"path", path,
		"stderr", "info+",
		"file", "debug+",
	)
	return logger, closer
}
