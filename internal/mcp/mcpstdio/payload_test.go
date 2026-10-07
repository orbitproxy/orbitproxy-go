package mcpstdio

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseExecPayloadReadsCatalogMetadata(t *testing.T) {
	t.Parallel()
	cfg, err := ParseExecPayload(json.RawMessage(`{
		"command": "npx",
		"args": ["--no-install", "@acme/mcp-server"],
		"family_key": "acme",
		"requires_env_file": true,
		"preflight_env": {"LOG_LEVEL": "debug", "ENABLE_LOGGING": "true"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FamilyKey != "acme" || !cfg.RequiresEnvFile {
		t.Fatalf("metadata not parsed: %+v", cfg)
	}
	if got := strings.Join(cfg.PreflightEnv, ","); got != "ENABLE_LOGGING=true,LOG_LEVEL=debug" {
		t.Fatalf("preflight env = %q (must be sorted KEY=VALUE)", got)
	}
}

func TestParseExecPayloadDefaultsWithoutMetadata(t *testing.T) {
	t.Parallel()
	cfg, err := ParseExecPayload(json.RawMessage(`{"command":"npx","args":["-y","@acme/mcp-server"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RequiresEnvFile || len(cfg.PreflightEnv) != 0 || cfg.FamilyKey != "" {
		t.Fatalf("missing metadata must mean no special requirements: %+v", cfg)
	}
}
