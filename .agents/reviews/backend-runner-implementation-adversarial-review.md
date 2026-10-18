# backend/runner remediation — adversarial implementation review

> **Scope**: independent, read-only adversarial review of the remediation recorded in
> `.agents/reviews/backend-runner-review.md` §0.4 / each finding's 「已实施」 note.
> Checkout: `.agents/worktrees/review-backend-runner`, branch `review-backend-runner`,
> HEAD `cb2183b` (`docs(runner): record the completed remediation and remaining residuals`).
> Baseline for the original review: `387e1d1`. Implementation diff examined:
> `git diff 387e1d1..HEAD -- backend/ proto/ frontend/src/ backend/migrator/`.
> **No product, test or documentation file was modified.** The only write is this report.
>
> **Method**: every 「已实施」 claim was re-read against the current code; concurrency,
> error paths, restart, empty/large sets and wire compatibility were attacked separately;
> all gates in the brief were actually run (see §4). Where a behavior is a deliberate
> accepted trade-off recorded in the report/docs, it is judged as such rather than
> re-reported as a defect.

---

## 1. Summary

**Overall verdict: the remediation is substantive and largely faithful. No Critical
silent-corruption path was found, and nothing here justifies a revert.** The R-01 merge,
R-02 dialect quoting, R-03/R-04 CAS/atomic state, R-19 settle fold, R-25 injective GUID
escaping, R-29 uniqueness migration and the R-21–R-23 hidden-database/operation work are
implemented as described and hold up under adversarial reading and real-PostgreSQL tests.

Four Medium issues and one boundary-case Low are worth acting on before calling the work
done; none is data-destroying:

1. `BatchSyncInstances` was **not** updated to the new `enable_full_sync` default: an unset
   field still means discovery-only there, contradicting the proto comment, the docs and
   the single-instance handler (A-01).
2. The LLM debug-log drain is claimed to be “bounded”, but `server.Shutdown` passes a
   context with **no deadline**; a queue of 64 jobs × 10 s timeouts can hold shutdown for
   roughly five minutes (A-02).
3. The R-09 statistics refresh is now on the **schema-sync critical path**: a stats read
   failure aborts the whole database sync, and a stats write failure rolls back the
   metadata transaction that holds the real schema changes (A-03).
4. The R-30 #9 backlog drain has a boundary case: when the still-unknown backlog is about
   one batch, the same runs' edges are rewritten up to **ten times per trigger**; even for
   larger backlogs the ten-batch drain runs ahead of retention in maintenance and can delay
   it (A-04).
5. R-26's “empty definition clears edges” also fires for a **privilege-limited/partial
   MySQL view snapshot**, silently dropping (recoverable) lineage; the recorded
   `error_message` is not exposed by any API, so the user still gets no explanation (A-05).

### Findings

| ID | Severity | Area | One-line claim | Verdict | Evidence |
| --- | --- | --- | --- | --- | --- |
| A-01 | Medium | R-30 #10 | `BatchSyncInstances` treats unset `enable_full_sync` as false, unlike `SyncInstance` (unset = full) | Confirmed bug | `backend/api/v1/instance_service.go:458` vs `:380`; `proto/v1/v1/instance_service.proto:244-246`; `.agents/docs/schemasync.md:33` |
| A-02 | Medium | R-30 #7 | Debug-log drain is not actually bounded in production; shutdown can block ~5 min | Confirmed bug (claim false) | `backend/component/llm/debug.go:14-19,89-120`; `backend/server/server.go:255-260`; `backend/bin/server/cmd/root.go:162-172` |
| A-03 | Medium | R-09 | Statistics read/write is fatal to the whole database sync, not best-effort | Risk (new failure mode) | `backend/runner/schemasync/syncer.go:859-866,1182-1185` |
| A-04 | Low | R-30 #9 | A one-batch still-unknown backlog is rewritten up to 10× per trigger; 10-batch drains can starve maintenance cleanup | Risk (write amplification) | `backend/runner/lineagevalidation/validation.go:145-177`; `backend/plugin/openlineage/lineage_revalidation.go:66-96,166-185`; `backend/runner/maintenance/maintenance.go:307` |
| A-05 | Medium | R-26 | Empty definition can be a privilege-limited snapshot; clears lineage and the reason is not user-visible | Risk (recoverable data loss) | `backend/runner/lineageanalyzer/analyzer.go:483-495`; `backend/plugin/db/mysql/sync.go:439-470,1055-1080` |
| A-06 | Low | R-17 | `AnalyzerVersion` is a hand-bumped constant; upgrades only self-heal if a developer remembers | Risk | `backend/plugin/lineage/lineage.go:20-25`; `backend/runner/lineageanalyzer/analyzer.go:319-327` |
| A-07 | Low | R-11/R-05 | 30-minute scan budget can cancel legitimate large-estate scans and report them as instance failures | Risk | `backend/runner/schemasync/syncer.go:455-459,472-500,552-564` |
| A-08 | Low | R-30 #8 | `queueScan`/`QueueAnalysis` are not atomic, so the “disjoint queues” claim is false; duplicate concurrent analysis possible | Risk | `backend/runner/lineageanalyzer/analyzer.go:132-152` |
| A-09 | Low | R-10 | Deletion dropped the type predicate; GUID-only matching is not type-safe for synthetic manual/app SQL GUIDs | Risk (contrived over-delete) | `backend/store/column_lineage.go:629-687`; `backend/store/manual_sql.go:18,70`; `backend/store/application_sql.go:20,212` |
| A-10 | Low | R-22 | Checker's `Deleted` branch works on a stale queue copy and is effectively dead; the in-flight residual is up to one DB sync, not “milliseconds” | Risk / test validity | `backend/runner/schemasync/syncer.go:359-365,781-785,31,826`; `backend/runner/schemasync/syncer_test.go:605` |
| A-11 | Low | R-09/R-20 | “Hourly/independent” statistics and DDL cadence is actually a minimum gated by the database sync frequency | Doc mismatch | `backend/runner/schemasync/syncer.go:43,859`; `.agents/docs/object-ddl-storage.md` |
| A-12 | Low | R-14 | “Non-text changes such as engine are covered by the hourly scan” is false; the registry mirror has no engine field | Doc mismatch | `backend/store/application_sql.go:139-145,468-481`; report `:459` |
| A-13 | Low | R-29 | Unique index removes duplicates but not the lost-update race between re-ingestion and revalidation of the same run | Residual risk | `backend/store/column_lineage.go:80-100`; `backend/plugin/openlineage/lineage_revalidation.go:166-185`; `backend/plugin/openlineage/processor.go:103` |
| A-14 | Nit | R-17 (report) | R-17's detail section has no 「已实施」 note although §0.4 says it was implemented | Doc mismatch | `.agents/reviews/backend-runner-review.md:480-489` vs `:92` |
| A-15 | Nit | R-11 (report) | R-11's note says jitter / per-iteration timeout were not done; both were implemented later | Doc mismatch | report `:430` vs `syncer.go:319,455`, `analyzer.go:245` |
| A-16 | Nit | Gates | `golangci-lint` fails (3 revive issues) on untouched `migrator.go`, while the report repeatedly says lint passes | Environment/doc mismatch | `backend/migrator/migrator.go:228,277,399` |
| A-17 | Nit | Tests | No test covers the new unset default, the batch budget, statistics through `SyncDatabaseSchema`, or debug drain with a production (deadline-free) context | Test validity | see §5 |
| A-18 | Nit | R-30 #7 | A test permanently closes the process-global debug writer, making later loggers silently dead | Test pollution | `backend/component/llm/debug_test.go:61-65` |
| A-19 | Nit | R-26/R-02 | `error_message` is not exposed by any RPC, but `.agents/docs/lineage-analyzer.md` says it reaches the RPC boundary | Doc mismatch | `.agents/docs/lineage-analyzer.md:176`; no `error_message` in `proto/v1/v1/*.proto` |

