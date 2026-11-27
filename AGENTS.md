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

## What Metaxisdata Is

Metaxisdata is a self-hosted data governance and metadata platform. It connects to MySQL/TiDB and PostgreSQL instances, syncs their schema into a metadata registry, derives table- and column-level lineage (both from SQL definitions and from ingested OpenLineage events), and uses LLM-backed tools to explain SQL. A deployment is a single Go server binary backed by PostgreSQL; the frontend is a Vue 3 SPA that talks to the server over ConnectRPC.

Product surface (routes in `frontend/src/router/index.ts`, sidebar in `frontend/src/components/layout/AppSidebar.vue`):

- **Data** — instances, databases, metadata browser (instance → database → schema → table → column → manual SQL, with history), manual SQL management.
- **Lineage** — table lineage graph and column-level lineage, analyzed from view/materialized-view SQL and from OpenLineage.
- **OpenLineage** — Overview / Jobs / Datasets / Events, namespace mapping, API keys, Airflow links.
- **Explain SQL** — LLM-assisted SQL explanation scoped to selected metadata, with caching.
- **Data Quality** — rules over tables and columns (row count, nulls, uniqueness, accepted values, ranges, regex, length, referential integrity, cross-column, freshness, custom SQL, expressions, standard codes), templates and code tables, scheduled runs, a problem ledger with in-app and outbound notifications, column profiles and semantic labels, LLM-assisted drafts, streamed exports and a weekly digest. **Quality agents** add a built-in per-agent runtime under Settings → Quality agents; there is no agent token or external agent API, and external consumers read the quality surface through the read-only MCP tools. See [.agents/docs/data-quality.md](.agents/docs/data-quality.md).
- **Settings** — users, audit logs, LLM providers, OpenLineage ingestion, outbound notification channels (webhook, chat robots, SMTP email, quiet hours, owner/team routing rules), connected apps, and the `mcp_enabled` switch.
- **Device** — the `/device` page approves a command line client's device login.
- **MCP** — when `mcp_enabled` is on, `/mcp` serves the registry as read-only tools over the Model Context Protocol, and the same server is the OAuth 2.1 authorization server those clients register and authorize against (`/oauth/consent` is the page that records a user's decision). **Connected apps** (Settings for an administrator, the avatar menu for a user's own) lists those authorizations and revokes them.

Feature design documents live in `.agents/docs/`, as maintenance references to the shipped feature. Read the matching one before reworking a subsystem, and write a new one there for a new subsystem-scale feature — there is no separate `plan/` or `spec/` tree. Review reports, implementation records and phase plans are not committed: they describe a moment in time, not shipped behavior, so keep them in the pull request or the ignored `temp/` tree; if one still matters after the work lands, fold its conclusions into the subsystem reference and let git hold the rest.

## Documentation

Two trees, split by audience. `docs/` is read by operators and integrators; `.agents/docs/` holds the maintenance references an agent needs before changing a subsystem — what the feature is, what must not break, and where the code and tests live.

| Read | When |
| --- | --- |
| [docs/deploy.md](docs/deploy.md) | Building and running the container image or the prebuilt binary, and every environment variable it reads |
| [docs/local-trial.md](docs/local-trial.md) | The compose stack for a throwaway instance on a development machine |
| [docs/mcp.md](docs/mcp.md) | Connecting an MCP client, and what the endpoint promises |
| [docs/application-sql-reporting.md](docs/application-sql-reporting.md) | Letting an application report the SQL it runs, and what the deployment stores |
| [docs/notifications.md](docs/notifications.md) | The inbox, outbound channels, the delivery body and signature, retries |
| [docs/data-quality.md](docs/data-quality.md) | Running quality checks: rule setup, cadence, statuses, issues, reports, agents, retention |
| [docs/security-posture.md](docs/security-posture.md) | Behavior that looks like a bug — the deliberate, accepted decisions |
| [.agents/docs/README.md](.agents/docs/README.md) | The index of maintenance references — read the row for the subsystem you are about to change |
| [.agents/docs/i18n-glossary.md](.agents/docs/i18n-glossary.md) | The one accepted rendering of each professional term per locale — read before writing user-facing copy in either language |
| [.agents/docs/security-open-items.md](.agents/docs/security-open-items.md) | Which security findings are still open |
| [.agents/docs/backend-open-items.md](.agents/docs/backend-open-items.md) | Open engineering debt that has no subsystem reference of its own |

### Documentation rules

A document explains **why** a decision was made and **what must not break**. How the code works is the code's job: do not restate proto fields, configuration tables, command or flag lists, enum values or file inventories — link to the source instead.

- **Never write a line number.** A location written as `file:line` rots within a week; name the symbol (`ProcessRunEvent`, `splitDatasetName`) or the package.
- **No moment-in-time content.** No phase or implementation tables, work-stream IDs, decision catalogs, `Status: implemented` headers, dates-as-status, commit hashes, PR references or struck-through "landed" items. That history belongs in the pull request; git holds it.
- **One owner per fact.** The operator page states observable behavior; the maintenance reference states the invariants behind it and links to the operator page instead of restating it.
- **English, always, in both trees.** Only [docs/deploy.md](docs/deploy.md) and [docs/local-trial.md](docs/local-trial.md) have a `_zh` twin; a pair must keep its headings and environment variables in parity. User-facing copy goes through [.agents/docs/i18n-glossary.md](.agents/docs/i18n-glossary.md).
- **Budgets.** A maintenance reference stays under ~460 lines, an operator page under ~720; both under 100 KB. A page that outgrows the budget is describing too much code.
- **Never hand-edit generated or embedded Markdown.** `proto/gen/grpc-doc/` is buf output; the table in `.agents/docs/i18n-glossary.md` is generated from `frontend/glossary.json` by its script; `backend/mcp/guide.md` and `cli/skill/SKILL.md` are `//go:embed` runtime assets, not documents.
- **The gate is `make check-docs`** (`.github/workflows/docs.yml`): links, index coverage, budgets, the English rule, bilingual parity, and the bans above.

