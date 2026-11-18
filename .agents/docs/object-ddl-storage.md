# Per-object DDL storage

`meta_registry_resource_schema` stores the engine's own DDL for a metadata object, keyed by the same `(guid, object_type)` as `meta_registry_resource`. `GetSchemaString` and the metadata browser read it, falling back to reconstruction when a row is absent. It is display data, not the structure fingerprint: the fingerprint is derived from metadata on every engine and answers whether a data-quality target's shape moved, while this table exists only where the driver can produce DDL and nothing diffs or validates from it. See [schema-fingerprint.md](schema-fingerprint.md).

Rebuilding DDL from metadata is lossy — StarRocks and Doris metadata carries no distribution, properties, partitioning or rollups, and those engines have no `plugin/schema` registration — so the syncer captures `SHOW CREATE …` and serves the engine's text verbatim. The metadata browser, Explain SQL and the schema dialog read it through `GetSchemaString`, so no proto, API or frontend change was needed.

The table keeps the text plus a `sha256` hash of it: the hash is the write-diff key and the upsert's guard, so unchanged DDL is never shipped back to PostgreSQL.

## Decisions

| Decision | Why |
| --- | --- |
| Per-object rows, not one database-level dump blob | The API addresses one object by GUID, so a blob would have to be parsed to serve it. |
| A separate table, not a column on `meta_registry_resource` | That table is hot, LRU-cached and read column-by-column, so a wide DDL column would have to be excluded from every reader. |
| No foreign key and no history table | Deletion paths already carry the key, and no consumer needs a past version of the DDL: versioned DDL is then unrecoverable, which is accepted. |

## Core mechanics

| Mechanism | Contract |
| --- | --- |
| Fetch | `db.ObjectDefinitionReader` is optional and receives the object's schema segment from its GUID and the plain object name; StarRocks/Doris answer `SHOW CREATE TABLE/VIEW/MATERIALIZED VIEW`, and any other type reports no definition before touching the connection. Identifiers are backtick-quoted (they cannot be bound), the DDL column is found by name because `SHOW CREATE VIEW` returns a different arity per engine, and MySQL error 1051 (dropped between listing and `SHOW CREATE`) is "no definition". A full refresh reads every schema-bearing resource; between refreshes only the changed ones are read, and a database with more than `fullDefinitionObjectLimit` objects reads one rotating GUID-sorted window per refresh with changed objects always included. A full refresh happens at most once per `objectDefinitionFullRefreshInterval` (hourly, through the process-local clock in `definitionRefreshDue`); a database whose sync is disabled never refreshes. Fetches run at `definitionFetchConcurrency` on the sync's deadline context. |
| Write diff | `ListMetaRegistrySchemaHashes` reads the stored hashes for the candidates — one indexed query, never the DDL — and the pure `diffDefinitions` drops unchanged DDL, upserts changed or new rows, and deletes a row only when the engine no longer has a definition and a row exists. A failed or non-UTF-8 fetch never reaches the diff, and the upsert's SQL guard is the safety net for concurrent writers. The write runs inside the metadata transaction after the metadata rows; the refresh clock and cursor advance only after a commit. |
| Read path | `GetSchemaString` looks the stored DDL up right after parsing the GUID and returns it on a hit, skipping the instance lookup and the metadata parse; a miss falls through to the existing `plugin/schema` reconstruction. That is the normal path for PostgreSQL, MSSQL, `MANUAL_SQL`, and any object whose first sync has not run. The read is one primary-key lookup, so there is no cache to invalidate. |
| Deletion | `BatchDeleteMetaRegistryAt` is the only path that deletes current metadata rows, and it deletes the matching DDL rows too, keeping this table a strict subset of `meta_registry_resource`. Both bulk statements pair `guid` with `object_type`; separate `ANY` predicates would match every cross combination. |

## Invariants

1. A DDL row exists only while the same-key metadata row exists; both are created, updated and deleted in the same transaction.
2. The stored DDL is display-only. Nothing diffs, migrates or validates from it.
3. A missing or unreadable definition degrades to reconstruction and never fails a sync or changes what metadata is stored.
4. A full refresh is never gated on the metadata hash, and the clock advances only after a commit, so a rolled-back sync never skips objects.
5. `db.Driver` is unchanged; an engine opts in by implementing `db.ObjectDefinitionReader`.

## Failure modes

| Scenario | Behavior |
| --- | --- |
| `SHOW CREATE` errors | The object is counted as failed, its stored DDL is untouched, and the sync warns with failure/total counts. |
| Object dropped between listing and `SHOW CREATE` | The reader reports no definition, and the stored row, if any, is deleted. |
| Definition is not valid UTF-8 | Skipped and counted; a PostgreSQL text column would reject it and abort the whole metadata transaction. |
| Engine does not implement the reader | No extra query is issued. |
| Object removed from the snapshot | Its metadata row is deleted, which deletes the DDL row in the same transaction. |

## Where things live

Table and store: `backend/migrator/migration/` and `backend/store/meta_resource_schema.go`. Fetch, diff and call site: `backend/runner/schemasync/object_definition.go` and `database_sync.go`, with the refresh clock in `refresh.go`; hermetic tests in `object_definition_test.go`. Deletion: `BatchDeleteMetaRegistryAt`. Read: `backend/api/v1/database_metadata.go`. Driver capability: `backend/plugin/db/driver.go`, implemented by `backend/plugin/db/starrocks/definition.go`. Gate: `gofmt`, `golangci-lint`, `go test ./...`, build, `make test-integration-smoke`.

The StarRocks/Doris `SHOW CREATE` path is covered only hermetically — the integration harness ships PostgreSQL and MySQL only — so a first real deployment must confirm the output against a live instance. `DiffMetadata` on a StarRocks/Doris object still answers "engine not supported", because it needs `schema.GenerateMigration`, which is unrelated to this store.
