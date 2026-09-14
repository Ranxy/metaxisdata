# Per-Object DDL Storage (`meta_registry_resource_schema`) — Reference

> Status: **implemented**. Maintenance reference for the per-object DDL store, the
> sync hook that fills it, and the `GetSchemaString` read path.
> Related: `plan/starrocks_doris_sync_plan.md` — the drivers that produce this DDL.

## What it is

`GetSchemaString` used to rebuild an object's DDL from structured metadata through
`backend/plugin/schema`. That is lossy — StarRocks/Doris metadata carries no
`DISTRIBUTED BY`, `PROPERTIES`, partitioning or rollups — and those engines have no
`plugin/schema` registration at all, so the call failed with
`engine STARROCKS is not supported`.

This feature captures the engine's own DDL (`SHOW CREATE ...`) during schema sync
and stores it per object, keyed by the same `(guid, object_type)` as
`meta_registry_resource`. `GetSchemaString` returns it verbatim and falls back to
reconstruction when there is no row. No proto, API or frontend change: the
metadata browser, `Explain SQL` and the schema dialog work unchanged.

## Decisions

| Decision | Why |
| --- | --- |
| Per-object rows, not one database-level dump blob | The API addresses one object by GUID, so a blob would have to be parsed to serve it. (A reference implementation stores the whole-database script and consequently cannot serve per-object DDL for these engines at all.) |
| Separate table, not a column on `meta_registry_resource` | That table is hot: a 65536-entry LRU holds its `MetaRegistryResource` values, and three readers select explicit columns — a wide DDL column would have to be excluded from all of them. |
| No FK to `meta_registry_resource.id` | Deletion happens on several paths that already carry `(guid, object_type)`; cascading from an id would also force every read to fetch the metadata row first. |
| No history table | No consumer needs a past version's DDL (`ListMetadataHistory` and `buildDatabaseSchemaAtTime` diff *metadata*; `GetSchemaString` has no version parameter). Note: versioned DDL is unrecoverable if wanted later. |
| Full fetch on every sync | A StarRocks property change does not move our metadata hash, so a metadata-gated fetch would leave the stored DDL stale forever. |
| The **write** is diffed; the **fetch** is not | A full fetch costs N `SHOW CREATE`s per database per interval — the price of exactness. Shipping every definition back to PostgreSQL on every sync is pure waste. |
| Optional driver interface | Keeps the `db.Driver` contract and the PostgreSQL/MSSQL/MySQL drivers untouched. |

## Storage

```sql
CREATE TABLE meta_registry_resource_schema (
    guid TEXT COLLATE "C" NOT NULL,
    object_type INT2 NOT NULL,
    schema TEXT NOT NULL,
    schema_hash BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (guid, object_type)
);
```

- `PRIMARY KEY (guid, object_type)` is exactly the read predicate, so reads are one
  index lookup with no join. `schema` is non-reserved in PostgreSQL, but every
  statement lists columns explicitly.
- `schema_hash` is the diff key (see Write diff). It also backs the upsert's guard.
- No cache: the read is a single row by primary key.

## Core mechanics

### Fetch — driver capability

`db.ObjectDefinitionReader` (optional; `db.Driver` is unchanged). StarRocks/Doris
implement it in `backend/plugin/db/starrocks/definition.go`:

| Object type | Statement |
| --- | --- |
| `TABLE` | ``SHOW CREATE TABLE `db`.`name` `` |
| `VIEW` | ``SHOW CREATE VIEW `db`.`name` `` |
| `MATERIALIZED_VIEW` | ``SHOW CREATE MATERIALIZED VIEW `db`.`name` `` |
| anything else | `("", false, nil)`, returned before touching the connection |

