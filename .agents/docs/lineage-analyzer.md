# Lineage analyzer — Reference

Maintenance reference for `backend/plugin/lineage/`, `backend/runner/lineageanalyzer/` and `backend/api/v1/lineage_service_analyze.go`.
Related: [lineage-semantics.md](lineage-semantics.md) (what an analyzer must keep producing), [lineage-dialects.md](lineage-dialects.md) (the engine-specific analyzers), [lineage-graph.md](lineage-graph.md) (field-level lineage on the graph page), [application-sql-ingestion.md](application-sql-ingestion.md) (the reported-SQL ingest path), [omni-upstream-defects.md](omni-upstream-defects.md) (upstream defects and the no-patch policy), [backend/plugin/lineage/AGENTS.md](../../backend/plugin/lineage/AGENTS.md) (the rules for changing the code).

## What it is

Three stages turn a SQL definition into table- and column-level lineage. The **analyzer package** `backend/plugin/lineage/` parses one statement into `[]model.ColumnRelation`; the **runner** `backend/runner/lineageanalyzer/` periodically and on events fetches the definitions of VIEW / MATERIALIZED_VIEW / MANUAL_SQL / APPLICATION_SQL, calls the analyzer, and writes the edges into `column_lineage`; the **API** in `backend/api/v1/lineage_service_analyze.go` is the stateless `AnalyzeSQL`, running the same analysis and GUID completion for the graph page and the CLI.

The contract is `lineage.Analyzer.Analyze(ctx, engine, sql) ([]model.ColumnRelation, error)`. `sql` may be a multi-statement script: the root package splits it above the dialects and hands each statement to a **single-statement** dialect analyzer, so no scope is shared across statements.

A result is one of three kinds — the core distinction of the subsystem:

| Kind | Trigger | Edges | Error | Consumer action |
| --- | --- | --- | --- | --- |
| Complete | every statement modelled | all | nil | store edges |
| Partial analysis | a statement or clause is **not modelled** | the edges found | `*lineage.UnsupportedStatementError{Diagnostics, Omitted}` | store edges + record the gap on the version row / RPC diagnostics |
| Hard failure | a statement **cannot be parsed** | none | plain error | clear the object's lineage + `error_message` |

## Package layout and assembly

Assembly is a **compile-time list with value passing**: no `init()` self-registration, no package-level registry. Each dialect exports a `Registration()`; the `engines` package lists all seven; the server builds one `*lineage.Analyzer` and injects it into each consumer explicitly.

| File | Role |
| --- | --- |
| `backend/plugin/lineage/lineage.go` | `NewAnalyzer(cat catalog.Provide, registrations ...EngineRegistration)` (a duplicate engine panics) and `Analyzer{catalog, engines}` — the value is owned by the caller, not package state; `ErrorEngineNotSupported` |
| `backend/plugin/lineage/engines/engines.go` | `Registrations()` lists MySQL / TiDB / MariaDB / PostgreSQL / StarRocks / Doris / MSSQL |
| `backend/server/server.go` | builds `lineage.NewAnalyzer(catalog.NewCatalogProvide(stores), engines.Registrations()...)` and passes the same value to `lineageanalyzer.NewAnalyzer`, the OpenLineage processor and the gRPC routers (LineageService / ExplainSQLService) |
| `backend/plugin/lineage/catalog/provide.go` | `AnalysisContext{InstanceID, Database, Schema}` with `WithAnalysisContext` / `GetAnalysisContext`; `GetTable` completes an unqualified name from the context |

OceanBase is deliberately not registered (see the `engines` package comment); `Analyze` answers it with `ErrorEngineNotSupported`. Doris is registered as a dialect package of its own.

## Runner lifecycle and change detection

