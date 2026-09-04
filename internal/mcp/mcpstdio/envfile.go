package mcpstdio

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// EndpointEnvFileReady reports whether <machineDir>/env/<endpointID>.env exists
// and contains at least one KEY=VAL pair.
func EndpointEnvFileReady(machineDir, endpointID string) bool {
	path := EndpointEnvFilePath(machineDir, endpointID)
	if path == "" {
		return false
	}
	pairs, err := readEnvFile(path)
	return err == nil && len(pairs) > 0
}

// ApplyEndpointEnvFile merges KEY=VAL from <machineDir>/env/<endpointID>.env
// into cfg.Env. Missing file is a no-op. Values override same keys already in cfg.Env.
func ApplyEndpointEnvFile(cfg SpawnConfig, machineDir, endpointID string) SpawnConfig {
	path := EndpointEnvFilePath(machineDir, endpointID)
	if path == "" {
		return cfg
	}
	pairs, err := readEnvFile(path)
	if err != nil || len(pairs) == 0 {
		return cfg
	}
	cfg.Env = mergeEnvPairs(cfg.Env, pairs)
	return cfg
}

// EndpointEnvFilePath is <machineDir>/env/<endpointID>.env.
func EndpointEnvFilePath(machineDir, endpointID string) string {
	machineDir = strings.TrimSpace(machineDir)
	endpointID = strings.TrimSpace(endpointID)
	if machineDir == "" || endpointID == "" {
		return ""
	}
	return filepath.Join(machineDir, "env", endpointID+".env")
}

func readEnvFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if q := val[0]; (q == '"' || q == '\'') && val[len(val)-1] == q {
				val = val[1 : len(val)-1]
			}
		}
		out = append(out, key+"="+val)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func mergeEnvPairs(existing, incoming []string) []string {
	seen := make(map[string]int, len(existing)+len(incoming))
	out := make([]string, 0, len(existing)+len(incoming))
	add := func(kv string) {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			return
		}
		if i, exists := seen[key]; exists {
			out[i] = kv
			return
		}
		seen[key] = len(out)
		out = append(out, kv)
	}
	for _, kv := range existing {
		add(kv)
	}
	for _, kv := range incoming {
		add(kv)
	}
	return out
}
