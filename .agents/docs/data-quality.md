# Data Quality — Reference

Data quality answers one question deterministically: is this data acceptable? A rule compiles to SQL — or to an in-process evaluation — over a table, view, external table or materialized view; every execution records a six-way verdict, a bounded failure sample and, when meaningful, a bounded sample of passing rows. A per-rule issue ledger turns persistent failure into a problem, and notifications carry it to the inbox and to outbound channels. Profiles, semantic labels, the suggestion queue, coverage and score read models, streamed exports, a single-target report, a weekly digest, contract and plan projections and the quality-agent runtime sit on top. Three LLM analyses draft rules, explain failures and suggest labels; none of them judges.

The unit of judgment is `QualityExecution`: one rule checked once. `quality_run` is an internal dispatch batch belonging to one target, exposed read-only. Scheduling is per target (`quality_target_schedule`); rules carry no timer. Everything stateful — in-flight dedupe, admission queues, LLM counters, schedule settlement — is process-local, so one replica.

Operator docs: [docs/data-quality.md](../../docs/data-quality.md). Accepted decisions that look like bugs: [docs/security-posture.md](../../docs/security-posture.md). Sibling references: [notifications.md](notifications.md), [quality-agent.md](quality-agent.md), [schema-fingerprint.md](schema-fingerprint.md), [instance-connections.md](instance-connections.md), [iam-and-permissions.md](iam-and-permissions.md), [runner.md](runner.md).

## Data model

The tables are `quality_*` (`dataQuality` names the protos, service and handlers). `backend/migrator/migration/LATEST.sql` is the shape; the part the DDL cannot show is identity:

- A target is `(registry GUID, MetaType)`. The path snapshot stored on a rule is display-only and is never resolved back.
- `quality_execution` has no `resource_id`, so its resource name ends in the row id. Other entities use random, non-recyclable ids; the internal batch has no resource name.
- `parent` accepts `workspaces/-`; the workspace segment must match the deployment or the call is `NotFound`.

## Judgment

`PASS`/`WARN`/`FAIL` are verdicts. `ERROR` reached no verdict (SQL, connection, permission, timeout, compile) and is a monitoring fault, never merged with `FAIL`. `SKIPPED` was deliberately not run (orphaned target, archived rule, the same rule already in flight). `NOT_EVALUATED` ran and had nothing to judge (no rows, NULL aggregate, no baseline).

- Rollups rank `ERROR > FAIL > WARN > NOT_EVALUATED > SKIPPED > PASS` — deliberately not the enum's numeric order.
- Only `on_empty` decides no-data semantics, and the decision is snapshotted on the execution, so a later config edit cannot reinterpret an old verdict. Threshold comparison is inclusive.
- `ABANDONED` (restart sweep, admission or batch timeout, failed terminal write) never writes a synthetic `ERROR`: no verdict is better than a fake one.
- Failure samples are the failing rows themselves, primary-key columns first so two failing rows on one column stay distinguishable; `NULL` and empty string are distinct. Over-limit values and results are flagged `truncated` — never a failure.

Issues follow the verdict, never the reverse (`PlanIssueTransition`):

| Execution | Open issue | Result |
| --- | --- | --- |
| `FAIL` | none | open and notify |
| `FAIL` | yes | touch; notifications dedupe for 1h |
| `PASS` after `FAIL` | yes | resolve as auto-recovered |
| `PASS` after anything else | yes | leave open |
| `WARN`/`NOT_EVALUATED`/`SKIPPED` after `FAIL` | yes | touch, no recovery — a degrade is still a problem |
| `ERROR` | any | no change; administrators get the execution-failure notice |

One unresolved issue per rule is a database constraint (partial unique index), so concurrent runs cannot open two. Muting silences issue opened/recovered only; execution failures are never muted. A failure storm replaces per-issue messages with one summary, but the issues and their recoveries still happen.

Two save-time judgments look like overreach and are deliberate: a value bound to an enum-typed column must be a declared label (PostgreSQL exact, MySQL through the collation including its implicit `""` member; with no label set known — domain, composite, unsynced schema — values are accepted rather than guessed), and column capabilities are per-engine allow-lists rather than a guess from the column kind, so a type a column cannot support is refused at save instead of failing at run time. Profiles read the same table, so the two cannot drift.