| Mechanism | Symbol | Behavior |
| --- | --- | --- |
| Periodic full scan | `Analyzer.Run`, `queueAllBounded`, `queueAll` | `lineageAnalysisInterval = 1h`, one scan at startup; results enter the **scan backlog** queue (`queueScan`) |
| Queue consumption | `drainAndAnalyze`, `takeAnalyzeBatch` | `analyzeCheckerInterval = 10s`, worker pool `MaxGoroutines = 10`; each tick drains the **event queue** first and fills the rest of the budget from the scan backlog, bounded by `maxAnalyzeBatch = MaxGoroutines*100`; entries over the cap stay queued |
| Per-object deadline | `analyzeObject`, `analysisTimedOut` | each object runs under `analyzeObjectTimeout = 2m`; a timeout or cancellation is a transient failure, retried with backoff, with **no failure state and no cleared edges** |
| Event-driven | `QueueAnalysis`; `schemasync.SyncDatabaseSchema` after commit; `database_manual_sql.go`; `application_sql_handler.go` | a schema sync enqueues changed VIEW / MV **after** it commits; saving a MANUAL_SQL enqueues it; an APPLICATION_SQL is enqueued only when the upsert **created** the statement — a repeated report is not re-analyzed (see [application-sql-ingestion.md](application-sql-ingestion.md)) |
| Change detection | `needsAnalysis` | compares `meta_registry_resource.metahash` and `column_lineage_version.meta_hash` **and** `analyzer_version`: equal hash and equal analyzer version means skip |
| Analysis write | `analyzeObject`, `buildSQL` | a VIEW/MV body is wrapped as `CREATE VIEW`/`CREATE MATERIALIZED VIEW <name> AS <body>` (a stored definition that is already a complete statement, and a MANUAL_SQL/APPLICATION_SQL text, are analyzed as written) → `Analyze` → filter `IsTemp` → `ReplaceColumnLineageIfCurrent` (edges + version + gap message in one transaction) |
| Write precondition | `lockAnalysisState` | `SELECT … FOR SHARE` against the `meta_hash` read for the analysis: object deleted → drop the result (`ErrAnalysisGone`); hash changed → drop and re-queue immediately (`ErrAnalysisStale`) |
| Deterministic failure | `markAnalysisFailed` | `MarkAnalysisFailedIfCurrent`: clear the object's lineage + write `error_message` + store the current `meta_hash`, in one transaction; a nil cause means the definition became empty |
| Empty definition | `analyzeObject`, `emptyDefinitionClearsLineage` | a MANUAL_SQL/APPLICATION_SQL text that was cleared is a definition that no longer exists → clear + explain; a VIEW/MV cannot lack a definition, so empty means this snapshot could not read it → **keep the last correct edges**, record the reason, store the current hash |
| Transient failure | `analysisRetryBackoff`, `retryAfterFailure` | bounded backoff 30s / 2m / 5m, at most 3 attempts, then wait for the next full scan; a store error before any result exists updates `error_message` only (`RecordAnalysisError`), leaving the old hash |
| Analyzer version | `lineage.AnalyzerVersion` | bump whenever a change can alter analyzer output (an omni/dialect upgrade, a rule change, registering an engine); the hourly scan then re-analyzes every mismatch by itself |
| Unsupported engine | `analyzeObject`'s `ErrorEngineNotSupported` branch | record `markAnalyzed` + "engine %s has no lineage analyzer; analysis skipped" without clearing edges |
| Deletion cleanup | `store.DeleteColumnLineageByMetaGUIDs` | on object deletion, filter by type (endpoints TABLE/VIEW/MV/EXTERNAL_TABLE; producers VIEW/MV) and delete the endpoint/producer edges plus the version row in a fixed number of statements, independent of how many objects were deleted |
| MANUAL_SQL special case | `analyzeObject` | every source edge targets the statement object itself, and a second edge from that object to each real target makes "what this SQL writes" visible on the graph |

Store side: `BatchReplaceColumnLineage` (OpenLineage path only), `ReplaceColumnLineageIfCurrent`, `MarkAnalyzedIfCurrent`, `MarkAnalysisFailedIfCurrent`, `RecordAnalysisError`, `DeleteColumnLineageByMetaGUIDs`, `ListColumnLineage`, `ListColumnLineageVersions` and `GetColumnLineageVersion`; the tables are `column_lineage` / `column_lineage_version` (the latter carries `analyzer_version`).

## Diagnostics and gaps

A gap is **structured data**, not a sentence: `model.Diagnostic{Category, Subject, Reference, Detail}`, with four categories (NotModelled / Unresolved / Ambiguous / CatalogUnavailable). `model.FormatDiagnostics` renders it, and `Omitted` becomes "N more not listed" when the cap truncates the list, so a statement full of unresolved references cannot grow the stored text without bound.

| Layer | Symbol | Behavior |
| --- | --- | --- |
| proto | `AnalyzeSQLResult.diagnostics`, `AnalyzeSQLResult.omitted_diagnostic_count`, `AnalyzeSQLDiagnostic`, `DiagnosticCategory` | |
| API | `LineageService.AnalyzeSQL` | a partial analysis is not a failure: the relations are **kept** and the gaps go into `result.Diagnostics` |
| API still failing | `analyzeSQLScope` | no analyzer → `FailedPrecondition`; invalid scope or parse failure → `InvalidArgument` |
| CLI | `cli/cmd/lineage.go` | JSON output `diagnostics` / `omittedDiagnosticCount`; table output uses `diagnosticText` for the same wording |

