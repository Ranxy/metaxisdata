# backend/migrator/AGENTS.md

Schema migrations for the metadata database. The root [AGENTS.md](../../AGENTS.md) and [backend/AGENTS.md](../AGENTS.md) still apply; this file wins for anything under `backend/migrator/`.

- `migration/LATEST.sql` is the cumulative schema at the newest version; `migration/{MAJOR.MINOR}/{NNNN}##{desc}.sql` are forward-only incremental migrations. The current version line is `0.1`, baseline version `0.1.0`.
- `migrator.MigrateSchema` runs in-process on every server startup, before any subsystem reads the schema. Fresh installs apply `LATEST.sql`; existing deployments apply only the pending incrementals; a database that already has the schema but predates the framework is adopted at the baseline version. A session-level advisory lock serializes migrations across replicas.
- `schema_migration_history` is the version ledger — never write it from application code.
- Every schema change must BOTH append the idempotent DDL to `LATEST.sql` (fresh installs) AND add an incremental file under the current `{MAJOR.MINOR}` directory (existing deployments). Keep `backend/store` queries, `LATEST.sql`, and the incremental in sync in the same change.
- JSONB columns bind to the `proto/store` message named in the column's SQL comment; keep that comment accurate when the binding changes. The protojson contract is in [backend/AGENTS.md](../AGENTS.md).

Design: `plan/schema_migration_plan.md`.
