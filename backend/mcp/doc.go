// Package mcp is the MCP resource server: the read-only tool table served on
// /mcp, the adapters that make its authentication and authorization identical to
// the ConnectRPC path, and the guidance it publishes to clients.
//
// The design is in plan/mcp_server_plan.md and the way a client uses it is in
// docs/mcp.md. `sdk_contract_test.go` pins the SDK behaviours this package
// depends on, `tool_guard_test.go` pins the invariant that a tool cannot require
// less than the RPC it wraps, and `tool_test.go` pins what each tool answers.
package mcp
