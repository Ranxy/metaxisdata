# Plan: Column-Level Lineage Analysis Runner

## TL;DR
Build a periodic + event-driven runner that analyzes SQL definitions from stored metadata (VIEW, MATERIALIZED_VIEW) to produce column-level lineage, storing results in PostgreSQL. Uses existing lineage analyzer interface (`GetAnalyzeRelation`) and metahash-based change detection to avoid redundant analysis.

## Decisions
- **Engine scope**: MySQL/TiDB only for now; implementation uses unified `lineage.GetAnalyzeRelation` interface for future engine extensibility
- **Object scope**: Phase 1 covers VIEW and MATERIALIZED_VIEW only; functions/procedures deferred
- **Trigger model**: Event-driven (triggered after schema sync changes) + periodic fallback (1h default)
- **Change detection**: Compare `meta_registry_resource.metahash` against `column_lineage_version.meta_hash` to skip unchanged objects
- **Target simplification**: Target is always the analyzed object itself (meta_guid), so no target_guid column needed

## Steps

### Phase 1: Database Schema (migration)

**File**: `backend/migrator/latest.sql` — append two new tables

**Table `column_lineage`**: stores individual lineage edges
- `id BIGSERIAL PRIMARY KEY`
- `meta_guid TEXT COLLATE "C" NOT NULL` — GUID of the analyzed object (VIEW/MV), links to `meta_registry_resource.guid`
- `meta_type INT2 NOT NULL` — `storepb.MetaType` of the analyzed object
- `source_guid TEXT COLLATE "C" NOT NULL` — full GUID of the upstream table
- `source_column TEXT COLLATE "C" NOT NULL` — upstream column name
- `target_column TEXT COLLATE "C" NOT NULL` — downstream column name in the analyzed object
- `relation_type INT2 NOT NULL DEFAULT 0` — maps to `model.RelationType`
- `transformation JSONB NOT NULL DEFAULT '[]'` — serialized `[]model.Transformation`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`
- **Indexes** (designed for large scale):
  - `idx_column_lineage_meta(meta_guid, meta_type)` — batch delete/query per object
  - `idx_column_lineage_source(source_guid, source_column)` — impact analysis: "what does this column affect?"
  - `idx_column_lineage_target(meta_guid, target_column)` — provenance: "where does this column come from?"

**Table `column_lineage_version`**: lightweight tracking of analysis state per object
- `meta_guid TEXT COLLATE "C" NOT NULL`
- `meta_type INT2 NOT NULL`
- `meta_hash BYTEA` — hash at time of analysis (compare with `meta_registry_resource.metahash`)
- `analyzed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`
- `error_message TEXT` — captures analysis errors for debugging
- `PRIMARY KEY (meta_guid, meta_type)` — one row per analyzed object

### Phase 2: Store Layer

**New file**: `backend/store/column_lineage.go` — following patterns from `database.go` / `meta_resource.go`

Types:
- `ColumnLineage` struct — maps to `column_lineage` table row
- `FindColumnLineageMessage` struct — flexible query builder with pointer fields (MetaGUID, MetaType, SourceGUID, SourceColumn, TargetColumn, Limit, Offset)
- `ColumnLineageVersion` struct — maps to `column_lineage_version` table row

Methods on `*Store`:
1. `BatchReplaceColumnLineage(ctx, metaGUID, metaType, lineages []*ColumnLineage)` — within a transaction: DELETE existing rows for meta_guid+meta_type, INSERT new rows using UNNEST batch pattern (like `BatchCreateMetaRegistryResource`)
2. `ListColumnLineage(ctx, *FindColumnLineageMessage) ([]*ColumnLineage, error)` — dynamic WHERE builder
3. `UpsertColumnLineageVersion(ctx, *ColumnLineageVersion)` — ON CONFLICT(meta_guid, meta_type) DO UPDATE
4. `GetColumnLineageVersion(ctx, metaGUID, metaType) (*ColumnLineageVersion, error)`
5. `DeleteColumnLineageByMeta(ctx, metaGUID, metaType)` — cleanup when meta object is deleted

### Phase 3: Catalog Context Enhancement

**Modify**: `backend/plugin/lineage/catalog/provide.go`

Problem: The analyzer creates `ObjectIdentifier` from SQL-parsed table names. Unqualified names like `users` produce GUID `";;;users"` which won't match stored metadata `"inst1;db1;public;users"`.

Solution: Add `AnalysisContext` to `context.Context`:
- Define `AnalysisContext` struct with `InstanceID`, `Database`, `Schema` fields
- Add context key + helper: `WithAnalysisContext(ctx, ac)` / `GetAnalysisContext(ctx)`
- In `provideImpl.GetTable()`: before GUID lookup, fill missing ObjectIdentifier parts from `AnalysisContext`
- This is backward-compatible — if no context is set, behavior is unchanged

### Phase 4: Runner

**New directory**: `backend/runner/lineageanalyzer/`
**New file**: `backend/runner/lineageanalyzer/analyzer.go`

Struct: `Analyzer`
- Fields: `store`, `analyzeMap sync.Map` (async queue), `profile`

Constructor: `NewAnalyzer(store *store.Store, profile *config.Profile) *Analyzer`

Lifecycle: `Run(ctx context.Context, wg *sync.WaitGroup)`
- **Goroutine 1**: Periodic full scan (default 1h, `lineageAnalysisInterval`)
  - List all instances → get engine type per instance
  - Query `meta_registry_resource` for VIEW + MATERIALIZED_VIEW types
  - For each object: compare metahash against `column_lineage_version` — skip if unchanged
  - Queue changed objects for analysis
- **Goroutine 2**: Queue consumer (check every 10s, `analyzeCheckerInterval`)
  - Drain `analyzeMap`, run analysis with concurrency pool (`MaxGoroutines`)
  - For each object:
    1. Extract context from GUID: instanceID, database, schema
    2. Look up instance engine type
    3. Get SQL definition from metadata (`ViewMetadata.Definition` / `MaterializedViewMetadata.Definition`)
    4. Wrap definition: `CREATE VIEW {name} AS {definition}` (needed because definition is just SELECT)
    5. Call `lineage.GetAnalyzeRelation(ctx, engine, wrappedSQL)`
    6. Filter out `IsTemp=true` relations
    7. Map Source ObjectIdentifier to full GUIDs using context
    8. Call `store.BatchReplaceColumnLineage()`
    9. Call `store.UpsertColumnLineageVersion()` with current metahash
    10. On error: store error in version table, log, continue

Public trigger: `QueueAnalysis(metaGUID string, metaType storepb.MetaType)` — puts object into `analyzeMap`

### Phase 5: Integration (*depends on Phase 1-4*)

1. **`backend/server/server.go`**: In `NewServer()`:
   - Create `lineageAnalyzer := lineageanalyzer.NewAnalyzer(stores, profile)`
   - Start: `s.runnerWG.Add(1); go lineageAnalyzer.Run(ctx, &s.runnerWG)`
   - Store reference on Server struct for schemasync to access

2. **`backend/runner/schemasync/syncer.go`**: In `SyncDatabaseSchema()`:
   - After successful metadata sync commit, queue changed objects:
   - For each VIEW/MATERIALIZED_VIEW in the batch, call `lineageAnalyzer.QueueAnalysis(guid, metaType)`
   - Syncer needs a reference to lineageAnalyzer (pass via constructor or server)

3. **Coupling approach**: Add `lineageAnalyzer *lineageanalyzer.Analyzer` field to `Syncer`, passed via `NewSyncer()` constructor update

## Relevant Files

- `backend/migrator/latest.sql` — append column_lineage + column_lineage_version CREATE TABLE
- `backend/store/column_lineage.go` — NEW: store CRUD for lineage data (reference `meta_resource.go` patterns for batch ops, `database.go` for query builder)
- `backend/runner/lineageanalyzer/analyzer.go` — NEW: runner (reference `schemasync/syncer.go` for lifecycle/pool/queue patterns)
- `backend/plugin/lineage/catalog/provide.go` — MODIFY: add AnalysisContext to fill missing ObjectIdentifier parts
- `backend/plugin/lineage/lineage.go` — reference only (existing API)
- `backend/plugin/lineage/model/relation.go` — reference only (`ColumnRelation`, `RelationType`)
- `backend/plugin/lineage/model/identifier.go` — reference only (`ObjectIdentifier.GUID()`)
- `backend/server/server.go` — MODIFY: create + start lineage analyzer runner
- `backend/runner/schemasync/syncer.go` — MODIFY: add lineageAnalyzer dependency, queue analysis after sync
- `backend/store/store.go` — reference only (Store struct, no changes needed)
- `backend/common/const.go` — reference only (`MetaGUIDSplit = ";"`)

## Verification

1. **Schema**: Run `psql` to create tables from migration, verify indexes with `\d column_lineage`
2. **Unit test**: `backend/store/column_lineage_test.go` — test BatchReplace, List, Version tracking
3. **Integration test**: Create mock meta_registry entries for a VIEW, run analyzer, verify lineage stored correctly
4. **Lint**: `golangci-lint run --allow-parallel-runners` until clean
5. **Build**: `go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go`
6. **Manual**: Start server, trigger schema sync, verify lineage rows populated via direct DB query
7. **Change detection**: Modify a VIEW definition, re-run sync, verify only changed object re-analyzed
8. **Error handling**: Test with invalid SQL definition, verify error captured in `column_lineage_version.error_message`

## Further Considerations

1. **Future PG analyzer**: Implementation is engine-agnostic via `GetAnalyzeRelation`. Adding PG support only requires implementing and registering a PG analyzer in `backend/plugin/lineage/pg/` — no changes to runner or store needed.
2. **Function/Procedure support**: When added, the runner just needs to include FUNCTION/PROCEDURE meta types in its scan and extract DML statements from function bodies for analysis.
3. **Cleanup on meta deletion**: When `schemasync` deletes stale metadata, it should also clean up corresponding `column_lineage` and `column_lineage_version` rows. This can be added to `batchMetaCreate.Run()` where deletes happen.