Query rules need a `READ_ONLY` source — never a fallback admin connection — and their SQL is one `SELECT`/`WITH` with every value parameterized. `custom_sql` is wrapped and never rewritten; `schema_drift` reads registry metadata, compiles to no statement and touches no database. Create (API, batch item, owner approval, agent adoption) probes the statement with `LIMIT 0`/`TOP 0` before storing, so an unbindable definition is refused while it is still a draft; update compiles only, because the editor previews first. A probe skipped for a saturated connection budget is logged, not fatal.

## Execution and scheduling

A run has two paths and neither expands a selection later: the API plans exactly the executions it selected (manual), the runner plans whole targets that are due (scheduled).

- The due scan is bounded in rules and targets, but never cuts a target's rules — whole targets or none.
- Planning does not advance the schedule. The cycle settles when no planned execution is pending, and only then is the next run computed from the stored cadence. A timed-out check retries on a later tick within its attempt budget; when the budget is spent the cycle settles without it.
- Missed cron slots are not caught up: a late tick schedules only the next occurrence; an interval target schedules `now + interval`.
- Each execution commits on its own, which is why one panic becomes `ERROR(EXECUTION)` and the rest continue; issue transition and notification follow best-effort after the query phase.
- A terminal write failure, a batch timeout or an unstarted execution abandons the batch; a partial rollup is never marked completed. Verdicts already committed still move issues and send their per-issue notifications.

A target schedules exactly one form: a fixed interval or a five-field cron in an IANA zone. Descriptors (`@daily`) and adjacent occurrences closer than 15 minutes are refused; there is no upper bound on cron spacing. A per-check frequency tier is a coarse filter over the target's due slots, not its own timer: unspecified follows the target, a denser tier is a no-op, a coarser one runs on the first cycle at least a tier period after the previous slot. Splitting the target is the only way to give one check its own schedule.

Data quality has no maintenance window of its own — the workspace quiet hours and one-off window from Notifications are it. Checks still run and write verdicts and inbox messages; only outbound delivery waits.

Time variables are a closed whitelist (`${now}`, `${today}`, `${yesterday}`, plus one integer offset on the latter two), accepted only as a whole filter value or inside custom SQL, and always bound as typed parameters or rendered from a server-generated literal — never spliced from user text. A temporal range bound must be one ISO-8601 literal; `"now - 1 day"` is refused at save with both alternatives named. There is no `${partition}`: partition windows are filters with variables, and an integer key needs custom SQL plus a cast.

Sampling is the user's "how much to read", and the mechanism belongs to the engine. A row limit is a bounded scan of the filtered rows; a percentage is the engine's native table sample and exists only on PostgreSQL and MSSQL tables. Elsewhere it is refused at save rather than silently downgraded to a predicate — a sample that is not a sample is worse than an error. Numbers derived from a sample are labeled as such.

## Profiles, labels and suggestions

Profiles are deterministic and model-free: one aggregate query plus one sample query per table, read through the same capability table as rule validation. Labels live in `column_semantic_label` and are a closed enum; manual writes overwrite and LLM output never does. Labeling a column as a resident ID card makes it personal data: profiles are redacted before storage, failure samples are masked for every rule type on that column, and clearing the label does not restore what was redacted.

The suggestion queue reconciles rather than appends. Each suggestion has a deterministic identity (`label|<column>`, `rule|<type>|<sorted columns>|<discriminator>`); a pass creates what is missing, refreshes a matching pending row in place, closes an identity whose fact now exists as `SUPERSEDED`, and skips what is already decided. A draft the proposing agent retracts closes as `WITHDRAWN`: terminal, but not a decision — no decider is recorded, the identity is not suppressed, and a corrected draft may be proposed again (see [quality-agent.md](quality-agent.md)). The summary reports created/refreshed/superseded/skipped so the UI can tell the truth instead of claiming "queued N". Explicit user intent outranks a derived pass, and a database upsert with a conditional update is the arbiter because two passes can race.

