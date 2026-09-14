// Package mcp is the MCP Gateway vertical on the OrbitProxy Machine SDK.
//
// Product concepts:
//
//   - Preflight (mcp/preflight): one-shot create/sync readiness. Official
//     runs CheckCommand + package version only. Custom lists via mcp/discover
//     following nextCursor. Success overwrites custom endpoint tools.
//   - Discover Tools (mcp/discover): independent tools/list sync for manual
//     "update tools". Does not run catalog checks by default.
//   - Health Probe (package health + optional mcp/probe): continuous liveness.
//     Exec/stdio typically tracks process death; forward may TCP ping.
//
// Do not invent a third check system name (readiness, etc.).
package mcp
