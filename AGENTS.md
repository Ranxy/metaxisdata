# AGENTS.md

Guidance for AI coding assistants (Claude Code, Codex, Copilot) working in this repository. `CLAUDE.md` is a symlink to this file — edit this one, never the symlink.

This file is the router: it holds only rules that apply across the whole repository. Directory-specific detail lives in nested `AGENTS.md` files beside the code, which are loaded and scoped automatically. **Before changing code under a directory, read its nested file — its rules win over this one.**

| Working in | Read first |
| --- | --- |
| `backend/` — Go server | [backend/AGENTS.md](backend/AGENTS.md) |
| `backend/plugin/lineage/` — lineage analyzers | [backend/plugin/lineage/AGENTS.md](backend/plugin/lineage/AGENTS.md) |
| `backend/migrator/` — schema and migrations | [backend/migrator/AGENTS.md](backend/migrator/AGENTS.md) |
| `backend/mcp/` — the MCP resource server | [backend/mcp/AGENTS.md](backend/mcp/AGENTS.md) |
| `frontend/` — Vue 3 SPA | [frontend/AGENTS.md](frontend/AGENTS.md) |
| `proto/` — ConnectRPC and store schemas | [proto/AGENTS.md](proto/AGENTS.md) |
| `cli/` — the `mxd` client | [cli/AGENTS.md](cli/AGENTS.md) |
| Connecting an MCP client, and what the endpoint promises | [docs/mcp.md](docs/mcp.md) |
| Behavior that looks like a bug | [docs/security-posture.md](docs/security-posture.md) |

## What Metaxisdata Is

Metaxisdata is a self-hosted data governance and metadata platform. It connects to MySQL/TiDB and PostgreSQL instances, syncs their schema into a metadata registry, derives table- and column-level lineage (both from SQL definitions and from ingested OpenLineage events), and uses LLM-backed tools to explain SQL. A deployment is a single Go server binary backed by PostgreSQL; the frontend is a Vue 3 SPA that talks to the server over ConnectRPC.

Product surface (routes in `frontend/src/router/index.ts`, sidebar in `frontend/src/components/layout/AppSidebar.vue`):

- **Data** — instances, databases, metadata browser (instance → database → schema → table → column → manual SQL, with history), manual SQL management.
- **Lineage** — table lineage graph and column-level lineage, analyzed from view/materialized-view SQL and from OpenLineage.
- **OpenLineage** — Overview / Jobs / Datasets / Events, namespace mapping, API keys, Airflow links.
- **Explain SQL** — LLM-assisted SQL explanation scoped to selected metadata, with caching.
- **Settings** — users, audit logs, LLM providers, OpenLineage ingestion, and the `mcp_enabled` switch.
- **Device** — the `/device` page approves a command line client's device login.
- **MCP** — when `mcp_enabled` is on, `/mcp` serves the registry as read-only tools over the Model Context Protocol, and the same server is the OAuth 2.1 authorization server those clients register and authorize against (`/oauth/consent` is the page that records a user's decision).

Feature design documents live in `spec/` (product/UX specs) and `plan/` (implementation plans). Read the matching doc before reworking one of these subsystems, and add one there for new subsystem-scale features.

## Cross-Directory Invariants

- **Never hand-edit generated code.** `backend/generated-go/`, `frontend/src/types/proto-es/` and `proto/gen/grpc-doc/` are buf output: regenerate with `cd proto && buf generate` and commit the result together with the proto change.
- **A schema change is two files.** Append the idempotent DDL to `backend/migrator/migration/LATEST.sql` *and* add an incremental under the current version directory, keeping the `backend/store` queries in sync — see [backend/migrator/AGENTS.md](backend/migrator/AGENTS.md).
- **JSONB columns hold `protojson.Marshal` output** of the `proto/store` message named in the column's SQL comment. Change the proto and the store together; `protojson` camelCases field names, so `enter_to_send` stores as `{"enterToSend": ...}`.
- **When modifying multiple files, run the file modifications in parallel** whenever possible instead of processing them one at a time.

## Code Style

Applies everywhere; language-specific rules are in the nested files.

- Follow the Google style guides (Go: https://google.github.io/styleguide/go/). API and proto design follows AIPs (https://google.aip.dev/general), which win when they conflict with the proto guide — enum values use `HELLO`, not `TYPE_HELLO`.
- Write clean, minimal code: fewer lines is better, and simplicity beats cleverness.
- Comment only what is essential to understand functionality or is non-obvious.
- Use American English. Avoid plural names like `xxxList`, which invite singular/plural ambiguity.
- Use conventional commit messages (e.g. `fix(lineage): ...`).
- Run the formatter and linter for the area you touched before committing; don't hand-format.

## Development Workflow

After a code change, run that area's checks — each nested file lists the exact commands:

| Area | Gate |
| --- | --- |
| Go | `gofmt`, `golangci-lint run --allow-parallel-runners` (repeat until clean), relevant tests, build — [backend/AGENTS.md](backend/AGENTS.md) |
| Frontend | `biome:check`, `lint`, `i18n`, `type-check`, `test run` — [frontend/AGENTS.md](frontend/AGENTS.md) |
| Proto | `buf format -w proto`, `buf lint proto`, `cd proto && buf generate` — [proto/AGENTS.md](proto/AGENTS.md) |

Go integration suites run the real server against PostgreSQL + MySQL behind the `integration` build tag: `make test-integration-smoke`, `make test-integration`, `make test-integration-mysql`. See [backend/test/integration/README.md](backend/test/integration/README.md).

## Deliberate, Accepted Decisions (Not Bugs)

These look like defects but were chosen knowingly. Read [docs/security-posture.md](docs/security-posture.md) before "fixing" any of them.

- **Authorization is workspace-scoped (single tenant)** — every member reads everything; writes need `workspaceAdmin`.
- **Stored credentials are obfuscated, not encrypted** — database read access recovers them all.
- **The CLI stores its token in clear text** (mode 0600).
- **Analysis scopes live only in the process environment** (`METAXISDATA_SCOPES`), never persisted.
- **Device login state is process-local** — run one replica or add sticky routing.
- **gRPC reflection is anonymous.**
- **Destructive schema sync is log-only** — no shrink threshold or confirmation flag.
- **`audit_log` and `meta_registry_resource_history` are kept forever.**
- **The MCP surface is off by default and its tokens are bound to `/mcp`** — enabling it needs `external_url`, and changing that address invalidates outstanding MCP tokens.
- **Every MCP tool call is audited, reads included** — a remote entry point a model drives, recorded in the permanent ledger.
- **OAuth pending state is process-local and no refresh token is issued** — the same replica must see an approval and its completion, and an expired token means running the flow again.
- **Reverse-proxy contract** — cookie writes trust `Origin`/`Sec-Fetch-Site`; audit trusts `X-Forwarded-For` only from `--trusted-proxies`.