Creating a rule from a suggestion claims it first: the pending → accepted decision is the atomic claim, and a failed rule write reopens it. Passing-row evidence is a comparison aid, never an input — taken only for `WARN`/`FAIL` from the same sampled relation, with rows whose predicate is unknown appearing in neither sample. `segment_check` derives its expectation from completed, untruncated runs of the current target and revision only; a truncated observation can never become a baseline, and a new segment is evidence, not a failure.

## The LLM analyses

`enable_llm_analysis` is the total switch, off by default; with it off the rule engine is fully functional and every LLM entry point reports `FailedPrecondition`. Three analyses draft rules, explain a failure and suggest labels; a fourth generator (`RecommendQualityChecks`) is deterministic, model-free and spends no quota. All four write into the same suggestion queue; none creates, enables or runs a rule, and none writes a label except through a human decision.

Model output is untrusted: a closed schema, strict decoding, no tools, no database connection. Raw sample rows are never sent on any path; only the agent's `get_table_context` sends profile extremes and frequent values, and only when `send_sample_values_to_llm` is on — a narrower disclosure than the switch's name suggests. Prompt data is neutralized so an embedded `</` cannot end its block. Parse failures are not cached; a truncated finish discards the draft and asks for a narrower request. Identical in-flight calls are consolidated, and a canceled leader does not poison its followers.

Quotas are process-local and reset at 00:00 UTC, so a restart zeroes them. A disabled switch, an unavailable provider or an exhausted budget fails the analysis and never the checks.

## Signals and read models

These read models are computed at read time and write nothing. They never exceed their inputs: a covered target needs a current registry row and at least one enabled rule; a covered column must still exist; a judgment needs the latest completed execution *on the same target*, so after a target move the rule is unscored until it runs again.

- Coverage is the static "is anything checking this?" share, not health. A workspace with no checks can never read 100%.
- Consumption coverage is a different denominator — assets with an outgoing column-lineage edge, self-loops excluded. It is displayed next to coverage, never merged into it.
- Score is severity-weighted, per check, from the latest verdict. With nothing judged it is unset (`-`), never 0 or 100.
- Recent pass rate is the plain share over the last 20 completed executions of the currently enabled checks; `WARN` is not a pass. Score and pass rate are different instruments and are not interchangeable.
- The trend is a pure read of judged executions normalized by the current definition, so a type change truncates it and an edit that changes a filter restarts comparability.
- Asset signals are a tri-state read of the same target summary: absent from an exact selection means "not monitored", present with zero enabled checks means "all paused", present with enabled checks means a real signal. Unmonitored and paused are never green.
- A structure-change signal never blocks execution and never pauses a check; it compares the saved shape with the current one, so it is a state, not a change log.

"No consumer" and "no root cause" mean "not in the recorded lineage", never "none exists" — every surface showing impact must say so.

## Contracts and plan

A contract is a declaration, not a gate: publishing never blocks an execution, and a rule save runs normally regardless. Publishing records an immutable snapshot of settings plus the ordered checks and their revisions, compared by fingerprint; an unchanged fingerprint returns the existing version. Snapshots are never compared across schema generations.

Plan coverage is an independent axis from the coverage metric, consumption coverage and the quality score: the four are shown side by side and must never be converted into one another. Requirements come from the criticality template unioned with explicit statements, de-duplicated by `(property, column)`; an exemption suppresses a gap but never counts as covered, and it expires at read time. Live rules remain the only membership truth — a plan never copies them.

## Exports, report and digest

Exports are repeated queries, not snapshots, so concurrent writes can appear in this or the next export. They are bounded and streamed, refuse rather than queue when the process is saturated, and defuse any cell that a spreadsheet would evaluate as a formula. Each writes one audit row with its filter and exported row count, because bulk egress is the thing worth auditing.

The single-target HTML report is the only outlet containing source rows, so its footer must warn that forwarding the file forwards them. It is self-contained and shrinks itself to fit a hard size budget rather than failing; its body is redacted by name in the audit row.

The weekly digest is an outbound event, not an inbox message: nothing reaches the inbox, its recipients are channels rather than owners, only channels that explicitly subscribe receive it, and a missed window is skipped with a warning rather than backfilled. One period is processed once, keyed by the ISO week.

## UI contract

