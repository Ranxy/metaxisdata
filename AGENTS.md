# AGENTS.md

This file provides guidance to AI coding assistants (Claude Code, Codex, Copilot) when working with code in this repository. `CLAUDE.md` is a symlink to this file — edit this one, never the symlink.

## What Metaxisdata Is

Metaxisdata is a self-hosted data governance and metadata platform. It connects to MySQL/TiDB and PostgreSQL instances, syncs their schema into a metadata registry, derives table- and column-level lineage (both from SQL definitions and from ingested OpenLineage events), and uses LLM-backed tools to explain SQL. A deployment is a single Go server binary backed by PostgreSQL; the frontend is a Vue 3 SPA that talks to the server over ConnectRPC.

Product surface (frontend routes in `frontend/src/router/index.ts`, sidebar in `frontend/src/components/layout/AppSidebar.vue`):

- **Data** — instances, databases, metadata browser (instance → database → schema → table → column → manual SQL, with history), manual SQL management.
- **Lineage** — table lineage graph and column-level lineage, analyzed from view/materialized-view SQL and from OpenLineage.
- **OpenLineage** — Overview / Jobs / Datasets / Events, namespace mapping, API keys, Airflow links.
- **Explain SQL** — LLM-assisted SQL explanation scoped to selected metadata, with caching.
- **Settings** — users, audit logs, LLM providers, OpenLineage ingestion.

Feature design documents live in `spec/` (product/UX specs) and `plan/` (implementation plans). Read the matching doc before reworking one of these subsystems, and add one there for new subsystem-scale features.

## Project Architecture

### Services and Package Ownership

| Path | Owns |
| --- | --- |
| `backend/bin/server/` | Server entrypoint and CLI flags/profile (`cmd/root.go`); requires `PG_URL` |
| `backend/server/` | HTTP wiring: Echo middleware and routes, ConnectRPC handler registration and interceptors, pprof, graceful shutdown |
| `backend/api/v1/` | ConnectRPC service implementations (`UserService`, `AuthService`, `InstanceService`, `DatabaseService`, `LineageService`, `OpenLineageService`, `LLMService`, `ExplainSQLService`, `AuditLogService`) plus the audit/debug interceptors |
| `backend/api/auth/` | JWT issuance and verification, auth interceptor, token extraction from metadata/headers |
| `backend/store/` | PostgreSQL store: table→message mapping, JSONB `protojson` columns, LRU caches, connection pool |
| `backend/component/dbfactory/` | Builds a `db.Driver` from an instance's data source |
| `backend/component/llm/` | LLM provider registry and the iterative agent loop (tools, events, messages) |
| `backend/component/state/` | In-memory server state (token-expire cache, per-instance connection limiter) |
| `backend/plugin/db/` | `db.Driver` interface and MySQL/PostgreSQL drivers |
| `backend/plugin/schema/` | Schema sync, diff, and migration-DDL generation (MySQL/PG) |
| `backend/plugin/lineage/` | Table/column lineage analyzers, catalog, and scope resolution (MySQL/PG) |
| `backend/plugin/openlineage/` | OpenLineage event parsing, processing, resolution, and Airflow links |
| `backend/plugin/idp/` | Identity providers (OAuth2/OIDC/LDAP) |
| `backend/plugin/metric/` | Metric collection and reporting |
| `backend/runner/` | Background runners: `lineageanalyzer`, `schemasync`, `maintenance` |
| `backend/migrator/` | Embedded, versioned schema migrations (`migration/LATEST.sql` + incrementals) and the startup migrator |
| `backend/generated-go/` | Generated protobuf/Connect/Gateway code — never hand-edit |
| `frontend/src/` | Vue 3 + TypeScript SPA (Vite, Pinia, vue-router, Tailwind, shadcn-vue) |
| `proto/v1/`, `proto/store/` | Public ConnectRPC service definitions and database row shapes |
| `spec/`, `plan/` | Feature specs and implementation plans |

### Protocol