Counts: **0 Critical, 0 High, 4 Medium, 9 Low, 6 Nit.**

---

## 2. Detail

### A-01 (Medium, R-30 #10) — `BatchSyncInstances` still uses the old default

- **Claim** (§0.4, last paragraph): `enable_full_sync` is proto3 `optional`; **unset = full**
  (“服务端按未设置判断”); the same proto comment is on the shared `SyncInstanceRequest`
  (`proto/v1/v1/instance_service.proto:239-246`) and `.agents/docs/schemasync.md:33` says
  unset = full for `SyncInstance`.
- **What the code does**: the single-instance handler correctly checks presence —
  `if req.Msg.EnableFullSync == nil || req.Msg.GetEnableFullSync()`
  (`backend/api/v1/instance_service.go:380`). The batch handler did not change:
  `if r.GetEnableFullSync()` (`:458`). `GetEnableFullSync()` returns false for nil, so an
  unset field in a batch request means **discovery-only** (only `newDatabases` are
  queued), exactly the old behavior R-30 #10 was meant to remove.
- **Failure scenario**: an external/client-generated `BatchSyncInstancesRequest` with
  `requests: [{name: "instances/i"}]` (no flag) returns success but only discovers new
  databases; existing databases are not refreshed. A user who has just fixed a target
  database and batch-syncs it gets “success” with stale metadata, while the single-instance
  button with the same empty flag does a full refresh. The bundled frontend does not call
  the batch RPC, so this is an API/integration-client defect rather than a UI one.
- **Blast radius**: wrong user-visible semantics; no data corruption. It also makes the
  proto comment false for one of the two RPCs that accept the message.
- **Suggested fix**: introduce one helper, e.g.
  `func fullSyncRequested(r *v1pb.SyncInstanceRequest) bool { return r.GetEnableFullSync() }`
  is not enough because proto3 presence is lost on `Get`; use
  `r.EnableFullSync == nil || r.GetEnableFullSync()` in both `SyncInstance` and
  `batchSyncInstance`, and add a handler-level test for unset in both paths.

### A-02 (Medium, R-30 #7) — the debug-log drain is unbounded in production

- **Claim**: the writer “排空并有界等待 (bounded wait)” before `store.Close()`
  (report §0.4, R-30 #7; `backend/server/server.go:255-260` comment says “bounded by the
  same shutdown context”).
- **What the code does**: `debugLogWriter.shutdown(ctx)` only respects the caller's
  `ctx` (`backend/component/llm/debug.go:89-120`). `Server.Shutdown` passes the process
  shutdown `ctx`, which in `backend/bin/server/cmd/root.go:162-172` is
  `context.WithCancel(context.Background())` with **no deadline** and is cancelled only
  *after* `Shutdown` returns. The queue is 64 entries, there are 2 workers, and one stuck
  insert is allowed 10 s (`debug.go:14-19`), so a database that is unavailable at shutdown
  can hold the drain for ≈ 64/2 × 10 s ≈ 5.5 minutes (plus an in-flight write). The test
  `TestDebugLogWriterShutdownHonorsTheContext` only proves that a *caller-supplied*
  deadline works; production never supplies one.
- **Failure scenario**: `SIGTERM` with `debug: true` LLM logging and a PostgreSQL outage /
  pool exhaustion. The process ignores the orchestrator's grace period, is SIGKILLed, and
  the “drain before store close” guarantee is lost anyway; deployments appear hung.
- **Blast radius**: shutdown latency/availability, lost debug rows; no product data.
- **Suggested fix**: bound it at the call site, e.g.
  `dctx, cancel := context.WithTimeout(ctx, runnerShutdownTimeout)` (or a dedicated 5 s
  debug-drain budget) before `ShutdownDebugLogWorkers(dctx)`; alternatively bound the
  number of jobs drained rather than the whole queue. Update the comment/test to use the
  production path.

### A-03 (Medium, R-09) — statistics refresh is fatal to schema sync

- **Claim**: statistics are an optional, UI-only estimate refreshed outside history; a
  failure should be logged (“失败只记日志” is the accepted design in the report's R-09
  recommendation).
- **What the code does**: `SyncDatabaseSchema` calls `ListMetaRegistryStatistics` before
  the write transaction and **returns the error** on failure
  (`backend/runner/schemasync/syncer.go:859-866`), so no metadata diff/write happens for
  that database. Inside the transaction, `BatchUpdateMetaRegistryStatisticsAt` failure is
  returned from `batchMetaCreate.write` (`:1182-1185`), which rolls back the metadata
  upserts/deletes and the DDL writes for real schema changes.
- **Failure scenario (concrete)**:
  - A row's `metadata->tableMetadata->rowCount` is not castable to `bigint` (hand-edited or
    legacy JSON, a JSON number containing whitespace/garbage), or the statistics query
    fails transiently. On the first sync after startup, the read fails; because
    `recordStatisticsRefresh` is only called after a successful commit, the database stays
    “due” forever, so **every** subsequent 15-minute sync of that database fails at the
    same point. Metadata/DDL for that database silently stops converging.
  - A stats UPDATE fails (e.g., a lock timeout on one of thousands of rows) and discards a
    whole batch of real schema changes; the sync retries, but the failure is attributable
    to an optional estimate.
