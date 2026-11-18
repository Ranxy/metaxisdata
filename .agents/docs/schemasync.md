# Schema sync (schemasync)

Maintenance reference for the schema syncer: the invariants a change must preserve, the known problems that remain, and the API-triggered sync semantics. Destructive sync being log-only and the `deleted` flag's direction are stated in [docs/security-posture.md](../../docs/security-posture.md) and not repeated here. Scope: `backend/runner/schemasync`.

## Invariants

| Invariant | Where | Consequence if broken |
| --- | --- | --- |
| The metadata transaction does only writes: the digest read, the `bmc.prepare()` diff, and the DDL fetch and diff all happen before `BeginTx` | `SyncDatabaseSchema` in `backend/runner/schemasync/database_sync.go` | Otherwise the `meta_registry_resource` row locks span every network round trip to the target instance, so one large-database sync can hold a transaction open for minutes and amplify vacuum bloat and lock waits. |
| Cache invalidation happens strictly after `Commit`, including rows a stats-only update touched | `SyncDatabaseSchema` (the post-commit invalidation) | A rolled-back sync leaves uncommitted cache entries behind. |
| DDL rows are a strict subset of metadata rows and are written in the same transaction | `SyncDatabaseSchema` | A rollback leaves a DDL row with no metadata row. |
| The statistics refresh writes only the `metadata` column and is best effort: no `meta_registry_resource_history` row, no `meta_hash` change, no entry in `bmc.updates` (so no lineage requeue and no revalidation trigger), a read failure only warns and still records the refresh, and a write failure only warns (outside the metadata transaction) | `SyncDatabaseSchema` (`ListMetaRegistryStatistics` and the post-commit `BatchUpdateMetaRegistryStatistics`); `backend/store/meta_resource_statistics.go` | Statistics are excluded from the fingerprint by `normalizeMetadataForHash`; treating them as an ordinary update produces a history version and a lineage recomputation on every row-count change, and putting them inside the metadata transaction would let one failed estimate roll back real schema changes and a persistent bad value fail that database's sync forever. |
| Statistics and the full DDL refresh have independent cadences (hourly per database, with a rotating window above the fetch limit), and the clock/cursor advance only after a commit | `SyncDatabaseSchema` (post-commit `recordStatisticsRefresh`/`recordDefinitionRefresh`); `backend/runner/schemasync/refresh.go`, `object_definition.go` | Following the 15-minute schema sync would turn a row-count estimate into a full JSONB rewrite every time and put `SHOW CREATE` pressure on the target for no change; advancing the clock on rollback skips objects that were never refreshed. |
| The metadata fingerprint uses a canonical encoding, not `protojson` | `backend/store/meta_hash.go` (`CalcMetaHash`/`encodeCanonicalMessage`) | `protojson` randomizes map field order, which rewrites the whole table. |
| Structure-fingerprint rows are a strict subset of metadata rows, written after them in the same transaction; computation and diff happen before `BeginTx`; deletion cascades only through `BatchDeleteMetaRegistryAt` | `prepareSchemaFingerprints` in `backend/runner/schemasync/schema_fingerprint.go`, `SyncDatabaseSchema`, `BatchDeleteMetaRegistryAt` in `backend/store/meta_resource.go` | A rollback leaves a fingerprint with no metadata row, and computing the whole database inside the transaction lengthens lock holds. See [schema-fingerprint.md](schema-fingerprint.md). |
| Fingerprint changes notify the data-quality check owners only after the commit, and only for objects whose previous row existed and differed | `reportSchemaFingerprintChanges`; `backend/store/quality_schema_change.go` | A rollback must not notify; a first backfill, a new object and a deleted object are not structure changes, and notifying them is noise. |
| Destructive deletes are log-only: no shrink threshold and no confirmation switch, and `logSchemaSyncDeletion` is the only trace | `logSchemaSyncDeletion` in `metadata_batch.go`, called from `SyncDatabaseSchema` | Deliberate: MySQL's `information_schema` lists only what the connected user may see, so a privilege change can empty the snapshot. See `docs/security-posture.md`. |
| `deleted = true` may be written by any observation that the target no longer has the database; `deleted = false` only by instance enumeration; a successful single-database sync never writes `deleted` | `SyncInstance` in `instance_sync.go`; the NotFound path of `SyncDatabaseSchema` | Reading a database's content is not an instance enumeration; a bidirectional write flaps. |
| Every driver open goes through the instance connection budget: sync uses `acquireInstanceConnection` (`TryAcquire`; on failure the database stays queued), and the connectivity test uses `Acquire` (Interactive, 5s). Queuing and priorities are in [instance-connections.md](instance-connections.md) | `acquireInstanceConnection` in `instance_sync.go`; `backend/api/v1/instance_service.go`; `backend/component/state/connection.go` | Bypassing the budget makes `maximum_connections` meaningless, lets a caller stack unbounded attempts at a target that only completes the TCP handshake, and a blocking sync would stall the checker's tick. |
| Instance metadata is written only as targeted patches: the runner and the API (UpdateInstance, data-source add/update/delete) all use `UpdateInstanceMessage.MetadataUpdates`, merged under the store's row lock; `last_sync_time`, `version` and `mysql_lower_case_table_names` are written only by the runner's patch | `patchInstanceMetadata` in `backend/store/instance.go`; `backend/api/v1/instance_service.go`; `SyncInstance` in `instance_sync.go` | A caller that writes back a whole metadata clone silently reverts the other half of a concurrent write — at worst a just-added data source or credential. |

