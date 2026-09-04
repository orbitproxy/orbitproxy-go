package userenv

import (
	"os"
	"os/user"
	"strings"
)

// Home is the user home directory MCP lookup and spawn should use.
// Prefers $HOME, then the passwd/profile home so systemd units without HOME still work.
func Home() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil {
		if home = strings.TrimSpace(home); home != "" {
			return home
		}
	}
	if u, err := user.Current(); err == nil {
		return strings.TrimSpace(u.HomeDir)
	}
	return ""
}

// EnsureHome sets HOME when the process inherited an empty environment (typical of systemd).
func EnsureHome() {
	if strings.TrimSpace(os.Getenv("HOME")) != "" {
		return
	}
	if home := Home(); home != "" {
		_ = os.Setenv("HOME", home)
	}
}
