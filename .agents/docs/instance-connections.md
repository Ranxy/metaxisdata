# Instance connection scheduling

Maintenance reference for `backend/component/state/connection.go` and every caller that opens a target database driver: `backend/runner/schemasync`, `backend/runner/qualityrunner`, and the connectivity test, rule dry-run probe and column profiling in `backend/api/v1`. Related: [data-quality.md](data-quality.md) (the internal-batch admission consumer), [schemasync.md](schemasync.md), [runner.md](runner.md) (single replica and process-local state), [rate-limiting.md](rate-limiting.md) (request budgets, a separate mechanism). Rules: [backend/AGENTS.md](../../backend/AGENTS.md).

## What it is

`maximum_connections` (an instance metadata field, defaulting to `common.DefaultInstanceMaximumConnections` = 10) is **an instance's concurrent-connection budget**. Every path that opens a target database driver takes a lease from the governor first and releases it when the driver closes.

A lease is **a window in which the holder occupies part of the instance's connection budget**: the holder opens a driver, runs statements sequentially and closes the driver. For an ordinary caller one lease is one session; a data-quality internal batch's lease covers its whole query phase (the batch executes serially, so at most one session at a time) and is released before problem migration and notification. The budget therefore bounds concurrently open target sessions, not the physical connections inside a driver pool — provided the holder never uses its driver concurrently (invariant I-2).

The governor is not a counter but **a fair, priority-ordered, blocking queue**: a request that cannot be admitted waits, and a release hands the slot directly to the head of the queue instead of returning an immediate error.

## Why (this is not a different error code)

The same normal background contention must not be interpreted as a result by its callers. A try-lock (`AcquireInstanceConnection` returning `ErrInstanceConnectionLimit` at once) makes three callers read the same condition three different ways:

| Caller | What a try-lock makes of contention | Problem |
| --- | --- | --- |
| schemasync | Requeue the database and retry next tick | Semantically fine, but each tick starts up to 100 goroutines for 10 slots, and most simply fail and requeue. |
| Connectivity test | `ResourceExhausted` | Acceptable, but with no wait. |
| qualityrunner | **One `ERROR(QUOTA)` result per rule plus an execution-failure notification to administrators** | Records contention as a rule failure; the target's `next_run_at` has already advanced, so the check is lost. |

Contention is structural: the database checker starts a goroutine every 10 seconds for each database that is due (globally capped at 100), all trying to take the instance's 10 slots; scheduled and manual data-quality batches and UI test connections compete for the same budget. Without a wait queue the losers can only report an error.

The conclusion: **capacity contention must appear as waiting or deferral, not as an outcome.** That is the governor's only reason to exist.

## Contract

### API

