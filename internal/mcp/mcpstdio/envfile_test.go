package mcpstdio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/mcp/mcperr"
)

func TestReadEnvFileMissing(t *testing.T) {
	t.Parallel()
	pairs, err := readEnvFile(filepath.Join(t.TempDir(), "missing.env"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Fatalf("pairs = %#v", pairs)
	}
}

func TestReadEnvFileParses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "mep_1.env")
	content := "# comment\n\nexport MYSQL_HOST=127.0.0.1\nMYSQL_USER=\"app\"\nMYSQL_PASS=secret\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	pairs, err := readEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"MYSQL_HOST=127.0.0.1", "MYSQL_USER=app", "MYSQL_PASS=secret"}
	if len(pairs) != len(want) {
		t.Fatalf("pairs = %#v", pairs)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Fatalf("pairs[%d] = %q, want %q", i, pairs[i], want[i])
		}
	}
}

func TestApplyEndpointEnvFileOverrides(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	envDir := filepath.Join(dir, "env")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "mep_1.env"), []byte("MYSQL_DB=prod\nFOO=fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ApplyEndpointEnvFile(SpawnConfig{
		Command: "npx",
		Env:     []string{"FOO=old", "KEEP=1"},
	}, dir, "mep_1")
	got := map[string]string{}
	for _, kv := range cfg.Env {
		k, v, _ := splitKV(kv)
		got[k] = v
	}
	if got["FOO"] != "fromfile" || got["KEEP"] != "1" || got["MYSQL_DB"] != "prod" {
		t.Fatalf("env = %#v", cfg.Env)
	}
}

func TestEndpointEnvFileReady(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if EndpointEnvFileReady(dir, "mep_1") {
		t.Fatal("missing file should not be ready")
	}
	envDir := filepath.Join(dir, "env")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "mep_1.env"), []byte("# only comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if EndpointEnvFileReady(dir, "mep_1") {
		t.Fatal("comment-only file should not be ready")
	}
	if err := os.WriteFile(filepath.Join(envDir, "mep_1.env"), []byte("MYSQL_HOST=127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !EndpointEnvFileReady(dir, "mep_1") {
		t.Fatal("file with KEY=VAL should be ready")
	}
}

func TestNewSessionRequiresEnvFileMissing(t *testing.T) {
	t.Parallel()
	_, err := NewSession(SessionConfig{
		SpawnConfig: SpawnConfig{
			Command:         "/bin/true",
			RequiresEnvFile: true,
		},
		EndpointID: "mep_missing",
		MachineDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected env_file_missing before spawn")
	}
	if code := mcperr.CodeOf(err); code != CodeEnvFileMissing {
		t.Fatalf("code = %q, want %s (%v)", code, CodeEnvFileMissing, err)
	}
	if !strings.Contains(err.Error(), "environment variable file not found") {
		t.Fatalf("err = %v, want environment variable file not found", err)
	}
}

// 不要求环境文件时，不应因文件缺失而拒绝启动；这里用一个立即退出的命令，预期落到握手阶段而非 prerequisite。
func TestNewSessionEnvFileNotRequiredSkipsCheck(t *testing.T) {
	t.Parallel()
	_, err := NewSession(SessionConfig{
		SpawnConfig:      SpawnConfig{Command: "/bin/true"},
		EndpointID:       "mep_plain",
		MachineDir:       t.TempDir(),
		HandshakeTimeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatal("expected failure from /bin/true exiting")
	}
	if code := mcperr.CodeOf(err); code == CodeEnvFileMissing {
		t.Fatalf("env file must not be required: %v", err)
	}
}

func TestApplyEndpointEnvFileMissing(t *testing.T) {
	t.Parallel()
	cfg := ApplyEndpointEnvFile(SpawnConfig{Command: "npx", Env: []string{"A=1"}}, t.TempDir(), "mep_x")
	if len(cfg.Env) != 1 || cfg.Env[0] != "A=1" {
		t.Fatalf("env = %#v", cfg.Env)
	}
}

func splitKV(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return kv, "", false
}
