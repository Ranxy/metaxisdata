# Background runners — Reference

> Status: **implemented**. Covers the common lifecycle of `backend/runner/` and the state each runner keeps in the process. Per-runner contracts live in [schemasync.md](schemasync.md) and [lineage-analyzer.md](lineage-analyzer.md); this file is the cross-runner map: startup, shutdown, in-process state, cadences and the single-replica assumption.

## The four runners

| Runner | Package | Cadence | First run | Purpose |
| --- | --- | --- | --- | --- |
| `schemasync` | `backend/runner/schemasync` | instance scan 15m, database checker 10s | immediate | enumerate instances/databases, diff and write schema metadata + DDL |
| `lineageanalyzer` | `backend/runner/lineageanalyzer` | full scan 1h, queue consumer 10s | immediate | analyze view/MV/manual/application SQL into `column_lineage` |
| `lineagevalidation` | `backend/runner/lineagevalidation` | event-driven, 5s settle | one pass at startup | revalidate ingested edges after a sync commits |
| `maintenance` | `backend/runner/maintenance` | 6h | immediate | retention prunes, token cleanup, ingested-lineage sweep |

Every runner implements `runner.Runner` (`Name()`, `Run(ctx)`) and is started by `runner.Go`, which wraps the body in a top-level `recover`: a panic is logged with its stack and takes down that runner, never the process. `backend/server/server.go` starts the slice, so adding a runner is one entry, not another hard-coded `Add`.

## Lifecycle

- **Startup**: `NewServer` wires the runners and starts them after the store and the analyzers are assembled; each `Run` returns only when the server's runner context is cancelled.
- **Shutdown**: `server.Shutdown` cancels the runner context, waits for the runner wait group with a timeout, then closes the store. A runner that cannot observe cancellation is a bug (see the cancellable revalidation semaphore in `lineagevalidation`).
- **Per-run timeouts**: every long loop that talks to a target database or the store has a bound (`syncTimeout`, `passTimeout`, `analyzeObjectTimeout`, `maintenance` pass deadline). A bound expiring is a transient failure and is retried; it must never be recorded as a deterministic analysis/sync failure.
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

Consequences worth knowing before changing this area:

- **No cross-process coordination is attempted** (no advisory lock, no lease): running two replicas would duplicate syncs and analyses, split operation messages between processes and double the target database load. Scale the database, not the server.
- **A restart loses queued work but converges**: the instance scan re-enqueues databases, the hourly analysis scan re-queues stale objects, and startup refreshes statistics and DDL.
- **Change detection still holds**: metadata fingerprint + `AnalyzerVersion` decide re-analysis; statistics and DDL refreshes are deliberately outside the fingerprint.

## Derived-data and cache cadences

| Data | Written by | Cadence / trigger | History? | Fingerprint / re-analysis? |
| --- | --- | --- | --- | --- |
| schema metadata | `schemasync` diff | every database sync (10s checker + 15m scan) | one row per change | is the fingerprint |
| object DDL | `schemasync` | changed objects every sync, full refresh hourly per database, rotating batch above 2000 objects | no (derived) | no |
| table statistics / sequence last value | `schemasync` | hourly per database | **no** | no |
| column lineage | `lineageanalyzer` | event queue first, then hourly scan backlog; bounded batch per tick | no (replaced per object) | `meta_hash` + `AnalyzerVersion` |
| ingested lineage revalidation | `lineagevalidation` / `maintenance` | after a sync commit / 6h | no (replaces edges) | n/a |

## Where things live

| Concern | Code | Tests |
| --- | --- | --- |
| runner interface + panic boundary | `backend/runner/runner.go` | `backend/runner/runner_test.go` |
| startup/shutdown wiring | `backend/server/server.go` | integration suites |
| sync queue/gates/operations | `backend/runner/schemasync/{syncer,operation}.go` | `syncer_test.go`, `operation_test.go`, MySQL integration |
| analysis queue/cadence | `backend/runner/lineageanalyzer/analyzer.go` | `analyzer_test.go`, `backend/test/integration/runner/schemasync_lineage_mysql_service_test.go` |
| validation trigger/settle | `backend/runner/lineagevalidation/validation.go` | `validation_test.go` |
| maintenance pass | `backend/runner/maintenance/maintenance.go` | `maintenance_test.go` |

## Known accepted gaps

- **No ticker jitter**: every runner starts immediately and ticks on a fixed interval, so a restart runs all first passes at once. Accepted for a single-replica self-hosted deployment; add jitter only if startup load becomes a problem.
- **No runner metrics**: failure/queue-depth counters are not collected (D8 in [backend-open-items.md](backend-open-items.md)); each pass logs its own summary line (maintenance) or per-item outcome.
- **Per-iteration timeout is per unit, not per whole pass**, except where a pass deadline exists (`lineagevalidation`, `maintenance`). A batch of pathological objects can therefore take longer than one interval; the next tick is skipped rather than overlapping.
