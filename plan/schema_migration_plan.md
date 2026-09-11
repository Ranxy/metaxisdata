# Plan: Embedded Schema Migration Framework

## TL;DR
Port laelia's metadata migrator (`backend/manager/migration`) into `backend/migrator`: an embedded, forward-only, semver-ordered SQL migration runner with a cumulative baseline for fresh installs and incremental files for upgrades. The application's current version line is **0.1** (baseline `0.1.0`). The server runs `migrator.MigrateSchema` at startup, so deployments no longer need an out-of-band `latest.sql` step.

## Decisions
- **Version scheme**: `migration/{MAJOR.MINOR}/{NNNN}##{desc}.sql` → semver `MAJOR.MINOR.NNNN`. Current line is `0.1`; the first incremental is `migration/0.1/0001##...sql` → `0.1.1`.
- **Baseline**: `baselineVersion = "0.1.0"`, the floor for the computed latest version when no incrementals exist. `migration/LATEST.sql` is the cumulative schema at the newest version and is applied only to fresh installs.
- **Fresh vs existing**: a database with no `principal` table is fresh (`LATEST.sql`); otherwise only incrementals newer than the recorded version are applied.
- **Pre-framework adoption**: a database that has `principal` but no `schema_migration_history` predates the framework (it was created out-of-band from `LATEST.sql`); the migrator creates the ledger and records `0.1.0`, then applies pending incrementals. The schema is assumed to already match the baseline.
- **Atomicity**: each migration file is executed as one multi-statement `ExecContext` inside a transaction together with its version insert; failure rolls back both.
- **HA safety**: a `pg_advisory_lock` held on a dedicated connection serializes migrations across replicas.
- **Go migrations**: `goMigrations` (keyed by version) runs Go code before the SQL file of the same version, for data backfills that are awkward in pure SQL. Empty today.

## Layout

```
backend/migrator/
  migrator.go                       MigrateSchema + version parsing/ordering
  migrator_test.go                  hermetic unit tests
  migration/
    LATEST.sql                      cumulative schema at the newest version
    0.1/                            incrementals for the current version line
      {NNNN}##{desc}.sql
```

Only `.sql` files may live under `migration/`; the walker parses every non-`LATEST.sql` file as a versioned migration and fails loudly on anything else.

## Adding a schema change

1. Append the idempotent DDL (`IF NOT EXISTS` / `ADD COLUMN IF NOT EXISTS`) to `migration/LATEST.sql`.
2. Add `migration/{current MAJOR.MINOR}/{next NNNN}##{short-desc}.sql` with the same DDL, written so it is safe on an existing deployment.
3. Add a content guard test in `backend/migrator/migrator_test.go` when the DDL is an invariant worth pinning.
4. Keep `backend/store` queries in sync in the same change.

## Relevant Files

- `backend/migrator/migrator.go` — NEW: `MigrateSchema`, version parsing/ordering, adoption path
- `backend/migrator/migration/LATEST.sql` — MOVED from `backend/migrator/latest.sql`, plus `schema_migration_history`
- `backend/migrator/migrator_test.go` — NEW: hermetic unit + `LATEST.sql` guard tests
- `backend/server/server.go` — runs `migrator.MigrateSchema` after opening the store
- `backend/test/integration/env/{testenv,service_env}.go` — harness runs the migrator instead of reading `latest.sql`