- **Blast radius**: stale schema metadata/DDL for the affected database; no corruption.
  This is a robustness regression introduced by putting a derived, non-essential value on
  the path that owns the source-of-truth rows.
- **Suggested fix**: make stats best-effort: wrap the read in a `slog.Warn` + empty map
  (and still record the refresh attempt so it does not fail every 15 minutes), and apply
  the stats UPDATE in a separate transaction after the metadata commit (or catch its error
  and log, leaving the metadata transaction to commit). Add a test where the stats query
  fails and the metadata diff still commits.

### A-04 (Low, R-30 #9) — draining a one-batch still-unknown backlog rewrites it up to ten times

- **Claim**: `Revalidate*` returns `hasMore`; validation/maintenance loop until the backlog
  is drained or a 10-batch budget is spent; the documented residual is only “同一批边重复读”.
- **What the code does**: `runOnce` loops up to `maxRevalidationBatches = 10`
  (`backend/runner/lineagevalidation/validation.go:145-177`; maintenance equivalent
  `backend/runner/maintenance/maintenance.go:307`). `hasMore` is
  `len(pending) >= limit` (`backend/plugin/openlineage/lineage_revalidation.go:96,144`), so
  whenever at least 500 edges still point at an unknown relation, every trigger runs ten
  batches. Each batch does not merely read: `revalidateRuns` re-validates the run and calls
  `BatchReplaceColumnLineage` for every affected run (`:166-185`), i.e. **DELETE + INSERT
  of every edge of up to 500 runs per batch**. For a backlog well above one batch this
  rotates (the replace assigns fresh `updated_at`/`id`, so the next batch selects different
  oldest rows, which is real progress). The pathological case is a backlog of **exactly one
  batch** of still-unknown edges: the query keeps returning the same ≤500 edges (now with a
  new `updated_at`), and the pass rewrites them ten times before stopping; `hasMore` is true
  at the boundary by construction.
- **Failure scenario**: ingestion leaves ~500 edges naming an unsynced table (a realistic
  page-sized backlog for one busy producer). Every sync that commits metadata
  (`syncer.go:1064` triggers validation when `bmc.updates > 0`) rewrites all of those runs'
  edges ten times; a 10-second checker cadence with frequent metadata changes multiplies
  the write load. For a larger backlog the churn is one pass over each edge, but ten
  batches still run before `DeleteEmptyOpenLineageDatasets` and the retention prune
  (`maintenance.go:206-227`), so a slow drain can consume the 1-hour pass deadline and
  fail the destructive cleanup, delaying retention by another 6 hours.
- **Blast radius**: write amplification on `column_lineage`, longer validation latency,
  retention starvation; no wrong end state (the last rewrite is the same set).
- **Suggested fix**: only continue draining when the batch made progress (e.g. resolved
  count > 0 or the pending set changed), or cap consecutive no-progress batches at 1;
  give revalidation its own sub-budget instead of the whole pass; consider running the
  retention/sweep items before revalidation, or interleaving items.

### A-05 (Medium, R-26) — “empty definition” can be a partial/privilege-limited snapshot

- **Claim**: an object whose definition became empty is “an object whose SQL was emptied”;
  clearing its edges is correct, and the reason is recorded in `error_message`.
- **What the code does**: after `buildSQL`, `definition == ""` calls
  `markAnalysisFailed(..., res.MetaHash, nil)`, which **deletes all edges** and writes the
  message “lineage analysis skipped: the object has no SQL definition”
  (`backend/runner/lineageanalyzer/analyzer.go:483-495`). `buildSQL` returns `""` whenever
  the stored `ViewMetadata.Definition` is empty (`:686-699`).
- **Where an empty definition comes from besides “the SQL was emptied”**:
  `backend/plugin/db/mysql/sync.go:439-446` reads `information_schema.VIEWS.VIEW_DEFINITION`
  (empty when the connecting account lacks `SHOW VIEW`), then
  `reconcileViewDefinition` (`:466-472,1055-1080`) overwrites it only when `SHOW CREATE`
  succeeds and returns a non-empty body. If privileges are temporarily reduced (the same
  condition the review itself treats as a destructive-sync hazard in R-10/R-25), the sync
  stores the view with an empty definition, the changed hash queues analysis, and the
  analyzer **clears previously correct lineage**. When privileges return, the definition
  changes again and the lineage is rebuilt — so the loss is recoverable, but the graph is
  silently empty in the meantime. An empty definition can also be a partial snapshot from
  a driver that returns rows without a body.
- **User-visible semantics**: `error_message` is written to `column_lineage_version`, but
  **no RPC returns it** (no `error_message` field in `proto/v1/v1/*.proto`; the only
  readers are the analyzer and tests). So the user sees “no lineage” with no reason — the
  exact complaint R-02 raised about the old failure path.
- **Blast radius**: recoverable lineage loss for views synced while the definition is
  unavailable; no corruption. It is a deliberate product decision for the “really emptied”
  case, but the trigger is broader than the decision describes.
- **Suggested fix**: distinguish “the driver had no definition for this object in this
  snapshot” from “the stored definition changed to empty”. Keep the previous edges and
  record a warning for the former (e.g., a `DefinitionUnavailable` flag on the sync
  metadata, or leave `error_message` and the old edges alone when the sync's view metadata
  came from a privilege-limited read); only clear when the definition was previously
  non-empty and the *same* sync source now positively reports an empty body. Also surface
  `error_message` through the metadata/lineage RPC if the explanation is meant to be seen.

### A-06 (Low, R-17) — analyzer version is still manual discipline

- **Claim**: “分析器升级/回滚后既有对象不重算” is closed; deploying an analyzer change
  refreshes lineage by itself.
- **What the code does**: `lineage.AnalyzerVersion = "1"` is a hand-written constant
  (`backend/plugin/lineage/lineage.go:20-25`), compared with the stored column in
  `needsAnalysis` (`backend/runner/lineageanalyzer/analyzer.go:319-327`). Nothing derives
  it from `omni`'s version, the engine registrations, or the analyzer body; nothing in CI
  fails when analyzer rules change without bumping it. `.agents/docs/lineage-analyzer.md:55`
  and invariant 9 correctly tell a human to bump it, so this is a documented process, not a
  hidden bug — but the report's R-17 claim (“升级不生效” fixed) is only true under that
  discipline.
- **Failure scenario**: an omni upgrade or a dialect rule fix ships without a bump; every
  existing object keeps its old lineage until its definition changes. The failure is
  invisible (no mismatch is computed).