## Cross-Directory Invariants

- **Never hand-edit generated code.** `backend/generated-go/`, `frontend/src/types/proto-es/` and `proto/gen/grpc-doc/` are buf output: regenerate with `cd proto && buf generate` and commit the result together with the proto change.
- **A schema change is two files.** Append the idempotent DDL to `backend/migrator/migration/LATEST.sql` *and* add an incremental under the current version directory, keeping the `backend/store` queries in sync — see [backend/migrator/AGENTS.md](backend/migrator/AGENTS.md).
- **JSONB columns hold `protojson.Marshal` output** of the `proto/store` message named in the column's SQL comment. Change the proto and the store together; `protojson` camelCases field names, so `enter_to_send` stores as `{"enterToSend": ...}`.
- **When modifying multiple files, run the file modifications in parallel** whenever possible instead of processing them one at a time.

## Code Style

Applies everywhere; language-specific rules are in the nested files.

- Follow the Google style guides (Go: https://google.github.io/styleguide/go/). API and proto design follows AIPs (https://google.aip.dev/general), which win when they conflict with the proto guide — enum values use `HELLO`, not `TYPE_HELLO`.
- Write clean, minimal code: fewer lines is better, and simplicity beats cleverness.
- Comment only the essential: the reason, the invariant, the constraint. Don't narrate what the next line does; the code already says that.
- Use American English. Avoid plural names like `xxxList`, which invite singular/plural ambiguity.
- Use conventional commit messages (e.g. `fix(lineage): ...`).
- Run the formatter and linter for the area you touched before committing; don't hand-format.

## Tests

- Every case answers "which future diff turns this red?" If the only answer is an edit to the test, delete the case.
- A case exists for one of three reasons: a bug that happened (it fails on the pre-fix code, and its name or comment states the bug in black-box terms — the Go suite already names tests as sentences, such as `TestGoRecoversAPanickingRunner`), a derived property (a boundary, an invariant, ordering, a round-trip, a paging cursor), or bookkeeping that is easy to forget (registry completeness, a field's lifecycle, a branch that must not fire, a value copied from an outside spec).
- Test the public contract: inputs to outputs, the emitted event, the stored row's observable fields. Don't snapshot a helper tuple, an internal page table or another encoding that can change without changing behavior.
- Go: table-driven with `t.Run`; `t.Parallel()` for independent cases; `t.Cleanup` for teardown; `require` by default and `assert` where the test should keep going. Tests are hermetic — `go test ./...` needs no PostgreSQL, MySQL or Docker; anything that needs a real server carries the `integration` tag.
- TS: Vitest, colocated `*.test.ts(x)`. `utils/`, `lib/` and `composables/` are under per-file coverage thresholds, so a new file there without a test fails the run; a component or page is tested through one concrete interaction instead.
- Mock an external or slow boundary only when the assertion still checks a result, a state change, an output or an error. Don't replace the code under test; Go's fakes are hand-written (the module has no mock framework).
- An ordering or race case forces the interleaving deterministically (a channel, a `sync.WaitGroup`, an emitted event), never a `sleep` that is "long enough in practice".
- Don't weaken a case to make it pass or run faster: no dropped assertion, no stubbed code under test, no tolerance widened without measured inputs. A changed contract gets a rewritten case.
- Add a case beside the suite that owns the behavior rather than in a new file for one case.


## Development Workflow

After a code change, run that area's checks — each nested file lists the exact commands:

| Area | Gate |
| --- | --- |
| Go | `gofmt`, `golangci-lint run --allow-parallel-runners` (repeat until clean), relevant tests, build — [backend/AGENTS.md](backend/AGENTS.md) |
| Frontend | `biome:check`, `lint`, `i18n`, `type-check`, `test run` — [frontend/AGENTS.md](frontend/AGENTS.md) |
| Proto | `buf format -w proto`, `buf lint proto`, `cd proto && buf generate` — [proto/AGENTS.md](proto/AGENTS.md) |

Go integration suites run the real server against PostgreSQL + MySQL behind the `integration` build tag: `make test-integration-smoke`, `make test-integration`, `make test-integration-mysql`. A pull request runs only `make test-integration-migrator`; the full suite runs when a change lands on main and in the nightly matrix, so a change to schema sync, lineage analysis or server wiring needs the full suite run locally. See [backend/test/integration/README.md](backend/test/integration/README.md).

## Deliberate, Accepted Decisions (Not Bugs)

Behavior in this repository that looks like a defect but was chosen knowingly — the read baseline, open signup, process-local state, anonymous machine endpoints, the clear-text CLI session, permanent audit rows, log-only destructive sync — is listed, with the trade-off behind each, in [docs/security-posture.md](docs/security-posture.md). Read that page before "fixing" any of it; each item needs a subsystem-level decision rather than a drive-by patch.
