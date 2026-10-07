package preflight

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcperr"
)

// installedNpxStub 造一个假的 npx 与已安装的 npm 包目录，返回 npx 路径与 workDir。
// npx 的行为由 body 决定（sh 脚本体）。
func installedNpxStub(t *testing.T, pkg, body string) (npx, workDir string) {
	t.Helper()
	npx = filepath.Join(t.TempDir(), "npx")
	if err := os.WriteFile(npx, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	workDir = t.TempDir()
	pkgDir := filepath.Join(workDir, "node_modules", pkg)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"`+pkg+`","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return npx, workDir
}

func execPayload(t *testing.T, npx, workDir string, extra map[string]any) json.RawMessage {
	t.Helper()
	payload := map[string]any{
		"delivery": "exec",
		"command":  npx,
		"args":     []string{"--no-install", "@acme/mcp-server"},
		"workDir":  workDir,
	}
	for key, value := range extra {
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRunSkipProtocolForwardNoDial(t *testing.T) {
	t.Parallel()
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		http.Error(w, "should not dial", http.StatusInternalServerError)
	}))
	defer server.Close()

	payload, err := json.Marshal(map[string]any{
		"localAddr": server.Listener.Addr().String(),
		"localPath": "/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), RunOptions{Payload: payload, SkipProtocol: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hits != 0 {
		t.Fatalf("SkipProtocol dialed upstream %d times", hits)
	}
	if len(out.Tools) != 0 {
		t.Fatalf("SkipProtocol must not return tools, got %d", len(out.Tools))
	}
}

func TestRunForwardHTTPStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream down", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	payload, _ := json.Marshal(map[string]any{"localAddr": server.Listener.Addr().String(), "localPath": "/mcp"})
	_, err := Run(context.Background(), RunOptions{Payload: payload})
	if err == nil {
		t.Fatal("expected failure")
	}
	code, msg := ClassifyError(err)
	if code != mcperr.CodeHTTPStatus {
		t.Fatalf("code = %q, want %q (%s)", code, mcperr.CodeHTTPStatus, msg)
	}
	if !strings.Contains(msg, "503") {
		t.Fatalf("message lacks status: %s", msg)
	}
}

func TestRunForwardDialFailed(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.NotFoundHandler())
	addr := server.Listener.Addr().String()
	server.Close()

	payload, _ := json.Marshal(map[string]any{"localAddr": addr, "localPath": "/mcp"})
	_, err := Run(context.Background(), RunOptions{Payload: payload})
	if code, _ := ClassifyError(err); code != mcperr.CodeDialFailed {
		t.Fatalf("code = %q, want %q (%v)", code, mcperr.CodeDialFailed, err)
	}
}

// family_key 不再触发 skipProtocol：预检必须真的拉起进程。这里 npx 立即退出，预期 exited_on_start。
func TestRunExecFamilyKeyStillSpawns(t *testing.T) {
	npx, workDir := installedNpxStub(t, "@acme/mcp-server", "echo 'boot failure' >&2\nexit 2\n")
	payload := execPayload(t, npx, workDir, map[string]any{"family_key": "acme"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Run(ctx, RunOptions{Payload: payload, EndpointID: "ep1", MachineDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected failure from exiting process")
	}
	code, msg := ClassifyError(err)
	if code != mcperr.CodeExitedOnStart {
		t.Fatalf("code = %q, want %q (%s)", code, mcperr.CodeExitedOnStart, msg)
	}
	if !strings.Contains(msg, "exit_code=2") || !strings.Contains(msg, "stderr=boot failure") {
		t.Fatalf("message lacks evidence: %s", msg)
	}
}

func TestRunExecSkipProtocolReturnsResolvedPath(t *testing.T) {
	npx, workDir := installedNpxStub(t, "@acme/mcp-server", "exit 0\n")
	payload := execPayload(t, npx, workDir, nil)
	out, err := Run(context.Background(), RunOptions{Payload: payload, SkipProtocol: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.ResolvedPath == "" {
		t.Fatal("expected resolved path from CheckCommand")
	}
}

func TestRunExecRequiresEnvFileMissing(t *testing.T) {
	npx, workDir := installedNpxStub(t, "@acme/mcp-server", "exit 0\n")
	payload := execPayload(t, npx, workDir, map[string]any{"requires_env_file": true})
	_, err := Run(context.Background(), RunOptions{Payload: payload, EndpointID: "ep1", MachineDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected env_file_missing")
	}
	if code, _ := ClassifyError(err); code != CodeEnvFileMissing {
		t.Fatalf("ClassifyError = %q, want %q (%v)", code, CodeEnvFileMissing, err)
	}
}

// 环境文件存在时，不得再报 env_file_missing；失败应落到进程阶段并带证据。
func TestRunExecEnvFilePresentFailsAtProcessStage(t *testing.T) {
	npx, workDir := installedNpxStub(t, "@acme/mcp-server", "echo 'Access denied' >&2\nexit 1\n")
	payload := execPayload(t, npx, workDir, map[string]any{"requires_env_file": true})
	machineDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(machineDir, "env"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(machineDir, "env", "ep1.env"), []byte("DB_HOST=127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Run(ctx, RunOptions{Payload: payload, EndpointID: "ep1", MachineDir: machineDir})
	code, msg := ClassifyError(err)
	if code == CodeEnvFileMissing {
		t.Fatalf("env file exists; must not report env_file_missing: %s", msg)
	}
	if code != mcperr.CodeExitedOnStart {
		t.Fatalf("code = %q, want %q (%s)", code, mcperr.CodeExitedOnStart, msg)
	}
	if !strings.Contains(msg, "stderr=Access denied") {
		t.Fatalf("message lacks stderr: %s", msg)
	}
}

// preflight_env 必须注入预检子进程。
func TestRunExecInjectsPreflightEnv(t *testing.T) {
	npx, workDir := installedNpxStub(t, "@acme/mcp-server", "echo \"ENABLE_LOGGING=$ENABLE_LOGGING\" >&2\nexit 1\n")
	payload := execPayload(t, npx, workDir, map[string]any{"preflight_env": map[string]string{"ENABLE_LOGGING": "true"}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Run(ctx, RunOptions{Payload: payload, EndpointID: "ep1", MachineDir: t.TempDir()})
	if _, msg := ClassifyError(err); !strings.Contains(msg, "stderr=ENABLE_LOGGING=true") {
		t.Fatalf("preflight_env not injected: %s", msg)
	}
}

func TestRunExecPackageNotInstalled(t *testing.T) {
	npx := filepath.Join(t.TempDir(), "npx")
	if err := os.WriteFile(npx, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := execPayload(t, npx, t.TempDir(), nil)
	_, err := Run(context.Background(), RunOptions{Payload: payload})
	if code, _ := ClassifyError(err); code != CodePackageNotInstalled {
		t.Fatalf("code = %q, want %q (%v)", code, CodePackageNotInstalled, err)
	}
}