- **Suggested fix**: derive the version from `omni`'s module version plus the analyzer
  registration set (a small `//go:generate`/build-info constant), or add a CI check that
  fails when `backend/plugin/lineage/**` or `go.mod`'s omni line changes without an
  `AnalyzerVersion` change. At minimum, add the bump to the release checklist.

### A-07 (Low, R-11/R-05) — 30-minute scan budget can cancel a legitimate scan

- **Claim**: R-11 adds per-run timeouts; the scan budget “still has the whole 30 minutes to
  enumerate its instances and queue their databases”.
- **What the code does**: `trySyncAllOnce` cancels the entire scan after
  `2 * instanceSyncInterval` = 30 minutes (`backend/runner/schemasync/syncer.go:455-459`).
  Instances run at `MaximumOutstanding = 100` (`:472`), each `SyncInstance` may take the
  15-minute `syncTimeout`; a few hundred slow/due instances therefore exceed 30 minutes.
  Cancelled `SyncInstance` calls return `context.Canceled`, and `reportInstanceSyncFailure`
  (`:555-564`) treats that like any failure: `slog.Warn` plus an administrator
  notification (hour-bucketed per instance). Connection-limit saturation is special-cased,
  but context cancellation is not.
- **Failure scenario**: 300 due instances × 15-minute targets ≈ 45 minutes; the scan is
  cancelled at 30, the last ~100 instances are reported as failures every round, their
  databases are skipped for the rest of the round (`failedInstances`), and admins get
  failure notifications for what is simply a too-small budget. Partial `CreateDatabaseDefault`
  work is retried next round, so the end state converges.
- **Suggested fix**: treat `errors.Is(err, context.DeadlineExceeded/Canceled)` on the
  scan-context path as a skip (debug log, no notification, no `failedInstances` entry, or
  leave them due for the next round), and/or make the budget scale with the number of due
  instances. Raise the budget or document it as a cap.

### A-08 (Low, R-30 #8) — the “disjoint queues” claim is false under concurrency

- **Claim**: “The two queues are disjoint by construction: `QueueAnalysis` removes the scan
  entry and `queueScan` skips a key the event queue already holds.”
- **What the code does**: `queueScan` does a non-atomic `Load` on `analyzeMap`, then
  `scanMap.Store` (`backend/runner/lineageanalyzer/analyzer.go:142-152`); `QueueAnalysis`
  does `scanMap.Delete` then `analyzeMap.Store` (`:132-140`). Interleaving
  `queueScan.Load` (misses) → `QueueAnalysis` (delete/insert) → `queueScan.Store` puts the
  same key in **both** maps. `takeAnalyzeBatch` selects from each map independently
  (`drainAndAnalyze`, `:379-385`), so one tick can launch two analyses of the same object
  concurrently. The generation machinery keeps both entries and both writes use the CAS
  hash guard, so the final edge/version state is the same, but the work is done twice and
  the two attempts' `retryMap`/`consume` bookkeeping can leave a stale backoff after a
  successful attempt (delaying a later re-analysis by up to 5 minutes).
- **Failure scenario**: hourly `queueAll` runs concurrently with an API/schema-sync
  `QueueAnalysis` for the same object; duplicate parse/N+1/write work, occasional
  redundant retry.
- **Suggested fix**: make enqueueing atomic per key (one `sync.Map` whose value carries the
  priority, or a per-key `sync.Mutex` around the move), or accept the duplicate work but
  document it; at minimum narrow the comment claim.

### A-09 (Low, R-10) — batch lineage deletion dropped the type predicate

- **Claim**: “a GUID from a schema snapshot names one object in the managed namespace, so
  the type adds nothing”; the batch deletes `source/target` by GUID and `meta_guid` by GUID.
- **What the code does**: `columnLineageDeleteByEndpointStatement` uses
  `source_guid = ANY($1) OR target_guid = ANY($1)` and
  `columnLineageDeleteByProducerStatement` uses `meta_guid = ANY($1)`
  (`backend/store/column_lineage.go:629-687`), with no `source_type`/`target_type`/
  `meta_type` predicate (the old per-object code matched all three pairs).
- **Why the assumption is not absolute**: `meta_registry_resource`'s identity is
  `(guid, object_type)`, and manual/application SQL use synthetic name segments
  (`__manual_sql__/<id>`, `__application_sql__/<id>`, `backend/store/manual_sql.go:18,70`;
  `backend/store/application_sql.go:20,212`). A view literally named
  `__manual_sql__/abc` in the same database/schema gets the same GUID as the manual SQL
  object with ID `abc`. Deleting that view would also delete the manual SQL's edges and its
  `column_lineage_version` row (the latter forces re-analysis, so it recovers).
  Same for `__application_sql__/`. This is contrived, but it is the exact class of
  assumption that the registry schema does not guarantee.
- **Blast radius**: contrived over-delete of ingested/manual lineage; recoverable by
  re-analysis/re-ingestion. Under-delete of legitimate edges was not found: the producer
  filter correctly covers VIEW/MV, and schema sync neither manages nor deletes
  manual/application SQL rows (`isSchemaSyncManagedMetaType`, `syncer.go:1251-1267`).
- **Suggested fix**: restore the type predicates for at least the `source_type`/
  `target_type` and `meta_type` directions (or add the types to the arrays), since the
  current code already has the keys; or make the synthetic GUID namespaces collision-proof
  (e.g. a character that cannot appear in an identifier, or include the type in the GUID).

### A-10 (Low, R-22) — the checker's `Deleted` check is effectively dead

- **Claim**: the checker drops queued `Deleted=true` rows and reports hidden to waiting
  operations.
- **What the code does**: the check at `backend/runner/schemasync/syncer.go:359-365`
  inspects the pointer stored in `databaseSyncMap`. `enqueueDatabase` skips deleted rows
  (`:244-245`) and stores the caller's copy; the real soft-delete path (`SyncInstance`,
  `:741-744`) calls `forgetQueuedDatabase`/`completeDatabase` directly instead of mutating
  queued copies. Therefore a queued copy's `Deleted` field is only true if a caller passes
  an already-deleted pointer, which `enqueueDatabase` rejects. The branch is defensive dead
  code, and `TestDropHiddenDatabaseReportsTheHiddenOutcome`
  (`backend/runner/schemasync/syncer_test.go:605`) exercises it only by mutating the queue
  copy by hand — a state production does not create.
- **Residual window wording**: the report calls the remaining “checker already dequeued,
  soft-delete commits afterwards” race “毫秒级”. In reality, if the checker is already
  inside `SyncDatabaseSchema`, the window is bounded by one database sync (up to
  `syncTimeout = 15m`, `syncer.go:406`), during which it can write `LastSyncTime` and
  metadata rows for a database the instance enumeration just hid. The row is not
  resurrected (`UpdateDatabase` never clears `deleted`), so the documented consequence is
  right; only the timing claim is off.
