//go:build !windows

package mcpstdio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcperr"
)

// fakeServer 写一个 sh 脚本模拟 stdio MCP server；脚本体由各用例给出。
func fakeServer(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-mcp.sh")
	script := "#!/bin/sh\n" +
		// reply 从请求行里取出 id 并回一个 initialize 成功应答
		"reply() { id=$(printf '%s' \"$1\" | sed -n 's/.*\"id\":\\([0-9]*\\).*/\\1/p'); " +
		"printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{},\"serverInfo\":{\"name\":\"fake\",\"version\":\"0\"}}}\\n' \"$id\"; }\n" +
		body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newFakeSession(t *testing.T, command string) (*Session, error) {
	t.Helper()
	return NewSession(SessionConfig{
		SpawnConfig:      SpawnConfig{Command: command},
		EndpointID:       "ep_fake",
		MachineDir:       t.TempDir(),
		HandshakeTimeout: 3 * time.Second,
		RequestTimeout:   3 * time.Second,
	})
}

func TestSessionExitedOnStartCarriesStderr(t *testing.T) {
	t.Parallel()
	cmd := fakeServer(t, "echo 'fatal: cannot start' >&2\nexit 3\n")
	_, err := newFakeSession(t, cmd)
	if err == nil {
		t.Fatal("expected failure")
	}
	typed, ok := mcperr.As(err)
	if !ok {
		t.Fatalf("expected *mcperr.Error, got %T %v", err, err)
	}
	if typed.Code != CodeExitedOnStart {
		t.Fatalf("code = %q, want %q (%v)", typed.Code, CodeExitedOnStart, err)
	}
	if typed.ExitCode == nil || *typed.ExitCode != 3 {
		t.Fatalf("exit code = %v, want 3", typed.ExitCode)
	}
	msg := err.Error()
	if !strings.Contains(msg, "exit_code=3") || !strings.Contains(msg, "stderr=fatal: cannot start") {
		t.Fatalf("message lacks evidence: %s", msg)
	}
}

func TestSessionExitedAfterHandshake(t *testing.T) {
	t.Parallel()
	cmd := fakeServer(t, "read line\nreply \"$line\"\nread line\necho 'Unknown database foo' >&2\nexit 1\n")
	_, err := newFakeSession(t, cmd)
	if err == nil {
		t.Fatal("expected failure")
	}
	typed, ok := mcperr.As(err)
	if !ok {
		t.Fatalf("expected *mcperr.Error, got %T %v", err, err)
	}
	if typed.Code != CodeExitedAfterHandshake {
		t.Fatalf("code = %q, want %q (%v)", typed.Code, CodeExitedAfterHandshake, err)
	}
	if typed.Stage != mcperr.StageSettle {
		t.Fatalf("stage = %q, want %q", typed.Stage, mcperr.StageSettle)
	}
	if !strings.Contains(err.Error(), "stderr=Unknown database foo") {
		t.Fatalf("message lacks stderr: %s", err.Error())
	}
}

func TestSessionSilentExitMarksStderrEmpty(t *testing.T) {
	t.Parallel()
	cmd := fakeServer(t, "read line\nreply \"$line\"\nread line\nexit 1\n")
	_, err := newFakeSession(t, cmd)
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "stderr=(empty)") {
		t.Fatalf("silent exit must be marked explicitly: %s", err.Error())
	}
}

func TestSessionHandshakeRejected(t *testing.T) {
	t.Parallel()
	cmd := fakeServer(t, "read line\n"+
		"id=$(printf '%s' \"$line\" | sed -n 's/.*\"id\":\\([0-9]*\\).*/\\1/p')\n"+
		"printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"error\":{\"code\":-32600,\"message\":\"nope\"}}\\n' \"$id\"\n"+
		"sleep 5\n")
	_, err := newFakeSession(t, cmd)
	if err == nil {
		t.Fatal("expected failure")
	}
	if code := mcperr.CodeOf(err); code != CodeHandshakeRejected {
		t.Fatalf("code = %q, want %q (%v)", code, CodeHandshakeRejected, err)
	}
}

func TestSessionHandshakeTimeout(t *testing.T) {
	t.Parallel()
	cmd := fakeServer(t, "read line\nsleep 10\n")
	_, err := NewSession(SessionConfig{
		SpawnConfig:      SpawnConfig{Command: cmd},
		EndpointID:       "ep_slow",
		MachineDir:       t.TempDir(),
		HandshakeTimeout: 500 * time.Millisecond,
		RequestTimeout:   5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if code := mcperr.CodeOf(err); code != CodeHandshakeTimeout {
		t.Fatalf("code = %q, want %q (%v)", code, CodeHandshakeTimeout, err)
	}
}

// 服务端把一行日志打到 stdout：读循环不得终止，握手仍应成功，噪声可取回。
func TestSessionStdoutNoiseDoesNotBreakHandshake(t *testing.T) {
	t.Parallel()
	cmd := fakeServer(t, "echo 'Server started on stdio'\nread line\nreply \"$line\"\nread line\nsleep 10\n")
	s, err := newFakeSession(t, cmd)
	if err != nil {
		t.Fatalf("handshake must survive stdout noise: %v", err)
	}
	defer s.Close()
	if noise := string(s.StdoutNoiseTail()); !strings.Contains(noise, "Server started on stdio") {
		t.Fatalf("stdout noise not captured: %q", noise)
	}
}

func TestWithPreflightEnvOnlyAffectsCopy(t *testing.T) {
	t.Parallel()
	base := SpawnConfig{Env: []string{"A=1"}, PreflightEnv: []string{"ENABLE_LOGGING=true", "A=2"}}
	merged := WithPreflightEnv(base)
	if strings.Join(merged.Env, ",") != "A=2,ENABLE_LOGGING=true" {
		t.Fatalf("merged env = %v", merged.Env)
	}
	if strings.Join(base.Env, ",") != "A=1" {
		t.Fatalf("base env must not change: %v", base.Env)
	}
}
