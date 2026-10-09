# backend/AGENTS.md

Rules for the Go server. Repo-wide rules live in the root [AGENTS.md](../AGENTS.md); this file wins for anything under `backend/`.

## Package Ownership

| Path | Owns |
| --- | --- |
| `backend/bin/server/` | Entrypoint and CLI flags/profile (`cmd/root.go`); requires `PG_URL` |
| `backend/server/` | HTTP wiring: Echo middleware and routes, ConnectRPC handler registration and interceptors, pprof, CSRF, graceful shutdown, SPA embedding (`server_frontend_embed.go` / `server_frontend_not_embed.go`) |
| `backend/api/v1/` | ConnectRPC service implementations (`UserService`, `AuthService`, `InstanceService`, `DatabaseService`, `LineageService`, `OpenLineageService`, `LLMService`, `ExplainSQLService`, `AuditLogService`) plus the audit/debug interceptors |
| `backend/api/auth/` | JWT issuance and verification, auth interceptor, token extraction from metadata/headers |
| `backend/store/` | PostgreSQL store: table→message mapping, JSONB `protojson` columns, LRU caches, connection pool |
| `backend/component/dbfactory/` | Builds a `db.Driver` from an instance's data source |
| `backend/component/llm/` | LLM provider registry and the iterative agent loop (tools, events, messages) |
| `backend/component/state/` | In-memory server state (token-revocation cache over the `revoked_token` table, per-instance connection limiter, pending device logins, request budgets) |
| `backend/plugin/db/` | `db.Driver` interface and the MySQL-wire, PostgreSQL and MSSQL drivers (`mysql`, `pg`, `mssql`, `starrocks` — the last serving StarRocks and Doris) |
| `backend/plugin/schema/` | Schema sync, diff, and migration-DDL generation (MySQL/PG/MSSQL; StarRocks/Doris are not registered) |
| `backend/plugin/lineage/` | Table/column lineage analyzers, catalog, scope resolution — see [plugin/lineage/AGENTS.md](plugin/lineage/AGENTS.md) |
| `backend/plugin/openlineage/` | OpenLineage event parsing, processing, resolution, and Airflow links |
| `backend/plugin/idp/` | Identity providers (OAuth2/OIDC/LDAP) |
| `backend/runner/` | Background runners: `lineageanalyzer`, `lineagevalidation`, `schemasync`, `maintenance` |
| `backend/migrator/` | Embedded, versioned schema migrations and the startup migrator — see [migrator/AGENTS.md](migrator/AGENTS.md) |
| `backend/test/integration/` | Integration harness — see [test/integration/README.md](test/integration/README.md) |
| `backend/generated-go/` | Generated protobuf/Connect/Gateway code — never hand-edit |

## Store Layer

- `backend/store/` maps database tables to Go. Store unit tests are hermetic — they need no live database.
- When a query's shape is itself the invariant (scoping predicates, GUID-subtree escaping, history mutations), add a guard test in the same package. `backend/store/meta_resource_test.go` is the pattern.

## Data and JSONB

- JSONB columns hold `protojson.Marshal` output of the `proto/store` message named in the column's SQL comment. Adding or removing proto fields means updating the proto and the store together.
- `protojson.Marshal` camelCases proto field names, so a column whose `proto/store` field is `enter_to_send` stores `{"enterToSend": ...}` rather than the snake_case key the column name suggests.

## Schema Changes

`backend/migrator/` owns the metadata schema; read [migrator/AGENTS.md](migrator/AGENTS.md) before any schema change. Keep the `backend/store` queries, `migration/LATEST.sql`, and the incremental in sync in the same change.

## Go Workflow

After any Go change:

1. **Format** — `gofmt -w` on modified files.
2. **Lint** — `golangci-lint run --allow-parallel-runners`. Run it **repeatedly until clean**: the linter has a max-issues limit and may not show every issue on the first pass.
3. **Auto-fix** — `golangci-lint run --fix --allow-parallel-runners`.
4. **Test** — the relevant `go test` targets; add the integration suites when the change touches schema sync, lineage analysis, or server wiring.
5. **Build** — `go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go`.
6. **Tidy** — `go mod tidy` after changing dependencies.