Checks are created and edited only in the target workspace; the cross-target directory exists for auditing, search and bulk governance, so it offers no create and no single edit. A check's detail is a drawer inside the workspace, not a page, and saving goes through the dry run.

The product calls a rule a "check" while the entity, API and tables stay `QualityRule`/`quality_rule` — a naming layer, not a schema change. `ERROR` and `FAIL` must stay visually and verbally distinct, and a new route must be registered in the layout audit script.

## Invariants — what must not break

| Rule | Guard |
| --- | --- |
| Domain semantics stay in `component/dataquality`; API and runner only orchestrate | keeps judgment testable with plain goldens, free of DB and HTTP detail |
| The rule head and its latest revision always agree, and an execution records the revision and engine version it actually ran | update transaction; `quality_rule_test.go` |
| `quality_run` is read-only: no write path, no independent runs page, and the execution stays the smallest judged unit | run read model and page tests |
| A manual batch executes exactly the executions the API planned; inflight dedupe keeps two runs of one rule from overlapping | enqueue by execution id; planning and admission tests |
| Single replica: inflight state, quotas, LLM counters, queues and instance slots are process-local; restart retries scheduled checks within the cycle and never manual ones | runner tests |
| The connection budget is admitted before a batch and held until the query phase ends; contention is never written as a verdict or `ERROR(QUOTA)` | admission tests; [instance-connections.md](instance-connections.md) |
| Batch RPCs are non-atomic per item, ordered, and never abort the rest; creating a rule from a suggestion claims it first and reopens it on write failure | batch handlers; integration concurrency test |
| One pending suggestion per identity; generation reconciles (upsert, supersede, skip) and reports truthful counts; a withdrawal is terminal but neither decided nor suppressing | partial unique index; upsert statements; withdrawal CAS |
| Identical in-flight model calls are consolidated; a canceled leader does not poison followers, and a panic is published to them | analysis-group tests under `-race` |
| Contract filters merge once at compile, rule clauses first; `custom_sql` is never rewritten; `schema_drift` has no SQL | compile and executor goldens |
| The structure fingerprint is derived and read-only, shares the metadata row's transaction, and is compared through two equivalent predicates (SQL and Go) that must change together; the legacy-hash branch only widens "unchanged"; acknowledgement never bumps a revision | [schema-fingerprint.md](schema-fingerprint.md) |
| Read models (coverage, score, pass rate, impact) never exceed their input boundary; a score with nothing judged is unset, not 0 or 100 | `score_test.go` contract; API tests |
| Templates carry no target and no history; code tables are bounded, their references resolved at save, and a referenced table cannot be deleted | template and code-table tests |
| Sampling never silently downgrades; time variables are never concatenated into SQL; a bad timeout is clamped, not turned into `ERROR(UNSUPPORTED)` | sampling, variable and timeout tests |
| Exports stay bounded and audited, contain no sample rows, the report stays self-contained with a redacted body, digest periods stay idempotent | export, report and digest tests |
| Personal data is redacted or masked before storage, never at read: labeled columns keep no raw profile values or failure samples, and clearing the label restores nothing | `pii_test.go`; personal-data integration test |
| SQL text and sample values are never logged; the provider's own `BAD_VALUE` message is the one exception, because only it names the literal to fix | execution error tests |
| Soft delete archives: revisions, executions and issues are retained, and rules are never hard-deleted | archive path; store filters |

## Failure modes

| Scenario | Behavior |
| --- | --- |
| Restart, or an execution left `PENDING`/`RUNNING` | the startup sweep abandons rows older than 15 minutes; scheduled checks retry within the cycle, manual ones do not |
| Instance connection budget saturated | scheduled: wait, then no batch and the rules stay due; manual: wait, then abandon; never `ERROR(QUOTA)` |
| Corrupt stored schedule, or a DST edge | falls back to the nominal interval, never becomes due every tick; a nonexistent spring-forward time is skipped, a repeated fall-back time fires once |
| Two replicas | unsupported: executions duplicate and process-local counters diverge, while the digest ledger stays table-unique; see [security-posture.md](../../docs/security-posture.md) |
| No `READ_ONLY` source at create, or removed later | create is refused (`FailedPrecondition`) and metadata rules need none; a later removal makes runs report `ERROR(NO_READ_ONLY_SOURCE)` and retry on the next schedule |
| Target connection or credentials fail | `ERROR(CONNECTION/AUTH/PERMISSION)` plus the administrator execution-failed notice; the next schedule retries |
| Target or instance deleted, or database hidden | the rule is orphaned, runs as `SKIPPED(ORPHANED)` and sends no notification |
| A stored config bypasses validation | rejected at validate/compile as `ERROR(UNSUPPORTED)`; a bad timeout is the exception and is clamped |

