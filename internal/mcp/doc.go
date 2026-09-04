// Package mcp is the MCP Gateway vertical on the OrbitProxy Machine SDK.
//
// Product concepts:
//
//   - Preflight (mcp/preflight): one-shot create/sync readiness. Always runs
//     tools/list via mcp/discover, then catalog-specific checks. Success
//     overwrites endpoint tools in the control plane.
//   - Discover Tools (mcp/discover): independent tools/list sync for manual
//     "update tools". Does not run catalog checks by default.
//   - Health Probe (package health + optional mcp/probe): continuous liveness.
//     Exec/stdio typically tracks process death; forward may TCP ping.
//
// Do not invent a third check system name (readiness, etc.).
package mcp
