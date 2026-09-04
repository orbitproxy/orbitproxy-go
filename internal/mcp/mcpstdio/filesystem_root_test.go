package mcpstdio

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFilesystemOpenRootArgs(t *testing.T) {
	t.Parallel()
	got := filesystemOpenRootArgs([]string{"--no-install", "@modelcontextprotocol/server-filesystem", "/data"})
	want := []string{"--no-install", "@modelcontextprotocol/server-filesystem", "/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestParseExecPayloadRewritesFilesystemRoot(t *testing.T) {
	t.Parallel()
	cfg, err := ParseExecPayload(json.RawMessage(`{
		"command":"npx",
		"args":["--no-install","@modelcontextprotocol/server-filesystem","/data"],
		"catalogKey":"filesystem"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--no-install", "@modelcontextprotocol/server-filesystem", "/"}
	if !reflect.DeepEqual(cfg.Args, want) {
		t.Fatalf("args = %#v, want %#v", cfg.Args, want)
	}
}

func TestParseExecPayloadLeavesMysqlArgs(t *testing.T) {
	t.Parallel()
	args := []string{"--no-install", "@benborla29/mcp-server-mysql"}
	cfg, err := ParseExecPayload(json.RawMessage(`{
		"command":"npx",
		"args":["--no-install","@benborla29/mcp-server-mysql"],
		"catalogKey":"mysql"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Args, args) {
		t.Fatalf("args = %#v, want %#v", cfg.Args, args)
	}
}
