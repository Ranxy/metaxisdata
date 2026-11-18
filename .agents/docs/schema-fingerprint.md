# Schema fingerprint

Every table, external table, view and materialized view has an engine-independent fingerprint of the shape a data-quality check is written against: ordered columns (name, type, nullability), the primary-key expressions, and for a view its `definition`. It is derived from synced metadata, not from the engine's DDL, so it exists on every engine, including MySQL and PostgreSQL, whose DDL the syncer never fetches. It answers one question: "is the shape this check was reviewed against still the current shape?"

The fingerprint feeds the quality target badges and directory, the target CSV export, MCP `get_quality_signal` and the structure-change notification. `schema_drift` rules compare a column snapshot a user wrote into the rule config, and an acknowledgement never touches them.

Because it is derived from what the registry already stores rather than from an engine-specific dump, two engines that describe the same table with the same columns, primary key and view body produce the same fingerprint, and every engine the syncer supports gets the signal whether or not its driver can produce DDL.

## What it encodes

| Included | Excluded |
| --- | --- |
| Column name, type, nullability and order; primary-key key expressions and order; a view's or materialized view's `definition` | Comments, defaults, character set, collation, non-primary indexes, view dependency lists, triggers, rules, `skip_dump`; table statistics; object names |

Changing what is encoded — a new field, another object type — means bumping the version domain: every stored fingerprint changes and every saved check reads "structure changed" once. StarRocks and Doris expose no index metadata from their driver's `information_schema`, so their fingerprint is columns only, and a primary-key change there is visible only to a `schema_drift` rule or a pre-fingerprint DDL-hash rule. Object types without a shape (column, schema, database, sequence, function, procedure) produce no row.

## Encoding and versioning

`store.CalcSchemaFingerprint` reduces the metadata to those fields and encodes them with the shared canonical encoder (`encodeCanonicalMessage`, not `protojson`) before SHA-256. Field numbers and map entries are sorted, so proto map iteration order cannot move the hash; the prefix `metaxisdata.schema.fingerprint.v1\0` versions the spec.

## Storage and write

Rows live in `meta_registry_schema_fingerprint`, keyed by `(guid, object_type)` with the 32-byte hash. The upsert is guarded by `fingerprint IS DISTINCT FROM`, so an unchanged shape rewrites nothing. Only an object whose previous row existed and differed counts as changed, so the first sync after the table landed is backfill, not an event, and the backfill produces no notification. The rows are written in the sync transaction after the metadata rows, and `BatchDeleteMetaRegistryAt` cascades the delete; see [schemasync.md](schemasync.md) for the transaction rules.

The read cost is one indexed hash read per database per sync, the same order as the existing digest read; writes happen only for a changed or first-time object.

## Read path and the legacy branch

`target_meta_hash` on a quality rule is the snapshot written when the rule is saved or edited; a target with no fingerprint yet gets NULL, meaning "not yet comparable" rather than "changed". The directory aggregate (`qualityTargetSchemaChangedSQL`) and the API predicate (`qualitySchemaChangedSince`) implement the same comparison: changed means the snapshot differs from the current fingerprint and, when the object has a legacy DDL hash, from that hash too. `ListMetaRegistrySchemaSignals` returns both values in one query.

The legacy branch is the upgrade path for StarRocks/Doris rules saved before the fingerprint existed: their snapshot is a DDL hash that cannot be converted, so a match against the current DDL hash also counts as unchanged. The branch widens only "unchanged", never "changed", and the SQL and Go predicates must change together.

## Structure-change notification

`TARGET_SCHEMA_CHANGED` fires only for an object whose previous fingerprint existed and differs; a new object, a backfill, a deleted object and an object with no live checks produce nothing. Recipients are the deduplicated owners of the target's non-archived checks, or workspace administrators when checks exist without an owner. The dedupe key is per `(kind, target GUID)` in a one-hour window, suppressed by an in-process gate plus the `(recipient_id, dedupe_key)` unique index. The detail carries only the target GUID and display path — a structure change belongs to the target, not to a rule or execution — and delivery is best effort: a failed write is logged.

