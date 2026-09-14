# StarRocks / Doris Driver — Reference

> Status: **implemented** (commit `8cb2c31`). Maintenance reference for
> `backend/plugin/db/starrocks` and the engine plumbing around it.
> Related: `plan/object_definition_storage_plan.md` — how these engines' DDL is
> captured and served per object.
> Provenance: the sync half was ported from a reference StarRocks/Doris driver; the
> port kept its engine-specific behavior and dropped everything this repository's
> `db.Driver` has no surface for.

## What it is

One package serving **both** `storepb.Engine_STARROCKS` and
`storepb.Engine_DORIS` over the MySQL wire protocol. It implements only what
`db.Driver` requires here — `SyncInstance` and `SyncDBSchema` (tables, columns,
views, materialized views) — so query execution, SQL split/rewrite, dump/DDL
emission and transaction control from the reference driver are absent by design.
Connection style follows `backend/plugin/db/mysql`.

## Decisions

| Decision | Why |
| --- | --- |
| Both engines in one package | They differ only in the system-database exclusion and the materialized-view catalog. |
| Enum values `STARROCKS = 15` / `DORIS = 16` un-reserved and restored | Keeps the store and v1 enums value-compatible with the reference project and with any historical row. **No DB migration**: the value only lives inside JSONB `metadata` blobs. |
| `interpolateParams=true` on the DSN | StarRocks/Doris have no usable server-side prepared statements, so the sync queries' `?` placeholders must be interpolated client-side. Dropping it would break every sync query. |
| `CHARACTER_SET_NAME` / `COLLATION_NAME` are never read | These catalogs return NUL-terminated values that the JSONB store rejects (`unsupported Unicode escape sequence`). |
| MySQL helpers are imported, not copied | `UnquoteMySQLString` / `UnescapeExpressionDefault` / `IsCurrentTimestampLike` come from `plugin/db/mysql`; the `allowAllFiles` extra-parameter guard was exported there as `ValidateExtraConnectionParameters` so the two drivers cannot drift. |
| No `plugin/schema`, lineage, OpenLineage or Explain SQL registration | Out of scope. See Open gaps for what that leaves unsupported. |

## Engine differences

| Dimension | StarRocks | Doris |
| --- | --- | --- |
| System databases excluded | `information_schema`, `_statistics_`, **`sys`** | `information_schema`, `_statistics_` (no `sys` metadatabase) |
| Materialized-view catalog | `information_schema.materialized_views`, `REFRESH_TYPE='ROLLUP'` rows excluded (synchronous rollups are base-table indexes, not MVs) | `mv_infos("database"=?)` table-valued function |
| MV reported in `information_schema.tables` as | `TABLE_TYPE='VIEW'` | `TABLE_TYPE='BASE TABLE'` |

Both are treated as "no schema concept" (`store.IsObjectCaseSensitive`'s default
branch is already correct, so it needed no change) and address objects as
`database.table`.

## Core mechanics

### Connection (`starrocks.go`)

DSN with `multiStatements=true`, `maxAllowedPacket=0`, `interpolateParams=true`;
`tcp`/`unix` protocol, SSH dialer via `util.GetSSHClient`, TLS via
`util.GetTLSConfig`. `Driver{dbType, db, databaseName, sshClient, openCleanUp}` —
same shape as `mysql.Driver`. `parseVersion` splits a MySQL-style version string and
keeps the suffix (`5.7.22-log`, `10.4.7-MariaDB`, `5.6.29_ddm_3.0.1.7`).

### SyncInstance (`sync.go`)

Version, then a **best-effort** `lower_case_table_names` (`SHOW VARIABLES LIKE ...`;
a failure is logged at debug and treated as `0`), then
`information_schema.SCHEMATA` minus the engine's system databases. Only
`SCHEMA_NAME` is selected — see the charset/collation decision. The result sets
`Instance.MysqlLowerCaseTableNames`.

### SyncDBSchema

Order: columns → views → materialized views → tables.

- Columns come from `information_schema.columns` with a `?` placeholder for
  `TABLE_SCHEMA`.
- `information_schema.tables` rows whose name is in the materialized-view set become
  `MaterializedViews` **regardless of `TABLE_TYPE`** — that membership is the
  authoritative signal, because the two engines disagree on the reported type.
  `BASE TABLE` → `Tables`, `VIEW` → `Views`, anything else is skipped at debug.
