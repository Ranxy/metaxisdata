# Background runners — Reference

> Cross-runner map of `backend/runner/`: startup and shutdown, in-process state, cadences and the single-replica assumption. Per-runner contracts live in [schemasync.md](schemasync.md), [lineage-analyzer.md](lineage-analyzer.md) and [data-quality.md](data-quality.md); the notification pipeline in [notifications.md](notifications.md).

## The ten runners

| Runner | Cadence | First run | Purpose |
| --- | --- | --- | --- |
| `schemasync` | instance scan 15m, database checker 10s | immediate | Enumerate instances/databases, diff schema, and write metadata plus DDL |
| `lineageanalyzer` | full scan 1h, queue consumer 10s | immediate | Analyze view, materialized-view, manual and application SQL into `column_lineage` |
| `lineagevalidation` | event-driven, 5s settle | one pass at startup | Revalidate ingested edges after a sync commits |
| `maintenance` | 6h | immediate | Retention prunes, token cleanup, ingested-lineage sweep |
| `qualityrunner` | due scan 10s, manual queue priority | first tick | Plan one batch per due target and execute it under the instance's connection budget; a target's timer makes all of its enabled checks due together, the scan only plans, and the schedule advances when the cycle settles. Contracts and the per-tick bounds are in [data-quality.md](data-quality.md) |
| `notificationdispatcher` | due-claim scan 2s | first tick | Lease due deliveries, render per the channel's language and POST them; backoff 1m/5m/15m/1h/6h, dead after 6 attempts. A batch whose channel list cannot be read is released, never marked dead ([notifications.md](notifications.md)) |
| `qualityagentrunner` | 60s | one pass immediately | Write each ACTIVE agent's scheduled patrol report from its most recent due schedule, deterministically, with no model call |
| `qualitydigestrunner` | 60s | one pass immediately | Enqueue the workspace weekly digest for subscribed channels; take the latest scheduled instant within a 24h catch-up window and record the period so a duplicate tick or a restart cannot enqueue the same week twice |
| `qualitysessionrunner` | due-session scan 3s | first tick | Host every active agent's target sessions and front-office conversation; claim due wakes (15m lease, 10m wake deadline) into a bounded worker pool and schedule the next wake. Durable truth is the session row, not the process |
| `qualityenrollmentrunner` | queue tick 60s, database scan 15m | first tick | Turn each ACTIVE agent's database scope into its target queue, reconcile scope changes and admit a bounded number of sessions per hour |

The five quality runners delegate their execution, scheduling, adoption and coverage detail to [data-quality.md](data-quality.md); the table above keeps only cadence and process state.

Every runner implements `runner.Runner` (`Name()`, `Run(ctx)`) and is started by `runner.Go`, which adds a panic boundary and logs start/stop with name and duration: a panic takes down that runner only, never the process. `backend/server/server.go` starts the slice, so adding a runner is one entry, not another hard-coded `Add`.

## Lifecycle

- **Startup**: `NewServer` wires the runners and starts them after the store and analyzers are assembled; each `Run` returns only when the server's runner context is cancelled.
- **Shutdown**: `server.Shutdown` cancels the runner context, waits for the wait group with a timeout, then closes the store. A runner that cannot observe cancellation is a bug (see the cancellable revalidation semaphore in `lineagevalidation`).
- **Per-run timeouts**: every long loop that talks to a target database or the store has a bound — `syncTimeout` per driver call, twice the scan interval for one instance scan pass, `analyzeObjectTimeout` per object, 5 minutes for one full-analysis queue scan, `passTimeout` for validation/maintenance passes. A bound expiring is a transient failure and is retried; it must never be recorded as a deterministic analysis or sync failure.
- **Jitter**: every periodic ticker that runs a full pass (`schemasync` instance scan, `lineageanalyzer` full scan, `maintenance`) uses `runner.Jitter` (0–10% added) so a restart does not keep its passes aligned with other replicas' or its own previous cadence. The 10s schema-sync checker and the 10s analysis consumer are deliberately not jittered: they are cheap tick checks and jitter would only add latency.
- **Panic boundary**: the shared wrapper recovers the runner body; individual write paths (one database sync, one object analysis, one maintenance item) recover on their own so one bad unit does not lose the batch.

## State kept in the process

All coordination state is in memory, and the deployment runs **one server process** (see [docs/security-posture.md](../../docs/security-posture.md)):