## Acknowledge and re-baseline

`AcknowledgeQualityTargetSchema` sets every non-archived, non-deleted check's `target_meta_hash` to the current fingerprint, clearing the signal. It writes only `target_meta_hash` and `updated_at`: a revision records the check's definition, while re-baselining records which shape that definition was reviewed against, so this is the explicit exception to revision immutability. The call is idempotent, returns `FailedPrecondition` when the target has no fingerprint, requires `dataQuality.rules.update`, writes an audit row and leaves `updated_by` alone.

## Invariants

1. A fingerprint row exists only while its metadata row exists, and both are written and deleted in the same transaction; deletion only through `BatchDeleteMetaRegistryAt`.
2. The fingerprint is derived read-only data: no history row, no `meta_hash` change, no lineage or validation trigger, and no effect on sync convergence.
3. The directory SQL predicate and the API Go predicate are one judgment; the legacy DDL-hash branch changes in both places or neither.
4. Notifications fire only after commit and only on an old-to-new transition; rollbacks, backfills and deletions are silent.
5. `target_meta_hash` has exactly two writers: saving a rule (with its revision) and acknowledging (without one).

## Engine metadata boundary

StarRocks and Doris have no primary-key or index metadata: their driver's `information_schema` does not expose it, so the fingerprint on those engines is column name/type/nullability/order only. A primary-key change there is covered by a `schema_drift` rule or a pre-fingerprint DDL-hash rule, so it does not light the badge for a new rule; this is recorded in [backend-open-items.md](backend-open-items.md). View and materialized-view `definition` exists on every engine, and column order is guaranteed by the sync query's ordering (`ORDINAL_POSITION`/`attnum`/`column_id`), so the fingerprint does not jitter with traversal order.

## Deliberate boundaries

- The signal is a boolean, not a diff: no "which columns changed", no fingerprint history, and the notification carries no change details.
- Functions, sequences and procedures are not covered, and `schema_drift`'s config snapshot is not advanced automatically because doing so would hide drift.
- A structure change does not block execution, pause checks or touch the issue ledger; acknowledging is a human action.
- Integration coverage runs through the MySQL journey; StarRocks/Doris DDL capture does not participate in the fingerprint, and PostgreSQL/SQL Server share the metadata code path without a separate journey.

## Failure modes

| Scenario | Behavior |
| --- | --- |
| Reading stored fingerprints fails | The database sync fails and retries; nothing is written halfway. |
| Type declares a shape, metadata has none | The object is skipped with a warning and the sync continues. |
| Database not synced since the table landed | No fingerprint: `schema_changed` stays false, acknowledge returns `FailedPrecondition`, and a rule saved in this gap keeps a NULL snapshot until edited or the target acknowledged. |
| Pre-fingerprint StarRocks/Doris rule | The legacy DDL-hash branch keeps it unchanged until the rule is edited or the target acknowledged. |
| Shape changes and changes back between two syncs | Same fingerprint, no event: the signal compares state, it is not a change log. |
| Structure changes while all checks are archived | Archived checks do not participate in notification or acknowledgement. |
| Notification write fails; discovery is late | The failure is logged only. Detection is bounded by the database's sync interval, which the instance form defaults to 15 minutes; an instance with no interval never syncs periodically. |

## Where things live

`backend/store/schema_fingerprint.go` (pure function), with `encodeCanonicalMessage` in `backend/store/meta_hash.go` and the table queries in `backend/store/meta_schema_fingerprint.go`; the write set and notification in `backend/runner/schemasync/schema_fingerprint.go`, called from `database_sync.go`; the read and acknowledge paths in `backend/api/v1/data_quality_service.go`, `data_quality_target.go`, `backend/store/quality_target.go` and `quality_schema_change.go`; notification copy in `backend/component/notification/` and `frontend/src/lib/notificationText.ts`. Tests: the store fingerprint and query-shape tests, the runner write-set and notification tests, the API schema-change tests, and the MySQL integration journey.
