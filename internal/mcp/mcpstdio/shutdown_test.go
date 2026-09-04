package mcpstdio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShutdownGraceful(t *testing.T) {
	// 启动 cat（读 stdin 直到 EOF 然后退出）
	proc, err := Spawn(SpawnConfig{Command: "cat"})
	if err != nil {
		t.Fatalf("spawn cat failed: %v", err)
	}

	cfg := ShutdownConfig{
		StdinCloseGrace: 2 * time.Second,
		TermGrace:       1 * time.Second,
	}

	start := time.Now()
	err = Shutdown(proc, cfg)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("shutdown error: %v", err)
	}
	// cat 在 stdin 关闭后应立即退出，不需要 SIGTERM
	if elapsed > 3*time.Second {
		t.Errorf("shutdown took %v, expected < 3s (graceful stdin close)", elapsed)
	}
}

func TestShutdownNilProcess(t *testing.T) {
	err := Shutdown(nil, DefaultShutdownConfig())
	if err != nil {
		t.Errorf("shutdown nil should not error: %v", err)
	}
}

func TestPIDRecordAndRemove(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pids.json")

	proc, err := Spawn(SpawnConfig{Command: "echo", Args: []string{"hi"}})
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	_ = proc.Wait()

	// 记录 PID
	if err := RecordPID(pidFile, proc); err != nil {
		t.Fatalf("RecordPID failed: %v", err)
	}

	// 读取确认
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	var records []pidRecord
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].PID != proc.PID() {
		t.Errorf("PID = %d, want %d", records[0].PID, proc.PID())
	}

	// 移除 PID
	if err := RemovePID(pidFile, proc.PID()); err != nil {
		t.Fatalf("RemovePID failed: %v", err)
	}

	data, _ = os.ReadFile(pidFile)
	json.Unmarshal(data, &records)
	if len(records) != 0 {
		t.Errorf("expected 0 records after remove, got %d", len(records))
	}
}

func TestCleanOrphansEmptyFile(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pids.json")

	// 空文件不报错
	cleaned, errs := CleanOrphans(pidFile)
	if cleaned != 0 {
		t.Errorf("expected 0 cleaned, got %d", cleaned)
	}
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}