| State | Owner | Lost on restart |
| --- | --- | --- |
| database sync queue, retry backoff, sync gates | `schemasync` | queued databases are re-discovered by the next instance scan; in-flight syncs are gone and the next scan retries |
| `SyncOperation` (user-visible sync results) | `schemasync` | a message for an operation in flight is not delivered |
| analysis event queue, scan backlog, retry backoff | `lineageanalyzer` | re-derived by the hourly scan |
| revalidation trigger, settle window, semaphore | `lineagevalidation` | a startup pass replaces it |
| table-statistics refresh clock, DDL refresh clock and rotation cursor | `schemasync` | a restart makes every database due for a refresh (intended first pass) |
| maintenance schedule | `maintenance` | the next tick is 6h away; startup runs one pass |
| data quality due scan, manual batch queue, in-flight rule ids, daily LLM counters | `qualityrunner` (quota counters live in `api/v1`) | targets re-converge from `quality_target_schedule.next_run_at`; abandoned attempts retry within the attempt budget; queued manual batches are lost and their executions are swept to ABANDONED at startup; LLM counters reset |
| last digest period and startup pass | `qualitydigestrunner` | a restart re-reads at most one period and may repeat the stale-period warning; the period table is the real idempotency guard |
| per-instance connection leases and wait queues | `state.State` (`instanceConnections`, shared by every driver-opening path) | held leases vanish; a waiting batch observes cancellation and gives up, and the next tick retries ([instance-connections.md](instance-connections.md)) |
| in-flight wake workers and the process last-tick | `qualitysessionrunner` | the session row (lease, wake count, tokens, summary watermark, transcript) is the durable truth; a restart clears stale leases and recovers sessions |
| per-database scan clock, last enrollment scan, scope fingerprint | `qualityenrollmentrunner` | every database is re-scanned once after a restart; the queue itself is durable, and a newly added database is scanned immediately because it has no clock entry |
| data quality agent LLM metrics | `component/llm` (features `dq_agent`/`dq_agent_compaction`) | call/duration/token counters are process metrics and reset |

Consequences worth knowing before changing this area:

- **No cross-process coordination is attempted** (no advisory lock, no lease): two replicas would duplicate syncs and analyses, split operation messages between processes and double the target database load. Scale the database, not the server.
- **A restart loses queued work but converges**: the instance scan re-enqueues databases, the hourly analysis scan re-queues stale objects, and startup refreshes statistics and DDL.
- **Change detection still holds**: metadata fingerprint plus `AnalyzerVersion` decide re-analysis; statistics and DDL refreshes are deliberately outside the fingerprint.
- **Capacity contention is waited out, never recorded as a failure**: a scheduled run that cannot lease its instance's connection budget leaves its rules due, and a manual run that waits too long is abandoned; neither writes a rule result. Budgets live in [instance-connections.md](instance-connections.md).

## Derived-data and cache cadences

| Data | Written by | Cadence / trigger | History? | Fingerprint / re-analysis? |
| --- | --- | --- | --- | --- |
| schema metadata | `schemasync` diff | every database sync (10s checker + 15m scan) | one row per change | is the fingerprint |
| object DDL | `schemasync` | changed objects every sync; a full refresh at most hourly per database and gated by that database's sync interval, rotating in batches above 2000 objects | no (derived) | no |
| table statistics / sequence last value | `schemasync` | at most hourly per database, gated by the database's sync interval; best effort (a failure is logged, never fails the sync) | **no** | no |
| column lineage | `lineageanalyzer` | event queue first, then hourly scan backlog; bounded batch per tick | no (replaced per object) | `meta_hash` + `AnalyzerVersion` |
| ingested lineage revalidation | `lineagevalidation` / `maintenance` | after a sync commit / 6h; one pass loops while the revalidator reports `hasMore`, bounded by the pass timeout | no (replaces edges) | n/a |

## Where things live

- Runner interface, panic boundary and jitter: `backend/runner/runner.go`; startup/shutdown wiring in `backend/server/server.go` (integration suites cover it).
- `schemasync`: `backend/runner/schemasync/` (`syncer`, `gate`, `queue`, `instance_sync`, `database_sync`, `metadata_batch`, `refresh`, `operation`); `lineageanalyzer`: `backend/runner/lineageanalyzer/`; `lineagevalidation`: `validation.go`; `maintenance`: `maintenance.go`.
- Quality runners: `backend/runner/qualityrunner/`, `qualitysessionrunner/`, `qualityenrollmentrunner/`, `qualityagentrunner/`, `qualitydigestrunner/`; each package has its own unit test, and `backend/test/integration/runner/` holds the real-server journeys (including the digest runner and the quality journey).
- Per-instance connection budget: `backend/component/state/connection.go`; notification dispatcher: `backend/runner/notificationdispatcher/`; runner metrics in `backend/metrics/` ([metrics.md](metrics.md)).

## Known accepted gaps

- **A dead runner is visible but not restarted**: `runner.Go` reports `metaxisdata_runner_up` and `metaxisdata_runner_panics_total`, and the sync and analysis runners expose their own outcome and duration counters. The enrollment runner has no dedicated counters; the enrollment read RPCs were removed with the external agent surface, so its queue is observable only through store rows and runner logs. A panicking runner still is not restarted — its `up` gauge drops to 0 while the process keeps serving.
- **Agent transcripts and reports are not pruned**: `quality_agent_message` (and its compaction checkpoints) and `quality_agent_report` are kept indefinitely by design; only the model context is compacted. Add a retention pass before enabling this on a workspace with a very large target catalog — automatic enrollment makes this a prerequisite at 10k-object scope, because triage creates one session per admitted target.
- **Per-iteration timeout is per unit or per pass, not per runner lifetime**: a batch of pathological objects can outlast one interval; the next tick is then skipped rather than overlapped.