- Identifiers are backtick-quoted with `` ` `` doubled — they cannot be bound as
  parameters.
- The DDL column is found by name (`create ` prefix), not by position: `SHOW CREATE
  VIEW` returns four columns on MySQL/StarRocks and two on Doris.
- MySQL error 1051 (object dropped between listing and `SHOW CREATE`) is reported as
  `ok=false`, not an error.

`batchMetaCreate.definitionCandidates()` (`backend/runner/schemasync/object_definition.go`)
selects what to fetch:

- schema-bearing types only (`TABLE`, `VIEW`, `MATERIALIZED_VIEW`, plus
  `FUNCTION`/`PROCEDURE` so a MySQL-family driver can opt in later) — never
  `COLUMN`/`DATABASE`/`SCHEMA`/`SEQUENCE`;
- **full set** = every such resource in the snapshot;
- **degraded set** = only this sync's changed objects when the full set exceeds
  `fullDefinitionObjectLimit`, with a warning each time.

```go
definitionFetchConcurrency = 8     // concurrent SHOW CREATE against one database
fullDefinitionObjectLimit  = 2000  // above this, degrade to changed-only
```

`fetchObjectDefinitions` runs the fetches on the sync's `deadlineCtx`, so a hung
target cannot outlive the sync deadline.

### Write diff

The write is proportional to the changes, not to the database size:

1. `ListMetaRegistrySchemaHashes` reads the stored hashes for the candidates — one
   indexed query returning `guid, object_type, schema_hash`, never the DDL.
2. `diffDefinitions(fetched, existing)` is pure and decides per object:
   unchanged (hash equal) → dropped; changed or new → upsert; no definition any
   more → delete, but only if a row exists; failed fetch or non-UTF-8 → never
   reaches it, so a transient failure cannot erase a good definition.

Without this, 2000 tables at ~2KB of DDL would resend ~4MB of parameters and take
2000 row locks per database per interval, to have the upsert discard all of it. The
upsert's SQL guard stays as a safety net for concurrent writers.

### Read path

`GetSchemaString` (`backend/api/v1/database_metadata.go`) looks the stored DDL up
right after the GUID is parsed and returns it on a hit, skipping both the instance
lookup and the JSONB unmarshal. A miss falls through to the existing
`plugin/schema` reconstruction. That is the normal path for PostgreSQL/MSSQL, for
`MANUAL_SQL` (whose "schema" is user SQL text and is never fetched), and for any
object whose first sync has not run.

### Deletion

`BatchDeleteMetaRegistryAt` (`backend/store/meta_resource.go`) is the only path that
deletes current meta registry rows, and it now deletes the matching DDL rows too.
That keeps `meta_registry_resource_schema` a strict subset of
`meta_registry_resource` for every caller: the schema syncer, `store/manual_sql.go`
and `store/maintenance.go`.

Both bulk statements use the paired predicate
`(guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))`.
`guid = ANY($1) AND object_type = ANY($2)` also matches every cross combination and
would hit unrequested rows.

## Invariants

1. A DDL row exists only while a metadata row with the same `(guid, object_type)`
   exists; both are created/updated/deleted inside the same transaction.
2. The stored DDL is **display-only**. Nothing diffs, migrates or validates from it.
3. A missing or unreadable definition degrades to reconstruction — it can never fail
   a sync or change what metadata is stored.
4. The fetch is never gated on the metadata hash. Gating it is the staleness trap
   described under Decisions.
5. `db.Driver` stays unchanged; the capability is opt-in, and engines without it are
   skipped by a type assertion in `SyncDatabaseSchema`.

## Failure modes

| Scenario | Behaviour |
| --- | --- |
| `SHOW CREATE` errors (permissions, timeout) | Object counted as failed, stored definition untouched, one `Warn` per sync with failure/total counts |
| Object dropped between listing and `SHOW CREATE` | Reader reports `ok=false`; the stored row is deleted if present |
| Definition is not valid UTF-8 | Skipped and counted — a PostgreSQL `text` column would reject the row and abort the whole metadata transaction |
| Database exceeds `fullDefinitionObjectLimit` | Degrades to changed-only; unchanged objects keep their previous DDL and every degraded sync warns |
| Engine does not implement the reader | `syncObjectDefinitions` is never called; no extra queries |
| Object removed from the snapshot | Its meta registry row is deleted, which deletes the DDL row in the same transaction |

## Where things live

| | |
| --- | --- |
| Table + migration | `backend/migrator/migration/LATEST.sql`, `backend/migrator/migration/0.1/0010##meta_registry_resource_schema.sql` (guard: `TestMigrateSchemaLATESTMatchesTheIncrementChain`, integration-tagged) |
| Store | `backend/store/meta_resource_schema.go`; query-shape guards in `backend/store/meta_resource_schema_test.go` |
| Delete hook | `backend/store/meta_resource.go` → `BatchDeleteMetaRegistryAt` |
| Fetch + write diff | `backend/runner/schemasync/object_definition.go`; tests in `object_definition_test.go` (hermetic) |
| Sync call site | `backend/runner/schemasync/syncer.go` → `SyncDatabaseSchema`, guarded by `driver.(db.ObjectDefinitionReader)` |
| Driver capability | `backend/plugin/db/driver.go`; StarRocks/Doris impl + tests in `backend/plugin/db/starrocks/definition.go` / `definition_test.go` |
| Read path | `backend/api/v1/database_metadata.go` → `GetSchemaString` |

Nothing to enable: an engine gains the behaviour by implementing
`db.ObjectDefinitionReader` and nothing else.

Non-goals: no DDL history/versioning, no whole-database dump RPC, no SDL export, no
`DiffMetadata` support, no change to any existing engine's behaviour.

## Open gaps

- The StarRocks/Doris `SHOW CREATE` path has only been exercised hermetically
  (quoting, column lookup, unsupported types): there is no integration coverage for
  it, because the integration harness ships only PostgreSQL and MySQL. A first real
  run has to confirm the output against a live instance.
- `DiffMetadata` on a StarRocks/Doris object still answers "engine not supported":
  it needs `schema.GenerateMigration`, which is unrelated to this store.
