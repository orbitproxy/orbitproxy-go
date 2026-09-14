// Package preflight implements create/sync-time checks for MCP endpoints.
//
// Official (SkipProtocol / family_key): CheckCommand + package version only.
// Custom: CheckCommand (exec) + mcp/discover tools/list following nextCursor.
//
// Independent manual "update tools" stays in mcp/discover + mcp_discover_tools.
//
// Health Probe (package health) is a separate runtime concept — do not mix.
package preflight
