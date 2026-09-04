package sdklog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenDefaultWritesUnderMachineLogsDir(t *testing.T) {
	root := t.TempDir()
	logger, path, closer, err := OpenDefault(FileConfig{
		Dir:        root,
		MachineKey: "ck_test_key",
		MaxSizeMB:  1,
		MaxAgeDays: 1,
		MaxBackups: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	wantDir := filepath.Join(root, "ck_test_key", "logs")
	if filepath.Dir(path) != wantDir {
		t.Fatalf("path=%s want dir %s", path, wantDir)
	}
	logger.Info("hello_info")
	logger.Debug("hello_debug")
	_ = closer.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "hello_info") || !strings.Contains(body, "hello_debug") {
		t.Fatalf("file missing expected lines: %s", body)
	}
}