- **Suggested fix**: re-read the row (or at least its `deleted` flag) before the write, or
  have the soft-delete path update the queued copy under the map key; correct the residual
  wording, and either remove the dead branch or give it a production-reachable path/test.

### A-11 (Low, R-09/R-20) — cadence is a floor, not an independent clock

- **Claim**: statistics and DDL get an independent hourly cadence (`statisticsRefreshInterval`
  / `objectDefinitionFullRefreshInterval`), decoupled from the 15-minute schema sync.
- **What the code does**: both clocks are only consulted **inside**
  `SyncDatabaseSchema` (`syncer.go:859,974`), which runs only when the database is due — and
  due-ness is the instance's `sync_interval`/`LastSyncTime`. With a 24-hour interval, a
  stats/DDL refresh can be 24 hours old; with sync disabled (interval meaning “never”), it
  never refreshes. The clocks are minimum intervals, not scheduled tasks.
- **Impact**: the docs (`object-ddl-storage.md`, `runner.md` table saying “hourly per
  database”) overstate freshness; no correctness issue. If a user configures a long sync
  interval, the UI estimates and StarRocks DDL stay stale much longer than the docs claim.
- **Suggested fix**: either run stats/DDL refresh from an independent ticker over due
  databases, or document “at most once per hour, and only when the database itself syncs”.

### A-12 (Low, R-14) — the “engine changes are covered by the hourly scan” claim is false

- **Claim** (report R-14 note): the upsert's `inserted` flag means only new text is queued;
  “engine 等非文本变化仍由小时全量扫描兜底”.
- **What the code does**: the conflict branch updates `engine = EXCLUDED.engine`
  (`backend/store/application_sql.go:139-145`), but the registry mirror
  (`buildApplicationSQLStoredMetadata`, `:468-481`) contains no engine field, so an engine
  change does not move the registry `meta_hash`; `needsAnalysis` compares only hash and
  analyzer version. The hourly scan therefore cannot notice it. (In practice the engine is
  instance-level and fixed for the lifetime of the instance, so the trigger is nearly
  unreachable, but the stated fallback does not exist.)
- **Also**: `(xmax = 0) AS inserted` is the usual PostgreSQL idiom; it can produce a false
  negative in unusual `ON CONFLICT`/lock cases, in which case the new text waits for the
  hourly scan. A false positive is harmless (extra analysis). Worth a comment/test.
- **Suggested fix**: either include the engine in the registry mirror (and hash) or
  re-queue on an engine change; correct the doc claim; add a test for the `inserted` flag
  with a real conflicting upsert (currently only the query shape is asserted).

### A-13 (Low, R-29) — uniqueness does not make concurrent replacements safe

- **Claim**: the unique index is “the concurrency contract: two writers ... delete and
  insert the same rows, and DO NOTHING keeps the later insert from duplicating an edge.”
- **What the code does**: `BatchReplaceColumnLineage` (used by ingestion and revalidation)
  deletes an object's rows and re-inserts its *read* set (`column_lineage.go:80-100`);
  `ON CONFLICT DO NOTHING` prevents duplicates, but a revalidation that read the run's
  edges before a re-ingestion inserted a new edge will, if it commits second, delete the
  new edge and write the stale set. `affectedRuns`/`revalidateRuns`
  (`lineage_revalidation.go:150-185`) and the ingestion processor
  (`processor.go:103`) are two different `Processor` instances in one process (server and
  API handler), so the semaphore does not serialize them.
- **Failure scenario**: an Airflow retry re-ingests a run while a revalidation pass is
  active; the new column edge disappears until the run is ingested/revalidated again. No
  duplicate rows (the fixed R-29 symptom), but a lost edge.
- **Blast radius**: transient missing ingested lineage. This is a pre-existing race, not
  introduced by the remediation, and the report never claimed to fix it; it should be
  recorded as a residual rather than implied by “the concurrency contract”.
- **Suggested fix**: serialize ingestion and revalidation per run (share the revalidation
  semaphore or a per-run advisory lock), or give run edges a version/CAS
  (`updated_at`/hash) so a stale replace is rejected.

### A-14 (Nit, R-17 report) — missing 「已实施」 note

R-17's detail section ends at the recommendation (`report:480-489`) without an
「已实施」 paragraph, unlike R-03/R-04/R-05 etc.; §0.4 (`:92`) says R-17 was implemented in
`d186476`. Add the note (including the `AnalyzerVersion` bump discipline) so a reader of
the section is not left believing it is unimplemented.

### A-15 (Nit, R-11 report) — stale “未做” list

R-11's note (`report:430`) says “**未做**：ticker jitter、per-iteration timeout、统一首跑/
日志字段”. Jitter now exists in `Syncer.Run` (`syncer.go:319`), `Analyzer.Run`
(`analyzer.go:245`) and `maintenance.Run`; a per-scan timeout exists in
`trySyncAllOnce` (`syncer.go:455`), `queueAllBounded` (`analyzer.go:277`) and the
maintenance pass. Only “unified first-run/log fields” is still partially open. Update the
note to avoid a false residual.

### A-16 (Nit, gate) — lint does not pass at this HEAD

`golangci-lint run --allow-parallel-runners` reports 3 `revive` `multiline-if-init`
issues in `backend/migrator/migrator.go:228,277,399`. Those lines are identical at
baseline `387e1d1` (`git diff 387e1d1..HEAD -- backend/migrator/migrator.go` is empty), so
this is a pre-existing tool/config drift, not a remediation regression — but the report's
multiple claims “lint 通过” are not reproducible with the installed linter. Record the
linter version/config used, or fix/ignore the three pre-existing lines.

### A-17 (Nit, tests) — coverage gaps in the riskiest new wiring

See §5. The most important: no test exercises the production `SyncDatabaseSchema` stats
path (the unit test calls the store update directly; the integration test also calls the
store directly), no test for `enable_full_sync` presence (single or batch), no test for the
30-minute scan budget, no test for the batch budget, and no test that the production
shutdown path passes a deadline to the debug drain.

### A-18 (Nit, test pollution) — the debug-writer test closes the global writer

`TestShutdownDebugLogWorkersWithoutAnyLogger` (`debug_test.go:61-65`) calls
`ShutdownDebugLogWorkers` on the package-global `debugLoggers`. After it runs,
`NewDBDebugLogger` can never write again in that test binary (the channel is closed and
`enqueue` drops). It passes today because no other test in the package constructs a
production logger; a newly added one would be silently order-dependent. Use a local
`debugLogWriter` in the test or provide a reset-only-for-test hook.