- `proto/v1/` defines the public ConnectRPC services (package `metaxisdata.v1`); `proto/store/` defines the row shapes stored in database JSONB columns.
- `cd proto && buf generate` regenerates Go code into `backend/generated-go/`, frontend types into `frontend/src/types/proto-es/` (only `metaxisdata.v1`), and API docs into `proto/gen/grpc-doc/`. Generated output is committed but never hand-edited.
- Commit the regenerated `backend/generated-go/`, `frontend/src/types/proto-es/`, and `proto/gen/grpc-doc/` output together with the proto change.

### Database Schema and Migrations

- `backend/migrator/` owns the metadata schema. `migration/LATEST.sql` is the cumulative schema at the newest version; `migration/{MAJOR.MINOR}/{NNNN}##{desc}.sql` are forward-only incremental migrations (the current version line is `0.1`, baseline version `0.1.0`).
- `migrator.MigrateSchema` runs in-process on every server startup, before any subsystem reads the schema. Fresh installs apply `LATEST.sql`; existing deployments apply only the pending incrementals; a database that already has the schema but predates the framework is adopted at the baseline version. A session-level advisory lock serializes migrations across replicas.
- `schema_migration_history` is the version ledger. Never write it from application code.
- Consequence: every schema change must BOTH append the idempotent DDL to `migration/LATEST.sql` (fresh installs) AND add an incremental file under the current `{MAJOR.MINOR}` directory (existing deployments). Keep `backend/store` queries, `LATEST.sql`, and the incremental in sync in the same change. See `plan/schema_migration_plan.md`.
- JSONB columns hold `protojson.Marshal` output of the `proto/store` message named in the column's SQL comment. When you add or remove proto fields, update the proto and the store together.

### Store Layer

- `backend/store/` maps database tables to Go. Store unit tests are hermetic — they need no live database. When a query's shape is itself the invariant (scoping predicates, GUID-subtree escaping, history mutations), add a guard test in the same package (see `backend/store/meta_resource_test.go` for the pattern).

## Testing

The default Go suite is hermetic: `go test ./...` needs no PostgreSQL, MySQL, or Docker.

Integration suites are gated by the `integration` build tag and run the real server against real PostgreSQL + MySQL:

| Mode | How | Notes |
| --- | --- | --- |
| Local (default) | `make test-integration` / `make test-integration-smoke` / `make test-integration-mysql` | Uses `testcontainers-go` to start PostgreSQL + MySQL; requires a working Docker daemon and skips (exit 0) when Docker is unavailable |
| External services (CI) | Set the env vars below, then run the same targets | Connects to already-running services; only container creation is skipped |

External-service env vars:

- `INTEGRATION_POSTGRES_HOST`, `INTEGRATION_POSTGRES_PORT`, `INTEGRATION_POSTGRES_DB` (optional, defaults to `metaxisdata`)
- `INTEGRATION_MYSQL_HOST`, `INTEGRATION_MYSQL_PORT`
- optional credential overrides: `INTEGRATION_POSTGRES_USER`/`INTEGRATION_POSTGRES_PASSWORD`, `INTEGRATION_MYSQL_USER`/`INTEGRATION_MYSQL_PASSWORD`

In both modes the harness performs readiness checks, runs the schema migrator (`backend/migrator`), and seeds the MySQL fixture schema. Partial env config fails fast rather than silently mixing modes. The external PostgreSQL database is only recreated when its name is the derived `{INTEGRATION_POSTGRES_DB}_{scope}_integration` one — the configured base database is never dropped. `make test-integration` and `make test-integration-smoke` also run `./backend/migrator/...`, which covers fresh install, incremental upgrade and legacy adoption. Full details: `backend/test/integration/README.md`.

Frontend tests are Vitest with jsdom (`frontend/vitest.config.ts`), colocated with source as `*.test.ts(x)`.

## Development Workflow

**ALWAYS follow these steps after making code changes:**

### Go Code Changes

1. **Format**: Run `gofmt -w` on modified files
2. **Lint**: Run `golangci-lint run --allow-parallel-runners` to catch issues
   - **Important**: Run golangci-lint repeatedly until there are no issues. The linter has a max-issues limit and may not show all issues in a single run.
