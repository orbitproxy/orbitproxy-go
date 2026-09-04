package mcpstdio

import (
	"os"
	"strings"
	"testing"
)

func TestSpawnEcho(t *testing.T) {
	proc, err := Spawn(SpawnConfig{
		Command: "echo",
		Args:    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	if proc.PID() <= 0 {
		t.Error("expected positive PID")
	}

	// 等待进程完成
	if err := proc.Wait(); err != nil {
		t.Errorf("wait failed: %v", err)
	}
}

func TestSpawnCommandNotFound(t *testing.T) {
	_, err := Spawn(SpawnConfig{
		Command: "/nonexistent/binary/path",
	})
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
}

func TestSpawnEmptyCommand(t *testing.T) {
	_, err := Spawn(SpawnConfig{})
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestBuildEnv(t *testing.T) {
	// 设置一个临时环境变量
	os.Setenv("ORBITPROXY_TEST_VAR", "test_value")
	defer os.Unsetenv("ORBITPROXY_TEST_VAR")

	env := buildEnv(
		[]string{"FOO=bar", "BAZ=qux"},
		[]string{"ORBITPROXY_TEST_VAR", "NONEXISTENT_VAR"},
	)

	found := make(map[string]bool)
	for _, kv := range env {
		found[kv] = true
	}

	if !found["FOO=bar"] {
		t.Error("missing explicit env FOO=bar")
	}
	if !found["BAZ=qux"] {
		t.Error("missing explicit env BAZ=qux")
	}
	if !found["ORBITPROXY_TEST_VAR=test_value"] {
		t.Error("missing passthrough ORBITPROXY_TEST_VAR")
	}
	if found["NONEXISTENT_VAR="] {
		t.Error("should not passthrough nonexistent var")
	}
}

func TestBuildEnvIncludesHomeWhenProcessHomeEmpty(t *testing.T) {
	t.Setenv("HOME", "")
	_ = os.Unsetenv("HOME")
	env := buildEnv(nil, nil)
	found := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") && kv != "HOME=" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("buildEnv missing HOME: %v", env)
	}
}

func TestBuildEnvExplicitOverridesPassthrough(t *testing.T) {
	os.Setenv("ORBITPROXY_TEST_OVERRIDE", "from_parent")
	defer os.Unsetenv("ORBITPROXY_TEST_OVERRIDE")

	env := buildEnv(
		[]string{"ORBITPROXY_TEST_OVERRIDE=explicit"},
		[]string{"ORBITPROXY_TEST_OVERRIDE"},
	)

	count := 0
	for _, kv := range env {
		if kv == "ORBITPROXY_TEST_OVERRIDE=explicit" {
			count++
		}
		if kv == "ORBITPROXY_TEST_OVERRIDE=from_parent" {
			t.Error("passthrough should be overridden by explicit")
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of explicit override, got %d", count)
	}
}