### A-19 (Nit, doc) — `error_message` never reaches the RPC boundary

`.agents/docs/lineage-analyzer.md:176` (pre-existing wording, now more relevant because
R-26 relies on this message) says the structured diagnostic “只到 RPC 边界”; there is no
`error_message` field in `proto/v1/v1/*.proto` and no API reads `ColumnLineageVersion`
except tests. If the explanation is meant to be visible, add it to the lineage/metadata RPC;
otherwise correct the doc to say it is store-only.

---

## 3. Verified good (claims attacked and not falsified)

These are the riskiest claims I tried to break; in each case the code and tests held.

1. **R-01 instance metadata merge.** `UpdateInstance` now rejects both `Metadata` and
   `MetadataUpdates` (`store/instance.go:154-157`), locks the row with `FOR UPDATE` and
   decrypts/applies/re-encrypts (`:210-257`). All product callers use `MetadataUpdates`;
   the only whole-metadata `UpdateInstance` caller left is a store unit test
   (`store/instance_test.go:271`). `TestApplyInstanceMetadataUpdatesKeepsFieldsItDoesNotName`
   and `...KeepsTheSyncFields` assert title/activation/last_sync_time/version/case flag and
   the decrypted credential survive. API data-source create/update/delete now evaluate
   their duplicate/missing checks inside the patch closure under that lock
   (`api/v1/instance_service.go:536-562,613-639,665-689`); `patchDataSource` is validated
   on a clone first, so the closure cannot fail with a partial write. `DeleteInstance`
   patches only `activation` (`:320-328`). No lost-update path via the old clone remains.
2. **R-02 quoting / complete statement.** MySQL family (MYSQL/TIDB/MARIADB/DORIS/STARROCKS)
   backticks, POSTGRES double quotes, MSSQL brackets (`analyzer.go:724-741`);
   `isCompleteStatement` skips `--` and `/* */` and accepts `CREATE\nVIEW`
   (`:744-782`). `TestBuildSQLWrapsParseForEveryRegisteredEngine` actually parses the
   wrapper with the registered engine's analyzer; the comment/DDL cases are pinned in
   `TestBuildSQL`. The old “StarRocks bare body unquoted” assertion was corrected.
3. **R-03/R-04 CAS + atomic failure.** `ReplaceColumnLineageIfCurrent`,
   `MarkAnalyzedIfCurrent`, `MarkAnalysisFailedIfCurrent` share `lockAnalysisState`
   (`SELECT meta_hash ... FOR SHARE`) and commit edges/version/gap together
   (`store/column_lineage.go:521-612`); `ErrAnalysisGone` drops, `ErrAnalysisStale`
   re-queues (`analyzer.go:807-819,370-390`); `RecordAnalysisError` leaves `meta_hash`
   alone (`column_lineage.go:735-742`). Real-PostgreSQL integration tests for stale/gone/
   current and for atomic failure were rerun with `-v` and passed (no skips).
4. **R-29 unique edge index.** Migration 0022 dedupes by edge identity with
   `row_number()` and creates `idx_column_lineage_edge`; `LATEST.sql` has the identical
   index and `analyzer_version` column; the migrator/LATEST-chain integration test passed.
   `columnLineageInsertStatement` uses `ON CONFLICT DO NOTHING`
   (`column_lineage.go:100-107`); `TestColumnLineageInsertIsIdempotent`,
   `TestColumnLineageDeleteByMetaGUIDsShape`, and the real-PG
   `TestColumnLineageEdgeUniquenessRealServerIntegration` passed. (The lost-update residual
   is A-13.)
5. **R-19 settle fold.** `Run` discards one pending trigger after settle and before
   `runOnce`, and keeps a trigger that arrives during the pass (`validation.go:113-121`).
   The injected `settleTimer` makes `TestTriggerDuringSettleIsFolded` deterministic;
   `TestTriggerCoalescesOneSyncRound` was restructured around a held startup pass.
   `go test -race -count=50 ./backend/runner/lineagevalidation/` passed.
6. **R-21/R-22/R-23 operation and hidden-database state machine.** `AbandonOperation`
   no-ops on `done` or `sealed && instanceRecorded`, keeps a more specific
   `instanceErr`, seals and clears pending (`operation.go:184-207`); `EnqueueDatabases`
   rejects sealed operations and registers pending before enqueueing
   (`:123-163`); `RecordInstanceResult` rejects late/duplicate results (`:95-108`);
   `expireOperations` marks never-recorded operations abandoned (`:257-298`);
   `ErrDatabaseHidden` is a sentinel, the checker does not retry/escalate it, and the API
   maps it to NotFound (`syncer.go:598-601,415-422,834-838`; `api/v1/database_service.go:46-58`).
   Handler defers are registered per operation, including inside `batchSyncInstance`
   (`api/v1/instance_service.go:176,371,447`). The operation tests cover the fan-out,
   deadline renewal, abandon and failure paths.
7. **R-25 injective GUID escaping.** `%` is escaped before `;` and decoded in reverse
   (`common/guid.go:14-31`); `TestEscapeGUIDPartIsInjective` covers `%`, `%3B`, `%25`,
   mixtures and empty names with round-trip and pairwise-distinctness assertions. Build/
   split symmetry is tested, and every production parser uses `SplitMetaGUID`
   (`grep` confirmed no raw `strings.Split(guid, ...)` in product code).
8. **R-13 cancellable revalidation.** The `sync.Mutex` became a capacity-1 channel;
   `acquireRevalidation` selects on `ctx.Done()` (`plugin/openlineage/lineage_revalidation.go:
   23-35`); `NewProcessor` initializes it. `TestRevalidationWaitIsCancellable` covers both
   methods and a held semaphore.
9. **R-31 batch budget.** A 5-minute `batchSyncBudget` wraps `getInstanceMessage`,
   `SyncInstance` and `SyncAllDatabases`; budget-exhausted instances are reported as failed
   and skipped (`instance_service.go:409-430`). The budget is checked before starting each
   instance and is inherited by the 15-minute driver deadline.
10. **R-32/R-33.** `Run` triggers one startup pass for a non-nil revalidator
    (`validation.go:105-107`); `trySyncAll` re-reads the clock after the instance round
    (`syncer.go:507-513`).