## Commands

```bash
# Deployment build; the release tag selects the prod profile (`make build-release` does the same).
go build -ldflags "-w -s" -p=16 -tags release -o ./build/metaxisdata ./backend/bin/server/main.go

# Dev build: dev profile, wide-open CORS. Local development only.
go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go

# The build metadata (`--version` and GET /api/version) comes from
# -X github.com/Ranxy/metaxisdata/backend/common/version.{Version,GitCommit,BuildTime};
# the Makefile targets inject it, a bare `go build` leaves the dev/unknown defaults.

# Run the server (requires PG_URL; port 8083 matches the frontend vite proxy)
PG_URL='postgres://dev:dev@localhost:5432/metaxisdata?sslmode=disable' go run ./backend/bin/server/main.go --port 8083 --debug

# Deployment image (SPA embedded, prod profile) and the local compose trial
make docker-build
make docker-up

# A single test, or several
go test -v -count=1 github.com/Ranxy/metaxisdata/backend/store -run ^TestFunctionName$
go test -v -count=1 github.com/Ranxy/metaxisdata/backend/api/v1 -run ^(TestOne|TestTwo)$

# Inspect the PostgreSQL store
psql -h localhost -p 5432 -U <user> -d metaxisdata -c "sql"
```

The default build does **not** embed the frontend: `backend/server/server_frontend_not_embed.go` serves a placeholder page, so run the frontend dev server or host the built `frontend/dist` separately. `make build-embed` builds the SPA and bundles it into the binary via the `embed_frontend` tag (`backend/server/server_frontend_embed.go`, which serves `frontend/dist` with an SPA fallback); the Docker image does the same build inside its own stage — see [docs/deployment.md](../docs/deployment.md).

## Testing

- `go test ./...` is hermetic — no PostgreSQL, MySQL, or Docker.
- Integration suites carry the `integration` build tag and run the real server against real PostgreSQL + MySQL: `make test-integration-smoke`, `make test-integration`, `make test-integration-mysql`. Locally they start `testcontainers-go` and skip (exit 0) when Docker is unavailable; in CI, external-service `INTEGRATION_*` env vars switch them to already-running services. The harness runs the schema migrator and seeds the MySQL fixture schema either way, and partial env config fails fast. Full details: [test/integration/README.md](test/integration/README.md).

## Go Style

- Standard Go error handling with detailed messages; be explicit but concise about error cases.
- Errors returned from `backend/api/v1` and `backend/store` carry a `common.Code` — use `common.Errorf(code, ...)`, `common.Wrap(err, code)`, or `common.Wrapf(err, code, ...)` rather than bare `fmt.Errorf`, so Connect handlers can map them to status codes.
- Always `defer` resource cleanup such as `rows.Close()` (sqlclosecheck). Avoid `defer` inside loops (revive) — use an IIFE or scope it properly.
- Organize imports sorted by path; goimports runs with the local prefix `github.com/Ranxy/metaxisdata`.
- Only export functions and types that other packages need, and don't add a receiver a function doesn't use.
- Keep signatures, naming and patterns consistent with the surrounding code.

## Lint Rules That Bite

General:

- Prefix unused parameters with `_` (e.g. `func foo(_ *Bar)`).
- Use `any`, not `interface{}`.
- Avoid names that differ only by capitalization, and if/else branches with identical bodies.
- Mark intentionally unused functions `// nolint:unused` rather than leaving the linter to guess.
- Run `golangci-lint run --allow-parallel-runners` **without file arguments**, or functions defined in sibling files are reported as "not defined".

Project-specific, enforced by `.golangci.yaml`:

- **forbidigo** rejects `ioutil.ReadDir` (use `os.ReadDir`), `protojson.Unmarshal` (use the `common.ProtojsonUnmarshaler` wrapper; `protojson.Marshal` is still allowed), and pre-1.21 `sort.*` helpers (`sort.Slice`, `sort.Strings`, `sort.Ints`, …) — use the `slices` package.
- **exhaustive** runs with `explicit-exhaustive-switch: true`: enum switches must list every case explicitly, including a default when one is intended.
- **revive** runs with `enable-all-rules: true` minus a short disable list; conform to it rather than fighting it.
