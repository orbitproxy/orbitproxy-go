// Package preflight implements create/sync-time checks for MCP endpoints.
//
// Run = CheckCommand (exec) + mcp/discover tools/list + CatalogCheck(catalogKey).
// Success returns tools for control-plane overwrite.
//
// Independent manual "update tools" stays in mcp/discover + mcp_discover_tools;
// that path does not run CatalogCheck.
//
// Health Probe (package health) is a separate runtime concept — do not mix.
package preflight