11. **R-24/R-27/R-28.** `BatchSoftDeleteDatabases` is one scoped `UPDATE ... WHERE
    instance = $1 AND name = ANY($2)` with cache invalidation (`store/database.go:289-316`);
    the deletion log is after `Commit` (`syncer.go:1015-1023`); instance connection-limit
    saturation only logs Debug and is excluded from `failedInstances` notifications
    (`syncer.go:548-564`).
12. **R-09 core semantics (excluding A-03/A-11).** Stats are excluded from the
    canonical hash (`store/meta_hash.go:70-84`), read by JSONB path with the correct
    protojson names (`tableMetadata.rowCount`, `dataSize`, `indexSize`, `dataFree`,
    `sequenceMetadata.lastValue`), written with a `meta_hash` guard and no history insert
    (`store/meta_resource_statistics.go:41-137`), not added to `bmc.updates`, and
    invalidated in the cache after commit (`syncer.go:1038-1046`). The store-level real-PG
    test confirms value updated, fingerprint unchanged and history count unchanged.
13. **R-20 plan rotation.** `planDefinitionFetch` sorts by GUID, takes a rotating
    `fullDefinitionObjectLimit` window and always merges the changed objects; the cursor
    and clock advance only after a committed sync (`object_definition.go:105-180`;
    `syncer.go:1025-1030`); the plan tests cover full/full-large/change-only/no-candidates.
14. **R-30 #1/#2.** `schemasync.LineageQueue` decouples the runner from the analyzer
    (`syncer.go:51-56`); `ObjectDefinitionReader.GetObjectDefinition` takes `schema`
    (`plugin/db/driver.go:133-147`), the StarRocks implementation deliberately ignores it
    (one schema per database), and `metaObjectSchema` uses `SplitMetaGUID` (escaping-safe)
    on the 4-segment GUIDs. `cd proto && buf generate` leaves `git status` clean;
    `buf format -d` and `buf lint` pass.
15. **R-30 #7 mechanics (excluding A-02).** `enqueue` holds `RLock` and checks `closed`
    before the non-blocking send, so the close cannot race a sender; `start` is
    `sync.Once`; entries after shutdown are dropped with a warning
    (`llm/debug.go:66-116`); `TestDebugLogWriterDropsAfterShutdown` and the drain test pass.
16. **R-30 #8 mechanics (excluding A-08).** `CompareAndDelete` consumes exactly the
    selected generation; `retryAfterFailure`/`scheduleRetry` do not overwrite a newer
    request (`analyzer.go:154-227`); the tests cover requeue-during-analysis and
    stale-failure/backoff cases; there is no separate generation map to leak.
17. **R-30 #9 semantics (excluding A-04/A-13).** `FindColumnLineageWithUnknownEndpoint`
    restricts to `meta_type = OPENLINEAGE` (`column_lineage.go:203-220`); the only callers
    are the revalidator, and `affectedRuns` already skipped non-OPENLINEAGE edges, so no
    legitimate revalidation path is lost; `hasMore` is bounded by
    `maxRevalidationBatches` in both callers.
18. **R-30 #10 single-instance path.** `SyncInstanceRequest.EnableFullSync` is `*bool`
    (`generated-go/v1/instance_service.pb.go:513`), the handler treats nil as full
    (`instance_service.go:380`), the frontend defaults to true
    (`frontend/src/api/instance.ts:148`, `store/modules/instance.ts:106`), and the JSON
    generated docs were regenerated.

---

## 4. Gates actually run on this HEAD (`cb2183b`)

| Gate | Result |
| --- | --- |
| `gofmt -l backend/` | clean (no output) |
| `go build ./backend/...` | pass |
| `go test -count=1 ./backend/...` | pass (51 packages `ok`, no `FAIL`/`SKIP` in tests) |
| `go test -race -count=1 ./backend/runner/... ./backend/store/ ./backend/api/v1/` | pass |
| `golangci-lint run --allow-parallel-runners` | **3 issues** (revive `multiline-if-init`) in `backend/migrator/migrator.go:228,277,399`; pre-existing/untouched file, see A-16 |
| `go test -count=1 -tags=integration -timeout=30m ./backend/test/integration/... ./backend/migrator/` | pass (`runner` 90.08s, `migrator` 19.76s) |
| Targeted `-v` rerun of the six new PG tests | all six ran and passed, 0 skipped |
| `go test -race -count=10 ./backend/runner/...` | pass |
| `go test -race -count=50 ./backend/runner/lineagevalidation/` | pass |
| `go test -race -count=20 ./backend/component/llm/ ./backend/plugin/openlineage/` | pass |
| `go test -race -count=3 ./backend/store/ ./backend/api/v1/ ./backend/component/llm/ ./backend/plugin/openlineage/ ./backend/runner/schemasync/ ./backend/runner/lineageanalyzer/` | pass |
| `cd proto && buf generate` | `git status` stays clean |
| `buf format -d`, `buf lint` | clean |
| Frontend gates | not run: `frontend/node_modules` is absent; generated TS was reviewed manually |

No flaky or order-dependent failure was observed in the repeated runs above. The only gate
that does not pass is the pre-existing lint failure (A-16).

---

## 5. Test and documentation defects found

1. **Stats tests bypass the production wiring.** `TestMySQLMetaRegistryStatisticsRefreshKeepsHistoryRealServerIntegration`
   (`backend/test/integration/runner/schemasync_statistics_integration_test.go:22`) and
   `TestStatisticsRefreshCadence` (`backend/runner/schemasync/syncer_test.go:554`) never run
   `SyncDatabaseSchema`/`bmc.diff`; they call `BatchUpdateMetaRegistryStatisticsAt` and the
   clock helper directly. A regression where `diff()` stops populating `statsUpdates`, or
   where the sync forgets to call the read/write, would leave both tests green. The
   hermetic `TestBatchMetaCreateDiff` covers the `statsUpdates` list, so the gap is the
   wiring, not the comparison.
2. **No test for the new `enable_full_sync` default** in either `SyncInstance` or
   `BatchSyncInstances`. `ServiceEnv.SyncInstance` always sets the field explicitly
   (`backend/test/integration/env/service_env.go:530-535`), so even the integration suite
   cannot catch A-01.
3. **No test for `batchSyncBudget`**, the budget-exhausted result text, or the defer-based
   `AbandonOperation` inside `batchSyncInstance`.
4. **No test for the production debug-drain context.** The unit test supplies a 20 ms
   deadline; production supplies none (A-02). A test of `Server.Shutdown` would need a
   fake/injectable writer.
5. **T3/T4 store tests are contract tests only.** `TestReplaceColumnLineageIfCurrent...`
   and `TestMarkAnalysisFailedIfCurrent...` call the store directly; they prove the SQL
   contract but not that `analyzeObject`/the drain calls them on the right paths (though
   the analyzer unit tests cover `analysisTimedOut`, `needsAnalysis`, batch selection and
   failure handling). Real-PG tests for the wiring would be stronger.