There is one boundary: **a parse failure clears lineage, an unmodelled clause only records a gap**. The first means "the text cannot be trusted"; the second means "coverage is incomplete, but the edges found are trustworthy". A failed catalog query belongs to the second: it degrades to `DiagnosticCatalogUnavailable` and the wildcard fallback, and analysis continues.

## The MySQL family's single source of truth

TiDB and MariaDB have **no traversal of their own**. The text below the sentinel in `mysql/analyzer.go` is the only editable copy; the generator writes it byte for byte into the two siblings' `analyzer_body_gen.go`.

| File | Role |
| --- | --- |
| `backend/plugin/lineage/mysql/analyzer.go` | the only source of truth: header + `// == MYSQL-FAMILY SHARED BODY ==` sentinel + body (`//go:generate go run ./gen`) |
| `backend/plugin/lineage/tidb/analyzer_body_gen.go`, `backend/plugin/lineage/mariadb/analyzer_body_gen.go` | generated bodies, `DO NOT EDIT` |
| `backend/plugin/lineage/tidb/dialect.go`, `backend/plugin/lineage/mariadb/dialect.go` | hand-written dialect headers: package doc, omni imports, the four marker constants, `Registration()` and the two hooks (`valuesQueryPrimary`, `rowAliasNames`) |
| `backend/plugin/lineage/mysql/gen/generate.go` | the generator's pure functions: `Sentinel`, `dialects`, `Split`, `Generate`, `Render`, `checkSeam` |
| `backend/plugin/lineage/mysql/gen/main.go`, `generate_test.go` | the IO shell (including `-check`), plus `TestGeneratedBodiesAreFresh`, `TestGeneratedBodiesCopyTheSourceVerbatim`, `TestSplitRejectsMalformedSource`, `TestSeamSelfCheck` |

Which layer to edit:

- **Traversal behavior** → only below the sentinel in `mysql/analyzer.go`, then `go generate ./backend/plugin/lineage/mysql` (see [backend/plugin/lineage/AGENTS.md](../../backend/plugin/lineage/AGENTS.md)).
- **Dialect facts** → only `dialect.go`: the four marker constants (`resultTableName`, `deletionFieldName`, `wildcardColumn`, `fileSourceMarker`) and the two hooks.
- The seam is checked **in both directions** (`seamIdentifiers` + `checkSeam`): a body that references a hook the dialect header does not declare fails generation, and a header that declares one the body no longer uses fails too. A new variation point therefore means changing the body and both headers together.

MariaDB has one shared-corpus parser gap: the MySQL 8.0.19 upsert row alias, which hard-fails in production and is recorded in `knownParserGaps` in `mariadb/analyze_test.go`.

## AST coverage method

The question a new dialect or a new AST field has to answer is "does the traversal actually reach every field that carries a column?". Answer it by machine over three dimensions rather than by reading code:

| Dimension | Source | Meaning |
| --- | --- | --- |
| **Read** | type-check the analyzer package with `go/types`, walk `SelectorExpr`, and attribute each field read to its declaring struct through `types.Selection.Index()` | avoids the false positives of grepping by name |
| **Generic walk** | parse omni's walker `walkChildren` switch and mark the fields it recurses into | only says "the field's contents get visited when handed to `Inspect`" |
| **Judgment** | classify every unread field and record the reason | |

The decisive distinction: **"not read explicitly" is not "not handled"**. Expression subtrees are covered by the generic `Inspect` walk, so the risk is **structural fields** — the analyzer must read those explicitly to descend, and a missed read makes a whole subtree unreachable. The out-of-scope categories are the ones no analyzer needs to reach: DDL/DCL/admin/transaction/replication statements (the `default` branch ignores them), expression nodes (the generic walk), and nodes omni can hold but the pinned grammar cannot produce. Re-run the audit when a field that changes what a statement means is added; the result is a decision about the code, not a report to keep.

## Invariants

