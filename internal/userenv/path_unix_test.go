//go:build unix

package userenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPATHUnchangedWithoutToolchains(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NVM_DIR", filepath.Join(home, ".nvm"))
	resetForTest()

	before := os.Getenv("PATH")
	got := PATH()
	if got != before {
		t.Fatalf("PATH() = %q, want process PATH %q", got, before)
	}
	if os.Getenv("PATH") != before {
		t.Fatal("Capture must not mutate process PATH")
	}
}

func TestLookPathUsesNvmPATHWithoutMutatingProcess(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	home := t.TempDir()
	bin := filepath.Join(home, "toolchain-bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	npx := filepath.Join(bin, "npx")
	if err := os.WriteFile(npx, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	nvmDir := filepath.Join(home, ".nvm")
	if err := os.MkdirAll(nvmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nvmSh := filepath.Join(nvmDir, "nvm.sh")
	script := "export PATH=" + bin + ":$PATH\n"
	if err := os.WriteFile(nvmSh, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("NVM_DIR", nvmDir)
	t.Setenv("PATH", "/usr/bin:/bin")
	resetForTest()

	before := os.Getenv("PATH")
	resolved, err := LookPath("npx")
	if err != nil {
		t.Fatalf("LookPath(npx): %v", err)
	}
	if resolved != npx {
		t.Fatalf("LookPath(npx) = %q, want %q", resolved, npx)
	}
	if os.Getenv("PATH") != before {
		t.Fatalf("process PATH mutated: %q -> %q", before, os.Getenv("PATH"))
	}
	if !strings.Contains(PATH(), bin) {
		t.Fatalf("cached PATH missing toolchain bin: %s", PATH())
	}
}

func TestLookPathFallsBackWhenNvmScriptFails(t *testing.T) {
	home := t.TempDir()
	nvmDir := filepath.Join(home, ".nvm")
	if err := os.MkdirAll(nvmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nvmDir, "nvm.sh"), []byte("return 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("NVM_DIR", nvmDir)
	t.Setenv("PATH", "/usr/bin:/bin")
	resetForTest()

	got := PATH()
	if got != "/usr/bin:/bin" && !strings.Contains(got, "/usr/bin") {
		t.Fatalf("expected fallback PATH, got %q", got)
	}
}