6. **`TestDropHiddenDatabaseReportsTheHiddenOutcome` tests a state production does not
   create** (A-10): it mutates `database.Deleted` on the queued copy.
7. **Report inconsistency**: R-17 has no 「已实施」 note despite §0.4; R-11's “未做” list is
   stale (A-14/A-15).
8. **Claim vs. doc**: `.agents/docs/schemasync.md:33` and the proto comment state
   “unset = full” without excluding `BatchSyncInstances` (A-01); `.agents/docs/object-ddl-storage.md`
   and `runner.md` describe hourly/independent stats/DDL refresh although it is gated by
   the database sync (A-11); `.agents/docs/lineage-analyzer.md:176` says `error_message`
   reaches the RPC boundary although no RPC exposes it (A-19).
9. **`error_message` visibility**: R-02's “users cannot distinguish no lineage from a
   failed analysis” remains true; the remediation stores a message that nothing shows.

---

## 6. Bottom line

The remediation is real work, not a paper trail: the highest-risk findings (R-01, R-02,
R-03/R-04, R-19, R-25, R-29, R-21–R-23) are implemented with tests, and the integration and
race gates pass on this HEAD. The four Medium items are operational/semantic rather than
silent corruption; A-01 is the most user-visible and A-03 the most likely to hurt in
production. Fixing A-01, A-02 and A-03 is local and cheap; A-04/A-05 need a small design
decision (progress-aware drain; definition-unavailable vs. definition-emptied) and should
at least be recorded as accepted residuals if not fixed.

---

## 6. Disposition (Lead, 2026-10-18)

Addressed in commit `75c0125` (`fix: address the adversarial review of the runner remediation`).
Gates on that commit: gofmt clean, `go build ./backend/...`, `go test ./backend/...`,
`-race` on the touched packages, full integration suite (`./backend/test/integration/...` +
`./backend/migrator/`) twice, `buf generate` no diff; `golangci-lint` still reports only the
three pre-existing `backend/migrator/migrator.go` `multiline-if-init` findings (baseline
`387e1d1`).

| ID | Disposition | What changed |
| --- | --- | --- |
| A-01 | **Fixed** | `fullSyncRequested` helper (`api/v1/instance_service.go`) used by both `SyncInstance` and `BatchSyncInstances`; presence semantics pinned by `TestFullSyncRequested` (nil/unset = full, explicit false = discovery). |
| A-02 | **Fixed** | `server.Shutdown` wraps the debug drain in `context.WithTimeout(ctx, debugLogDrainTimeout=5s)`; the comment no longer claims the caller's ctx is bounded. |
| A-03 | **Fixed** | `ListMetaRegistryStatistics` failure only logs and records the refresh window; `BatchUpdateMetaRegistryStatistics` now opens its own transaction and runs after the metadata commit, so a stats failure cannot abort/roll back the schema sync. Integration test updated to exercise the standalone method. |
| A-04 | **Fixed** | `revalidateRuns` skips a run whose validated edge set equals the stored one (`sameLineageEdges`, tested), and `hasMore` additionally requires the batch to have changed at least one run, so a one-batch still-unknown backlog is read (and rewritten) once, not ten times. |
| A-05 | **Fixed** | `emptyDefinitionClearsLineage`: only `MANUAL_SQL`/`APPLICATION_SQL` clear edges on an empty definition; `VIEW`/`MATERIALIZED_VIEW` keep their last correct edges, record `"the definition was unavailable in this sync"`, and re-analyze when the definition becomes readable. Unit test + docs updated. `error_message` remains store-only (A-19 wording corrected). |
| A-06 | **Accepted / documented** | `AnalyzerVersion` remains a hand-bumped constant; invariant 9 and the R-17 note now state the bump discipline and no longer claim unconditional self-healing. A CI guard is not added in this pass. |
| A-07 | **Fixed** | A scan-budget or shutdown cancellation of `SyncInstance` is logged at Debug, does not enter `failedInstances`, and does not notify administrators. |
| A-08 | **Fixed** | A `queueMu` serializes `QueueAnalysis` and `queueScan`, restoring the disjoint-queue invariant; the drain keeps using generations/CompareAndDelete. |
| A-09 | **Fixed** | Batch lineage deletion restored the `(guid, type)` predicates for source/target/producer and the version table; guard test updated. |
| A-10 | **Fixed** | Dead checker `Deleted` branch and `dropHiddenDatabase` removed; the test now exercises the production soft-delete sequence (`forgetQueuedDatabase` + `completeDatabase(ErrDatabaseHidden)`); the residual window is documented as up to one database sync (≤15m), not milliseconds. |
| A-11 | **Fixed (docs)** | `object-ddl-storage.md`, `runner.md` and `schemasync.md` now say cadence is "at most once per interval, gated by the database's sync interval", not an independent clock. |
| A-12 | **Fixed (report) + accepted** | The false "engine changes covered by the hourly scan" claim is corrected in the R-14 note; the `inserted` flag keeps a shape guard but no DB-backed conflicting-upsert test (engine is instance-level, trigger practically unreachable). |
| A-13 | **Accepted residual** | Re-ingestion vs revalidation can still lose an edge for the last writer; recorded as a residual (pre-existing, not introduced by R-29; the unique index fixes duplicates only). |
| A-14 | **Fixed** | R-17 section has its 已实施 note (and the bump-discipline caveat). |
| A-15 | **Fixed** | R-11 note no longer lists jitter/timeouts as undone. |
| A-16 | **Fixed (report)** | The status paragraph records the linter's pre-existing baseline instead of claiming a clean run. |
| A-17 | **Partially fixed** | Added the unset-default test and `sameLineageEdges` test; no hermetic test exercises `SyncDatabaseSchema`'s stats path end-to-end, the scan budget, or the production shutdown ctx — these remain test gaps (the store has no fake and the path is integration-only). |
| A-18 | **Fixed** | The test that closed the process-global debug writer was removed; only per-writer instances are tested. |
| A-19 | **Fixed (docs)** | `lineage-analyzer.md` says `column_lineage_version.error_message` is store-only; structured diagnostics reach the RPC boundary only through `AnalyzeSQL`. |

**Residual risks after disposition** (also recorded in
`.agents/reviews/backend-runner-review.md` §0.4): A-06 version-discipline, A-13
ingestion/revalidation lost update, A-17's integration-only test gaps, and the
pre-existing lint baseline. None of them is a silent data-corruption path.
