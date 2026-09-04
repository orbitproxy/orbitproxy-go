package userenv

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const npmQueryTimeout = 3 * time.Second

var (
	npmRootOnce sync.Once
	npmRoot     string
)

const extraNpmRootEnv = "ORBITPROXY_NPM_ROOT_G"

// NpmGlobalRoot is `npm root -g` under Home()+PATH(). Empty when npm is missing or the query fails.
func NpmGlobalRoot() string {
	EnsureHome()
	npmRootOnce.Do(func() {
		npmRoot = queryNpmRootG()
	})
	return npmRoot
}

// ExtraNpmRoots are precomputed global node_modules dirs (service-install snapshot).
func ExtraNpmRoots() []string {
	raw := strings.TrimSpace(os.Getenv(extraNpmRootEnv))
	if raw == "" {
		return nil
	}
	return []string{raw}
}

// NpmPrefix is the npm prefix that corresponds to NpmGlobalRoot (for npm_config_prefix).
func NpmPrefix() string {
	return npmPrefixFromRoot(NpmGlobalRoot())
}

func npmPrefixFromRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	slash := filepath.ToSlash(root)
	if strings.HasSuffix(slash, "/lib/node_modules") {
		return filepath.Dir(filepath.Dir(root))
	}
	if filepath.Base(root) == "node_modules" {
		return filepath.Dir(root)
	}
	return ""
}

func queryNpmRootG() string {
	npm, err := LookPath("npm")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), npmQueryTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, npm, "root", "-g")
	cmd.Env = withHomeAndPath(os.Environ())
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bytes.TrimSpace(out)))
}

func withHomeAndPath(env []string) []string {
	env = replaceEnvKey(env, "PATH", PATH())
	if home := Home(); home != "" {
		env = replaceEnvKey(env, "HOME", home)
	}
	return env
}

func replaceEnvKey(env []string, key, val string) []string {
	if val == "" {
		return env
	}
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			if !replaced {
				out = append(out, prefix+val)
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, prefix+val)
	}
	return out
}

func resetNpmRootForTest() {
	npmRootOnce = sync.Once{}
	npmRoot = ""
}
