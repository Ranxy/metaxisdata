# Data quality

Data quality watches the **data** in your databases, not just its schema. A check
is a typed assertion over a table or column (row count, nulls, uniqueness,
accepted values, ranges, regex, string length, referential integrity, cross-column
relations, freshness, aggregates, grouped row counts, schema drift, cross-table
reconciliation, custom SQL or a CEL expression). A scheduler runs the checks, and
each execution records a status, the observed value, the thresholds it was
judged against, and (optionally) the rows that failed.

Everything is in the **Data Quality** section of the sidebar. The check engine is
fully offline; three model-assisted features are opt-in.

## Before the first check

1. **Give the instance a read-only data source.** Query checks run over the
   instance's `READ_ONLY` data source and never over the admin one; an instance
   without it cannot create or preview a query check. Settings → Instances → the
   instance → Data sources → add a `READ_ONLY` account whose database grants are
   `SELECT` only. The schema-drift check is the exception: it reads metadata and
   needs no data source.
2. **Check the permissions.** Reading checks, executions, issues and contracts is
   part of the member baseline. Creating, editing, running, acknowledging and
   resolving need `metaxisdata.dataQuality.rules.*` / `.issues.update`; publishing
   a contract needs `metaxisdata.dataQuality.contracts.publish` (workspace
   administrator or a custom role). Model-assisted paths also need `.suggest` or
   `.explain`.
3. *(Optional)* **Turn on model assistance.** Settings → Data Quality → enable
   *LLM analysis* and pick a provider profile. The separate switches for failure
   samples and for sending raw sample values are independent: storing samples is
   local to the deployment, and the "send sample values" switch is reserved —
   no model path includes raw sample rows today. Everything else works with
   model assistance off.

## Adding a check

Data Quality → **Targets** → **Start monitoring** picks a table or view from the
metadata tree and opens its contract workspace with the check dialog already
open — this is the section's own way to begin work on a table without visiting
the metadata browser. The same entry is on the overview and the checks
directory. For an already-monitored target, open it and use **New check** (or the
add button in the dataset or column section it belongs to). The form opens as a dialog with the
target fixed, so a second check on the same table never walks the target picker
again. The catalogue on the left is grouped into common checks (row count, nulls,
uniqueness, accepted values, freshness, custom SQL), dataset checks, column
checks and advanced types; picking one shows its parameters on the right with a
one-line description. A column section's add button pre-selects that column and
the column-scoped types.

Fill in the parameters; the dialog compiles the check server-side and shows the
SQL it will run before you save. Saving validates the target, the columns and the
parameters; a check that cannot compile is refused. Filters, owner, tags and the
empty-result behavior are under **Details** in the same dialog. A view or
materialized view only offers table-level checks (row count, row count change,
freshness, custom SQL, schema drift), because those objects have no column-level
type authority. Editing a check opens the same dialog from its row or drawer.

- **Schedule**: scheduling belongs to the target, not to one check. All enabled
  checks on a target run together on the target's schedule, so a new check joins
  the target's next run; use **Run now** to check it immediately. The dialog's
  **Run cadence** select can make one check run *less* often than its target —
  see [Check run cadence](#check-run-cadence).
- **Thresholds**: a count-type check compares failed rows (or the failed
  percentage) with a warning and a failure level; an aggregate compares its value
  with a range; freshness compares the lag in seconds. Custom SQL checks always
  count failed rows — they have no row total to take a percentage of.
- **Empty data**: by default a query that returns nothing is `NOT_EVALUATED`,
  not a pass. Choose pass or fail per check if that is the intent; row count,
  custom SQL and schema drift always produce a value, so the setting does not
  apply to them.
- **Row count change**: compares the current row count with the check's most
  recent completed execution that measured a non-zero count — whatever that
  run's verdict was — within the last seven days and two target intervals (for a
  cron target, twice the expression's nominal interval). The first run has no
  baseline and is not judged; a newly created check's dialog defaults *empty
  data* to pass, so that first observation only records the baseline. The
  execution detail shows the baseline value and time the comparison used.
- **Filters** narrow the rows the check reads (structured AND conditions only);
  write a custom-SQL check when the condition needs more. A target-level filter
  from the contract is ANDed on top.
- **Preview** runs the compiled statement with `LIMIT 0` against the read-only
  source: it proves syntax and permission without scanning the table. If it is
  refused, the error shows the statement and its bound values; a successful dry
  run does not display the statement. Time variables are resolved when the check
  actually runs, in the workspace's check time zone.
- **Schema drift** compares each column's type and nullability — plus, if you ask
  for it, order and extra columns — with the snapshot saved when the check was
  created. Columns recorded by an older snapshot that did not store nullability
  stay unjudged until the check is saved again. Use *Fill from target* to record
  the current definition.

### Check run cadence

Every check runs whenever its target runs, and that is all most checks need; the
target's own schedule is covered in [The target schedule](#the-target-schedule).
When one check is more expensive than the rest of the target — a cross-table
reconciliation, or an aggregate over a large table — open it in the check dialog
and choose a **Run cadence**:

| Run cadence | How often the check runs |
| --- | --- |
| Follow target (default) | in every target cycle, like the rest of the checks |
| Hourly | at most about once an hour |
| Daily | at most about once a day |
| Weekly | at most about once a week |

The target's own schedule is the **floor**: a check never runs more often than
its target, and a cadence denser than the target's does nothing — *Hourly* on a
target that runs every two hours still only runs when the target does.

A cadence is measured on the target's own cycles: a check runs in a cycle only
when the cycle its previous completed scheduled run belonged to is at least the
cadence period earlier. So *Weekly* on a daily target runs in the 7th following
cycle, *Hourly* on a 15-minute target in the 4th, and a cadence equal to the
target's own never skips a cycle when the target's cycles are all the same
length. The comparison uses the cycle a run belonged to, not when it actually
executed, so a late run or a skipped cycle cannot shift it — and a skipped cycle
(the server was down, the instance's connection budget stayed busy, or you
rescheduled the target) is not made up later. A daylight-saving transition can
make one cron cycle slightly shorter than the cadence period; that single cycle
is skipped, the next one runs, and the cadence does not drift.

**Run now** ignores the cadence and always runs the check, and a manual run does
not count toward the cadence. Changing the cadence only affects future scheduled
runs: it does not change the check's definition or create a new revision of it,
so its history and thresholds are untouched. A cadence is not a schedule of its
own — there is no per-check cron expression. If a check needs a rhythm independent
of its target, move it to its own target.

The cadence appears as a small badge on the check's row and in its drawer only
when it is not *Follow target*.

### Scoping and limiting a check

A check does not have to read everything on a partitioned or append-only table.
Three settings in the dialog's **Details** section bound what one execution
reads and how long it may run:

- **Partition window**: add a filter condition whose value is a time variable —
  `${today}`, `${yesterday}` or `${now}`, optionally shifted, for example
  `${today-7d}` (units `m`, `h`, `d`). The variable is resolved when the check
  runs, in the workspace's check time zone (Settings → Data Quality → **Check
  time zone**, UTC by default), so `created_at >= ${today-7d}` keeps meaning "the
  last seven days" as the schedule moves. Variables work in filter values and in
  custom-SQL text, and are refused anywhere else. A filter value is either a
  literal or one whole variable; write `created_at >= ${today-7d}`, not
  `'${today-7d}'` and not `prefix-${today}`.