3. **Auto-fix**: Use `golangci-lint run --fix --allow-parallel-runners` to fix issues automatically
4. **Test**: Run relevant tests before committing, plus the integration suites when your change touches schema sync, lineage analysis, or server wiring (see Testing)
5. **Build**: `go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go`
6. **Tidy**: After changing Go dependencies, run `go mod tidy` to clean up `go.mod` and `go.sum`

### Frontend Code Changes

1. **Format + lint + imports** — Run `pnpm --dir frontend biome:check` (Biome over `src/`, excluding generated `src/types/proto-es/`) or `cd frontend && pnpm biome check --write <path>` for specific files
2. **Lint** — Run `pnpm --dir frontend lint` (ESLint: Vue rules plus vue-i18n missing/unused key checks; the script already applies `--fix`)
3. **Type check** — Run `pnpm --dir frontend type-check`
4. **Test** — Run `pnpm --dir frontend test run`

### Proto Changes

1. **Format**: Run `buf format -w proto`
2. **Lint**: Run `buf lint proto`
3. **Generate**: Run `cd proto && buf generate`
4. Commit the regenerated `backend/generated-go/`, `frontend/src/types/proto-es/`, and `proto/gen/grpc-doc/` output together with the proto change

## Build/Test Commands

### Backend

```bash
# Build for deployment: `make build-release` (adds -tags release, which selects the prod profile)
go build -ldflags "-w -s" -p=16 -tags release -o ./build/metaxisdata ./backend/bin/server/main.go

# Build without the release tag: dev profile, wide-open CORS. Local development only.
go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go

# Start backend (requires PG_URL; default port 8080 matches the frontend vite proxy)
PG_URL='postgres://dev:dev@localhost:5432/metaxisdata?sslmode=disable' go run ./backend/bin/server/main.go --port 8080 --debug

# Run a single test
go test -v -count=1 github.com/Ranxy/metaxisdata/backend/store -run ^TestFunctionName$

# Run multiple tests
go test -v -count=1 github.com/Ranxy/metaxisdata/backend/api/v1 -run ^(TestFunctionName|TestFunctionNameTwo)$

# Lint
golangci-lint run --allow-parallel-runners

# Tidy after dependency changes
go mod tidy
```

### Integration Tests

```bash
# All integration-tagged tests (real server + testcontainers by default)
make test-integration-smoke

# Real-server schemasync/lineage scenarios plus the migrator suite
make test-integration

# MySQL real-server scenarios only
make test-integration-mysql

# CI mode: use existing services instead of testcontainers
INTEGRATION_POSTGRES_HOST=127.0.0.1 INTEGRATION_POSTGRES_PORT=5432 INTEGRATION_POSTGRES_DB=metaxisdata \
INTEGRATION_MYSQL_HOST=127.0.0.1 INTEGRATION_MYSQL_PORT=3306 \
make test-integration
```

### Frontend

```bash
# Install dependencies
pnpm --dir frontend i

# Dev server (http://localhost:3000; proxies /v1 and /metaxisdata.v1 to localhost:8080)
pnpm --dir frontend dev

# Format + lint + organize imports (Biome over src/)
pnpm --dir frontend biome:check

# Lint only (ESLint; Vue + i18n rules, applies --fix)
pnpm --dir frontend lint

# Type check
pnpm --dir frontend type-check

# Test (watch mode; use "test run" for a one-shot CI run)
pnpm --dir frontend test
pnpm --dir frontend test run
pnpm --dir frontend test:coverage

# Production build
pnpm --dir frontend build
```

### Proto

```bash
# Format
buf format -w proto

# Lint
buf lint proto

# Generate
cd proto && buf generate
```

### Database

```bash
# Connect to the PostgreSQL store
psql -h localhost -p 5432 -U <user> -d metaxisdata -c "sql"
```

## Code Style

- **General**: Follow Google style guides for all languages
  - **Go**: https://google.github.io/styleguide/go/