## Deliberate gaps

| Gap | Why it stays |
| --- | --- |
| The daily execution limit skips the scheduled scan silently, with no alert | rules stay due and resume the next day; a fake quota execution or an alert was rejected |
| `row_count_change` needs a baseline within 7 days and 2× the target cadence | a monthly or yearly cron can never establish one; the baseline is the latest non-empty completed run regardless of verdict |
| No distribution-drift detection and no whole-contract validation | non-goals: a check validates one object, and there is no contract-wide validation pass |
| No workspace-level report, PDF or scheduled subscription | the report stays a single target; `truncated` is binary and there is no remaining-row count |
| Creating a rule from a suggestion has a crash window after the claim | the suggestion can read accepted with no rule — legal and recoverable; the alternative was double rules |
| Suggestions written before the identity scheme are not re-keyed | a reviewer decides them or they age out; no auto-merge |
| Enum labels are the last synced ones; PostgreSQL domains and composites have none | buys a static check with no target connection; a renamed label or a domain can still reach SQLSTATE 22P02 |
| Built-in standard data is a build-time snapshot | revoked codes remain accepted as history; updating means refetching and running the sanity tests |
| The MCP quality surface is strictly read-only and does not expose explain | the caller is itself a model and should explain from evidence; no publish, decide, run or write |
| The whole-message settings update can clear the check time zone | an older client updating another setting resets it to UTC; the digest schedule is the only field that preserves itself when omitted |
| Coverage and score read the registry on every request | no cache or roll-up; a large registry wants one before this becomes a hot path |
| A dynamic accepted-value query cannot be typed at save | its value column, its emptiness and its cross-database reach surface at preview or first execution; the set is re-read per statement |
| Cross-table reconciliation is aggregate-only, against a synced registry object | per-row reconciliation is custom SQL; two connections mean two snapshots |

## Where to look

- Judgment, compile, profiles, suggestions, trends, scores: `backend/component/dataquality/`. `rule_type_spec.go` is the single catalog every per-type list derives from — a new rule type is a proto enum value and one row there. `evidence.go` and `health.go` are the model-facing projections; `diagnosis.go` classifies a failed run and never asks the model.
- Dispatch, planning, admission, execution, issues, notifications: `backend/runner/qualityrunner/`; digest: `qualitydigestrunner/`; agent runners: `qualitysessionrunner/`, `qualityenrollmentrunner/`, `qualityagentrunner/`.
- HTML report: `backend/component/qualityreport/`; cron and interval parsing: `backend/common/schedule/`.
- Read-only query capability: `backend/plugin/db/`; connection budget: `backend/component/state/connection.go`.
- Agent evidence reads: `backend/api/v1/data_quality_agent_evidence.go`, gated by the non-baseline `rules.evidence` permission and filtered by `agentpolicy.AllowedActions`.
- Handlers: `backend/api/v1/data_quality_*.go`; store queries: `backend/store/quality_*.go`; MCP tools: `backend/mcp/tool.go`.
- Protos: `proto/store/store/data_quality.proto`, `proto/v1/v1/data_quality_service.proto`, `proto/store/store/setting.proto`; conversion: `backend/api/v1/data_quality_convert.go`. Never hand-edit generated code.
- Permissions: `backend/common/permission/permission.json`, `backend/store/predefined_roles.go`; mirror in `frontend/src/lib/permissions.ts`.
- Frontend: pages in `frontend/src/pages/data-quality/`, components in `frontend/src/components/data-quality/`, helpers in `frontend/src/utils/dataQuality*.ts`, API wrapper `frontend/src/api/data-quality.ts`.
- Integration coverage: `backend/test/integration/runner/data_quality_*_test.go`.
