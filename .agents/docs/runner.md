# Background runners — Reference

> Status: **implemented**. Covers the common lifecycle of `backend/runner/` and the state each runner keeps in the process. Per-runner contracts live in [schemasync.md](schemasync.md) and [lineage-analyzer.md](lineage-analyzer.md); this file is the cross-runner map: startup, shutdown, in-process state, cadences and the single-replica assumption.

## The six runners

| Runner | Package | Cadence | First run | Purpose |
| --- | --- | --- | --- | --- |
| `schemasync` | `backend/runner/schemasync` | instance scan 15m, database checker 10s | immediate | enumerate instances/databases, diff and write schema metadata + DDL |
| `lineageanalyzer` | `backend/runner/lineageanalyzer` | full scan 1h, queue consumer 10s | immediate | analyze view/MV/manual/application SQL into `column_lineage` |
| `lineagevalidation` | `backend/runner/lineagevalidation` | event-driven, 5s settle | one pass at startup | revalidate ingested edges after a sync commits |
| `maintenance` | `backend/runner/maintenance` | 6h | immediate | retention prunes, token cleanup, ingested-lineage sweep |
| `qualityrunner` | `backend/runner/qualityrunner` | due scan 10s, manual queue priority | first tick | schedule data quality checks **per target**: a target's one timer (`quality_target_schedule.next_run_at`) makes all of its enabled checks due together, so each due target plans every enabled check as one execution and they run back to back; plans are grouped into internal per-instance dispatch batches (100-rule cap aligned with the per-table rule limit) and only a >100-rule target can span batches. The runner drives the problem ledger and notifications, and each batch leases the instance's connection budget before it starts (see [data-quality.md](data-quality.md), [instance-connections.md](instance-connections.md)) |
| `qualitysuggestionrunner` | `backend/runner/qualitysuggestionrunner` | pending-job scan 5s | startup sweep, then drains pending immediately | claim a durable AI rule-drafting job (`quality_suggestion_job`, 1–20 targets), resolve the provider configuration once per job, draft one target at a time and persist per-target progress; drafts land in the review queue (see [data-quality.md](data-quality.md)) |

Every runner implements `runner.Runner` (`Name()`, `Run(ctx)`) and is started by `runner.Go`, which wraps the body in a top-level `recover` and logs the runner's start and stop (name + run duration): a panic is logged with its stack and takes down that runner, never the process. `backend/server/server.go` starts the slice, so adding a runner is one entry, not another hard-coded `Add`.

## Lifecycle

- **Startup**: `NewServer` wires the runners and starts them after the store and the analyzers are assembled; each `Run` returns only when the server's runner context is cancelled.
- **Shutdown**: `server.Shutdown` cancels the runner context, waits for the runner wait group with a timeout, then closes the store. A runner that cannot observe cancellation is a bug (see the cancellable revalidation semaphore in `lineagevalidation`).
- **Per-run timeouts**: every long loop that talks to a target database or the store has a bound — `syncTimeout` per driver call, 2× the scan interval for one instance scan pass, `analyzeObjectTimeout` per object, 5 minutes for one full-analysis queue scan, `passTimeout` for validation/maintenance passes. A bound expiring is a transient failure and is retried; it must never be recorded as a deterministic analysis/sync failure.
- **Jitter**: every periodic ticker that runs a full pass (`schemasync` instance scan, `lineageanalyzer` full scan, `maintenance`) uses `runner.Jitter` (0–10% added) so a restart does not keep its passes aligned with other replicas' or its own previous cadence. The 10s schema-sync checker and the 10s analysis consumer are deliberately not jittered: they are cheap tick checks, and jitter would only add latency.
- **Panic boundary**: the shared wrapper recovers the runner body; individual write paths (one database sync, one object analysis, one maintenance item) have their own recover/isolation so one bad unit does not lose the batch.

## State kept in the process

All coordination state is in memory, and the deployment runs **one server process** (see [docs/security-posture.md](../../docs/security-posture.md)):