- **Conciseness**: Write clean, minimal code; fewer lines is better. Prioritize simplicity for effective and maintainable software.
- **Comments**: Only include comments that are essential to understanding functionality or convey non-obvious information
- **Go**: Use standard Go error handling with detailed error messages
- **API and Proto**: Follow AIPs at https://google.aip.dev/general. When AIP and the proto guide conflict, AIP takes precedence. For example, use HELLO for enum names, not TYPE_HELLO.
- **Naming**: Use American English, avoid plurals like "xxxList" for simplicity and to prevent singular/plural ambiguity stemming from poor design
- **Git**: Follow conventional commit format
- **Imports**: Use organized imports (sorted by the import path); goimports runs with the local prefix `github.com/Ranxy/metaxisdata`
- **Formatting**: Use linting/formatting tools before committing
- **Error Handling**: Be explicit but concise about error cases
- **API Errors**: Errors returned from `backend/api/v1` and `backend/store` carry a `common.Code` — use `common.Errorf(code, ...)`, `common.Wrap(err, code)`, or `common.Wrapf(err, code, ...)` rather than bare `fmt.Errorf`, so Connect handlers can map them to status codes
- **Go Resources**: Always use `defer` for resource cleanup like `rows.Close()` (sqlclosecheck)
- **Go Defer**: Avoid using `defer` inside loops (revive) - use IIFE or scope properly
- **Frontend**: Vue 3 + TypeScript. All user-facing display text goes through vue-i18n in `frontend/src/locales/{en-US,zh-CN}.json` — add the key to both locales, and note that ESLint enforces missing/unused keys. Prefer shared shadcn-vue primitives from `frontend/src/components/ui/` over hand-rolled markup. Call the API through the ConnectRPC clients in `frontend/src/api/client.ts`, not ad-hoc fetch.

## Common Go Lint Rules

Always follow these guidelines to avoid common linting errors:

- **Unused Parameters**: Prefix unused parameters with underscore (e.g., `func foo(_ *Bar)`)
- **Modern Go Conventions**: Use `any` instead of `interface{}` (since Go 1.18)
- **Confusing Naming**: Avoid similar names that differ only by capitalization
- **Identical Branches**: Don't use if-else branches that contain identical code
- **Unused Functions**: Mark unused functions with `// nolint:unused` comment if needed for future use
- **Function Receivers**: Don't create unnecessary function receivers; use regular functions if receiver is unused
- **Proper Import Ordering**: Maintain correct grouping and ordering of imports
- **Consistency**: Keep function signatures, naming, and patterns consistent with existing code
- **Export Rules**: Only export (capitalize) functions and types that need to be used outside the package
- **Linting Command**: Always run `golangci-lint run --allow-parallel-runners` without appending filenames to avoid "function not defined" errors (functions are defined in other files within the package)

Project-specific rules enforced by `.golangci.yaml`:

- **forbidigo** rejects these outright:
  - `ioutil.ReadDir` — use `os.ReadDir`
  - `protojson.Unmarshal` — use the `common.ProtojsonUnmarshaler` wrapper (`protojson.Marshal` is still allowed)
  - pre-1.21 `sort.*` helpers (`sort.Slice`, `sort.Strings`, `sort.Ints`, ...) — use the `slices` package
- **exhaustive** runs with `explicit-exhaustive-switch: true` — enum switches must list every case explicitly, including a default when intended
- **revive** runs with `enable-all-rules: true` minus a short disable list; conform to it rather than fighting it

## Miscellaneous

- The database JSONB columns store JSON marshalled by `protojson.Marshal` in Go code. `protojson.Marshal` produces camelCased proto field names rather than the snake_case keys suggested by the SQL column names: a column whose `proto/store` field is `enter_to_send` stores `{"enterToSend": ...}`.
- `frontend/src/types/proto-es/`, `backend/generated-go/`, and `proto/gen/grpc-doc/` are buf output — regenerate with `cd proto && buf generate`, never hand-edit.
- The default Go build does not embed the frontend (`backend/server/server_frontend_not_embed.go` serves a placeholder page); run the frontend dev server or host the built `frontend/dist` separately. `make build-embed` builds the SPA and bundles it into the binary via the `embed_frontend` tag (`backend/server/server_frontend_embed.go`), which serves `frontend/dist` with an SPA fallback.
- When modifying multiple files, run file modification tasks in parallel whenever possible, instead of processing them sequentially.