| API | Behavior |
| --- | --- |
| `AcquireInstanceConnection(ctx, InstanceConnectionRequest)` | Admit immediately, or queue by priority; returns `ErrInstanceConnectionLimit` when ctx ends (wrapping ctx's cause). The returned release is idempotent. |
| `TryAcquireInstanceConnection(request)` | Succeeds only when it can be admitted immediately **and** there is no queued waiter for the instance; for callers that can retry later, and it never jumps the queue. |

`InstanceConnectionRequest.MaximumConnections <= 0` means the instance sets no ceiling: the request is admitted at once but still counted for diagnostics. A caller resolves `MaximumConnections` from the instance row or the deployment default (see each caller).

### Priority and fairness

| Priority | Used by |
| --- | --- |
| `ConnectionPriorityInteractive` | API actions where a user waits for the result: test connection, rule dry-run probe, column profile. |
| `ConnectionPriorityManual` | A user-triggered manual data-quality batch that the user does not wait on. |
| `ConnectionPriorityBackground` | Periodic work: the schema syncer's database sync and scheduled data-quality batches. |

- A released slot goes to the highest priority waiter, FIFO within a priority.
- `TryAcquire` always fails while the instance has any waiter: a background bulk retry must not starve a request that is already waiting.
- A free slot is handed directly to the head of the queue, with no thundering herd that wakes every waiter to race.

### Invariants

| # | Invariant | Guard |
| --- | --- | --- |
| I-1 | Every target driver open goes through the governor: schemasync's instance and database syncs, the connectivity test, the data-quality internal batch, the rule dry-run probe and column profiling. If a rule's target moves to another instance after admission, `ruleConnectionLease` takes a lease for the actual instance (a single try; on failure the rule is `SKIPPED`, never blocking on another instance while holding the batch's lease), and no driver is ever opened outside the batch lease. | Each caller's `acquire*` is the only entry point; `TestRuleConnectionLeaseCoversARetargetedRule` |
| I-2 | A lease holder never uses its driver concurrently (one query per data-quality execution, one sync session per database), so the budget is approximately the concurrent physical-connection ceiling. | Code structure; every new caller must honor it |
| I-3 | Release is idempotent; the count never goes negative; a canceled waiter leaves the queue without holding a slot; a waiter granted a slot and then canceled hands it back. | `backend/component/state/connection_test.go` |
| I-4 | A waiter outranks a try-acquirer; `TryAcquire` never overtakes a waiter. | `connection_test.go` |
| I-5 | The governor's state is process-local, under the single-replica assumption (see [runner.md](runner.md)). | Code structure |

## Callers and wait budgets

| Caller | API | Priority | Wait budget | When it cannot get a slot |
| --- | --- | --- | --- | --- |
| schemasync database checker's database sync | `TryAcquire` | Background | 0 | The database stays queued and retries next tick; it is not a failure and does not advance the backoff. |
| Scheduled data-quality batch | `Acquire` | Background | 2m (`scheduledAdmissionWait`) | Neither the batch nor any execution row is created, no result is written and no notification is sent; the target stays due, as do the instance's remaining due targets, until the next tick. |
| Manual data-quality batch | `Acquire` | Manual | 5m (`manualAdmissionWait`) | The already-planned PENDING executions (and the internal batch) are marked `ABANDONED` and logged. |
| Connectivity test | `Acquire` | Interactive | 5s (`dataSourceConnectionWait`) | `ResourceExhausted`: the instance is busy, retry shortly. |
| Rule dry-run probe (the `LIMIT 0` of `PreviewQualityRule`) | `Acquire` | Interactive | 3s (`qualityPreviewConnectionWait`) | `ResourceExhausted`: retry the dry run later. |
| Column profile (`ProfileColumns`) | `Acquire` | Interactive | 10s (`qualityProfileConnectionWait`) | `ResourceExhausted`: retry the profile later. |

### Why data quality leases per batch

A batch's executions run serially against the same instance, so at most one session exists at a time and the batch needs a single lease. Taking it before the batch and its executions are planned means no admission failure can occur mid-batch, so `QUOTA` can never become an execution result. Consequences:

- Batches against the same instance are naturally serial; `maxConcurrentInstanceBatches` (2) only limits different instances.
- A scheduled batch that cannot get a lease leaves no rows at all: no batch, no planned executions, and the target's `next_run_at` has not advanced; the remaining due targets of the same instance are also left for the next tick, so each target does not wait its own 2 minutes.
- The lease covers the query phase and every execution's terminal write; it is released as soon as the execution loop ends, and problem migration and notification hold no slot.
- When a rule is retargeted to another instance after admission, `ruleConnectionLease` tries the actual instance once: on failure it records `SKIPPED` and runs on the next schedule, rather than blocking while holding the current batch's lease and letting two concurrent batches wait on each other's instances.
- A manual batch's admission wait happens in the runner's main loop (batches already execute serially), so due scanning pauses while it waits; that is the trade-off for "user requests first, simple main loop".
- A metadata-only rule (`schema_drift`) does not query the target, but other executions in the same batch may, so the batch still holds one slot; that is a deliberate simplification, occupying at most 1/N of the instance's budget for one batch's duration. If the budget stays saturated, a manual batch containing only metadata rules can still be marked `ABANDONED` after 5 minutes (a scheduled batch is merely deferred).

### Known gap: API-triggered instance and single-database sync

`SyncInstance` and `SyncDatabase` execute synchronously in the request goroutine; when no slot is available they still return an error and write a persistent operation failure notification (the operation's ERROR message). Making this path queue requires making the API sync asynchronous ([schemasync.md](schemasync.md)'s known problem 1 records that a request can hang for 15 minutes); that is out of scope here. What this reference guarantees is the database checker's queued path, data-quality batches, and the read-only API queries wired through this file.

## Why there is no persistent queue

- The deployment is a single replica ([runner.md](runner.md)). The `quality_execution` rows themselves are the durable record of a manual run: they are `PENDING` while queued and visible on the executions page. The `quality_run` row is the dispatch batch the runner reports on, not a queue; the manual queue is in-process, capacity 512. After a restart, the startup sweep marks `PENDING`/`RUNNING` executions and batches older than 15 minutes `ABANDONED`; a scheduled check is retried within its cycle's attempt budget on a later tick, and a manual execution is not retried.
- The scheduled batch's "queue" is the target's `quality_target_schedule.next_run_at`: when it cannot get a lease the target stays due, and the next 10-second tick retries naturally.
- Multiple replicas would need a database lease or a cross-process queue, which is an explicit non-goal.

## Failure modes

| Scenario | Behavior |
| --- | --- |
| An instance's budget is full with 10 database syncs and a scheduled data-quality batch becomes due | The batch queues (up to 2 minutes); if a slot frees it starts immediately, otherwise it gives up for this tick and the target stays due, with the instance's other targets too. |
| Same, but the user triggers it manually | The execution rows queue as `PENDING` (up to 5 minutes); on timeout the executions and the batch are marked `ABANDONED`, visible on the executions page. |
| The user clicks "test connection" while the budget is full | Queue up to 5 seconds; if still no slot, `ResourceExhausted` and no unmetered connection is opened. |
| The service shuts down during a wait | ctx is canceled and the waiter leaves the queue; already-planned PENDING manual executions and the batch are closed by `abandonUnadmittedBatch` with a detached ctx. If the process is killed, leftover PENDING executions are swept only by a later startup, once they are older than the 15-minute cutoff (chosen so a fresh start does not sweep rows the API just created). |
| Release is called twice | The second call is a no-op (`sync.Once`) and does not release someone else's slot. |
| The instance was deleted or has no metadata | The rule resolves to `SKIPPED(ORPHANED)`, no connection is opened, and data quality takes no lease for it. |
| Many waiters | Served by priority and FIFO; background `TryAcquire` yields and cannot starve them. |

## Tests and gate

- `backend/component/state/connection_test.go`: shared budget, blocking wait, wait budgets, cancellation and dequeuing, priority/FIFO, `TryAcquire` not jumping the queue, idempotent release, no ceiling, and a canceled ctx holding no slot.
- `backend/runner/qualityrunner/qualityrunner_test.go`: batch-level lease holding and waiting, deferral while the budget stays saturated, no lease for a missing instance, and a non-positive ceiling falling back to the deployment default.
- `backend/runner/schemasync/syncer_test.go`: `TryAcquire` semantics and `errInstanceConnectionsExhausted`.
- Gate: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./backend/...` (hermetic).

## Where things live

| Surface | Location |
| --- | --- |
| Governor, priority, queue | `backend/component/state/connection.go` |
| State ownership and construction | `backend/component/state/state.go` (`State.instanceConnections`) |
| Data-quality batch admission | `backend/runner/qualityrunner/admission.go` (`admitBatch`, `instanceForAdmission`, `instanceConnectionLimit`, `abandonUnadmittedBatch`, `abandonBatch`) |
| Data-quality rule-level fallback | same file (`ruleConnectionLease`: a lease for the actual instance when a rule was retargeted after admission) |
| schemasync admission | `backend/runner/schemasync/instance_sync.go` (`acquireInstanceConnection` → `TryAcquire`) |
| Connectivity-test admission | `backend/api/v1/instance_service.go` (`pingDataSource`) |
| Dry-run/profile admission | `backend/api/v1/data_quality_service.go` (`acquireQualityConnection`, used by `probeQualityRule` and `qualityReadOnlyQueryer`) |
| Tests | `backend/component/state/connection_test.go`, `backend/runner/qualityrunner/qualityrunner_test.go`, `backend/runner/schemasync/syncer_test.go` |
