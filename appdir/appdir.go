// Package appdir resolves OrbitProxy client data directories shared by SDK and CLI.
package appdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	rootDirName   = ".orbitproxy"
	logsDirName   = "logs"
	logFileName   = "orbitproxy.log"
	configName    = "orbitproxy.yml"
	execPIDFileName = "exec_pids.json"
	envDirName      = "env"
)

// DefaultRoot returns ~/.orbitproxy (all OS), or a temp fallback if home is unavailable.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, rootDirName)
	}
	return filepath.Join(os.TempDir(), "orbitproxy")
}

// ResolveRoot returns an absolute data root. Empty explicit uses DefaultRoot().
func ResolveRoot(explicit string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit == "" {
		return filepath.Abs(DefaultRoot())
	}
	return filepath.Abs(explicit)
}

// SanitizeMachineKey makes machineKey safe as a single path segment.
func SanitizeMachineKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	key = strings.ReplaceAll(key, string(filepath.Separator), "_")
	key = strings.ReplaceAll(key, "/", "_")
	key = strings.ReplaceAll(key, "\\", "_")
	key = strings.ReplaceAll(key, "..", "_")
	return key
}

// MachineDir is <root>/<machineKey>.
func MachineDir(root, machineKey string) (string, error) {
	root = strings.TrimSpace(root)
	key := SanitizeMachineKey(machineKey)
	if key == "" {
		return "", fmt.Errorf("machine key is required")
	}
	if root == "" {
		var err error
		root, err = ResolveRoot("")
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(root, key), nil
}

// LogsDir is <machineDir>/logs.
func LogsDir(machineDir string) string {
	return filepath.Join(machineDir, logsDirName)
}

// LogFilePath is <root>/<machineKey>/logs/orbitproxy.log.
func LogFilePath(root, machineKey string) (string, error) {
	machineDir, err := MachineDir(root, machineKey)
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(LogsDir(machineDir), logFileName))
}

// ConfigFileName is the CLI config basename under a machine dir.
func ConfigFileName() string { return configName }

// ConfigPath is <machineDir>/orbitproxy.yml.
func ConfigPath(machineDir string) string {
	return filepath.Join(machineDir, configName)
}

// ExecPIDFile is <root>/<machineKey>/exec_pids.json for stdio MCP orphan cleanup.
func ExecPIDFile(root, machineKey string) (string, error) {
	machineDir, err := MachineDir(root, machineKey)
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(machineDir, execPIDFileName))
}

// EndpointEnvFile is <root>/<machineKey>/env/<endpointID>.env
func EndpointEnvFile(root, machineKey, endpointID string) (string, error) {
	machineDir, err := MachineDir(root, machineKey)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(endpointID)
	if id == "" {
		return "", fmt.Errorf("endpoint id is required")
	}
	id = strings.ReplaceAll(id, string(filepath.Separator), "_")
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, "\\", "_")
	id = strings.ReplaceAll(id, "..", "_")
	return filepath.Abs(filepath.Join(machineDir, envDirName, id+".env"))
}
