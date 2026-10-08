# Per-Object DDL Storage (`meta_registry_resource_schema`) — Reference

> Status: **implemented**. Maintenance reference for the per-object DDL store, the sync hook that fills it, and the `GetSchemaString` read path. Related: [starrocks-doris-driver.md](starrocks-doris-driver.md) — the drivers that produce this DDL.

## What it is

`GetSchemaString` used to rebuild an object's DDL from structured metadata through `backend/plugin/schema`. That is lossy — StarRocks/Doris metadata carries no `DISTRIBUTED BY`, `PROPERTIES`, partitioning or rollups, and those engines have no `plugin/schema` registration at all, so the call failed with `engine STARROCKS is not supported`. This feature captures the engine's own DDL (`SHOW CREATE …`) during schema sync and stores it per object, keyed by the same `(guid, object_type)` as `meta_registry_resource`; `GetSchemaString` returns it verbatim and falls back to reconstruction when there is no row. No proto, API or frontend change: the metadata browser, `Explain SQL` and the schema dialog work unchanged.

## Decisions

| Decision | Why |
| --- | --- |
| Per-object rows, not one database-level dump blob | The API addresses one object by GUID, so a blob would have to be parsed to serve it (a reference implementation stores the whole-database script and consequently cannot serve per-object DDL for these engines at all). |
| Separate table, not a column on `meta_registry_resource` | That table is hot: a 65536-entry LRU holds its `MetaRegistryResource` values, and three readers select explicit columns — a wide DDL column would have to be excluded from all of them. |
| No FK to `meta_registry_resource.id`, and no history table | Deletion happens on several paths that already carry `(guid, object_type)`, and cascading from an id would force every read to fetch the metadata row first. No consumer needs a past version's DDL (`ListMetadataHistory` and `buildDatabaseSchemaAtTime` diff *metadata*; `GetSchemaString` has no version parameter) — note that versioned DDL is then unrecoverable if wanted later. |
| Full fetch on every sync; the **write** is diffed but the **fetch** is not | A StarRocks property change does not move our metadata hash, so a metadata-gated fetch would leave the stored DDL stale forever. A full fetch costs N `SHOW CREATE`s per database per interval — the price of exactness; shipping every definition back to PostgreSQL on every sync is pure waste. |
| Optional driver interface | Keeps the `db.Driver` contract and the PostgreSQL/MSSQL/MySQL drivers untouched. |

## Storage (`backend/migrator/migration/0.1/0010##meta_registry_resource_schema.sql`, mirrored at `LATEST.sql:231`; queries in `backend/store/meta_resource_schema.go`)

| Column | Role |
| --- | --- |
| `guid`, `object_type` | `PRIMARY KEY (guid, object_type)` is exactly the read predicate: one index lookup, no join. No FK. |
| `schema` | The engine's DDL, verbatim. Non-reserved in PostgreSQL, but every statement lists columns explicitly. |
| `schema_hash` | `sha256` of `schema`; the write-diff key and the upsert's guard. |
| `updated_at` | Row timestamp. No cache: the read is a single row by primary key. |

## Core mechanics

