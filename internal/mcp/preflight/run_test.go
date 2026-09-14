package preflight

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFamilyKeyFromPayload(t *testing.T) {
	t.Parallel()
	if got := FamilyKeyFromPayload(json.RawMessage(`{"family_key":"mysql"}`)); got != "mysql" {
		t.Fatalf("got %q, want mysql", got)
	}
	if got := FamilyKeyFromPayload(json.RawMessage(`{"catalogKey":"mysql"}`)); got != "" {
		t.Fatalf("catalogKey must not be read, got %q", got)
	}
	if got := FamilyKeyFromPayload(json.RawMessage(`{}`)); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestRunOfficialSkipProtocolNoDial(t *testing.T) {
	t.Parallel()
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		http.Error(w, "should not dial", http.StatusInternalServerError)
	}))
	defer server.Close()

	payload, err := json.Marshal(map[string]any{
		"family_key": "playwright",
		"localAddr":  server.Listener.Addr().String(),
		"localPath":  "/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), RunOptions{
		Payload:      payload,
		SkipProtocol: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hits != 0 {
		t.Fatalf("official skipProtocol dialed upstream %d times", hits)
	}
	if len(out.Tools) != 0 {
		t.Fatalf("official must not return tools/list, got %d", len(out.Tools))
	}
}

func TestRunOfficialFamilyKeySkipsList(t *testing.T) {
	npxDir := t.TempDir()
	npx := filepath.Join(npxDir, "npx")
	if err := os.WriteFile(npx, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	pkgDir := filepath.Join(workDir, "node_modules", "@benborla29", "mcp-server-mysql")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"@benborla29/mcp-server-mysql","version":"2.0.9"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"family_key": "mysql",
		"delivery":   "exec",
		"command":    npx,
		"args":       []string{"--no-install", "@benborla29/mcp-server-mysql@2.0.9"},
		"workDir":    workDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	machineDir := t.TempDir()
	envDir := filepath.Join(machineDir, "env")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "ep1.env"), []byte("MYSQL_HOST=127.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := Run(ctx, RunOptions{
		Payload:    payload,
		EndpointID: "ep1",
		MachineDir: machineDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out.Tools) != 0 {
		t.Fatalf("official family_key must not tools/list, got %d tools", len(out.Tools))
	}
	if out.ResolvedPath == "" {
		t.Fatal("expected resolved path from CheckCommand")
	}
}

func TestRunOfficialVersionMismatch(t *testing.T) {
	npxDir := t.TempDir()
	npx := filepath.Join(npxDir, "npx")
	if err := os.WriteFile(npx, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	pkgDir := filepath.Join(workDir, "node_modules", "@benborla29", "mcp-server-mysql")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"@benborla29/mcp-server-mysql","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"family_key": "mysql",
		"command":    npx,
		"args":       []string{"--no-install", "@benborla29/mcp-server-mysql@2.0.9"},
		"workDir":    workDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), RunOptions{Payload: payload, SkipProtocol: true})
	if err == nil {
		t.Fatal("expected version mismatch")
	}
	code, _ := ClassifyError(err)
	if code != CodePackageVersionMismatch {
		t.Fatalf("ClassifyError = %q, want %q (%v)", code, CodePackageVersionMismatch, err)
	}
}