1. **A dialect analyzes exactly one statement** (`AnalyzeRelationFunc`); script splitting lives only in the root package, and no scope is shared across statements.
2. **The registry is a compile-time value**: `engines.Registrations()` + `NewAnalyzer(...)`; do not restore `init()` self-registration, `RegisterAnalyzeRelation`, package-level mutable variables or a lineage blank import. A duplicate engine is an assembly error and panics.
3. **Everything below the sentinel in `mysql/analyzer.go` is the only editable family body**; never hand-edit `analyzer_body_gen.go`. Regenerate after every edit; `TestGeneratedBodiesAreFresh` fails with the command.
4. **Parse failure = clear lineage; unmodelled = keep lineage + gap.** Never turn a gap into a silent drop, and never into a hard failure.
5. **A gap is data, not a sentence**: always through `model.Diagnostic`, mapped to the proto enum by the API and rendered with the same wording by the CLI; `error_message` stores the same rendered text.
6. **Lineage is cleared only by a parse failure, an emptied definition or object deletion.** An unregistered engine is an explicit skip (record `markAnalyzed`) and is not a failure.
7. **An unchanged metahash is not re-queued**: success, skip and failure all write the current hash; otherwise the hourly scan retries forever. A transient error updates `error_message` only and leaves the hash alone.
8. **The write is preconditioned on the hash read for the analysis**: `ReplaceColumnLineageIfCurrent` / `MarkAnalyzedIfCurrent` / `MarkAnalysisFailedIfCurrent` check the registry row and `meta_hash` under `SELECT … FOR SHARE` in one transaction; deleted → drop, changed → re-queue. Never overwrite edges or a version unconditionally.
9. **Re-analysis is decided by metahash + analyzer_version**: any change that can alter analyzer output must bump `lineage.AnalyzerVersion`, and the hourly scan then rebuilds everything; never rely on clearing `column_lineage_version` by hand.
10. **Sentinel literals have one home**: `__result__` / `__deletion__` / `__file__` / `*` are defined only in `backend/plugin/lineage/model/relation.go`; non-test Go code must not repeat the literals.
11. **A failed catalog query does not silently degrade**: record `DiagnosticCatalogUnavailable` and continue with the wildcard, so a degraded result stays distinguishable from a complete one.
12. **Every output change (edges, transformations, error classification) goes through the golden corpus**, which matches edges exactly rather than approximately.
13. **Events first, backfill never blocks events**: `QueueAnalysis` feeds the event queue, which each tick drains first; the hourly scan only fills the backlog queue with the remaining budget. The event path must not regress to "drain the whole registry at once" (it starves a just-saved statement); every tick needs a cap and every object a deadline, with timeouts treated as transient. Queue entries carry a generation: the drain removes only the generation it saw with `CompareAndDelete`, the failure path rewrites backoff only when the generation is unchanged, and no "range then delete" implementation may remove an entry enqueued during the drain.
14. **One edge is one row**: the unique index `idx_column_lineage_edge` (meta_guid, meta_type, source_guid, source_column, target_guid, target_column, relation_type) is the concurrency contract, and inserts use `ON CONFLICT DO NOTHING`. Two writers replacing one object's edges at once must neither duplicate an edge nor fail the batch; transformation is a fact about the edge, not part of its identity, and is not in the key.

## Where things live

| Concern | Code | Test / gate |
| --- | --- | --- |
| Gap model | `backend/plugin/lineage/model/diagnostic.go` | `go test ./backend/plugin/lineage/model/ -run TestDiagnosticRendersTheAnalyzersWording` |
| API / proto / CLI | `backend/api/v1/lineage_service_analyze.go`, `proto/v1/v1/lineage_service.proto`, `cli/cmd/lineage.go` | `go test ./cli/cmd/ -run TestDiagnosticTextMatchesTheAnalyzersWording`; `cd proto && buf lint` |
| Runner | `backend/runner/lineageanalyzer/` (`analyzer.go`, `queue.go`, `analyze.go`, `persist.go`, `build_sql.go`, `classify.go`) | `go test ./backend/runner/lineageanalyzer/` |
| Store, schema, cleanup | `backend/store/column_lineage.go`, `backend/store/meta_resource.go`, `backend/migrator/migration/LATEST.sql`; `SyncDatabaseSchema` calls `DeleteColumnLineageByMetaGUIDs` | `go test ./backend/store/`, `go test ./backend/runner/schemasync/` (a schema change needs an incremental migration too) |
| MySQL-family generation | `backend/plugin/lineage/mysql/analyzer.go` + `mysql/gen/` | `go generate ./backend/plugin/lineage/mysql`; `go test ./backend/plugin/lineage/mysql/gen/` |
| Corpus and shared-mechanism guard | `backend/plugin/lineage/{mysql,tidb,mariadb,postgresql,starrocks,doris,mssql}/testdata/analyze/`, `backend/plugin/lineage/shared_analysis_test.go` | `go test ./backend/plugin/lineage/...`; `go test ./backend/plugin/lineage/ -run TestSharedAnalysisStaysShared` |
| End to end | `backend/test/integration/runner/` (including an MSSQL identifier-case case that registers an instance and a registry row by hand, with no real SQL Server) | `make test-integration` (real PostgreSQL + MySQL) |

The corpus size and per-dialect split are kept in [lineage-dialects.md](lineage-dialects.md); this file does not repeat them. What each engine must keep producing is [lineage-semantics.md](lineage-semantics.md); field-level lineage on the graph page is [lineage-graph.md](lineage-graph.md); omni defects and the no-patch policy are [omni-upstream-defects.md](omni-upstream-defects.md).
