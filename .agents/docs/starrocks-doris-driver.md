# StarRocks / Doris driver — Reference

> Maintenance reference for `backend/plugin/db/starrocks` and the engine plumbing around it.
> Related: [object-ddl-storage.md](object-ddl-storage.md) (how these engines' DDL is captured and served per object).

## What it is, and why one package

One package serves **both** `storepb.Engine_STARROCKS` and `storepb.Engine_DORIS` over the MySQL wire protocol. It implements only what `db.Driver` requires here — `SyncInstance` and `SyncDBSchema` (tables, columns, views, materialized views) — so query execution, SQL split/rewrite, dump/DDL emission and transaction control are absent by design. Connection style follows `backend/plugin/db/mysql`.

The two engines share one package because they differ only in the system-database exclusion and the materialized-view catalog. Splitting them would duplicate the sync and lose the shared registration.

## Decisions

| Decision | Why |
| --- | --- |
| Both engines in one package | They differ only in the system-database exclusion and the MV catalog. |
| Enum values `STARROCKS = 15` / `DORIS = 16` un-reserved and restored | Keeps the store and v1 enums value-compatible with the reference project and with any historical row. **No DB migration**: the value only lives inside JSONB `metadata` blobs. |
| `interpolateParams=true` on the DSN | Neither engine has usable server-side prepared statements, so the sync queries' `?` placeholders must be interpolated client-side. Dropping it breaks every sync query. |
| `CHARACTER_SET_NAME` / `COLLATION_NAME` are not read from `SCHEMATA` | That catalog returns NUL-terminated values the JSONB store rejects, and neither engine has a meaningful database-level charset. The column-level fields are selected and pass through `sanitizeCatalogString`; in the versions exercised both engines returned NULL for them. |
| MySQL helpers are imported, not copied | `UnquoteMySQLString` / `UnescapeExpressionDefault` / `IsCurrentTimestampLike` come from `plugin/db/mysql`; the `allowAllFiles` guard was exported there as `ValidateExtraConnectionParameters` so the two drivers cannot drift. |
| No `plugin/schema`, lineage, OpenLineage or Explain SQL registration | Out of scope; see Open gaps for what that leaves unsupported. |

## Engine differences

| Dimension | StarRocks | Doris |
| --- | --- | --- |
| System databases excluded | `information_schema`, `_statistics_`, **`sys`** | `information_schema`, `_statistics_`, **`__internal_schema`** |
| Materialized-view catalog | `information_schema.materialized_views`, `REFRESH_TYPE='ROLLUP'` rows excluded (synchronous rollups are base-table indexes, not MVs) | `mv_infos("database"=?)` table-valued function |
| MV reported in `information_schema.tables` as | `TABLE_TYPE='VIEW'` | `TABLE_TYPE='BASE TABLE'` |

Both are treated as having no schema concept (`store.IsObjectCaseSensitive`'s default branch is already correct) and address objects as `database.table`.

## Contracts worth knowing before touching the sync

- **Connection.** DSN with `maxAllowedPacket=0`, `interpolateParams=true`; `multiStatements` stays off so a statement built from catalog names cannot run an injected second statement. `parseVersion` splits a MySQL-style version string and keeps the suffix.
- **`SyncInstance`.** Version, then a **best-effort** `lower_case_table_names` (`SHOW VARIABLES LIKE …`; a failure is logged at debug and treated as `0`), then `information_schema.SCHEMATA` minus the engine's system databases. Only `SCHEMA_NAME` is selected. The result sets `Instance.MysqlLowerCaseTableNames`.
- **`SyncDBSchema`.** Order: columns → views → materialized views → tables. An `information_schema.tables` row whose name is in the materialized-view set becomes an MV **regardless of `TABLE_TYPE`** — membership is the authoritative signal, because the two engines disagree on the reported type. `BASE TABLE` → tables, `VIEW` → views, anything else skipped at debug. Both MV queries are best-effort: an unsupported engine/version logs at debug and yields an empty MV set rather than failing the sync.
- **Column defaults.** The MySQL rule **minus `ON UPDATE`** (neither engine supports it): unquote, current-timestamp-like → bare function name, `DEFAULT_GENERATED` → `(expr)`, otherwise the literal; an `AUTO_INCREMENT` column with no default gets the symbol, a nullable one gets `NULL`. A BITMAP/HLL default is not text — the catalog reports a single `0x00` byte, which `sanitizeCatalogString` drops.

## Invariants

1. StarRocks and Doris stay in **one** registered package.
2. `interpolateParams=true` stays on the DSN, and every catalog query keeps using `?` placeholders.
3. Every catalog value the driver copies into `StoredMetadata` or the DDL store passes through `sanitizeCatalogString`, which drops NUL bytes: BITMAP/HLL defaults and NUL-terminated catalog values both carry them, and PostgreSQL jsonb/TEXT cannot represent U+0000. `CalcStoreMetaHash` is the second line of defense for meta-registry metadata — it replaces any remaining U+0000 with U+FFFD, so one such value no longer fails a database's batch write. The configured database name is not rewritten: it names GUIDs, so a NUL there still fails loudly.
4. Materialized-view membership, not `TABLE_TYPE`, decides whether a table row is an MV, and both catalog queries stay best-effort.
5. `db.Driver` must not grow a query/execute surface for these engines without a corresponding test: nothing in this repository calls one.

## Failure modes

| Scenario | Behavior |
| --- | --- |
| `lower_case_table_names` unavailable | Logged at debug, treated as `0`, sync continues |
| MV catalog unsupported by the engine version | Logged at debug, empty MV set, sync continues |
| Engine reports an unrecognized `TABLE_TYPE` | Skipped at debug |
| A catalog value contains a NUL byte | Dropped by `sanitizeCatalogString` before the snapshot or DDL is built; a NUL reaching the meta-registry metadata becomes U+FFFD |
| `SHOW CREATE` for an object fails | Not this driver's sync; see [object-ddl-storage.md](object-ddl-storage.md) |

## Where things live

- Package `backend/plugin/db/starrocks/`: `starrocks.go` (connection, registration, `parseVersion`), `sync.go` (`SyncInstance`, `SyncDBSchema`, `systemDatabaseExclusion`, `isMaterializedView`, `isSyncRollup`), `definition.go` (`SHOW CREATE`), `errors.go`. All tests are hermetic, no live database.
- Registration is a blank import in server startup plus `db.Register` in the package's `init()`. An instance becomes creatable only once its engine is in `supportedStoreEngines` (`backend/api/v1`), and everything else follows from the registration.
- The enum mirrors in `proto/store/store/common.proto` and `proto/v1/v1/common.proto` must match; regenerate with buf.
- The SPA names these engines as literal strings (instance picker, instance list/detail, metadata browser, database management), so there are no locale keys to update.
- Gate: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./...`, build; SPA `biome:check`, `type-check`, `test run`.

## Open gaps

- **Never exercised against a live StarRocks/Doris.** All coverage is hermetic, and the integration harness only provides PostgreSQL + MySQL. The StarRocks lineage work did drive the real driver against a live container for schema sync, which covers the MV catalog and definitions, but system-database discovery, column defaults and `SHOW CREATE` output remain unconfirmed.
- **`DiffMetadata` still answers "engine not supported"** for these engines: it needs `schema.GenerateMigration`, which is unrelated to the sync and the DDL store.
- **Lineage** is served by `backend/plugin/lineage/starrocks` and `backend/plugin/lineage/doris` — sibling packages rather than one grammar, because their ASTs differ on parenthesized queries, table functions and `LATERAL VIEW`. See [lineage-dialects.md](lineage-dialects.md).

Non-goals: query execution, SQL validation/splitting, dump/SDL emission, DDL diff (`DiffMetadata`), lineage analysis, OpenLineage resolution and Explain SQL.
