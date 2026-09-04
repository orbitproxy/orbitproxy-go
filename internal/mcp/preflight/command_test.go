package preflight

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckCommandNotFound(t *testing.T) {
	result := CheckCommand(CommandConfig{
		Command: "definitely-not-a-real-command-" + filepath.Base(t.TempDir()),
	})
	if result.OK {
		t.Error("expected not OK for non-existent command")
	}
	if result.ErrorCode != CodeCommandNotFound {
		t.Errorf("ErrorCode = %q, want %q", result.ErrorCode, CodeCommandNotFound)
	}
}

func TestCheckCommandFound(t *testing.T) {
	result := CheckCommand(CommandConfig{Command: "echo"})
	if !result.OK {
		t.Errorf("expected OK for 'echo', got error: %s", result.ErrorMessage)
	}
	if result.ResolvedPath == "" {
		t.Error("expected non-empty ResolvedPath")
	}
}

func TestCheckCommandEmpty(t *testing.T) {
	result := CheckCommand(CommandConfig{})
	if result.OK {
		t.Error("expected not OK for empty command")
	}
	if result.ErrorCode != CodeCommandNotFound {
		t.Errorf("ErrorCode = %q, want %q", result.ErrorCode, CodeCommandNotFound)
	}
}

func TestCheckCommandWorkDirNotExist(t *testing.T) {
	result := CheckCommand(CommandConfig{
		Command: "echo",
		WorkDir: "/tmp/orbitproxy-test-nonexistent-" + filepath.Base(t.TempDir()),
	})
	if result.OK {
		t.Error("expected not OK for non-existent workDir")
	}
	if result.ErrorCode != CodeSpawnFailed {
		t.Errorf("ErrorCode = %q, want %q", result.ErrorCode, CodeSpawnFailed)
	}
	if result.ResolvedPath == "" {
		t.Error("expected non-empty ResolvedPath even if workDir fails")
	}
}

func TestCheckCommandNotExecutable(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "not-exec")
	if err := os.WriteFile(f, []byte("#!/bin/sh\necho hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := CheckCommand(CommandConfig{Command: f})
	if result.OK {
		t.Error("expected not OK for non-executable file")
	}
	if result.ErrorCode != CodeCommandNotExecutable && result.ErrorCode != CodeCommandNotFound {
		t.Errorf("ErrorCode = %q, want %q or %q", result.ErrorCode, CodeCommandNotExecutable, CodeCommandNotFound)
	}
}

func TestPackageDirName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"@benborla29/mcp-server-mysql":        "@benborla29/mcp-server-mysql",
		"@benborla29/mcp-server-mysql@1.0.0":  "@benborla29/mcp-server-mysql",
		"left-pad":                            "left-pad",
		"left-pad@1.3.0":                      "left-pad",
	}
	for in, want := range cases {
		if got := packageDirName(in); got != want {
			t.Errorf("packageDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckCommandNpxBinaryMissingReportsPackage(t *testing.T) {
	result := CheckCommand(CommandConfig{
		Command: filepath.Join(t.TempDir(), "npx"),
		Args:    []string{"--no-install", "@modelcontextprotocol/server-filesystem"},
	})
	if result.OK {
		t.Fatal("expected missing npx launcher to fail")
	}
	if result.ErrorCode != CodePackageNotInstalled {
		t.Fatalf("ErrorCode = %q, want %q (%s)", result.ErrorCode, CodePackageNotInstalled, result.ErrorMessage)
	}
	if !strings.Contains(result.ErrorMessage, "@modelcontextprotocol/server-filesystem") {
		t.Fatalf("ErrorMessage = %q, want package name", result.ErrorMessage)
	}
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("preflight: env_file_missing: environment variable file not found: /tmp/x.env"), CodeEnvFileMissing},
		{fmt.Errorf("preflight: package_not_installed: MCP package not installed locally: @foo/bar"), CodePackageNotInstalled},
		{fmt.Errorf("preflight: command_not_found: command not found in PATH: echo"), CodeCommandNotFound},
		{fmt.Errorf("preflight (mysql): mysql_query failed: connection refused"), "preflight_failed"},
	}
	for _, tc := range cases {
		got, _ := ClassifyError(tc.err)
		if got != tc.want {
			t.Errorf("ClassifyError(%q) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestCheckCommandNpxPackageMissing(t *testing.T) {
	npx, err := exec.LookPath("npx")
	if err != nil {
		t.Skip("npx not on PATH")
	}
	result := CheckCommand(CommandConfig{
		Command: npx,
		Args:    []string{"--no-install", "@orbitproxy/definitely-not-installed-mcp-pkg"},
	})
	if result.OK {
		t.Fatal("expected missing npm package to fail")
	}
	if result.ErrorCode != CodePackageNotInstalled {
		t.Fatalf("ErrorCode = %q, want %q (%s)", result.ErrorCode, CodePackageNotInstalled, result.ErrorMessage)
	}
}

func TestNpmPackageCandidateDirsUsesConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ORBITPROXY_NPM_ROOT_G", root)
	dirs := npmPackageCandidateDirs("/usr/bin/npx", "")
	found := false
	for _, dir := range dirs {
		if dir == root {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("candidate dirs missing configured root %q: %v", root, dirs)
	}
	foundNpxLayout := false
	for _, dir := range dirs {
		if dir == "/usr/lib/node_modules" {
			foundNpxLayout = true
			break
		}
	}
	if !foundNpxLayout {
		t.Fatalf("candidate dirs missing npx-adjacent layout: %v", dirs)
	}
}

func TestCheckCommandFindsPackageOutsideNpxLayout(t *testing.T) {
	npxDir := t.TempDir()
	npx := filepath.Join(npxDir, "npx")
	if err := os.WriteFile(npx, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	pkgDir := filepath.Join(root, "@wonderwhy-er", "desktop-commander")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"@wonderwhy-er/desktop-commander"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORBITPROXY_NPM_ROOT_G", root)
	result := CheckCommand(CommandConfig{
		Command: npx,
		Args:    []string{"--no-install", "@wonderwhy-er/desktop-commander@0.2.47"},
	})
	if !result.OK {
		t.Fatalf("expected package at extra npm root to pass, got %s %s", result.ErrorCode, result.ErrorMessage)
	}
}

func TestUvxPackageSpecAndToolName(t *testing.T) {
	if got := uvxPackageSpec("uvx", []string{"mcp-server-docker==0.3.0"}); got != "mcp-server-docker==0.3.0" {
		t.Fatalf("uvxPackageSpec = %q", got)
	}
	if got := uvToolName("mcp-server-docker==0.3.0"); got != "mcp-server-docker" {
		t.Fatalf("uvToolName = %q", got)
	}
}

func TestCheckCommandNpxPackagePresent(t *testing.T) {
	npx, err := exec.LookPath("npx")
	if err != nil {
		t.Skip("npx not on PATH")
	}
	workDir := t.TempDir()
	pkgDir := filepath.Join(workDir, "node_modules", "@benborla29", "mcp-server-mysql")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"@benborla29/mcp-server-mysql"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result := CheckCommand(CommandConfig{
		Command: npx,
		Args:    []string{"--no-install", "@benborla29/mcp-server-mysql"},
		WorkDir: workDir,
	})
	if !result.OK {
		t.Fatalf("expected installed package to pass, got %s %s", result.ErrorCode, result.ErrorMessage)
	}
}
