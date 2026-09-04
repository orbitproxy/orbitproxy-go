package userenv

import (
	"os"
	"testing"
)

func TestHomePrefersEnv(t *testing.T) {
	t.Setenv("HOME", "/tmp/orbitproxy-home-test")
	if got := Home(); got != "/tmp/orbitproxy-home-test" {
		t.Fatalf("Home() = %q", got)
	}
}

func TestHomeFallsBackWhenUnset(t *testing.T) {
	t.Setenv("HOME", "")
	_ = os.Unsetenv("HOME")
	got := Home()
	if got == "" {
		t.Fatal("Home() empty after Unsetenv; UserHomeDir should still work")
	}
}

func TestEnsureHomeSetsMissingEnv(t *testing.T) {
	t.Setenv("HOME", "")
	_ = os.Unsetenv("HOME")
	EnsureHome()
	if os.Getenv("HOME") == "" {
		t.Fatal("EnsureHome left HOME empty")
	}
}