- **Sampling**: *No sampling* (the default), *By row limit* ("read at most N rows
  of the filtered relation"), or *By percentage* ("read approximately P% of the
  table"). The choice is an intent, not a query hint: the engine's own mechanism
  implements it. *By percentage* is offered for PostgreSQL and SQL Server tables
  only. MySQL/TiDB/MariaDB/OceanBase have no query-level sampling clause, and
  Doris/StarRocks sample storage units whose row statistics can lag behind a fresh
  load — such a query may read everything while still reporting itself as a sample
  — so all of those tables offer the row limit only, and a view never offers
  percentage sampling. The dialog only offers what the target can express (while
  the instance is still loading it may offer the percentage optimistically, and
  the server refuses it), and a sampled execution is marked as such — its verdict
  is judged on the sample.
- **Query timeout**: how long one query of this check may run, 1–300 seconds;
  empty means the 60-second default. A target's scan batch still ends after 15
  minutes, so a long timeout can be cut short by the batch.

Sampling is not available for a check whose verdict describes the table as a
whole (row count, row count change, freshness), for a check that reads two
relations (referential integrity, cross-table reconciliation), or for custom SQL,
schema drift and CEL checks: custom SQL is never rewritten, schema drift reads
metadata rather than rows, and a CEL expression has its own row limit. The
dialog explains the reason when a saved check's sampling no longer applies — for
example after moving it to an engine that has no percentage sampling.

Once a check has been judged at least five times, its edit dialog offers
**Suggested thresholds** derived from the check's own history: for a check that
counts failing rows, measures a lag, or reconciles a table-to-table difference,
warning at the 95th percentile and failure at the 99th (rounded up to the
check's unit); for a row count check, the observed range
widened by 10%, and only for the bounds the check already sets. Using them only
fills the form — the check changes when you save it. The suggestion is
unavailable for aggregate, row count change and schema drift checks, and while
the form's check type or measurement unit differs from the saved check, because
the history then describes a different measurement.

Checks can also start from a proposal instead of a blank form. **Recommend
checks** reads the columns' stored profiles and drafts the checks that follow
from them — a volume floor, a freshness bound on the relation's event-time
column, a null-rate bound on a column that has nulls, an enumeration for a
low-cardinality column, uniqueness on a column whose values are all distinct, an
observed range on a numeric column. It needs no model and no provider, so it
works with model analysis switched off; the drafts land in the same review queue
as every other proposal, and nothing runs until a person creates the check. The
observed threshold is a starting point: the draft's reason states the
observation and the arithmetic, and the check dialog can change it. On the target
workspace the entry is in the copilot panel's quick strip; on a table's Data
Quality tab it sits beside **Target workspace**.

Checks can also start from a model draft: draft in the target workspace's AI
copilot, or open **Data Quality → Suggestions** and use **Draft with AI** to pick
one or more tables and describe the check once. A queued draft appears inline in
its target's workspace, where it can be accepted or adjusted in the check dialog;
multi-target drafting runs as a background job, so you do not have to keep the
page open.

## Browsing targets and checks

Data Quality → **Targets** is the directory: one row per table or view that has
checks, with its check count, covered columns, latest result, **quality score**
(a dash until a check has been judged — see
[Coverage and quality score](#coverage-and-quality-score)), open issues,
pending AI drafts and last run. **Start monitoring** opens the metadata picker
and lands on the chosen target's workspace ready to add its first check. Open a target to get its **workspace**: every check
on that table, grouped into dataset-level checks, cross-column checks and one
section per column, with the latest result and a strip of each check's recent
executions beside it. Its header sums the target up, including the **quality
score** and the **recent pass rate** (see
[Coverage and quality score](#coverage-and-quality-score)). The workspace can run all of the target's checks at once,
add or edit a check without walking the target picker again, show which columns
have no check yet, and draft checks with the AI copilot on the right — drafts
appear inside the section they belong to until you create them as checks.

If a table's **structure changed after its checks were saved** — a column added,
dropped, renamed, retyped or made nullable, a primary key changed, or a view's SQL
rewritten — the target row and the target workspace show a **Target schema
changed** badge. This works on every engine: the platform fingerprints each
table's columns, primary key (on engines that report index metadata) and a
view's definition during schema sync, not just the few engines whose DDL it
stores. The badge means "review these checks against the new shape"; it does not
change, pause or fail them. From the target workspace, **Acknowledge schema
change** (the button beside the badge, for users who can edit checks) records
that the checks were reviewed against the current schema and clears the badge.
Saving a check re-baselines that check only, and only when the edit changes its
definition (type, target, filter, thresholds or parameters) — renaming it,
changing its severity or its owner does not. The target keeps the badge until
every check is re-baselined or acknowledged. A check that should notice the
change on every run is a **schema drift** check, which compares against the
column list saved with it.

The Targets, Checks, Issues and Suggestions lists keep their filters in the
address bar, so refreshing or sharing a link returns to the same view.

Data Quality → **Checks** is the cross-target directory for audit and search:
every check with its target and latest execution, narrowed by the same target
scope, text search, check type, dimension, severity, state and owner filters.
It is read-only by design — a check is created and edited inside its target. Its
**Target scope** filter follows the metadata tree: pick an instance, then drill
into a database, schema or table/view, and apply at any level. Filters take
effect when you press **Apply**.

## Contracts and target-level settings

### The target schedule

A target has **one schedule**, shown in its workspace header next to the health
summary: for a fixed interval, "Schedule: every 2h · next in 12 minutes"; for a
cron schedule, the expression and the time zone it is evaluated in, plus the next
run. It applies to every enabled check on the table or view: when it comes due,
all of them run in one scan batch. Use the pencil next to it to change the
schedule. The dialog offers two exclusive forms under a **Runs every / Cron
expression** switch:

- **Runs every** — a fixed interval from 15 minutes to 7 days. This is the
  default form, and the workspace's Data Quality settings seed the interval a new
  target starts with.
- **Cron expression** — a standard five-field expression (minute, hour,
  day-of-month, month, day-of-week) evaluated in an IANA time zone, for example
  `0 2 * * *` for every day at 02:00 or `0 9 * * 1` for every Monday at 09:00.
  Occurrences must be at least 15 minutes apart; there is no upper bound, so a
  monthly or yearly expression is allowed. Pick the time zone in the dialog from a
  short list of common zones, your browser's zone and the schedule's current
  zone. If a request leaves the time zone empty, the server resolves it to the
  workspace's **check time zone** (Settings → Data Quality) and stores the
  resolved zone, so changing that setting later does not move an existing
  target.

A change reschedules the target from now and never runs the checks immediately;
the header shows the next run. While every check on the target is paused there is
no next run, and resuming a check schedules the target from its saved schedule
instead of firing at once. A check you move to a different target joins that
target's schedule.

A cron expression is local wall-clock time: a time that does not exist on a
daylight-saving transition day is skipped rather than shifted, and the repeated
hour on the fall-back day fires once. A run missed while the server was down is
not caught up — the next run is the next occurrence after the server returns.

Editing a single check cannot change the target's schedule — that is deliberate,
so one table cannot have checks pulling in different directions. There is also no
per-check cron expression; a check's **Run cadence** can only make it run *less*
often than its target (see [Check run cadence](#check-run-cadence)).

A target's **contract** is the target-level layer above its checks: defaults for
authoring and a record of what has been published. It lives on the same target
workspace, under the health summary.

- **Target filter** — conditions every check on the table or view is checked with.
  They are ANDed with each check's own filter at compile time, so changing the
  contract filter changes the next execution without rewriting any check. Written
  custom-SQL checks are not rewritten and stay outside the contract filter.
- **Authoring defaults** — a default severity seeds the New check dialog. It
  never rewrites an existing check. The schedule is not a contract setting: it
  belongs to the target (see above) and is not part of published contract
  versions.
- **Publish** — recording the live checks and settings as a version. Publishing
  does not gate execution: checks keep running as they are saved. It keeps a
  history with what changed since the previous version (added, changed, removed
  checks); republishing unchanged state is a no-op that returns the existing
  version. Publishing needs `metaxisdata.dataQuality.contracts.publish`; editing
  the settings needs `rules.update`.

## Executions and statuses

An execution is **one check checked once**, on demand or by the scheduler. If a scheduled batch times out or is abandoned before a check runs, that check is retried in the same target cycle; the execution detail shows its attempt number (attempt 2 or 3), and the batch card counts how many checks were retries. A check is attempted at most three times per cycle. Open
Data Quality → Executions to watch them: every check the scheduler or a manual
run started is one row, and a queued or running row is polled until it finishes.
The list filters by lifecycle state, outcome and trigger; open a row for the full
evidence behind it.

An execution's lifecycle is *pending* (planned, waiting for the runner),
*running*, or *completed*. A queued or running execution left behind by a stopped
server process is marked abandoned by a startup sweep instead of being resumed: an
abandoned row means "the check never finished". A scheduled check is retried on a
later scheduler tick of the same target cycle, within its three-attempt budget; a
manual execution is not retried, so use *Run now* again. Once an execution
completes, its outcome is one of six statuses:

| Status | Meaning |
| --- | --- |
| `PASS` / `WARN` / `FAIL` | The check ran: within thresholds / past warning / past failure. |
| `ERROR` | The check could not run (connection, permission, timeout, an unparsable value). **This is not a data failure.** |
| `SKIPPED` | Deliberately not run: the target disappeared, the check is archived, or the same check was already executing. |
| `NOT_EVALUATED` | The check ran but had nothing to judge (no rows after the filter, a NULL timestamp or aggregate). |

An abandoned execution has no outcome. Failed executions can be expanded for
samples and the exact SQL. For a column check on a table whose metadata records
a primary key, each sample is a numbered failed row led by the key columns and
then the checked column(s), so two failures on the same column do not look like
one row repeated; a grouped check returns the grouping key instead. A sample
recorded after this release shows SQL `NULL` and an empty string as different
values rather than a blank cell; samples stored before it cannot tell the two
apart and are labelled as such. The execution detail also shows the check
revision, engine version, duration, trigger, instance and, for a manual run, who
started it. Executions are the durable record: batching checks together for
scheduling is an internal detail.

Opening a check — its row in the target workspace, or its name in the checks
directory, an execution, an issue or a table's quality tab — opens a drawer over
the workspace. It answers "is this check healthy?" first — the latest status and
its observed value, the pass rate, and a strip of the last 20 executions in one
color per status (green pass, amber warning, red fail, dark red execution error,
grey skipped/not evaluated). Below that sits the recent execution table, whose
rows open the execution behind them, and the check's definition (target, type,
revision, tags, filter, empty behavior). *Run now*, *Edit*,
*pause/resume* and *Delete* appear there for callers who hold the matching
permission.

The drawer also plots the check's **observed value trend**: the value each
recent judged execution measured, oldest to newest, colored by its verdict,
with the warning and failure bounds as reference lines. Only judged runs
(`PASS`/`WARN`/`FAIL`) are points, so an execution that errored or had nothing
to judge shows in the history strip above the chart rather than as a fake zero.
The bounds come from the execution that judged the check's current definition,
so a check edited since its last run says the band is not drawn instead of
showing stale bounds. A percentage check is plotted in percent; a row count
change is plotted as rows, without a band, because its thresholds bound a
percentage change against a baseline rather than the count; a sampled check
marks its sample points. Click a point to open that execution. The series stops
at a rule-type change and says so, because values measured by a different kind
of check are not comparable. Editing the check's filter changes which rows it
measures: the line still spans both regimes, so read across a filter change
with care.

## Coverage and quality score

Two read-only numbers answer different questions: coverage says how much of the
metadata is watched, and the quality score says how healthy the watched checks
are. Neither is stored or computed by a background job — each is assembled when
the overview, a target row or a report is read, from the registry and from the
checks' completed executions. Viewing them writes nothing, changes no check,
issue or notification, and they are not audit-logged (they are reads).

### Coverage

Coverage compares the objects this deployment can monitor with the objects that
have at least one enabled check.

- **The denominator** is every table, external table, view and materialized
  view in the metadata registry under a live instance (a deleted instance
  contributes nothing). The column total comes from the registry's own column
  lists; the registry stores per-column rows only for tables, so the columns of
  views, materialized views and external tables are read from the object
  definition in the registry.
- **The numerator** is the distinct target with at least one enabled check, and
  the distinct columns those checks inspect, each counted only while it still
  exists in the target's current registry column list — the column named in a
  check's configuration, not the display snapshot stored on the check.
  Table-level and multi-column checks cover their target but no single column. A
  column dropped or renamed by schema sync stops counting immediately: the check
  stays enabled (and starts returning execution errors) but it no longer covers
  a column, so schema drift cannot push coverage past 100% either.
- The overview breaks coverage down by instance and by environment; the row for
  instances with no environment has a blank key.
- An enabled check whose target has no registry row any more (the object was
  deleted, or a rename changed its GUID so the old check no longer resolves) or
  whose instance was deleted is left out of the workspace coverage and quality
  score: there is no denominator row to count it against. Such checks still
  appear in the Targets directory with the *Target unavailable* badge and still
  count in the enabled-check total, so coverage can never exceed 100% and the
  instance/environment rows add up to the workspace totals.

A target whose checks have never produced a verdict is **monitored but not
judged**. Coverage is not a health measure: no check on a table means it counts
in the denominator and not in the numerator, so a workspace without checks is at
0% coverage, never 100%.

### Quality score

The quality score is 0–100, built from the **latest completed execution of each
enabled check**. Paused and archived checks do not enter it.

| Check kind | Score of one check |
| --- | --- |
| Row/unit checks — nulls, uniqueness (single and combination), accepted values, ranges, regex, string length, referential integrity, cross-column, CEL expression, schema drift, group check | `1 - failed rows / total rows`, clamped to 0–1; an execution with no total rows falls back to the status scale |
| The other six — row count, row count change, freshness, custom SQL, aggregate, cross-table | `PASS` = 1, `WARN` = 0.5, `FAIL` = 0 |
| `ERROR`, `SKIPPED`, `NOT_EVALUATED` | Not judged: left out of both the numerator and the denominator |

Each judged check is weighted by its severity — warning 1, error 2, critical 3 —
and the score is `100 × Σ(weight × check score) / Σweight`. The same formula
gives a target's score, the workspace score and the per-dimension and per-owner
breakdowns. The workspace score is weighted over checks, not an average of
target scores, so a table with many checks carries more weight than a table with
one.

A check is only scored from an execution against the target it currently
watches. After a check is moved to another table, it is not judged until it
completes a run there, and its earlier executions are not attributed to the new
target.

A dash (`-`) means **nothing was judged** — never "0" and never "100". The score
is unset when no enabled check has a judged result: there is no enabled check,
none has run yet, or all of them ended in `ERROR`, `SKIPPED` or
`NOT_EVALUATED`; the overview also shows how many enabled checks are judged and
how many are not, so a dash can be told apart from a low score.

### Recent pass rate

A target's row shows a second number: the pass share of its **most recent 20
completed executions** (across the checks that are currently enabled):
`PASS / (PASS + WARN + FAIL)`. The window is taken after filtering to enabled
checks, so a paused or archived check's executions never occupy one of the 20
slots. The window also follows a check to its current target: when a check is
moved to another table, the executions it recorded against the old table leave
the old target's window (they remain in the Executions list) and they do not
count for the new target until the check runs there. `ERROR`, `SKIPPED` and
`NOT_EVALUATED` are excluded because they are not verdicts, and a warning is not
a pass. This is a different measure from the
quality score — the score weights each check once by severity and always looks
at the latest execution, while the pass rate counts executions — so do not
compare the two as if they were one number. A dash means none of the recent
executions was judged. (The pass rate in a check's drawer is still a per-check
figure over that check's own recent executions.)

### Reading and trusting the numbers

- A freshly configured workspace with synced metadata shows 0% coverage and a
  dash for the score: no check exists yet. Adding the first check moves coverage;
  the score appears after the first execution completes.
- Both numbers are derived, not stored: they are recomputed on every read, so
  they always match the executions you can open. Nothing is written by viewing
  them, and they do not trigger notifications. The target HTML report that shows
  a score is audited like the other exports (request and file name, never the
  HTML body).
- A hidden or soft-deleted database keeps its last metadata snapshot until the
  instance sync rewrites it, so its tables can still count toward the coverage
  denominator for a while; their checks usually become *orphaned* and stop
  running.
- The score and the recent pass rate both cover enabled checks only. Pausing a
  check drops it from the score and out of the pass-rate window immediately: its
  completed executions remain in the Executions directory, but they no longer
  occupy one of the target's 20 slots.

### Trust signals in the metadata browser

The metadata browser carries the same answer where a consumer actually looks —
Data → browse, the global search and every table/view/external-table/
materialized-view page:

- **One column per object** shows a status point and a word (pass, warning,
  failing, error, not run), with a second line naming column coverage and the
  last run: *covered 4/9 columns · 2 hours ago*. The asset page repeats the same
  signal in its header, and its Data Quality tab opens with the summary
  (status, quality score, recent pass rate, column coverage, last run).
- **A table with no check is marked "not monitored"**. It is never green and
  never reads as 100%: nothing is known about it, so nothing is claimed. The
  list column itself is text only (a link would swallow the row click); the
  asset header badge and the Data Quality tab offer "Start monitoring" into the
  target workspace.
- **Column coverage is per object**: how many of the table's current columns at
  least one enabled check reads, out of the columns the registry currently holds.
  A check that names no column (row count, freshness on the table itself, custom
  SQL), a paused check and a column that schema sync dropped cover nothing. The
  number never exceeds the total because the numerator and the denominator come
  from the same registry read.
- **The score is the same target score as on Data Quality → Targets and in the
  target workspace header** (same weighted formula as the workspace score on
  Data Quality → Overview, but a target aggregate rather than a workspace one);
  when no enabled check has a judged result it shows a dash, not 0 and not 100.
- These signals are read-only. Viewing a metadata page writes nothing, audits
  nothing and sends no notification; a member's read baseline already covers
  them, while a custom role without the data-quality read permission does not see
  the column or the badge.
- Integrations that read the registry over MCP get the same fields from the
  `get_quality_signal` tool, which answers `monitored: false` for an object no
  check watches. The same read-only surface exposes the quality directory, one
  target's checks and problems, the problem ledger, the execution list and one
  execution's full evidence (`list_quality_targets`, `get_quality_target`,
  `list_quality_issues`, `list_quality_executions`, `get_quality_execution`);
  none of these asks a model to explain a failure — the calling agent reads the
  evidence and reasons about it itself.

## Problems and notifications

A check that starts failing opens an **issue**; it is the same issue until it
recovers, and its occurrence count grows while it keeps failing. Data Quality →
Issues lets you acknowledge it, resolve it with a reason, or mute it for a while.
When a check passes again its issue resolves itself as *auto-recovered*. Each
issue links the first and the latest execution that observed the problem.

Issues produce in-app notifications (bell icon) for the check's owner, or for the
workspace administrators when there is no owner; a recovered issue notifies the
same audience. Execution failures always notify the administrators and cannot be
muted. When more than ten checks in one target's scan batch fail, one summary
replaces the per-check messages. An administrator can also forward the same events to a webhook under Settings → Notifications; see [notifications.md](notifications.md).

The workspace **quiet hours** and one-off **maintenance windows** under
Settings → Notifications are the data quality maintenance window: checks keep
running and judging, and in-app notifications are still written to the bell list,
but outbound deliveries to webhook, chat and email channels are held until the
window ends. A channel marked **Deliver during quiet hours** still sends. Nothing
is dropped, so an alert raised during the window arrives when it closes; see
[notifications.md](notifications.md#quiet-hours-and-maintenance-windows).

A **structure change** on a watched table is its own message, sent once when the
change is synced: to the owners of that target's checks, or to the workspace
administrators when none of the checks has an owner. Tables nobody checks are not
reported. The message names the target and links to its workspace, where you can
review and acknowledge the change. Repeated syncs of the same new shape do not
page again.

## Reports, exports and the weekly digest

Three things leave the screen; all of them are read-only snapshots of what the
checks already recorded.

**CSV export.** The Executions, Issues and Targets directories each have an
**Export CSV** button. It exports the **whole set your current filters select**,
not just the visible page: state, outcome, trigger, rule, target, instance,
owner, and time window all apply. The file is UTF-8 with a header row and opens
directly in a spreadsheet.

- The export stops at **100,000 rows**. When it does, the file ends with the rows
  it could fit and the app tells you it was capped — narrow the filters (for
  example the time window) to get the rest.
- It is a sequence of queries, not a database snapshot: rows written while it
  runs may land in this export or the next one. If the stream fails midway the
  file is incomplete — discard it and run the export again.
- Text cells that begin with `=`, `+`, `-`, `@`, a tab or a carriage return get
  a leading single quote, so a check title or table name cannot turn into a
  spreadsheet formula; numeric, enum and timestamp columns are left as they are.
- Exporting is a **bulk read**, so it is recorded in the audit log (who exported,
  with which filters, how many rows) and counts against the caller's request
  budget. At most four exports or reports stream at once per server; a fifth
  request is refused with "too many concurrent exports" rather than queued.
- An export stops after 15 minutes; the app can also cancel it by navigating
  away, and a cancelled export does not leave a file behind.
- Exported files contain metadata and verdicts only — never the sampled failure
  rows.

**Target report.** The target workspace has **Export report**. It downloads one
self-contained HTML file (styles and chart are embedded; nothing is loaded from
the network) that you can open anywhere or forward as an attachment. It covers
the last 30 days by default (up to 90) and contains the report scope, the current
result summary and pass rate, the target's **quality score** (with how many
checks were judged and how many were not), the daily trend, the check list, the
unresolved problems, a capped sample summary of failed checks, and the monitoring
basis (target, instance, schedule, rule revisions, engine versions). It is
generated in the interface language you are using. The score card uses the same
formula as the workspace (see
[Coverage and quality score](#coverage-and-quality-score)); the score and the
six-status pass rate are different measures.

The report is the one export that contains **real values from your data** — up to
5 sample rows per failed check (at most 20 checks), capped at 2 MiB in total, with
an explicit note when anything was cut. Treat the file as sensitive: forwarding it
forwards those rows. Generating it is audited; the HTML body itself is never
written to the audit log.

**Weekly digest.** Settings → Data Quality has a **Weekly digest** card: enable
it, pick the weekday, the `HH:MM` time and the IANA time zone. The digest is a
workspace-level summary of the last rolling seven days — executions by verdict,
problems opened/recovered/still open, and the owners and targets with the most to
look at — delivered to the outbound notification channels, not to the bell icon.

- Configure the destination first under **Settings → Notifications**: create a
  channel and tick **Data quality digest** in its event types. Delivery is
  **opt-in**: a channel with no event types selected receives the event
  notifications but *not* the digest.
- Email channels need at least one fixed address; the digest has nobody in
  particular to address, so "include the audience" cannot resolve recipients.
- If the server is down at the scheduled minute, the digest is still sent when it
  comes back within **24 hours**. A longer outage skips that week (one warning is
  logged); the next week's digest runs normally.
- Routing rules (owner/team) do not apply to the digest — it is one workspace
  summary, with owners used as grouping inside the text.
- A digest is queued once per ISO week; retries reuse the same delivery record, so
  a restart cannot send the same week twice.

## Profiles, labels and suggestions

The table detail page has a **Data Quality** tab: how many checks watch the table,
their recent executions, its open issues, and per-column profiles and semantic
labels. *Profile columns* computes null counts, distinct counts, extremes,
lengths, frequent values and value shapes with two queries; *AI label fields*
asks the model to name each column's semantic type and proposes check drafts from
a fixed mapping; *Recommend checks* derives check drafts from the profile itself
and needs no model. Both only produce suggestions — nothing becomes a check until a
person creates the check. When the table has proposals waiting, the tab also
shows a **Suggestions awaiting review** card (it stays hidden otherwise): each
row names the column, or the check draft's title, with the proposed semantic
label or the check type and severity, the confidence and the model's reason.
Label proposals are accepted or rejected right there — accepting writes that
column's label and moves it into the columns table — while a check draft opens
in its target's workspace, where the check dialog can be pre-filled from it and
created on save. At most five proposals are previewed; *Open the suggestion
center* leads to the full queue, where the rest are reviewed in bulk. The card and
its actions need the `.suggest` permission, so members without it never see it;
opening a draft in the dialog additionally needs the check-creation permission, so
a reviewer without it can still reject the draft.

The **Suggestions** page is the review center. Suggestions arrive from three
places:

- **Draft with AI** on the Suggestions page: pick one or more tables (a single
  target can also limit its columns), describe in your own words what must hold,
  and submit. This starts a **background drafting job** on the server: you can
  close the page or the browser tab and the model calls continue. The job appears
  in the **AI drafting jobs** panel on the Suggestions page with its per-target
  progress, how many suggestions it has queued, and any per-target error; an active
  job can be canceled, which stops it after the target currently being drafted.
  A failed target explains itself: the failure class (for example *Output limit*),
  a plain-language next step, the provider/model, stop reason, token budget and
  prompt size, and a **correlation ID** to quote in a support request. When runtime
  LLM debug was enabled, *View raw request/response* shows the exact prompt and
  model response (the debug log is kept seven days).
- **Recommend checks** on a target workspace or a table's Data Quality tab: profile
  the columns first, then derive checks and starting thresholds from those
  profiles. It uses no model and no provider, so it is available with model analysis
  off, and it is the only pass that proposes an observed threshold. The same button
  re-runs the pass on a later day, so a drifted profile refreshes its pending drafts.
- **AI label fields** on a table's Data Quality tab (and in each field's section on
  the target workspace): let the model name each column's semantic type and propose
  the deterministic check types those labels imply. The label action needs model
  analysis on; without it the button is not offered.

The queue groups suggestions by target table and filters by status, kind, source
and target scope. Suggestions produced by a finished drafting job appear like any
other and are selected on the current page when the job completes. Select rows on
the current page to work in bulk (up to 200 per submit):

- **Check drafts** — *Create checks* opens one dialog where you set severity,
  dimension and owner for the whole selection; expand a row to edit its
  title, severity or parameters. One submit creates them all, and each check is reported
  separately: a draft that cannot compile is reported with its error and stays
  pending while the rest are created. Creating a check accepts its draft.
  *Ignore drafts* records a rejection instead.
- **Column labels** — *Accept labels* writes the semantic labels immediately;
  *Reject labels* discards them. Accepting a label does not touch data.

### What a recommendation reads, and what it will not propose

A recommendation is only as good as the profile behind it. The pass refuses a
column whose profile is missing or older than seven days, so *Recommend checks*
runs the profile step first when the reader may; the queue then holds drafts whose
reason cites the observation ("0.42% NULL across 12,345 rows; the proposed bound
is 0.63%, 1.5× the observation"). The tolerances are fixed and conservative: half
of the observed row count as a volume floor, twice the observed freshness lag
(never under an hour; a table whose newest value is more than fifteen days old
gets no freshness draft, because the bound would then be less than twice what we
observed), 1.5× the observed null rate up to 50% (a column that is at least half
NULL gets none), a tenth of the observed numeric span on each side, a fifth above
the observed longest text value. A text-length draft is only proposed for a column
already labeled free text, because a short code's length today says nothing about
the length it should have. An enumeration is only proposed when the profile could
list every value without cutting one for display, so a draft never contains a
shortened spelling of a value the column holds.

It deliberately proposes nothing where a static threshold would be a guess:
custom SQL, CEL expressions, cross-table reconciliation, grouped checks and
aggregate checks are not derived, and a primary key gets no uniqueness draft
(the engine already enforces it, so the check could never fail). It also proposes
nothing for a column whose profile cannot support the check — a mostly-NULL
column, an estimated distinct count, a value set the sample could not enumerate
completely. Those columns are reported as skipped rather than filled with a
made-up bound.

### The queue does not accumulate duplicates

Suggestions are proposals about a fact, and the queue keeps **one pending proposal
per proposal identity**: one per column for a label, one per check type plus the
columns it covers for a check draft. Clicking *Suggest labels* or *Draft with AI*
again therefore never adds a second copy: a check-draft proposal that is still
pending is **refreshed** in place (the row keeps its identity, and its *refreshed*
time moves), while a column that already has a pending label proposal is skipped
before the model is called. The result toast reports what happened — created,
refreshed, skipped — and names the columns it derived no proposal for (up to six, with a count
for the rest).

A generation pass also reconciles proposals with reality:

- Columns that already have a semantic label are left out, and a pending label
  proposal for such a column is closed as *Superseded*.
- Check drafts already covered by a saved check are not queued, and a pending draft
  they cover is closed as *Superseded*.
- Proposals you already accepted or rejected are not proposed again. *Regenerate*
  (on the table's Data Quality tab and in the drafting workbench) re-analyzes those
  columns and checks; it still refreshes rather than duplicates.
- A pending proposal belongs to the pass that produced it. Running a different
  pass does not overwrite it: **AI label fields** will not put its default
  parameters back over a recommendation's observed ones, and *Draft with AI* is not
  overwritten by a derived pass either. The one exception is deliberate — a
  recommendation may replace a pending label-pass draft, whose parameters are the
  fixed defaults, with the thresholds it observed. *Regenerate* overrides this for
  the pass you are running. The queue write enforces this itself, so two passes
  running at the same moment cannot override each other.
- Submitting the same drafting job twice while the first one is still pending or
  running returns the first job instead of starting a second analysis.

Because there is only ever one pending proposal per identity, accepting or
rejecting acts on exactly that proposal: accepting a label writes it (and it will
not be suggested again while it exists), rejecting it keeps it out of later passes
until you explicitly regenerate. *Superseded* is not a decision — nobody accepted
or rejected it; the reason is recorded in its note.

Creating a check from a draft is idempotent as well: if the same draft is used to
create a check twice at the same time (two tabs, a retried request), only one of the
requests writes a check and the other is told the draft was already claimed. If the
check write itself fails, the draft returns to *Pending* so it can be retried.

A single row can also be adjusted in the check dialog or turned into a check on
its own; the dialog's target is fixed to the target the draft was written for, so
a draft can never be created against another target.
Decided suggestions stay visible under *All statuses* with who decided them and
their note. Nothing the model proposes is enabled or executed without a person
creating the check, and the model never sees raw sample rows.

## Settings

Settings → Data Quality controls: the model-analysis switch and provider,
whether failure samples are stored and how many, the **check time zone** (IANA
name, empty = UTC) used to resolve time variables in filters and custom SQL and
to resolve a cron schedule saved without a time zone, the default schedule a
target starts with when it gets its first check, the daily execution ceiling, the **daily AI analysis ceiling (L1+L2,
default 10,000)**, the daily column-analysis ceiling, the **concurrent LLM calls
(default 8, range 1–64)**, the columns per analysis, whether raw sample
values may be sent to the model, and the **weekly digest** schedule (see
[Reports, exports and the weekly digest](#reports-exports-and-the-weekly-digest)).
A read-only **Today's AI usage** card shows
used/limit for both analysis buckets, in-flight/concurrency and when the counters
reset (00:00 UTC). The counters are process-local: a restart starts them at zero.
Zero in a field means the server default; the page shows the effective values.
The daily execution ceiling only pauses the *scheduled* scans — **Run now** still
works when it is reached.

Settings → LLM providers configures each provider profile, including **Max output
tokens** (0 = server default 32,768; 256–131,072). It is the ceiling sent to the
model for every data quality analysis using that profile; unused tokens are not
charged. If the provider or model caps output lower, the request is rejected and
the failed target reports a provider-status diagnostic. The DQ limits and the
usage card are the place to raise the daily/concurrency budget when the defaults
are too low for your workload.

## Retention

Executions are kept 90 days, resolved issues 90 days, column profiles and the
model result cache 7 days, decided or superseded suggestions 90 days, finished AI drafting jobs
90 days, and captured raw LLM request/response debug entries 7 days; the
maintenance runner prunes them. Checks, open issues and accepted labels are not
pruned. The weekly-digest period table gains at most one row per week and is not
pruned.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| A check reports `ERROR(NO_READ_ONLY_SOURCE)` | The instance has no read-only data source, or it was removed. |
| `ERROR(UNSUPPORTED)` | The check's engine cannot express it (for example regex on SQL Server, or `PERCENTILE` outside PostgreSQL/SQL Server), or a column the check names was dropped. |
| A dry run fails with a parameter or SQL error | The message shows the compiled SQL and its bound values; check that the filter values and range/aggregate bounds match the column's type (for example a date written as a plain string, or a numeric bound for a text column). |
| Checks never run | The check is paused or archived, the instance is gone, or the daily execution ceiling was reached (the next day resumes it; **Run now** still works). |
| A cron expression is rejected when saving | It must be a standard five-field expression (minute hour day-of-month month day-of-week); shortcuts such as `@daily` are refused. Two occurrences must be at least 15 minutes apart, the expression must fire within five years, and the time zone must be a valid IANA name. A target whose time zone was left empty stores the workspace's check time zone. |
| A cron target did not run at the expected time | The expression is evaluated in the target's own time zone, shown in the header — not your browser's or the server's. A local time that does not exist on a daylight-saving transition day is skipped, and the repeated hour on the fall-back day fires once. A run missed while the server was down is not caught up: the next run is the next occurrence after the server returns. Occurrences closer than 15 minutes are refused when saving, so `*/5 * * * *` cannot be scheduled. The next-run time in the header is the authority. |
| I cannot change one check's schedule | Scheduling is per target: all enabled checks on the target run together on the schedule in its workspace header. To make one check run less often, open it and change its **Run cadence** (follow target, hourly, daily, weekly); a cadence can only be coarser than the target, and there is no per-check cron expression. For a rhythm independent of the target, move the check to its own target. **Run now** always runs it regardless of cadence. |
| An execution stays pending, or ends abandoned | The instance's connection budget stayed busy past the runner's wait, or the server restarted mid-run. A scheduled check is retried on a later tick of the same target cycle (at most three attempts); a manual execution is not retried, so use **Run now** again. |
| A check that used to run now ends `ERROR(UNSUPPORTED)` | The check uses a configuration this build no longer accepts, for example an aggregate `COUNT` (use a row count check instead) or a column type the engine cannot express. Re-create the check with the supported type. |
| No notification for a failure | The issue was already open (notifications fire on transitions), the check is muted, or more than ten checks failed in one target's scan batch and a summary was sent instead. |
| Model buttons are disabled | Model analysis is off, no allowed provider profile exists, or the caller lacks `suggest`/`explain`. |
| An AI drafting job failed | Open the **AI drafting jobs** panel and expand the target. A job-level error means the provider became unavailable or the daily analysis quota ran out after it started; a target-level diagnostic names the failure class. *Output limit* means the model hit the output-token budget before finishing its JSON: retry with a narrower intent or fewer columns (the budget is the provider profile's Max output tokens, default 32,768). Provider errors carry the provider/model and stop reason but not the provider's body; the server retries once at 4,096 tokens when a model rejects the requested budget, and if it is still rejected, lower the profile's Max output tokens or use a model that supports that output budget; quote the **correlation ID** to an operator, who can find the matching server log line (and, with runtime debug on, the raw request/response within seven days). Queued suggestions from earlier targets are kept. |
| An AI drafting job stays queued or running | The server processes one job at a time; a queued job starts on the next runner tick (about five seconds). If the server restarted mid-job, the job is marked failed on startup and is not resumed — submit it again. |
| An AI analysis is rejected as over quota | Settings → Data Quality → Today's AI usage shows used/limit and concurrency. Raise **Daily AI analyses** (L1+L2, max 1,000,000) or **Concurrent LLM calls** (1–64); the change applies to the next analysis without a restart. |
| A model rejects the output budget | The provider profile's **Max output tokens** is above what that model accepts. The server first retries that request once at 4,096 tokens; if it is still rejected, lower the profile budget (Settings → LLM providers) or use a model with a larger output cap. The failure shows a provider-status diagnostic. |
| A label suggestion cannot be accepted | Profile the columns first; label suggestions require profiles fresher than seven days. |
| *Recommend checks* proposes nothing, or far fewer checks than expected | The columns were never profiled (the server refuses to recommend without a profile fresher than seven days), every check the profile supports is already a saved check, or the proposals were already accepted/rejected. Rejecting a recommendation keeps it out of later passes until you use *Regenerate*. A column can also be listed as skipped because its profile cannot support a check (for example a mostly-NULL column, a value set the sample could not enumerate completely, or a value the profile had to shorten for display). |
| *Recommend checks* proposed a threshold that looks too loose or too tight | The bound is the observation plus a fixed tolerance and is a starting point, not a verdict: edit it in the check dialog before saving, or lower the tolerance by making the check stricter. A range check will legitimately fail when the data moves past today's extremes; widen it or replace it with an aggregate check once the shape is understood. |
| The trend chart is empty | The check has no judged execution yet (never run, or only `ERROR`/`NOT_EVALUATED` outcomes), or it is a schema drift check, which has no numeric value to plot. |
| The trend has no threshold band | The check was edited after its last judged run, so its current thresholds never judged the drawn points; the row count change is plotted without a band on purpose, because its thresholds bound a percentage change against a baseline. |
| Suggested thresholds are unavailable | The check has fewer than five judged executions; aggregate, row count change and schema drift checks have no suggestion; a row count check with an exact row count has nothing to suggest; and if the form's check type or measurement unit was changed, the suggestion describes the saved check and is disabled. |
| A row count change check reports `NOT_EVALUATED` on its first run | It has no baseline yet: the first completed run records it and the comparison starts with the second. Newly created checks default *empty data* to pass so the first run does not look like a problem; an older check keeps its saved setting, which can be changed under Details. |
| The row count change baseline seems stale | The baseline is the most recent completed execution with a non-zero measured count within seven days and two target intervals (for a cron target, the expression's nominal interval); zero-row and unmeasured executions are skipped, and the check falls back to its empty-data behavior when no baseline qualifies. A target that runs less often than once every seven days — a monthly or yearly cron schedule, for example — never has a fresh enough baseline. |
| Running *AI label fields* changed the recommendation thresholds | It should not: a pending recommendation is not overwritten by a label-pass draft. If a draft shows the default parameters instead, the recommendation was rejected/decided first, or *Regenerate* was used on the label pass. |
| A CSV export stops early | The selection is capped at 100,000 rows; the app warns when the file was cut. Narrow the filters (especially the time window) and export again. |
| "Too many concurrent exports" | Four exports already stream from this server. Wait for one to finish; a refused export writes nothing. |
| The weekly digest did not arrive | Check that the digest is enabled with a weekday/time/time zone, that at least one **enabled** notification channel has **Data quality digest** ticked (email channels also need a fixed address and a working SMTP setting), and that the server was up within 24 hours after the scheduled minute. A longer outage skips that week on purpose. |
| The digest email has no recipients | The digest has no per-person audience: an email channel must carry fixed addresses; "include the audience" cannot resolve anyone for it. |
| A check says it cannot be sampled | The method does not apply to this check: row count, row count change and freshness describe the whole table; referential integrity and cross-table checks read two relations; custom SQL, schema drift and CEL checks never sample. Remove the sampling or pick another check type. |
| *By percentage* is unavailable | The target is a view, or its engine has no reliable table sampling clause (MySQL, TiDB, MariaDB, OceanBase, StarRocks, Doris). Use *By row limit*. |
| A sampled check returned no failure sample | The failed rows are read from the same sample as the counts. If the sample held no failing row the evidence is empty while the verdict still stands — the counts decide it, not the evidence rows. |
| A filter value with a time variable is rejected | A filter value must be one whole variable (`${today-7d}`); quoting it or mixing it with text is refused. Check the name (`now`/`today`/`yesterday`), the unit (`m`/`h`/`d`) and the size (at most 366 days). A date variable cannot be compared with a numeric column. |
| The partition window looks a day off | Time variables resolve in the workspace's check time zone (Settings → Data Quality; empty means UTC). An integer partition column (`20261101`) is not a date to the engine — use a custom SQL check with an explicit cast. |
| A long timeout still ended abandoned | The 15-minute batch deadline is the hard limit for one target's scan; a per-check timeout only bounds that check's own query. |

The API surface, resource names, data model and invariants are documented for
maintainers in [.agents/docs/data-quality.md](../.agents/docs/data-quality.md).

## See also

- [.agents/docs/data-quality.md](../.agents/docs/data-quality.md) — maintenance reference (contracts, decisions, failure modes).
- [security-posture.md](security-posture.md) — the deliberate single-tenant and process-local decisions this feature inherits.
