package discover

import (
	"encoding/json"
	"net"
	"net/http"
)

// IsPlaywrightPayload detects Playwright MCP so Host rewrite stays scoped.
// Only family_key == "playwright".
func IsPlaywrightPayload(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var payload struct {
		FamilyKey string `json:"family_key"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return false
	}
	return payload.FamilyKey == "playwright"
}

// LocalhostHostForAddr maps 127.0.0.1:port / [::1]:port to localhost:port.
func LocalhostHostForAddr(localAddr string) string {
	host, port, err := net.SplitHostPort(localAddr)
	if err != nil {
		return ""
	}
	if host != "127.0.0.1" && host != "::1" {
		return ""
	}
	if port == "" {
		return "localhost"
	}
	return net.JoinHostPort("localhost", port)
}

func rewritePlaywrightLoopbackHost(req *http.Request) {
	if req == nil || req.URL == nil {
		return
	}
	hostPort := req.URL.Host
	if hostPort == "" {
		hostPort = req.Host
	}
	if host := LocalhostHostForAddr(hostPort); host != "" {
		req.Host = host
	}
}