## Known problems

| # | Severity | Problem | Where |
| --- | --- | --- | --- |
| 1 | Medium | **An API-triggered single-instance sync can hang for 15 minutes.** `SyncInstance` and `SyncDatabase` run synchronously in the request goroutine with an internal `syncTimeout` of 15 minutes, so a hung target holds the HTTP request that long (`BatchSyncInstances` already has a 5-minute total budget; the single-instance RPC was not changed). A shorter API timeout or an async-plus-poll API would fix it. | `syncTimeout` in `syncer.go`; `GetInstanceMeta` in `instance_sync.go`; `SyncDatabaseSchema` in `database_sync.go` |
| 2 | Low | The checker calls `ListInstances` unconditionally every 10 seconds; with an empty queue that is pure waste, and a cheap `Range` check could come first. | `trySyncAll` in `queue.go` |
| 3 | Low | Seven near-identical type loops could be table-driven; the materialized-view one does not reuse `schemaGUIDPrefix` like its neighbors. | the type loop in `SyncDatabaseSchema` |
| 4 | Low | One redundant `proto.Clone` per column: `CalcMetaHash` already clones internally, and there is no mutable path afterwards. | `buildColumnMetadataResources` in `metadata_batch.go` |

## API-triggered sync semantics

`SyncInstance`'s `enable_full_sync` is proto3 `optional`: **unset means full** (refresh instance metadata, enqueue and sync every database, like the background scan and `CreateInstance`), while an explicit `false` is the discovery-only mode that queues only databases this enumeration discovered and leaves existing ones to the background scan. The frontend sends `true`; a caller that wants discovery-only must pass `false` explicitly.

## Known debt

| Item | Where |
| --- | --- |
| `MaximumOutstanding` (the checker's 100-goroutine cap) is exported but used only in-package and undocumented; it should be `maximumOutstanding`. | `syncer.go` |
| `Run`'s comment still says it "will run the schema syncer once", but it is a permanent loop. | `syncer.go` |
| The deadline is written as `context.WithDeadline(time.Now().Add(syncTimeout))` where `WithTimeout` would do; the same `deadlineCtx` covers both `SyncDBSchema` and the DDL fetch, so a slow first phase squeezes the second (it fails safe by keeping the old DDL, but it is worth knowing). | `SyncDatabaseSchema` |
| `SyncInstance` returns four values that most callers discard; the signature never converged after its evolution. | `SyncInstance` in `instance_sync.go`; the API's `_, _, _, _` |
| `supportsObjectDefinition` reserves FUNCTION/PROCEDURE, but only the StarRocks driver implements `ObjectDefinitionReader` today. | `mergeDefinitionResources` and `supportsObjectDefinition` in `object_definition.go` |
| Structure fingerprints hash every object in the database once per sync (O(objects) rows), the same order as the digest read; only a very large database is worth a change-set-plus-periodic-full refresh. | `prepareSchemaFingerprints` |
| Several "used to … / instead of …" comments explain a fix; they aid understanding but mark this as an incident-driven hot spot where fixes stack on the old structure instead of refactoring it. | `instance_sync.go` (`SyncInstance`), `operation.go` |

## Tests and gate

- Hermetic: `syncer_test.go` (digest diff, stats-only refresh, statistics/DDL refresh cadence, connection limiting, gate serialization, backoff ladder, `shouldSyncNow` boundaries), `object_definition_test.go` (changed/full/rotating windows), `schema_fingerprint_test.go` (fingerprint write set, backfill is not a change, notification targets and dedupe key, path rendering), `operation_test.go`, `meta_resource_statistics_test.go` (read/update statement shapes) and `meta_schema_fingerprint_test.go` (fingerprint query shapes).
- Integration: `make test-integration-smoke`, `make test-integration`, `make test-integration-mysql` (see `backend/test/integration/README.md`).
- Gate: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./backend/runner/schemasync/...`; add the integration suites when a change touches schema sync.
