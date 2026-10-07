package mcperr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func intPtr(v int) *int { return &v }

func TestErrorFormatWithoutProcess(t *testing.T) {
	t.Parallel()
	err := New(StageDial, CodeDialFailed, "dial http://127.0.0.1:1 failed")
	if got := err.Error(); got != "dial http://127.0.0.1:1 failed" {
		t.Fatalf("got %q", got)
	}
}

func TestErrorFormatWithProcessAndStderr(t *testing.T) {
	t.Parallel()
	err := New(StageSettle, CodeExitedAfterHandshake, "subprocess exited shortly after MCP handshake").
		WithProcess(intPtr(1), "Unknown database 'foo'\n", "")
	want := "subprocess exited shortly after MCP handshake; exit_code=1; stderr=Unknown database 'foo'"
	if got := err.Error(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestErrorFormatSilentProcessMarksEmpty(t *testing.T) {
	t.Parallel()
	err := New(StageHandshake, CodeExitedOnStart, "initialize: subprocess exited").WithProcess(intPtr(1), "", "")
	if got := err.Error(); !strings.HasSuffix(got, "; exit_code=1; stderr=(empty)") {
		t.Fatalf("got %q", got)
	}
}

func TestErrorFormatIncludesStdoutNoise(t *testing.T) {
	t.Parallel()
	err := New(StageHandshake, CodeHandshakeTimeout, "initialize: no response").WithProcess(nil, "", "banner line")
	if got := err.Error(); got != "initialize: no response; stdout=banner line" {
		t.Fatalf("got %q", got)
	}
}

func TestErrorTruncatesLongEvidenceFromTail(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 600) + "TAIL"
	err := New(StageSettle, CodeExitedAfterHandshake, "x").WithProcess(intPtr(2), long, "")
	msg := err.Error()
	if !strings.HasSuffix(msg, "TAIL") {
		t.Fatalf("tail must be preserved: %q", msg[len(msg)-20:])
	}
	if strings.Contains(msg, strings.Repeat("a", 513)) {
		t.Fatal("stderr evidence must be capped at 512 bytes")
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"typed", New(StagePackage, CodePackageNotInstalled, "x"), CodePackageNotInstalled},
		{"wrapped typed", fmt.Errorf("outer: %w", New(StageSpawn, CodeSpawnFailed, "x")), CodeSpawnFailed},
		{"deadline", context.DeadlineExceeded, CodeTimeout},
		{"wrapped deadline", fmt.Errorf("ctx: %w", context.DeadlineExceeded), CodeTimeout},
		{"plain", errors.New("something"), CodeInternal},
		{"nil", nil, CodeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code, _ := Classify(tc.err); code != tc.code {
				t.Fatalf("code = %q, want %q", code, tc.code)
			}
		})
	}
}

// 文本里出现其它码的字样不得影响分类——分类只看类型。
func TestClassifyIgnoresMessageText(t *testing.T) {
	t.Parallel()
	err := New(StageProtocol, CodeToolsListRejected, "env_file_missing exit_code=1 not installed locally")
	if code, _ := Classify(err); code != CodeToolsListRejected {
		t.Fatalf("code = %q", code)
	}
	if code, _ := Classify(errors.New("env_file_missing: environment variable file not found")); code != CodeInternal {
		t.Fatalf("plain error text must not be sniffed, got %q", code)
	}
}

func TestIsProcessExit(t *testing.T) {
	t.Parallel()
	if !IsProcessExit(New(StageSettle, CodeExitedAfterHandshake, "x")) {
		t.Fatal("exited_after_handshake is a process exit")
	}
	if !IsProcessExit(New(StageHandshake, CodeHandshakeTimeout, "x").WithProcess(intPtr(1), "", "")) {
		t.Fatal("any error carrying an exit code is a process exit")
	}
	if IsProcessExit(New(StageDial, CodeDialFailed, "x")) {
		t.Fatal("dial failure is not a process exit")
	}
	if IsProcessExit(errors.New("plain")) {
		t.Fatal("plain error is not a process exit")
	}
}
