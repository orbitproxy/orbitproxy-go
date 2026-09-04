// Package health is the unified runtime health model for every endpoint.
//
// Health answers: is this endpoint healthy right now, and why?
// Strategies may be combined per delivery mode:
//
//   - ActiveProbe: periodic Check (e.g. TCP dial, optional MCP ping)
//   - Passive observation: process death, dial failure, broken pipe, etc.
//
// Either path marks the endpoint unhealthy. Supporting both is dual insurance.
//
// This is separate from create/sync jobs (mcp/preflight, mcp/discover), which
// use request/response results and are not the health state machine.
package health
