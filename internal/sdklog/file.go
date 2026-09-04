package sdklog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/orbitproxy/orbitproxy-go/appdir"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	envLogDisabled  = "ORBITPROXY_LOG_DISABLED"
	envLogDir       = "ORBITPROXY_LOG_DIR"
	envLogMaxSizeMB = "ORBITPROXY_LOG_MAX_SIZE_MB"
	envLogMaxAge    = "ORBITPROXY_LOG_MAX_AGE_DAYS"
	envLogMaxBackup = "ORBITPROXY_LOG_MAX_BACKUPS"
	envLogCompress  = "ORBITPROXY_LOG_COMPRESS"

	defaultMaxSizeMB  = 100
	defaultMaxAgeDays = 7
	defaultMaxBackups = 14
)

// FileConfig configures the default dual-channel logger (stderr Info+ and file Debug+).
// MachineKey must be the runtime SDK machine key (never a separate "log key").
// Dir is the data root (default ~/.orbitproxy); file is Dir/<machineKey>/logs/orbitproxy.log.
type FileConfig struct {
	Dir         string
	MachineKey  string
	MaxSizeMB   int
	MaxAgeDays  int
	MaxBackups  int
	Compress    bool
	CompressSet bool
}

// Disabled reports whether ORBITPROXY_LOG_DISABLED requests no default file logging.
func Disabled() bool {
	v := strings.TrimSpace(os.Getenv(envLogDisabled))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// DefaultRootDir returns the shared OrbitProxy data root (~/.orbitproxy).
func DefaultRootDir() string {
	return appdir.DefaultRoot()
}

// ResolveFilePath returns <root>/<machineKey>/logs/orbitproxy.log.
func ResolveFilePath(rootDir, machineKey string) (string, error) {
	return appdir.LogFilePath(rootDir, machineKey)
}

// OpenDefault builds stderr(Info+) + lumberjack file(Debug+) tee logger.
func OpenDefault(cfg FileConfig) (*slog.Logger, string, io.Closer, error) {
	cfg = applyDefaults(cfg)
	machineKey := appdir.SanitizeMachineKey(strings.TrimSpace(cfg.MachineKey))
	if machineKey == "" {
		return nil, "", nil, fmt.Errorf("machine key is required for default logger")
	}

	root := strings.TrimSpace(cfg.Dir)
	if root == "" {
		root = appdir.DefaultRoot()
	}

	absPath, err := appdir.LogFilePath(root, machineKey)
	if err != nil {
		return nil, "", nil, err
	}
	logDir := filepath.Dir(absPath)
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		fallbackRoot := filepath.Join(os.TempDir(), "orbitproxy")
		fallbackPath, err2 := appdir.LogFilePath(fallbackRoot, machineKey)
		if err2 != nil {
			return nil, "", nil, fmt.Errorf("create log dir %s: %w", logDir, err)
		}
		fallbackDir := filepath.Dir(fallbackPath)
		if err2 = os.MkdirAll(fallbackDir, 0o700); err2 != nil {
			return nil, "", nil, fmt.Errorf("create log dir %s: %w", logDir, err)
		}
		_, _ = fmt.Fprintf(os.Stderr, "orbitproxy: log dir %s not writable (%v); using %s\n", logDir, err, fallbackDir)
		absPath = fallbackPath
		logDir = fallbackDir
	}
	_ = os.MkdirAll(filepath.Dir(logDir), 0o700) // machine dir

	rotator := &lumberjack.Logger{
		Filename:   absPath,
		MaxSize:    cfg.MaxSizeMB,
		MaxAge:     cfg.MaxAgeDays,
		MaxBackups: cfg.MaxBackups,
		LocalTime:  true,
		Compress:   cfg.Compress,
	}

	console := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	fileHandler := slog.NewTextHandler(rotator, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(&teeHandler{console: console, file: fileHandler})
	return logger, absPath, rotator, nil
}

// ConsoleOnly returns an Info+ stderr logger when file logging cannot be opened.
func ConsoleOnly() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func applyDefaults(cfg FileConfig) FileConfig {
	if cfg.MaxSizeMB <= 0 {
		if v, ok := envInt(envLogMaxSizeMB); ok && v > 0 {
			cfg.MaxSizeMB = v
		} else {
			cfg.MaxSizeMB = defaultMaxSizeMB
		}
	}
	if cfg.MaxAgeDays <= 0 {
		if v, ok := envInt(envLogMaxAge); ok && v >= 0 {
			cfg.MaxAgeDays = v
		} else {
			cfg.MaxAgeDays = defaultMaxAgeDays
		}
	}
	if cfg.MaxBackups <= 0 {
		if v, ok := envInt(envLogMaxBackup); ok && v >= 0 {
			cfg.MaxBackups = v
		} else {
			cfg.MaxBackups = defaultMaxBackups
		}
	}
	if !cfg.CompressSet {
		if v := strings.TrimSpace(os.Getenv(envLogCompress)); v != "" {
			cfg.Compress = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		}
	}
	if strings.TrimSpace(cfg.Dir) == "" {
		if d := strings.TrimSpace(os.Getenv(envLogDir)); d != "" {
			cfg.Dir = d
		}
	}
	return cfg
}

func envInt(key string) (int, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}
