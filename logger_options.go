package orbitproxy

import "log/slog"

// LoggerOptions configures SDK logging (nested under StartOptions.Logger / Options.Logger).
// When Slog is non-nil, it replaces the default dual-channel logger and Dir/rotation are ignored.
// When Slog is nil, Dir/rotation configure the default stderr(Info+)+file(Debug+) logger.
type LoggerOptions struct {
	// Slog is an optional custom slog handler sink. Nil enables defaults.
	Slog *slog.Logger
	// Dir is the data/log root (default ~/.orbitproxy). Machine key is always appended.
	Dir string
	// MaxSizeMB is lumberjack max size in MB (default 100).
	MaxSizeMB int
	// MaxAgeDays is lumberjack retention in days (default 7).
	MaxAgeDays int
	// MaxBackups is lumberjack max backup count (default 14).
	MaxBackups int
	// Compress enables lumberjack compression (default false).
	Compress bool
}

// LogRotation is kept as an alias helper for callers that group rotation fields.
// Prefer setting fields on LoggerOptions directly.
type LogRotation struct {
	MaxSizeMB  int
	MaxAgeDays int
	MaxBackups int
	Compress   bool
}
