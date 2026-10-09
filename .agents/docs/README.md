# Agent documentation

Maintenance references for the shipped subsystems. Read the one covering the code you are about to change: the nested `AGENTS.md` beside that code holds the rules, these hold the contracts, the decisions behind them and the invariants that must not break. Operator- and integrator-facing documentation lives in [docs/](../../docs/README.md) instead.

Everything here describes **shipped behavior**, not a plan: implementation steps, phases and fixed-defect histories are not kept — that history is in git. Behavior that looks like a bug but was chosen knowingly is in [docs/security-posture.md](../../docs/security-posture.md); read it before "fixing" anything it lists.

| Reference | Covers | Read when |
| --- | --- | --- |
| [security-open-items.md](security-open-items.md) | Security findings that are still open, verified against the current code | Touching auth, credentials, outbound connections, pprof or the audit ledger |
| [backend-open-items.md](backend-open-items.md) | Open engineering debt that has no subsystem reference of its own | Touching `store`, `runner`, the request path or test infrastructure |
| [iam-and-permissions.md](iam-and-permissions.md) | Permission catalog, predefined and custom roles, the workspace IAM policy, the ACL interceptor | Touching authorization or adding an RPC |
| [environments.md](environments.md) | The environment catalog, `EnvironmentService` and the selection UI | Touching instances, sync or environment scoping |
| [schemasync.md](schemasync.md) | Schema sync: what it writes, its deliberate log-only deletion, its known issues | Touching `backend/runner/schemasync/` |
| [notifications.md](notifications.md) | Inbox messages: recipient and dedup rules, the server-push subscription stream | Touching sync results, ingestion failures or the notification UI |
| [mcp-server.md](mcp-server.md) | The MCP resource server, its tools, the OAuth 2.1 authorization server, and the security invariants | Touching `backend/mcp/` or `backend/api/oauth/` |
| [credential-encryption.md](credential-encryption.md) | The `v1:` AES-256-GCM credential store, its key hierarchy and its residuals | Touching stored credentials or `backend/common/crypto` |
| [rate-limiting.md](rate-limiting.md) | Every request budget: where it is enforced, what it counts, what it does not bound | Adding an endpoint or changing a budget |
| [refresh-tokens.md](refresh-tokens.md) | Rotating refresh tokens for MCP OAuth clients and web sessions | Touching `/oauth/token`, `AuthService.Refresh` or `revoked_token` |
| [object-ddl-storage.md](object-ddl-storage.md) | Per-object DDL storage, the sync hook that fills it, and the `GetSchemaString` read path | Touching DDL capture or `meta_registry_resource_schema` |
| [starrocks-doris-driver.md](starrocks-doris-driver.md) | The StarRocks/Doris driver over the MySQL wire protocol and its engine plumbing | Touching `backend/plugin/db/starrocks/` |
| [cli-mxd.md](cli-mxd.md) | The `mxd` client: addressing, exit codes, scopes, device login | Touching `cli/` |
| [frontend-ui.md](frontend-ui.md) | Layout and information-architecture conventions, the OpenLineage pages, lineage node/edge encoding | Touching the shell, a page header, settings navigation, or the lineage canvas |
| [lineage-analyzer.md](lineage-analyzer.md) | Lineage package layout, assembly, runner lifecycle and change detection, the MySQL-family generator, the AST coverage audit method | Touching `backend/plugin/lineage/` or the lineage runner |
| [lineage-semantics.md](lineage-semantics.md) | What each analyzer must keep producing: the transformation model, per-dialect semantics, engine-measured clause visibility | Changing what an analyzer emits |
| [lineage-graph.md](lineage-graph.md) | The field trail and the field-scoped expansion on the lineage graph page | Touching the lineage canvas interaction |
| [openlineage-lineage.md](openlineage-lineage.md) | How an ingested OpenLineage run becomes lineage edges, and why the producer's column facet is not trusted | Touching OpenLineage ingestion or its lineage writer |
| [omni-upstream-defects.md](omni-upstream-defects.md) | Defects in the pinned `bytebase/omni` dependency, and the deliberate no-patch/no-report policy | Debugging a parser or AST surprise, or considering a dependency bump |
| [docker-build.md](docker-build.md) | The container image and the release binaries: build stages, proxy and mirror arguments, runtime environment, and the build metadata they report | Changing the Dockerfile, the build scripts, the release workflows, or the injected version metadata |

A new subsystem-scale design belongs here: add the reference file and a row in this table, rather than a separate plan document.
