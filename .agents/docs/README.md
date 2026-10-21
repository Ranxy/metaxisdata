# Agent documentation

Maintenance references for the shipped subsystems. Read the one covering the code you are about to change: the nested `AGENTS.md` beside that code holds the rules, these hold the contracts, the decisions behind them and the invariants that must not break. Operator- and integrator-facing documentation lives in [docs/](../../docs/README.md) instead.

Everything here describes **shipped behavior**, not a plan: implementation steps, phases and fixed-defect histories are not kept — that history is in git. Behavior that looks like a bug but was chosen knowingly is in [docs/security-posture.md](../../docs/security-posture.md); read it before "fixing" anything it lists.

| Reference | Covers | Read when |
| --- | --- | --- |
| [security-open-items.md](security-open-items.md) | Security findings that are still open, verified against the current code | Touching auth, credentials, outbound connections, pprof or the audit ledger |
| [backend-open-items.md](backend-open-items.md) | Open engineering debt that has no subsystem reference of its own | Touching `store`, `runner`, the request path or test infrastructure |
| [iam-and-permissions.md](iam-and-permissions.md) | Permission catalog, predefined and custom roles, the workspace IAM policy, the ACL interceptor | Touching authorization or adding an RPC |
| [sso.md](sso.md) | SSO: the identity provider admin surface, the OAuth2 login flow, subject binding and account adoption, the encrypted client secret | Touching auth, the `idp` tables, the login page or the provider settings page |
| [environments.md](environments.md) | The environment catalog, `EnvironmentService` and the selection UI | Touching instances, sync or environment scoping |
| [schemasync.md](schemasync.md) | Schema sync: what it writes, its deliberate log-only deletion, its known issues | Touching `backend/runner/schemasync/` |
| [runner.md](runner.md) | The five background runners: common lifecycle, in-process state, derived-data cadences, and the single-replica assumption | Touching `backend/runner/`, server startup/shutdown, or scheduling |
| [instance-connections.md](instance-connections.md) | The per-instance connection budget every driver-opening path leases from: the fair priority queue, the wait budgets, and which caller may block | Touching how a target driver is opened, the schema syncer's requeue, or data-quality run admission |
| [notifications.md](notifications.md) | Inbox messages: recipient and dedup rules, the server-push subscription stream | Touching sync results, ingestion failures or the notification UI |
| [data-quality.md](data-quality.md) | The data quality subsystem: 18 rule types and their judgment semantics, the scheduled/manual runner, the issue ledger and inbox notifications, the three LLM analyses with their quotas, and the permissions and settings around them | Touching `backend/component/dataquality/`, `backend/runner/qualityrunner/`, the `data_quality_*` handlers, the `quality_*` tables or the data-quality pages |
| [mcp-server.md](mcp-server.md) | The MCP resource server, its tools, the OAuth 2.1 authorization server, and the security invariants | Touching `backend/mcp/` or `backend/api/oauth/` |
| [credential-encryption.md](credential-encryption.md) | The `v1:` AES-256-GCM credential store, its key hierarchy and its residuals | Touching stored credentials or `backend/common/crypto` |
| [rate-limiting.md](rate-limiting.md) | Every request budget: where it is enforced, what it counts, what it does not bound | Adding an endpoint or changing a budget |
| [refresh-tokens.md](refresh-tokens.md) | Rotating refresh tokens for MCP OAuth clients and web sessions | Touching `/oauth/token`, `AuthService.Refresh` or `revoked_token` |
| [connected-apps.md](connected-apps.md) | The Connected apps surface over MCP OAuth grants: list/filter/derived state, revoke, Logout semantics, grant-liveness enforcement | Touching `ConnectedAppService`, the grant row, `/oauth/token` rotation, or the connected-app pages |
| [object-ddl-storage.md](object-ddl-storage.md) | Per-object DDL storage, the sync hook that fills it, and the `GetSchemaString` read path | Touching DDL capture or `meta_registry_resource_schema` |
| [starrocks-doris-driver.md](starrocks-doris-driver.md) | The StarRocks/Doris driver over the MySQL wire protocol and its engine plumbing | Touching `backend/plugin/db/starrocks/` |
| [cli-mxd.md](cli-mxd.md) | The `mxd` client: addressing, exit codes, scopes, device login | Touching `cli/` |
| [frontend-ui.md](frontend-ui.md) | Layout and information-architecture conventions, the OpenLineage pages, lineage node/edge encoding | Touching the shell, a page header, settings navigation, or the lineage canvas |
| [i18n-glossary.md](i18n-glossary.md) | The one accepted rendering of every professional term per locale, the rule model behind it, and the generated term table | Writing user-facing copy, translating `src/locales/*.json`, or renaming a concept |
| [lineage-analyzer.md](lineage-analyzer.md) | Lineage package layout, assembly, runner lifecycle and change detection, the MySQL-family generator, the AST coverage audit method | Touching `backend/plugin/lineage/` or the lineage runner |
| [lineage-semantics.md](lineage-semantics.md) | What each analyzer must keep producing: the transformation model, per-dialect semantics, engine-measured clause visibility | Changing what an analyzer emits |
| [lineage-graph.md](lineage-graph.md) | The field trail and the field-scoped expansion on the lineage graph page | Touching the lineage canvas interaction |
| [application-sql-ingestion.md](application-sql-ingestion.md) | What an application's reported SQL becomes: the sanitizer and its fingerprint, the ingest gate, the two-level aggregation, the read/write classification, and the third canvas origin | Touching `backend/plugin/sqlsanitize/`, `dmltarget`, `store/application*.go`, the reporting endpoint or the application SQL pages |
| [openlineage-lineage.md](openlineage-lineage.md) | How an ingested OpenLineage run becomes lineage edges, and why the producer's column facet is not trusted | Touching OpenLineage ingestion or its lineage writer |
| [mssql-lineage.md](mssql-lineage.md) | The T-SQL dialect: what its AST shape forces, what it models and what it reports as a gap, and how it splits a script | Touching `backend/plugin/lineage/mssql/` |
| [doris-lineage.md](doris-lineage.md) | The Doris dialect: how its AST differs from StarRocks', the LATERAL VIEW relation it adds, and where its MV definition comes from | Touching `backend/plugin/lineage/doris/` |
| [omni-upstream-defects.md](omni-upstream-defects.md) | Defects in the pinned `bytebase/omni` dependency, and the deliberate no-patch/no-report policy | Debugging a parser or AST surprise, or considering a dependency bump |
| [docker-build.md](docker-build.md) | The container image and the release binaries: build stages, proxy and mirror arguments, runtime environment, and the build metadata they report | Changing the Dockerfile, the build scripts, the release workflows, or the injected version metadata |

A new subsystem-scale design belongs here: add the reference file and a row in this table, rather than a separate plan document.