| Mechanism | Contract |
| --- | --- |
| Fetch — driver capability, and what is fetched | `db.ObjectDefinitionReader` is optional and `db.Driver` is unchanged (`backend/plugin/db/driver.go:133`). StarRocks/Doris implement it in `backend/plugin/db/starrocks/definition.go`: ``SHOW CREATE TABLE/VIEW/MATERIALIZED VIEW `db`.`name` ``; anything else returns `("", false, nil)` before touching the connection (`:26`, `:70`). Identifiers are backtick-quoted with `` ` `` doubled (they cannot be bound); the DDL column is found by name (``create `` prefix), not position, because `SHOW CREATE VIEW` returns four columns on MySQL/StarRocks and two on Doris (`:88`). MySQL error 1051 (object dropped between listing and `SHOW CREATE`) is `ok=false`, not an error. `batchMetaCreate.definitionCandidates()` (`backend/runner/schemasync/object_definition.go:97`) takes schema-bearing types only — `TABLE`, `VIEW`, `MATERIALIZED_VIEW`, plus `FUNCTION`/`PROCEDURE` so a MySQL-family driver can opt in later (`:129`) — never `COLUMN`/`DATABASE`/`SCHEMA`/`SEQUENCE`; the full set is every such resource in the snapshot, the degraded set only this sync's changed objects once it exceeds `fullDefinitionObjectLimit = 2000` (with a warning each time). Fetches run at `definitionFetchConcurrency = 8` on the sync's `deadlineCtx`. |
| Write diff | `ListMetaRegistrySchemaHashes` reads stored hashes for the candidates — one indexed query returning `guid, object_type, schema_hash`, never the DDL (`backend/store/meta_resource_schema.go:64`); the pure `diffDefinitions` (`object_definition.go:245`) drops unchanged (equal hash), upserts changed/new, and deletes only when the engine no longer has a definition **and** a row exists. A failed fetch or a non-UTF-8 definition never reaches it (`:191`), so a transient failure cannot erase a good definition. The upsert's SQL guard stays as the safety net for concurrent writers. |
| Read path | `GetSchemaString` (`backend/api/v1/database_metadata.go:20`) looks the stored DDL up right after the GUID is parsed and returns it on a hit, skipping the instance lookup and the JSONB unmarshal (`:36`); a miss falls through to the existing `plugin/schema` reconstruction. That is the normal path for PostgreSQL/MSSQL, for `MANUAL_SQL`, and for any object whose first sync has not run. |
| Deletion | `BatchDeleteMetaRegistryAt` (`backend/store/meta_resource.go:516`) is the only path that deletes current meta registry rows, and it deletes the matching DDL rows too, keeping the table a strict subset of `meta_registry_resource`. Both bulk statements use the paired predicate `(guid, object_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))`; `guid = ANY($1) AND object_type = ANY($2)` would match every cross combination. |

## Invariants

1. A DDL row exists only while a metadata row with the same `(guid, object_type)` exists; both are created/updated/deleted inside the same transaction.
2. The stored DDL is **display-only**. Nothing diffs, migrates or validates from it.
3. A missing or unreadable definition degrades to reconstruction — it can never fail a sync or change what metadata is stored.
4. The fetch is never gated on the metadata hash. Gating it is the staleness trap described under Decisions.
5. `db.Driver` stays unchanged; the capability is opt-in, and engines without it are skipped by a type assertion in `SyncDatabaseSchema` (`backend/runner/schemasync/syncer.go:730`).

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

- Table, migration and store: `backend/migrator/migration/0.1/0010##meta_registry_resource_schema.sql`, `LATEST.sql:231`, `backend/store/meta_resource_schema.go`; guard `TestMigrateSchemaLATESTMatchesTheIncrementChain` (integration-tagged, `backend/migrator/migrator_integration_test.go:100`) and query-shape guards in `backend/store/meta_resource_schema_test.go`.
- Fetch + write diff + sync call site: `backend/runner/schemasync/object_definition.go`, `backend/runner/schemasync/syncer.go:730` (guarded by `driver.(db.ObjectDefinitionReader)`); hermetic tests `object_definition_test.go`.
- Delete hook: `backend/store/meta_resource.go:516`. Read path: `backend/api/v1/database_metadata.go:20`. Driver capability: `backend/plugin/db/driver.go:133` with the impl + tests in `backend/plugin/db/starrocks/definition.go` / `definition_test.go`.
- Gate: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./...`, build; `make test-integration-smoke` (migration guard).
- Nothing to enable: an engine gains the behaviour by implementing `db.ObjectDefinitionReader` and nothing else.

## Open gaps

- The StarRocks/Doris `SHOW CREATE` path has only been exercised hermetically (quoting, column lookup, unsupported types): there is no integration coverage for it, because the integration harness ships only PostgreSQL and MySQL. A first real run has to confirm the output against a live instance.
- `DiffMetadata` on a StarRocks/Doris object still answers "engine not supported" (`backend/api/v1/database_metadata.go:197` → `backend/plugin/schema/schema.go:46`): it needs `schema.GenerateMigration`, which is unrelated to this store.

Non-goals: no DDL history/versioning, no whole-database dump RPC, no SDL export, no `DiffMetadata` support, no change to any existing engine's behaviour.
