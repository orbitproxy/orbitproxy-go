//go:build unix

package userenv

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const captureTimeout = 3 * time.Second

func captureToolchainPATH() string {
	fallback := os.Getenv("PATH")
	home := Home()
	if home == "" {
		return fallback
	}

	nvmDir := strings.TrimSpace(os.Getenv("NVM_DIR"))
	if nvmDir == "" {
		nvmDir = filepath.Join(home, ".nvm")
	}
	nvmSh := filepath.Join(nvmDir, "nvm.sh")
	hasNVM := fileExists(nvmSh)

	fnmBin := firstExistingFile(
		filepath.Join(home, ".local", "share", "fnm", "fnm"),
		filepath.Join(home, ".fnm", "fnm"),
	)
	hasFnm := fnmBin != ""

	voltaHome := filepath.Join(home, ".volta")
	hasVolta := isDir(filepath.Join(voltaHome, "bin"))

	if !hasNVM && !hasFnm && !hasVolta {
		return fallback
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		return fallback
	}

	var script strings.Builder
	if hasNVM {
		fmt.Fprintf(&script, "export NVM_DIR=%s\n", strconv.Quote(nvmDir))
		fmt.Fprintf(&script, ". %s >/dev/null 2>&1 || true\n", strconv.Quote(nvmSh))
	}
	if hasFnm {
		fmt.Fprintf(&script, "eval \"$(%s env)\" 2>/dev/null || true\n", strconv.Quote(fnmBin))
	}
	if hasVolta {
		fmt.Fprintf(&script, "export VOLTA_HOME=%s\n", strconv.Quote(voltaHome))
		script.WriteString("export PATH=\"$VOLTA_HOME/bin:$PATH\"\n")
	}
	script.WriteString("printf '%s' \"$PATH\"\n")

	ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "--noprofile", "--norc", "-c", script.String())
	cmd.Env = os.Environ()
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("userenv: toolchain PATH capture failed", "err", err)
		return fallback
	}
	captured := string(bytes.TrimSpace(out))
	if captured == "" {
		return fallback
	}

	sources := make([]string, 0, 3)
	if hasNVM {
		sources = append(sources, "nvm")
	}
	if hasFnm {
		sources = append(sources, "fnm")
	}
	if hasVolta {
		sources = append(sources, "volta")
	}
	slog.Info("userenv: loaded user toolchain PATH", "home", home, "sources", sources)
	return captured
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func firstExistingFile(paths ...string) string {
	for _, path := range paths {
		if fileExists(path) {
			return path
		}
	}
	return ""
}
