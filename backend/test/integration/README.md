# Integration Tests

This directory contains the backend integration test harnesses and runner-level end-to-end scenarios.

## Execution Modes

The integration harness supports two database startup modes.

### Local mode

Local runs use `testcontainers-go` by default.

When no integration service environment variables are present, the harness will:

- start PostgreSQL with `testcontainers`
- start MySQL with `testcontainers`
- apply the backend migrator SQL to PostgreSQL
- seed the MySQL fixture schema used by the integration scenarios

This is the default behavior for commands such as:

```bash
make test-integration-mysql
```

or:

```bash
go test -count=1 -tags=integration -run 'RealServerIntegration' ./backend/test/integration/runner
```

### External service mode

When the following environment variables are set, the harness skips `testcontainers` for database startup and connects to already-running services instead:

- `INTEGRATION_POSTGRES_HOST`
- `INTEGRATION_POSTGRES_PORT`
- `INTEGRATION_POSTGRES_DB` (optional, defaults to `metaxisdata`)
- `INTEGRATION_MYSQL_HOST`
- `INTEGRATION_MYSQL_PORT`

In this mode the harness still performs the normal readiness checks, PostgreSQL migration, and MySQL schema seeding. Only container creation is skipped.

This mode is intended for CI systems that provide database services directly.

## GitHub Actions

GitHub CI is configured in [/.github/workflows/mysql-integration.yml](/home/ran/gocode/metaxisdata/.github/workflows/mysql-integration.yml).

That workflow uses GitHub Actions `services` for:

- `postgres:16-alpine`
- `mysql:8.4`

and passes the service endpoints into the tests through the integration environment variables listed above.

So the current split is:

- local development: `testcontainers`
- GitHub Actions CI: workflow-managed service containers

## Important Harness Entry Points

The dual-mode database selection lives in:

- [/backend/test/integration/env/testenv.go](/home/ran/gocode/metaxisdata/backend/test/integration/env/testenv.go)
- [/backend/test/integration/env/service_env.go](/home/ran/gocode/metaxisdata/backend/test/integration/env/service_env.go)

The main MySQL real-server scenarios live in:

- [/backend/test/integration/runner/schemasync_lineage_mysql_service_test.go](/home/ran/gocode/metaxisdata/backend/test/integration/runner/schemasync_lineage_mysql_service_test.go)

## Running CI Mode Locally

You can simulate the GitHub Actions path by starting your own PostgreSQL and MySQL instances, then exporting the integration variables before running the tests.

Example:

```bash
export INTEGRATION_POSTGRES_HOST=127.0.0.1
export INTEGRATION_POSTGRES_PORT=5432
export INTEGRATION_POSTGRES_DB=metaxisdata
export INTEGRATION_MYSQL_HOST=127.0.0.1
export INTEGRATION_MYSQL_PORT=3306

make test-integration-mysql
```

## Notes

- The MySQL integration suite starts the real backend server process and exercises the public API surface.
- The harness still seeds MySQL test data even in CI service mode, so the external MySQL service should be treated as disposable test infrastructure.
- If only part of the required external-service environment is set, the tests will fail fast with a clear assertion rather than silently mixing startup modes.
