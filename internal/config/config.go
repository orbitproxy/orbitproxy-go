package config

import "log/slog"

// Config is the in-memory runtime config for one SDK client process.
// Identity fields come from Register (or a loaded Identity); no disk I/O here.
type Config struct {
	MachineKey     string
	EdgeAddr      string
	MachineCACert string
	PrivateKeyPEM string
	SoftVersion   string

	Logger *slog.Logger
}
