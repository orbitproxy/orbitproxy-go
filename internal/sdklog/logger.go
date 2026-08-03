package sdklog

import "log/slog"

// Nop returns a logger that discards all messages.
func Nop() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
