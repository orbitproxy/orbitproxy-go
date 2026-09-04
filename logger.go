package orbitproxy

import (
	"log/slog"

	"github.com/orbitproxy/orbitproxy-go/internal/sdklog"
)

// DiscardLogger returns a slog.Logger that drops all records.
// Use as LoggerOptions.Slog to disable the default file+stderr logger:
//
//	orbitproxy.StartOptions{Logger: orbitproxy.LoggerOptions{Slog: orbitproxy.DiscardLogger()}}
func DiscardLogger() *slog.Logger {
	return sdklog.Nop()
}
