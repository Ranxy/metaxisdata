# Integration Tests

This directory contains the backend integration test harnesses and runner-level end-to-end scenarios.

## Execution Modes

The integration harness supports two database startup modes.

### Local mode

Local runs use `testcontainers-go` by default.

When no integration service environment variables are present, the harness will:

- start PostgreSQL with `testcontainers`
- start MySQL with `testcontainers`
- run the backend schema migrator (`backend/migrator`) against PostgreSQL
- seed the MySQL fixture schema used by the integration scenarios

This is the default behavior for commands such as:

```bash
make test-integration-mysql
```

or:

```bash
go test -count=1 -tags=integration ./backend/test/integration/runner
```

### External service mode

When the following environment variables are set, the harness skips `testcontainers` for database startup and connects to already-running services instead:

- `INTEGRATION_POSTGRES_HOST`
- `INTEGRATION_POSTGRES_PORT`
- `INTEGRATION_POSTGRES_DB` (optional, defaults to `metaxisdata`)
- `INTEGRATION_MYSQL_HOST`
- `INTEGRATION_MYSQL_PORT`

The credentials for those services default to the `testcontainers` image defaults and can be overridden with `INTEGRATION_POSTGRES_USER` / `INTEGRATION_POSTGRES_PASSWORD` and `INTEGRATION_MYSQL_USER` / `INTEGRATION_MYSQL_PASSWORD`.

In this mode the harness still performs the normal readiness checks, PostgreSQL migration, and MySQL schema seeding. Only container creation is skipped.

This mode is intended for CI systems that provide database services directly. Setting only part of the four required variables (the two hosts and the two ports) fails fast instead of silently mixing external services with `testcontainers`.

### Real model runs (opt-in)

One scenario drives a real OpenAI-compatible provider so the model-assisted label pass
and the deterministic recommendation pass can be proven against each other end to end.
It is skipped unless an API key is present:

```bash
export METAXISDATA_INTEGRATION_LLM_API_KEY=...                                # required
export METAXISDATA_INTEGRATION_LLM_BASE_URL=https://api.deepseek.com          # default
export METAXISDATA_INTEGRATION_LLM_MODEL=deepseek-flash                       # default
export METAXISDATA_INTEGRATION_LLM_PROVIDER_TYPE=LLM_PROVIDER_TYPE_DEEPSEEK   # default

go test -v -count=1 -tags=integration \
  -run '^TestDataQualityLLMRecommendationPrecedenceRealServerIntegration$' \
  ./backend/test/integration/runner
```

The scenario creates a temporary provider profile and enables the workspace model switch,
then removes the profile and restores the switch. It never runs by default, so the other
suites stay free and hermetic. The credential is read from the environment only; the
scenario never writes it to the repository or its output.

### Docker-less runs

Without external services, the harness probes the container runtime before starting anything. When no Docker-compatible daemon is reachable the suite prints `skipping integration tests: docker is unavailable` and exits 0, so `make test-integration-smoke`, `make test-integration` and `make test-integration-mysql` are safe to run on machines without Docker.

### Destructive operations

The harness treats the external services as disposable, but it only drops databases it derives itself: PostgreSQL metadata databases are named `{INTEGRATION_POSTGRES_DB}_{scope}_integration` and the configured base database is never dropped. On MySQL it recreates `it_app` / `it_drop_me` and the fixture tables.

## GitHub Actions

GitHub CI is configured in [/.github/workflows/mysql-integration.yml](../../../.github/workflows/mysql-integration.yml).

That workflow uses GitHub Actions `services` for:

- `postgres:16-alpine`
- `mysql:8.4`

and passes the service endpoints into the tests through the integration environment variables listed above.

So the current split is:

- local development: `testcontainers`
- GitHub Actions CI: workflow-managed service containers

## Important Harness Entry Points

The dual-mode database selection lives in:

- [/backend/test/integration/env/testenv.go](env/testenv.go)
- [/backend/test/integration/env/service_env.go](env/service_env.go)

The shared container-runtime probe used to skip instead of fail lives in:

- [/backend/test/integration/dockerutil/dockerutil.go](dockerutil/dockerutil.go)

The main MySQL real-server scenarios live in:

- [/backend/test/integration/runner/schemasync_lineage_mysql_service_test.go](runner/schemasync_lineage_mysql_service_test.go)

The data-quality end-to-end scenarios live in:

- [data_quality_journey_service_test.go](runner/data_quality_journey_service_test.go) — the whole module
- [data_quality_service_test.go](runner/data_quality_service_test.go) — the service surface, including the
  target scheduling scenarios (`TestDataQualityScheduleTiersRealServerIntegration`)

Run them together with `make test-integration-data-quality`; the shared harness still boots for each
selected scenario, so it takes the same setup time as any other test. To run one scenario directly:

```bash
go test -v -count=1 -tags=integration \
  -run '^TestDataQualityScheduleTiersRealServerIntegration$' \
  ./backend/test/integration/runner
```

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
- If only part of the required external-service environment is set, the tests fail fast with a clear message rather than silently mixing startup modes.
- `make test-integration-smoke` and `make test-integration` also run `./backend/migrator/...`, which covers fresh install, incremental upgrade and legacy adoption against its own throwaway PostgreSQL container.