| State | Owner | Lost on restart |
| --- | --- | --- |
| database sync queue + retry backoff | `schemasync` | queued databases are re-discovered by the next instance scan |
| database/instance sync gates | `schemasync` | in-flight syncs are simply gone; the next scan retries |
| `SyncOperation` (user-visible sync results) | `schemasync` | a message for an operation in flight is not delivered |
| analysis event queue + scan backlog + retry backoff | `lineageanalyzer` | re-derived by the hourly scan |
| revalidation trigger + settle window + semaphore | `lineagevalidation` | a startup pass replaces it |
| table-statistics refresh clock, DDL refresh clock + rotation cursor | `schemasync` | a restart makes every database due for a refresh (intended first pass) |
| maintenance schedule | `maintenance` | the next tick is 6h away; startup runs one pass |
| data quality due scan, manual batch queue, in-flight rule ids, daily LLM counters | `qualityrunner` (quota counters live in `api/v1`) | targets re-converge from `quality_target_schedule.next_run_at`; queued manual batches are lost (their planned executions and batches are swept to ABANDONED at startup); LLM counters reset |
| claimed suggestion job + per-target progress snapshot | `qualitysuggestionrunner` | the startup sweep fails every RUNNING job the previous process left; suggestions already queued stay in the review queue |
| per-instance connection leases and their wait queues | `state.State.instanceConnections` (shared by every driver-opening path) | held leases vanish; a waiting batch observes its context and gives up, and the next scheduled tick retries. See [instance-connections.md](instance-connections.md) |

Consequences worth knowing before changing this area:

- **No cross-process coordination is attempted** (no advisory lock, no lease): running two replicas would duplicate syncs and analyses, split operation messages between processes and double the target database load. Scale the database, not the server.
- **A restart loses queued work but converges**: the instance scan re-enqueues databases, the hourly analysis scan re-queues stale objects, and startup refreshes statistics and DDL.
- **Change detection still holds**: metadata fingerprint + `AnalyzerVersion` decide re-analysis; statistics and DDL refreshes are deliberately outside the fingerprint.
- **Capacity contention is waited out, never recorded as a failure**: a scheduled run that cannot lease its instance's connection budget leaves its rules due, and a manual run that waits too long is abandoned; neither writes a rule result. The queue and its budgets are in [instance-connections.md](instance-connections.md).

## Derived-data and cache cadences

| Data | Written by | Cadence / trigger | History? | Fingerprint / re-analysis? |
| --- | --- | --- | --- | --- |
| schema metadata | `schemasync` diff | every database sync (10s checker + 15m scan) | one row per change | is the fingerprint |
| object DDL | `schemasync` | changed objects every sync, full refresh at most hourly per database (gated by the database's sync interval), rotating batch above 2000 objects | no (derived) | no |
| table statistics / sequence last value | `schemasync` | at most hourly per database, gated by that database's sync interval; best effort (a failure is logged, never fails the sync) | **no** | no |
| column lineage | `lineageanalyzer` | event queue first, then hourly scan backlog; bounded batch per tick | no (replaced per object) | `meta_hash` + `AnalyzerVersion` |
| ingested lineage revalidation | `lineagevalidation` / `maintenance` | after a sync commit / 6h; one pass loops while the revalidator reports `hasMore`, bounded by the pass timeout | no (replaces edges) | n/a |

## Where things live

| Concern | Code | Tests |
| --- | --- | --- |
| runner interface + panic boundary | `backend/runner/runner.go` | `backend/runner/runner_test.go` |
| startup/shutdown wiring | `backend/server/server.go` | integration suites |
| sync queue/gates/operations | `backend/runner/schemasync/{syncer,operation}.go` | `syncer_test.go`, `operation_test.go`, MySQL integration |
| analysis queue/cadence | `backend/runner/lineageanalyzer/analyzer.go` | `analyzer_test.go`, `backend/test/integration/runner/schemasync_lineage_mysql_service_test.go` |
| validation trigger/settle | `backend/runner/lineagevalidation/validation.go` | `validation_test.go` |
| maintenance pass | `backend/runner/maintenance/maintenance.go` | `maintenance_test.go` |
| per-instance connection budget | `backend/component/state/connection.go` | `backend/component/state/connection_test.go` |

## Known accepted gaps

- **Runner metrics exist, and a dead runner is visible**: `runner.Go` reports `metaxisdata_runner_up` and `metaxisdata_runner_panics_total`, and each runner exposes its own success/queue/pass counters (see [metrics.md](metrics.md)). A panicking runner still is not restarted, but its `up` gauge drops to 0 while the process keeps serving.
- **Per-iteration timeout is per unit or per pass, not per runner lifetime** (`lineagevalidation`, `maintenance` and one analysis scan have pass bounds; a database sync has its driver deadline). A batch of pathological objects can therefore take longer than one interval; the next tick is skipped rather than overlapping.
