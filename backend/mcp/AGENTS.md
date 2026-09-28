# backend/mcp/AGENTS.md

The MCP resource server: one stateless endpoint (`/mcp`) that exposes the metadata
registry and its lineage as read-only tools. The root [AGENTS.md](../../AGENTS.md)
and [backend/AGENTS.md](../AGENTS.md) still apply.

- **Every tool goes through `Server.dispatch`.** Identity, permission check, the
  call and the audit row happen there, in that order, once. A tool registered
  anywhere else would lose its authorization check.
- **A tool declares `RPC` and `Permission`.** `tool_guard_test.go` looks the RPC up
  in protoregistry and requires the permission to equal that method's
  `metaxisdata.v1.permission` annotation, and to be one of `readPermissions`. Add
  a read permission there deliberately; never add a write one. A tool that wraps
  an annotation-free method goes into `methodsWithoutAPermission` instead, which
  is the only allowlist.
- **Never build a GUID.** `ref.go` resolves a name path through the same read RPCs
  a client could call, so the engine-specific segment layout stays the server's
  business. A resolution that matches nothing, or several things, returns the
  candidates instead of guessing.
- **Tools return plain Go values**; `dispatch` marshals them into
  `structuredContent` and nothing else. A protobuf message must be rendered with
  the local `protoJSON` helper first — `encoding/json` would use the generated
  struct tags (snake_case) and break the field-name contract the CLI and the
  gateway share.
- **The SDK's generic `AddTool` is deliberately not used.** It appends a text copy
  of the structured result, which doubles every listing; `sdk_contract_test.go`
  measures that difference, so the choice is not re-litigated by accident.
- **The endpoint is stateless** (required for protocol revision 2026-07-28), so
  there is no session to keep anything in: a tool gets what it needs from the
  request or from the store.
- **`sdk_contract_test.go` pins the SDK behaviours this package depends on** —
  the protocol revision, the identity reaching a handler, the one-copy wire shape,
  the challenge shape. Run it first when upgrading the SDK.
