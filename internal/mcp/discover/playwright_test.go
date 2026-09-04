package discover

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestIsPlaywrightPayload(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "catalogKey", raw: `{"catalogKey":"playwright","localAddr":"127.0.0.1:8931"}`, want: true},
		{name: "name playwright", raw: `{"name":"Playwright","localAddr":"127.0.0.1:8931"}`, want: true},
		{name: "legacy name typo", raw: `{"name":"playright","localAddr":"127.0.0.1:8931"}`, want: true},
		{name: "other mcp", raw: `{"catalogKey":"filesystem","localAddr":"127.0.0.1:9000"}`, want: false},
		{name: "empty", raw: `{"localAddr":"127.0.0.1:8931"}`, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPlaywrightPayload(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("IsPlaywrightPayload() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLocalhostHostForAddr(t *testing.T) {
	t.Parallel()
	if got := LocalhostHostForAddr("127.0.0.1:8931"); got != "localhost:8931" {
		t.Fatalf("got %q", got)
	}
	if got := LocalhostHostForAddr("192.168.1.2:8931"); got != "" {
		t.Fatalf("non-loopback should be empty, got %q", got)
	}
}

func TestRewritePlaywrightLoopbackHost(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8931/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	rewritePlaywrightLoopbackHost(req)
	if req.Host != "localhost:8931" {
		t.Fatalf("Host = %q, want localhost:8931", req.Host)
	}
}
