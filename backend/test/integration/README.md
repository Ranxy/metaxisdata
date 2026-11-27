# Integration Tests

This directory contains the backend integration test harnesses and runner-level end-to-end scenarios. The runner suites carry the `integration` build tag, start the real server and run against real PostgreSQL + MySQL; the `env/` and `dockerutil/` unit tests are hermetic.

## Execution modes

### Local mode

Local runs use `testcontainers-go`. With no integration service environment variables present, the harness starts PostgreSQL and MySQL containers, runs the backend schema migrator against PostgreSQL, and seeds the MySQL fixture schema. This is the default for `make test-integration-mysql` and for a direct `go test -count=1 -tags=integration ./backend/test/integration/runner`.

### External service mode

When `INTEGRATION_POSTGRES_HOST`, `INTEGRATION_POSTGRES_PORT`, `INTEGRATION_MYSQL_HOST` and `INTEGRATION_MYSQL_PORT` are set, the harness skips container creation and connects to already-running services instead. `INTEGRATION_POSTGRES_DB` is optional (default `metaxisdata`); credentials default to the container image defaults and can be overridden with `INTEGRATION_POSTGRES_USER`/`INTEGRATION_POSTGRES_PASSWORD` and `INTEGRATION_MYSQL_USER`/`INTEGRATION_MYSQL_PASSWORD`.

The harness still performs its readiness checks, PostgreSQL migration and MySQL schema seeding; only container creation is skipped. CI systems that provide database services directly use this mode. Setting only part of the four required variables fails fast instead of silently mixing external services with `testcontainers`.

### Real model runs (opt-in)

One scenario drives a real OpenAI-compatible provider so the model-assisted label pass and the deterministic recommendation pass can be proven against each other end to end. It is skipped unless an API key is present:

```bash
export METAXISDATA_INTEGRATION_LLM_API_KEY=...                                # required
export METAXISDATA_INTEGRATION_LLM_BASE_URL=https://api.deepseek.com          # default
export METAXISDATA_INTEGRATION_LLM_MODEL=deepseek-flash                       # default
export METAXISDATA_INTEGRATION_LLM_PROVIDER_TYPE=LLM_PROVIDER_TYPE_DEEPSEEK   # default

go test -v -count=1 -tags=integration \
  -run '^TestDataQualityLLMRecommendationPrecedenceRealServerIntegration$' \
  ./backend/test/integration/runner
```

The scenario creates a temporary provider profile and enables the workspace model switch, then removes the profile and restores the switch. It never runs by default, so the other suites stay free and hermetic. The credential is read from the environment only; the scenario never writes it to the repository or its output.

### ExplainSQL tool wiring (no provider credential)

`runner/explain_sql_service_test.go` drives the shipping ExplainSQL loop against the real server with a local OpenAI-compatible `httptest` stub registered as a throwaway provider profile: no real model, no API key. The refusal branch is covered hermetically by `TestExplainSQLToolCallsArePermissionGated` in `backend/api/v1`, because every authenticated principal holds `databases.read` through the workspace-member baseline, so the public API cannot construct a caller who may explain SQL yet fails the tool's permission check.

### Docker-less runs

Without external services, the harness probes the container runtime before starting anything. When no Docker-compatible daemon is reachable the suite prints `skipping integration tests: docker is unavailable` and exits 0, so all the make targets below are safe on machines without Docker.

### Destructive operations

The harness treats external services as disposable, but only drops databases it derives itself: PostgreSQL metadata databases are named `{INTEGRATION_POSTGRES_DB}_{scope}_integration`, and the configured base database is never dropped. On MySQL it recreates `it_app`, `it_drop_me` and the fixture tables.

## GitHub Actions

CI is split into three tiers, so the slow suite never sits between a change and its merge:

- **Pull request** — [integration.yml](../../../.github/workflows/integration.yml) runs `make test-integration-migrator`, and only when a change touches `backend/migrator/**` or the container probe the migrator shares with the harness. LATEST.sql and the increment chain disagreeing is the failure that breaks every upgrade, and the check costs about a minute.
- **Merge to main** — the same workflow calls [integration-suite.yml](../../../.github/workflows/integration-suite.yml) for the full `make test-integration`. Nobody waits on it, and main is verified minutes after the merge instead of at the next nightly.
- **Nightly** — [nightly.yml](../../../.github/workflows/nightly.yml) runs that same reusable suite on three legs at 02:00 Asia/Shanghai: the baseline (PostgreSQL 16 + MySQL 8.4), PostgreSQL 17, and MySQL 8.0. A red leg opens or refreshes one tracking issue; the next green run closes it. `workflow_dispatch` takes a `ref`, so a risky branch can be put through the full matrix before it merges.

All three use GitHub Actions `services` for PostgreSQL and MySQL and pass the endpoints in through the `INTEGRATION_*` variables above. Local development keeps using `testcontainers`.

## Important harness entry points

- [env/testenv.go](env/testenv.go) and [env/service_env.go](env/service_env.go): the dual-mode database selection, environment validation, readiness, migration and fixtures.
- [dockerutil/dockerutil.go](dockerutil/dockerutil.go): the shared container-runtime probe that turns "no Docker" into a skip rather than a failure.
- [runner/schemasync_lineage_mysql_service_test.go](runner/schemasync_lineage_mysql_service_test.go): the main MySQL real-server scenarios.
- [runner/data_quality_journey_service_test.go](runner/data_quality_journey_service_test.go): the whole data-quality module; [runner/data_quality_service_test.go](runner/data_quality_service_test.go) covers the service surface, including `TestDataQualityScheduleTiersRealServerIntegration`.

## How to run

```bash
make test-integration-migrator       # LATEST.sql versus the increment chain only; this is the pull-request gate
make test-integration-smoke          # every integration test plus migrator fresh-install, upgrade and legacy-adoption paths; skips (exit 0) without Docker or external services
make test-integration                # every real-server scenario (-race), plus the migrator paths; the merge-to-main and nightly gate
make test-integration-mysql          # the MySQL real-server scenarios only
make test-integration-data-quality   # the data-quality journey plus the scheduling scenarios
```

Each selected scenario still boots the shared harness, so the setup time is the same as for the whole package. To run one scenario directly:

```bash
go test -v -count=1 -tags=integration \
  -run '^TestDataQualityScheduleTiersRealServerIntegration$' \
  ./backend/test/integration/runner
```