- Both MV queries are best-effort: an unsupported engine/version logs at debug and
  yields an empty MV set rather than failing the sync.
- `DatabaseSchemaMetadata.CharacterSet` / `Collation` stay empty.

### Column defaults

`setColumnMetadataDefault` is the MySQL rule **minus `ON UPDATE`** (neither engine
supports it): `UnquoteMySQLString`, then `IsCurrentTimestampLike` → bare function
name, `DEFAULT_GENERATED` → `(expr)`, otherwise the literal value; an
`AUTO_INCREMENT` column with no default gets the symbol; a nullable column with no
default gets `NULL`.

## Invariants

1. StarRocks and Doris must stay in **one** registered package: splitting them would
   duplicate the sync and lose the shared registration.
2. `interpolateParams=true` must stay on the DSN, and every catalog query must keep
   using `?` placeholders.
3. The charset/collation columns must not be selected — the JSONB store rejects their
   NUL-terminated values.
4. Materialized-view membership, not `TABLE_TYPE`, decides whether a table row is an
   MV. Both catalog queries must remain best-effort.
5. `db.Driver` must not grow a query/execute surface for these engines without a
   corresponding test: nothing in this repository calls one.

## Failure modes

| Scenario | Behaviour |
| --- | --- |
| `lower_case_table_names` unavailable | Logged at debug, treated as `0`, sync continues |
| MV catalog unsupported by the engine version | Logged at debug, empty MV set, sync continues |
| Engine reports an unrecognized `TABLE_TYPE` | Skipped at debug |
| `SHOW CREATE` for an object fails | Not part of this driver's sync; see `plan/object_definition_storage_plan.md` for the DDL fetch's degradation rules |

## Where things live

| | |
| --- | --- |
| Package | `backend/plugin/db/starrocks/`: `starrocks.go` (connection/registration), `sync.go`, `definition.go` (`SHOW CREATE`, see the related doc) |
| Tests | `starrocks_test.go` (`parseVersion`), `sync_test.go` (`systemDatabaseExclusion`, `isMaterializedView`, `isSyncRollup`), `definition_test.go` — all hermetic, no live database |
| Driver registration | `backend/server/ultimate.go` blank import; `db.Register` in the package's `init()` |
| Instance plumbing | `backend/api/v1/common.go` (`convertToEngine`/`convertEngine`), `backend/api/v1/instance_service.go` (`supportedStoreEngines`), tests in `common_test.go` / `instance_data_source_test.go` |
| Enum | `proto/v1/v1/common.proto`, `proto/store/store/common.proto` (must mirror) + `buf generate` |
| Frontend | `InstanceManagementPage.vue` (picker + label/icon/color maps), `InstanceDetailPage.vue`, `HomePage.vue`, `InstanceList.vue` (labels), `MetadataBrowserPage.vue` (`isMySQLInstance`), `TableMetadataDetail.vue` (`isMySQLFamily`), `DatabaseManagementPage.vue` (`engineOptions`) — engine names are literal strings there, so no locale keys |

An instance becomes creatable only once its engine is in `supportedStoreEngines`;
everything else follows from `db.Register`.

Non-goals: query execution, SQL validation/splitting, dump/SDL emission, DDL diff
(`DiffMetadata`), lineage analysis, OpenLineage resolution and Explain SQL.

## Open gaps

- **Never exercised against a live StarRocks/Doris.** All coverage is hermetic, and
  the integration harness only provides PostgreSQL + MySQL. A first real run should
  confirm: system-database discovery, column defaults, the MV catalogs, and
  `SHOW CREATE` output via `GetSchemaString`. (The StarRocks lineage work did drive
  the real driver against a live container for schema sync — see
  `plan/starrocks_lineage_plan.md` — which covers the MV catalog and definitions,
  but the remaining items above are still unconfirmed here.)
- **`DiffMetadata` still answers "engine not supported"** for these engines: it needs
  `schema.GenerateMigration`, which is unrelated to the sync and the DDL store.
- **Lineage:** `STARROCKS` is now served by `backend/plugin/lineage/starrocks` (see
  `plan/starrocks_lineage_plan.md`), so synced StarRocks views and materialized
  views do produce column lineage. `DORIS` is still deliberately unregistered: it
  keeps resolving to `ErrorEngineNotSupported`, the runner records a per-object
  skip so nothing retries forever, and its views produce no column lineage until a
  follow-up plan registers it.
