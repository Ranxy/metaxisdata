// Package mcp will hold the MCP resource server: the tool table exposed on
// /mcp, the adapters that keep its authentication and authorization identical
// to the ConnectRPC path, and the guidance it publishes to clients.
//
// Today it holds only the Phase 0 contract tests for the MCP Go SDK, which pin
// the behaviors the design depends on. The design is in
// plan/mcp_server_plan.md; the tests deliberately touch no Backend code.
package mcp
