package appdir

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLogFilePathLayout(t *testing.T) {
	root := t.TempDir()
	path, err := LogFilePath(root, "ck_abc")
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := filepath.Join("ck_abc", "logs", "orbitproxy.log")
	if !strings.HasSuffix(path, wantSuffix) {
		t.Fatalf("path=%s want suffix %s", path, wantSuffix)
	}
}

func TestSanitizeMachineKey(t *testing.T) {
	got := SanitizeMachineKey(".." + string(filepath.Separator) + "x")
	if strings.Contains(got, "..") || strings.ContainsRune(got, filepath.Separator) {
		t.Fatalf("unsafe key segment: %q", got)
	}
}
