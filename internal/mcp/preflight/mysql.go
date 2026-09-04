package preflight

import (
	"context"
	"fmt"
)

// mysqlCatalogCheck is the mysql catalog preflight: tools/call mysql_query SELECT 1.
type mysqlCatalogCheck struct{}

func (mysqlCatalogCheck) CatalogKey() string { return "mysql" }

func (mysqlCatalogCheck) Check(ctx context.Context, transport Transport, tools []Tool) error {
	toolName := ""
	for _, tool := range tools {
		if tool.Name == "mysql_query" {
			toolName = tool.Name
			break
		}
	}
	if toolName == "" {
		return fmt.Errorf("mysql_query tool not found after tools/list")
	}

	resp, err := callTool(ctx, transport, 3, toolName, map[string]any{
		"sql": "SELECT 1",
	})
	if err != nil {
		return fmt.Errorf("mysql_query SELECT 1: %w", err)
	}

	text, isError, rpcErr, err := decodeToolCallResult(resp)
	if err != nil {
		return err
	}
	if rpcErr != "" {
		return fmt.Errorf("%s (check local MCP/MySQL configuration)", rpcErr)
	}
	if isError {
		msg := text
		if msg == "" {
			msg = "tool returned error"
		}
		return fmt.Errorf("%s (check local MCP/MySQL configuration)", truncate(msg, 300))
	}
	return nil
}
