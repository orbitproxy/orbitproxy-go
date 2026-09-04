package userenv

import (
	"path/filepath"
	"testing"
)

func TestNpmPrefixFromRoot(t *testing.T) {
	t.Parallel()
	if got := npmPrefixFromRoot("/usr/local/lib/node_modules"); got != "/usr/local" {
		t.Fatalf("unix prefix = %q", got)
	}
	root := filepath.Join("opt", "npm", "node_modules")
	want := filepath.Join("opt", "npm")
	if got := npmPrefixFromRoot(root); got != want {
		t.Fatalf("node_modules parent = %q, want %q", got, want)
	}
	if got := npmPrefixFromRoot(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
}
