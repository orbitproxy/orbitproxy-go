package discover

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

// IsPlaywrightPayload detects Playwright MCP so Host rewrite stays scoped.
// Primary signal: catalogKey=playwright; also name=playwright / legacy playright.
func IsPlaywrightPayload(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var payload struct {
		CatalogKey string `json:"catalogKey"`
		Name       string `json:"name"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(payload.CatalogKey), "playwright") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(payload.Name)) {
	case "playwright", "playright":
		return true
	default:
		return false
	}
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
