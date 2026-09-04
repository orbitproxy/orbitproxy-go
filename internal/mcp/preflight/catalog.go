package preflight

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Transport is the open MCP JSON-RPC channel used by catalog preflight steps.
type Transport interface {
	Send(ctx context.Context, request json.RawMessage) (response json.RawMessage, err error)
}

// Tool is one MCP tools/list entry needed by catalog checks.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// CatalogCheck is a catalogKey-specific preflight step run after tools/list.
// Implementations must not read Machine env or platform-stored secrets.
type CatalogCheck interface {
	CatalogKey() string
	Check(ctx context.Context, transport Transport, tools []Tool) error
}

// Registry maps catalogKey → CatalogCheck.
type Registry struct {
	mu    sync.RWMutex
	byKey map[string]CatalogCheck
}

// NewRegistry creates an empty catalog-check registry.
func NewRegistry() *Registry {
	return &Registry{byKey: make(map[string]CatalogCheck)}
}

// Register adds or replaces a catalog check.
func (r *Registry) Register(c CatalogCheck) {
	if r == nil || c == nil {
		return
	}
	key := NormalizeCatalogKey(c.CatalogKey())
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKey[key] = c
}

// Lookup returns the check for catalogKey, or nil.
func (r *Registry) Lookup(catalogKey string) CatalogCheck {
	if r == nil {
		return nil
	}
	key := NormalizeCatalogKey(catalogKey)
	if key == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byKey[key]
}

// Run executes the catalog check for catalogKey.
// Missing / unregistered key is a no-op (that MCP needs no extra preflight step).
func (r *Registry) Run(ctx context.Context, catalogKey string, transport Transport, tools []Tool) error {
	c := r.Lookup(catalogKey)
	if c == nil {
		return nil
	}
	if err := c.Check(ctx, transport, tools); err != nil {
		return fmt.Errorf("preflight (%s): %w", NormalizeCatalogKey(catalogKey), err)
	}
	return nil
}

var (
	defaultRegistry    = NewRegistry()
	registerChecksOnce sync.Once
)

func ensureDefaultChecks() {
	registerChecksOnce.Do(func() {
		defaultRegistry.Register(mysqlCatalogCheck{})
	})
}

// RegisterCatalogCheck registers a check on the process-wide default registry.
func RegisterCatalogCheck(c CatalogCheck) {
	ensureDefaultChecks()
	defaultRegistry.Register(c)
}

// RunCatalog runs the default-registry catalog preflight for this endpoint.
func RunCatalog(ctx context.Context, catalogKey string, transport Transport, tools []Tool) error {
	ensureDefaultChecks()
	return defaultRegistry.Run(ctx, catalogKey, transport, tools)
}

// CatalogKeyFromPayload reads catalogKey from endpoint local_service_payload.
func CatalogKeyFromPayload(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		CatalogKey string `json:"catalogKey"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return NormalizeCatalogKey(payload.CatalogKey)
}

// NormalizeCatalogKey lowercases and trims a catalog key.
func NormalizeCatalogKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func callTool(ctx context.Context, transport Transport, id int, name string, arguments map[string]any) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal tools/call: %w", err)
	}
	return transport.Send(ctx, payload)
}

func decodeToolCallResult(resp json.RawMessage) (text string, isError bool, rpcErr string, err error) {
	var envelope struct {
		Result *struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp, &envelope); err != nil {
		return "", false, "", fmt.Errorf("decode tools/call: %w", err)
	}
	if envelope.Error != nil {
		return "", true, envelope.Error.Message, nil
	}
	if envelope.Result == nil {
		return "", false, "", nil
	}
	msg := ""
	for _, c := range envelope.Result.Content {
		if c.Text != "" {
			msg = c.Text
			break
		}
	}
	return msg, envelope.Result.IsError, "", nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
