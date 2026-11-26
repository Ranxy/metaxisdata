# Data quality

Data quality watches the **data** in your databases, not just its schema. A check is a typed assertion over a table or column; this
build has 20 check types — row count, row count change, null rate, single-column and combination uniqueness, accepted values, ranges,
regex, string length, referential integrity, cross-column relations, freshness, aggregates, grouped row counts, segment checks, schema
drift, cross-table reconciliation, custom SQL, CEL expressions and standard codes. A scheduler runs the checks, and each execution
records a status, the observed value, the thresholds it was judged against, and (optionally) failed and passing rows.

Everything is in **Data Quality** in the sidebar. The check engine is fully offline; model assistance and the built-in quality agent
runtime are opt-in.

## Before the first check

1. **Give the instance a read-only data source.** Query checks run over the instance's `READ_ONLY` data source, never the admin one;
without it you cannot create or preview a query check. Under Settings → Instances → the instance → Data sources, add a `READ_ONLY`
account with `SELECT`-only grants. Schema drift is the exception: it reads metadata only.
2. **Check the permissions.** Reading checks, executions, issues and contracts is part of the member baseline. Creating, editing,
running, acknowledging and resolving need `metaxisdata.dataQuality.rules.*` / `.issues.update`; publishing a contract needs
`metaxisdata.dataQuality.contracts.publish`. Model-assisted paths also need `.suggest` or `.explain`.
3. *(Optional)* **Turn on model assistance.** Under Settings → Data Quality, enable *LLM analysis* and pick the provider profile and model the analyses use. The
switches for storing failure samples and for sending raw sample values are independent; the second is reserved and no model path
sends raw sample rows today. Everything else works with model assistance off.
4. *(Optional)* **Turn on quality agents.** An administrator creates an agent under Settings → Quality agents and turns off **Pause all
agents**; see [Quality agents](#quality-agents). Agents are independent of the check engine: with the runtime off, checks keep
running. An agent also needs a provider profile and a model it can call — the dialog cannot create one without them —
and **LLM analysis** must be on for the agent to reason.

## Adding a check

Data Quality → **Targets** → **Start monitoring** picks a table or view from the metadata tree and opens its workspace with the check
dialog already open. For a monitored target, open it and use **New check**, or the add button in the dataset, cross-column or column
section it belongs to. A view or materialized view offers table-level checks only (row count, row count change, freshness, custom SQL,
schema drift), because those objects have no column-level type authority.

**Templates.** The catalogue opens with **Templates**: one preset per built-in standard code plus common presets, filtered to what the
target can express. Picking one fills the form and writes nothing until you save. **Workspace templates** lists your team's saved
templates; applying one prefills the form and never saves or runs a check, while `templates.create` saves the current form as one. A
custom SQL template is refused because the statement names its tables and columns.

Fill in the parameters; the dialog compiles the check server-side and shows the SQL before you save, and a check that cannot compile is
refused. Filters, owner, tags and empty-result behavior are under **Details**.

- **Schedule**: scheduling belongs to the target, not to one check. All enabled checks on a target run together, so a new check joins
the target's next run; use **Run now** to check it immediately. A check's **Run cadence** can make it run *less* often — see [Check
run cadence](#check-run-cadence).
- **Thresholds**: a count-type check compares failed rows (or their percentage) with a warning and a failure level; an aggregate
compares its value with a range; freshness compares the lag in seconds. Custom SQL always counts failed rows, and a standard-code
check's percentage is over the rows its sample read.
- **Empty data**: by default a query that returns nothing is `NOT_EVALUATED`, not a pass. Choose pass or fail per check if that is the
intent; row count, custom SQL and schema drift always produce a value, so the setting does not apply to them. An empty standard-code
sample follows the same setting.
- **Row count change**: compares the current row count with the most recent completed execution that measured a non-zero count within
seven days and two target intervals (for cron, twice the nominal interval). The first run only records the baseline and is not judged;
a new check defaults *empty data* to pass.
- **Filters** narrow the rows the check reads (structured AND conditions only); write custom SQL for more. A contract filter is ANDed on
top.
- **Accepted values** takes its set from a fixed list (at most 200 values), another column of the same table, or a single-column query
read over the target's read-only source. The query is one `SELECT`/`WITH` statement (at most 8 KiB, time variables allowed) with the
reach of that connection; on SQL Server it must be a plain `SELECT`, because the server embeds it as a derived table. The server drops
NULLs from a dynamic set, *Allow NULL* controls the checked column only, and a query returning **no rows** fails every non-NULL value.
A fixed list on an **enum column** may only use the labels the type declares; the editor lists them as one-click values (PostgreSQL
reads them from the synced schema metadata, the MySQL family from the column's type), and save or preview refuses any other value with
the label list. Labels are matched the way the database compares them: exactly on PostgreSQL, through the column collation (usually
case-insensitive) on MySQL. If the enum gained a label since the instance was last synced, re-sync the instance. **Preview** before saving proves it
fits the column.
- **Segment check** groups rows by one to five columns and fails when a segment its recent runs saw is gone — the "a whole region
quietly disappeared" case. The baseline is the last three **usable** completed runs against the current table (1–10; truncated
observations do not count), and a segment is expected when at least two of them saw it. A missing segment is one failure; new segments
never fail the check. The first run only records the set, and an edit that changes the check's definition restarts the baseline.
Sampling and views do not apply.
- **Schema drift** compares each column's type and nullability — plus, if you ask for it, order and extra columns — with the snapshot
saved when the check was created. Columns from an older snapshot without nullability stay unjudged until the check is saved again;
*Fill from target* records the current definition.
- **Standard code** validates a text column against a national or industry code standard. It reads a bounded sample of its own
(`sample_rows`, 10,000 by default, at most 100,000) and validates every returned value in the server, so the shared *Sampling* setting
does not apply; see [Standard codes](#standard-codes).
- **Reference table** (cross-table reconciliation): the other side is any table, view or external table the workspace has synced. A
reference in the target's own database is read in the same statement; elsewhere it needs its own read-only connection, so **both
instances need a Read-only data source**, and a second database on the same PostgreSQL or SQL Server instance needs **at least two
connections** (the target's scan holds one). The check's filter and the contract filter bound the target side only; the reference has
its own filter. An unsynced reference cannot be reconciled against (write custom SQL), and a dropped one fails save or records an
`UNSUPPORTED` error until you re-point it.
- **Suggested thresholds**: once judged at least five times, the edit dialog offers warning and failure bounds from the check's history
(95th/99th percentile, or the observed row-count range widened by 10%). Using them only fills the form. Unavailable for aggregate, row
count change and schema drift checks, and while the type or unit differs from the saved check.

**Recommend checks** drafts checks from the columns' stored profiles with no model needed; the drafts join the suggestions queue and
nothing runs until a person creates the check (see [Profiles, labels and suggestions](#profiles-labels-and-suggestions)).

### Check run cadence

Every check runs whenever its target runs; the target's schedule is covered in [The target schedule](#the-target-schedule). When one
check is more expensive than the rest — a cross-table reconciliation or an aggregate over a large table — open it and choose a **Run
cadence**:

| Run cadence | How often the check runs |
| --- | --- |
| Follow target (default) | in every target cycle, like the rest of the checks |
| Hourly | at most about once an hour |
| Daily | at most about once a day |
| Weekly | at most about once a week |

The target's schedule is the **floor**: a check never runs more often than its target, and a denser cadence does nothing. A cadence is
measured on the target's own cycles: a check runs in a cycle only when the cycle its previous completed scheduled run belonged to is at
least the cadence period earlier. The comparison uses the cycle a run belonged to, so a late run or a skipped cycle cannot shift it, and
a skipped cycle is not made up later.

**Run now** ignores the cadence and does not count toward it. Changing the cadence does not change the check's definition or history; it
is not a schedule of its own, and there is no per-check cron expression. If a check needs an independent rhythm, move it to its own
target.

### Scoping and limiting a check

A check does not have to read everything on a partitioned or append-only table. Three settings under **Details** bound what one
execution reads and how long it may run:

- **Partition window**: add a filter condition whose value is a time variable — `${today}`, `${yesterday}` or `${now}`, optionally
shifted, for example `${today-7d}` (units `m`, `h`, `d`). The variable resolves when the check runs, in the workspace's **check time
zone** (Settings → Data Quality, UTC by default), so `created_at >= ${today-7d}` keeps meaning "the last seven days". Variables work
in filter values and custom-SQL text and are refused anywhere else; a value is either a literal or one whole variable.
- **Sampling**: *No sampling* (the default), *By row limit* ("read at most N rows of the filtered relation") or *By percentage* ("read
approximately P% of the table"). *By percentage* is offered for PostgreSQL and SQL Server tables only: MySQL-family engines have no
table-sampling clause and Doris/StarRocks sample storage units unreliably, so those tables and views offer the row limit only. A
sampled execution is marked as sampled and judged on the sample.
- **Query timeout**: how long one query of this check may run, 1–300 seconds; empty means the 60-second default. A target's scan batch
still ends after 15 minutes.

Sampling is not available for a check whose verdict describes the table as a whole (row count, row count change, freshness), for a check
that reads two relations (referential integrity, cross-table), or for custom SQL, schema drift, CEL and segment checks: custom SQL is
never rewritten, schema drift reads metadata, and CEL and segment checks have their own bounds.

## Standard codes

A **standard code** check validates one text column against a national or industry code standard. Five are built in — unified social
credit code, organization code, resident ID card, administrative division and industry classification — each enforcing its own value
shape and check digit where the standard defines one; a workspace **code table** adds your own code set, and *Custom code table* accepts
a value only on membership. Nothing is fetched at run time and no model is involved; the check reads a bounded sample, so its verdict is
a sample verdict.

- **Configuration**: *Column* (required, text), *Standard*, *Code table* (required for *Custom code table*, optional for a built-in
standard — which then requires both its own algorithm and membership), *Allow NULL* (off by default, so a NULL is a violation),
*Sample rows* (0 keeps the default of 10,000; 1–100,000) and failure and warning thresholds in failing rows or percent of rows. A
percentage threshold divides by the rows the sample read; the check's sample is its own, and an empty sample follows the empty-data
setting.
- **Engines and objects**: it runs on MySQL/TiDB/MariaDB/OceanBase, PostgreSQL, SQL Server, StarRocks and Doris. It is a column-level
check, so a view or materialized view cannot carry it.
- **Labels**: the five standards are also semantic labels (`UNIFIED_SOCIAL_CREDIT_CODE`, `ORGANIZATION_CODE`, `RESIDENT_ID_CARD`,
`ADMINISTRATIVE_DIVISION`, `INDUSTRY_CLASSIFICATION`). Once a column carries one, *Recommend checks* proposes a pending standard-code
check for that standard; nothing is created or enabled until a person accepts it.
- **Workspace code tables**: a code table is a named list of accepted codes you maintain in Settings → Data Quality (one code per line,
or a CSV whose first column is the code). The server trims, de-duplicates, sorts and caps the list (5,000 codes, each at most 256
bytes). A built-in standard with a code table keeps its own rules and additionally requires membership — useful when your
authoritative list is newer than the bundled snapshot. A built-in standard normalizes case; a custom table is an exact, case-sensitive
match. An empty table is not offered in the picker, *Custom code table* requires one, and deleting a referenced table is refused.
- **Snapshot and scope**: the division and industry tables are compiled into the server, so updating them needs a new release; retired
or newer codes can be absent. SG 3-2021 code sets (enterprise status, registration authority) are not bundled — import them as a
workspace code table or use *Accepted values* / *Referential integrity*. The unified social credit code's internal organization-code
digit is not verified, and the resident ID card check compares the province-level prefix only and rejects 15-digit legacy numbers.
- **Normalization and privacy**: values are upper-cased and trailing ASCII spaces are removed before comparison; leading whitespace is a
violation. Personal data is redacted before storage: on a column labeled **resident ID card** every check's failed-row evidence keeps
only the division prefix and the last four characters and marks the cell as masked; a standard-code resident-ID check does the same
without the label. A cached profile of such a column masks its extremes and drops its frequent values, and accepting the label redacts
an already-cached profile. Clearing the label does not restore redacted values; profile again. Other labels are not treated as
personal data yet.

## Browsing targets and checks

Data Quality → **Targets** is the directory: one row per table or view that has checks, with its check count, covered columns, latest
result, **quality score** (a dash until a check has been judged), open issues, pending AI drafts and last run. Open a target for its
**workspace**: every check on the table, grouped into dataset-level, cross-column and per-column sections, each with its latest result
and a strip of recent executions. Its header sums the target up, including the quality score and the **recent pass rate**, and lets you
run all of its checks at once, add or edit a check, and see uncovered columns and pending drafts.

If a table's **structure changed after its checks were saved** — a column added, dropped, renamed, retyped or made nullable, a primary
key changed, or a view's SQL rewritten — the target row and workspace show a **Target schema changed** badge. It means "review these
checks against the new shape"; it does not change, pause or fail them. **Acknowledge schema change** records that the checks were
reviewed and clears the badge. Saving a check re-baselines that check only, and only when the edit changes its definition — renaming it,
or changing its severity or owner, does not. The target keeps the badge until every check is re-baselined or acknowledged.

Data Quality → **Checks** is the cross-target directory for audit and search: every check with its target and latest execution, narrowed
by target scope, text, type, dimension, severity, state and owner. Checks are authored inside their target, but the directory supports
**bulk editing**: tick rows on the current page (up to 100) and choose **Edit checks** to set severity, dimension, owner, run state,
empty-data behavior or fail/warn thresholds in one action. A mixed selection asks you to choose a field deliberately; the edit applies
check by check and the result panel lists what changed, what failed and why. An edit that changes a definition fails for a check someone
else changed meanwhile; a severity, owner or run-state edit is last-write-wins. Thresholds can be set in bulk only for checks of the
same type. The **Target scope** filter follows the metadata tree and applies when you press **Apply**.

## Plan and declared coverage

A target workspace shows a read-only **Plan & coverage** card next to its contract. It is a projection, not a second set of checks: a
target with a contract is *planned* and carries the minimum coverage template for its criticality (volume and schema stability today);
one without a contract is *unplanned*, and the card says so instead of claiming coverage. Each required property is marked **Covered**
or **Gap** from the target's live checks.

## Contracts and target-level settings

### The target schedule

A target has **one schedule**, shown in its workspace header: for a fixed interval, "Schedule: every 2h · next in 12 minutes"; for a
cron schedule, the expression, its time zone and the next run. It applies to every enabled check on the table or view: when it comes
due, all of them run in one scan batch. Use the pencil to change it. The dialog offers two exclusive forms:

- **Runs every** — a fixed interval from 15 minutes to 7 days. This is the default, and the workspace's data quality settings seed the
interval a new target starts with.
- **Cron expression** — a standard five-field expression (minute, hour, day-of-month, month, day-of-week) evaluated in an IANA time
zone, for example `0 2 * * *` or `0 9 * * 1`. Occurrences must be at least 15 minutes apart; there is no upper bound. If a request
leaves the zone empty, the server resolves it to the workspace's **check time zone** and stores the resolved zone, so changing that
setting later does not move an existing target.

A change reschedules the target from now and never runs the checks immediately. While every check on the target is paused there is no
next run; resuming a check schedules the target from its saved schedule instead of firing at once. A check moved to another target joins
that target's schedule.

A cron expression is local wall-clock time: a time that does not exist on a daylight-saving transition day is skipped, and the repeated
hour on the fall-back day fires once. A run missed while the server was down is not caught up.

Editing a check cannot change the target's schedule — deliberate, so one table cannot have checks pulling in different directions. A
check's **Run cadence** can only make it run *less* often (see [Check run cadence](#check-run-cadence)).

A target's **contract** is the target-level layer above its checks: defaults for authoring and a record of what has been published. It
lives on the same workspace, under the health summary.

- **Target filter** — conditions every check on the table or view is checked with, ANDed with each check's own filter at compile time,
so changing the contract filter changes the next execution without rewriting any check. Custom SQL stays outside it.
- **Authoring defaults** — a default severity seeds the New check dialog and never rewrites an existing check. The schedule is not a
contract setting and is not part of published versions.
- **Publish** — records the live checks and settings as a version. It does not gate execution. Publishing needs
`metaxisdata.dataQuality.contracts.publish`; editing the settings needs `rules.update`.

## Executions and statuses

An execution is **one check checked once**, on demand or by the scheduler. If a scheduled batch times out or is abandoned before a check
runs, that check is retried in the same target cycle; the execution detail shows its attempt number, and a check is attempted at most
three times per cycle. Data Quality → **Executions** lists every run started by the scheduler or a manual run as one row; filter by
lifecycle state, outcome and trigger, and open a row for the evidence.

An execution's lifecycle is *pending* (planned, waiting for the runner), *running*, or *completed*. A queued or running execution left
behind by a stopped server process is marked abandoned by a startup sweep instead of being resumed: an abandoned row means "the check
never finished". A scheduled check is retried on a later tick of the same cycle, within its three-attempt budget; a manual execution is
not retried, so use **Run now** again. Once an execution completes, its outcome is one of six statuses:

| Status | Meaning |
| --- | --- |
| `PASS` / `WARN` / `FAIL` | The check ran: within thresholds / past warning / past failure. |
| `ERROR` | The check could not run (connection, permission, timeout, an unparsable value). **This is not a data failure.** |
| `SKIPPED` | Deliberately not run: the target disappeared, the check is archived, or the same check was already executing. |
| `NOT_EVALUATED` | The check ran but had nothing to judge (no rows after the filter, a NULL timestamp or aggregate). |

An abandoned execution has no outcome. A check that cannot run is surfaced separately: the target header and the workspace overview show
a **cannot run** count, and those checks are left out of *failing*, the recent pass rate and the quality score — a broken check must
never look like bad data or hide behind a green score.

Failed executions can be expanded for samples and the exact SQL. For a column check on a table with a primary key, each sample is a
numbered failed row led by the key columns, so two failures on the same column do not look like one row repeated; a grouped check
returns the grouping key instead. When a row-level check with a meaningful passing row ends `WARN` or `FAIL`, the execution also keeps a
bounded sample of the **passed** rows so failures can be compared with the population; it costs one extra bounded read (in-process
sample checks reuse their rows), uses the same limit and redaction, and is absent for a `PASS`, for non-verdict outcomes, and for
measurement, grouped, duplicate-key, segment-set and custom-SQL checks. A row whose comparison is unknown (a NULL in a range or length
comparison) is in neither sample. The report includes failed rows only. The execution detail also shows the revision, engine version,
duration, trigger, instance and, for a manual run, who started it.

Opening a check — from its row, the checks directory, an execution, an issue or a table's quality tab — opens a drawer that answers "is
this check healthy?" first: the latest status and observed value, the pass rate, and a strip of the last 20 executions in one color per
status. Below sit the recent execution table and the check's definition, with *Run now*, *Edit*, *pause/resume* and *Delete* for callers
holding the matching permission.

The drawer also plots the check's **observed value trend**: the value each recent judged execution measured, oldest to newest, colored
by verdict, with the warning and failure bounds as reference lines. Only judged runs are points, so an errored or unevaluated execution
shows in the history strip rather than as a fake zero. Bounds come from the execution that judged the current definition, so a check
edited since its last run says the band is not drawn. A row count change is plotted without a band; sampled points are marked. The
series stops at a check-type change.

## Lineage impact and possible causes

A failing check is one symptom; the question that follows is who depends on the table and what else may be wrong. The execution and
issue details carry an **Impact** section built from the recorded lineage:

- **Affected downstream** lists the tables, views and external tables the target's data reaches, nearest first, with each asset's own
quality state: unmonitored, all checks paused, or its worst latest result with its unresolved problem count and last run.
- **Possible upstream causes** lists the upstream targets whose checks failed or whose structure changed in the 24 hours before the run,
strongest first: a failing check outranks a structure change, then the nearer source wins, then the more recent evidence. Each
candidate links to the failing run.

Both lists come from the **recorded** lineage — view and materialized-view definitions, manual and reported SQL, and ingested
OpenLineage events — and are *not* a complete list of consumers: a BI tool, a script or an application that reads the table without
reporting its SQL is invisible here. An empty list means "nothing is recorded", not "nothing depends on it". The walk is bounded to two
levels downstream and a hundred assets, and the section says when it was cut short. The upstream window ends when the run started and
reaches 24 hours back (on an issue page it ends when the problem was first seen); a panel with no failing run says so instead of
implying a cause.

## Coverage and quality score

Two read-only numbers answer different questions: coverage says how much of the metadata is watched, and the quality score says how
healthy the watched checks are. Neither is stored or computed by a background job — each is assembled when the overview, a target row or
a report is read. Viewing them changes nothing and is not audit-logged.

### Coverage

Coverage compares the objects this deployment can monitor with the objects that have at least one enabled check.

- **The denominator** is every table, external table, view and materialized view in the metadata registry under a live instance (a
deleted instance contributes nothing). Column totals come from the registry's own column lists.
- **The numerator** is the distinct target with at least one enabled check, and the distinct columns those checks inspect, each counted
only while it still exists in the target's current registry column list — the column named in the configuration, not the display
snapshot stored on the check. Table-level and multi-column checks cover their target but no single column. A column dropped or renamed
by schema sync stops counting immediately, so coverage cannot exceed 100%.
- The overview breaks coverage down by instance and environment.
- An enabled check whose target has no registry row any more or whose instance was deleted is left out of workspace coverage and the
score: there is no denominator row for it. Such checks still appear with a *Target unavailable* badge and still count in the
enabled-check total, so coverage never exceeds 100%.

A target whose checks have never produced a verdict is **monitored but not judged**. Coverage is not a health measure: a table with no
check counts in the denominator and not the numerator, so a workspace without checks is at 0% coverage, never 100%.

### Consumption coverage

A second, narrower read answers "how much of the data other assets actually read is watched?": among registry tables and views whose
lineage shows another object reading them, how many have at least one enabled check. The overview shows that share and lists the
unmonitored consumed assets with the most consumers. The two ratios are not interchangeable: an unmonitored table that nothing reads
lowers only the first. The read is a separate request, writes nothing and is not audit-logged.

### Quality score

The quality score is 0–100, built from the **latest completed execution of each enabled check**; paused and archived checks do not enter
it.

Each judged check contributes a 0–1 score — the share of rows that passed (or, when a check measures no row total, `PASS` 1, `WARN` 0.5,
`FAIL` 0) — weighted by severity, and the score is the weighted average times 100. `ERROR`, `SKIPPED` and `NOT_EVALUATED` are not
judged. The same formula gives a target's score, the workspace score and the per-dimension and per-owner breakdowns; the workspace score
is weighted over checks, so a table with many checks carries more weight than a table with one.

A check is only scored from an execution against the target it currently watches. After a check is moved, it is not judged until it
completes a run there.

A dash (`-`) means **nothing was judged** — never "0" and never "100". It appears when no enabled check has a judged result: there is no
enabled check, none has run yet, or all ended in `ERROR`, `SKIPPED` or `NOT_EVALUATED`. The overview also shows how many enabled checks
are judged and how many are not.

### Recent pass rate

A target's row shows a second number: the pass share of its **most recent 20 completed executions** across its enabled checks: `PASS /
(PASS + WARN + FAIL)`. Paused and archived checks never occupy one of the 20 slots. The window follows a check to its current target:
executions recorded against the old table leave the old target's window. `ERROR`, `SKIPPED` and `NOT_EVALUATED` are excluded, and a
warning is not a pass. The score weights each check once by severity and looks at the latest execution, while the pass rate counts
executions, so do not compare them as one number. A dash means none of the recent executions was judged.

### Reading and trusting the numbers

Both numbers are derived, not stored: they are recomputed on every read, so they always match the executions you can open, and viewing
them changes nothing. A freshly configured workspace shows 0% coverage and a dash for the score; the score appears after the first
execution completes, and pausing a check drops it from both numbers immediately.

### Trust signals in the metadata browser

The metadata browser carries the same answer where a consumer looks — Data → browse, the global search and every table, view,
external-table and materialized-view page:

- **One column per object** shows a status point and a word (pass, warning, failing, error, not run), with a second line naming column
coverage and the last run: *covered 4/9 columns · 2 hours ago*. The asset page repeats the signal in its header and on its Data
Quality tab.
- **A table with no check is marked "not monitored"**. It is never green and never reads as 100%: nothing is known about it. The list
column is text only (a link would swallow the row click); the asset header badge and the Data Quality tab offer "Start monitoring".
- **Column coverage is per object**: how many of the object's current columns at least one enabled check reads, out of the columns the
registry holds. A check that names no column, a paused check and a column schema sync dropped cover nothing.
- Integrations over MCP get the same fields from `get_quality_signal`, which answers `monitored: false` for an object no check watches;
the same read-only surface exposes the target directory, one target's checks and problems, executions and their evidence, the lineage
impact and consumption coverage.

## Problems and notifications

A check that starts failing opens an **issue**; it is the same issue until it recovers, and its occurrence count grows while it keeps
failing. Data Quality → Issues lets you acknowledge it, resolve it with a reason, or mute it for a while; a passing check resolves its
issue as *auto-recovered*. Each issue links the first and the latest execution that observed the problem.

Issues produce in-app notifications (bell icon) for the check's owner, or the workspace administrators when there is no owner; a
recovered issue notifies the same audience. Execution failures always notify the administrators and cannot be muted. When more than ten
checks in one batch fail, one summary replaces the per-check messages. An administrator can also forward the same events to a webhook
under Settings → Notifications; see [notifications.md](notifications.md).

The workspace **quiet hours** and one-off **maintenance windows** under Settings → Notifications are the data quality maintenance
window: checks keep running and judging, and in-app notifications are still written to the bell list, but outbound deliveries to
webhook, chat and email channels are held until the window ends. A channel marked **Deliver during quiet hours** still sends. Nothing is
dropped, so an alert raised during the window arrives when it closes; see
[notifications.md](notifications.md#quiet-hours-and-maintenance-windows).

A **structure change** on a watched table is its own message, sent once when the change is synced: to the owners of that target's
checks, or the workspace administrators when none has an owner. Tables nobody checks are not reported. Repeated syncs of the same new
shape do not page again.

## Reports, exports and the weekly digest

Three things leave the screen; all are read-only snapshots of what the checks already recorded.

**CSV export.** The Executions, Issues and Targets directories each have **Export CSV**. It exports the **whole set your current filters
select**, not just the visible page: state, outcome, trigger, rule, target, instance, owner and time window all apply. The file is UTF-8
with a header row.

- The export stops at **100,000 rows**. When it does, the file ends with the rows it could fit and the app tells you it was capped —
narrow the filters to get the rest. Exporting is a **bulk read**, so it is recorded in the audit log (who exported, with which
filters, how many rows) and counts against the caller's request budget. At most four exports or reports stream at once per server; a
fifth request is refused with "too many concurrent exports" rather than queued. An export stops after 15 minutes.
- Text cells that begin with `=`, `+`, `-`, `@`, a tab or a carriage return get a leading single quote, so a check title cannot become a
spreadsheet formula; numeric, enum and timestamp columns are left as they are.
- Exported files contain metadata and verdicts only — never the sampled failure rows.

**Target report.** The target workspace has **Export report**. It downloads one self-contained HTML file (styles and chart embedded;
nothing loaded from the network) that you can open anywhere or forward as an attachment. It covers the last 30 days by default (up to
90) and contains the report scope, the current result summary and pass rate, the target's **quality score** (with how many checks were
judged and how many were not), the daily trend, the check list, the unresolved problems, a capped sample summary of failed checks, the
lineage impact anchored at the newest failure in the window, and the monitoring basis (target, instance, schedule, rule revisions,
engine versions).

The report is the one export that contains **real values from your data** — up to 5 sample rows per failed check (at most 20 checks),
capped at 2 MiB in total, with an explicit note when anything was cut. It prints failed rows only. Treat the file as sensitive:
forwarding it forwards those rows. Generating it is audited; the HTML body is never written to the audit log.

**Weekly digest.** Settings → Data Quality has a **Weekly digest** card: enable it, and pick the weekday, the `HH:MM` time and the IANA
time zone. The digest is a workspace-level summary of the last rolling seven days — executions by verdict, problems
opened/recovered/still open, and the owners and targets with the most to look at — delivered to the outbound notification channels, not
the bell icon.

- Configure the destination first under **Settings → Notifications**: create a channel and tick **Data quality digest** in its event
types. Delivery is **opt-in**: a channel with no event types selected receives the event notifications but *not* the digest.
- Email channels need at least one fixed address; the digest has nobody in particular to address, so "include the audience" cannot
resolve recipients.
- If the server is down at the scheduled minute, the digest is still sent when it comes back within **24 hours**. A longer outage skips
that week (one warning is logged); the next week's digest runs normally.
- Routing rules (owner/team) do not apply to the digest — it is one workspace summary, with owners used as grouping inside the text.
- A digest is queued once per ISO week; retries reuse the same delivery record, so a restart cannot send the same week twice.

## Profiles, labels and suggestions

The table detail page has a **Data Quality** tab: how many checks watch the table, its open issues, and per-column profiles and semantic
labels. *Profile columns* computes null counts, distinct counts, extremes, lengths, frequent values and value shapes; *AI label fields*
asks the model to name each column's semantic type and proposes check drafts; *Recommend checks* derives drafts from the profile and
needs no model. These entry points only produce suggestions. When proposals are waiting, the tab shows a **Suggestions awaiting review**
card: each row names the column or draft, the proposed label or check type and severity, the confidence and the reason. Label proposals
are accepted or rejected there; a check draft opens in its target's workspace, where the dialog can be pre-filled and created on save.
The card needs `.suggest`.

The **Suggestions** page is the review center. Suggestions arrive from three places:

- **Quality agents.** An active agent's triage pass drafts checks for uncovered targets in its database scope, and an agent with *Adopt*
can accept low-risk drafts on its owner's behalf under its guardrails (see [Quality agents](#quality-agents)). Agent drafts are
ordinary proposals; an automatic adoption is recorded on the agent.
- **Recommend checks** on a table's Data Quality tab: profile the
columns first, then derive checks and starting thresholds. It uses no model, so it works with model analysis off, and it is the only
pass that proposes an observed threshold. Re-running it refreshes its pending drafts.
- **AI label fields** on a table's Data Quality tab (and in each field's section on the target workspace): the model names each column's
semantic type and proposes the deterministic check types those labels imply. It needs model analysis on.

The queue groups suggestions by target table and filters by status, kind, source and target scope. Select rows on the current page to
work in bulk (up to 200 per submit). *Create checks* opens one dialog where you set severity, dimension and owner for the whole
selection; expand a row to edit its title, severity or parameters. One submit creates them all, each reported separately: a draft that
cannot compile is reported with its error and stays pending while the rest are created. Creating a check accepts its draft; *Ignore
drafts* records a rejection. *Accept labels* writes labels immediately and *Reject labels* discards them.

### What a recommendation reads, and what it will not propose

A recommendation is only as good as the profile behind it. The pass refuses a column whose profile is missing or older than seven days,
so *Recommend checks* runs the profile step first when the reader may. The tolerances are fixed and conservative: half the observed row
count as a volume floor, twice the observed freshness lag (never under an hour), 1.5× the observed null rate up to 50%, a tenth of the
observed numeric span on each side, and a fifth above the observed longest text value. A text-length draft is only proposed for a column
already labeled free text. Each draft's reason cites the observation and the arithmetic.

It deliberately proposes nothing where a static threshold would be a guess: custom SQL, CEL expressions, cross-table reconciliation,
grouped and aggregate checks are not derived, and a primary key gets no uniqueness draft (the engine already enforces it). It also
proposes nothing for a column whose profile cannot support the check — a mostly-NULL column, an estimated distinct count, a value set
the sample could not enumerate — and reports those columns as skipped rather than filling in a made-up bound.

### The queue does not accumulate duplicates

The queue keeps **one pending proposal per proposal identity**: one per column for a label, one per check type plus the columns it
covers for a check draft. Running *Suggest labels* or *Recommend checks* again therefore never adds a second copy: a pending check draft
is **refreshed** in place, and a column with a pending label proposal is skipped before the model is called. A generation pass also
reconciles proposals with reality: a column that already has a label is left out and its pending proposal closes as *Superseded*; a
draft already covered by a saved check is not queued and its pending copy closes as *Superseded*; proposals you already decided are not
proposed again unless you *Regenerate*. A pending proposal belongs to the pass that produced it — a label pass will not put its fixed
defaults back over a recommendation's observed ones. Accepting or rejecting acts on exactly that one proposal: accepting a label writes
it and it is not suggested again while it exists; rejecting keeps it out until you regenerate. *Superseded* is not a decision. Creating
a check from a draft is idempotent: two simultaneous submissions create one check and the other is told the draft was claimed; if the
check write fails, the draft returns to *Pending*. Decided suggestions stay visible under *All statuses* with who decided them and their
note. Nothing is enabled or executed without a person creating the check, and the model never sees raw sample rows.

## Quality agents

A **quality agent** is the built-in autonomous runtime for this workspace. It is not an API client and it has no token: an agent is one
identity with one configuration — an owner, databases, capabilities, a report schedule, adoption guardrails and budgets — and the server
runs it in-process. Everything it does is scoped to the databases you give it; it discovers their tables and views, watches them on a
schedule, reacts to failed checks, drafts checks into the review queue and escalates to its owner when it needs a person.

### Where agents live

- **Data Quality → Agents** (`/data-quality/agents`) lists the agents you may see: administrators see every agent, other members see the
ones they own.
- **An agent's home** is its working page: the conversation with its owner, its **Work sessions**, its **Patrol reports**, its
**Needs you** attention list, its auto-adoption setting, and its scope and state. A breadcrumb and the **Conversation / Work sessions**
tabs keep the agent and its two views visible; the attention list leads with each target and expands in place instead of opening a
reply dialog.
- **Work sessions** (`/data-quality/agents/{workspace}/{id}/sessions`) lists one long-lived conversation per target the agent watches.
Opening one shows the full transcript: what woke the agent, its answer, its reasoning when the model returns it, every tool call with
its arguments and result, and the runner's notes. The transcript opens on the newest message and keeps up with a running session while you
stay at the bottom; scroll back to read history and the previous page loads when you reach the top. A **Jump to latest** control appears
when you have scrolled away, with the number of messages that arrived meanwhile, and **Show more** opens a blocked session's full reason.
The run-state rail also shows how full the model's context is: the tokens the last model call used against the model's context window, where
the engine starts compacting, and which model produced the reading. The reading is provider-reported when the provider returns usage and
marked **estimated** otherwise; a session without a model has no reading. A new session shows that the input has not been measured yet until
its first model wake.
The owner and administrators can read it; only an administrator can resume a stuck
session.
- **Settings → Quality agents** (`/settings/quality-agents`) is where an administrator creates and edits agents and turns the runtime on
or off.

### Turning the runtime on

Settings → Quality agents has one workspace-wide switch, **Pause all agents**. It starts paused; while it is on, no agent wakes, no
report is generated and no failure event is delivered. Each agent also has its own state, so one can be paused without stopping the
others.

Agents reason only while **LLM analysis** is on under Settings → Data Quality; that switch is independent of the provider and model you
give an agent. With it off, an agent still surveys deterministic evidence and keeps its conversation open, but triage waits for a person.

### Creating an agent

An administrator creates an agent with:

- **Owner** — the person the agent works for. The owner is the only person who can talk to it; the owner and administrators can read its
home, work sessions, reports and attention list, but administrators cannot hold a conversation with it.
- **Language** — English or Simplified Chinese, or *follow the owner*. The agent writes owner-visible answers, reports and escalations in
this language; table names, column names and other identifiers stay unchanged. Raw work-session internals remain English.
- **Model** — the LLM provider profile and the model the agent calls. Both are required, and the model is chosen explicitly: one profile
can enable several models at different prices and quality, so the agent never inherits "whichever model comes first". The list offers
every profile that enables at least one model; a profile that enables several asks you to pick one. Every model call the agent makes —
wakes, compaction, triage and the drafts it writes — uses this pair. If the profile or model is later deleted, or all its models are
disabled, the agent stops reasoning instead of silently switching: its conversation answers with the deterministic scope summary, a
target that still needs triage is not admitted until a model is back (its enrollment row records `no-model`), and Settings → Quality
agents marks the pair as unavailable until you choose another. Editing the agent's other fields still works while the pair is stale;
only a changed pair is validated. Agents do not follow the workspace pair in Settings → Data Quality; that setting is the
analyses' provider and model.
- **Database scope** — the databases the agent may watch. Every table, external table, view and materialized view in them is discovered
and either triaged (no checks yet) or monitored; there is no per-target enrollment screen.
- **Capabilities** — each selected capability shapes the prompt and tool set the agent runs with, composed from the agent's product,
capabilities and adoption settings: *Suggest* (draft checks from what it learns), *Adopt* (accept low-risk drafts for the owner under
the guardrails below, and adopt pending drafts when the owner asks in conversation) and *Patrol* (the scheduled review of
the scope and its reports). A capability change takes effect on the next wake; nothing restarts.
- **Report schedule** — off, daily or weekly, with a time, a time zone and (for weekly) a weekday.
- **Adoption guardrails** — off, paused or active; the eligible rule types, a minimum model confidence, a daily ceiling, and whether
critical assets may be adopted. *Paused* creates an adopted check disabled for the owner to enable; *Active* creates it enabled. The
owner can turn adoption off or back to paused; the rest is administrator-managed. A draft carries no dimension of its own: the adopted
check takes the dimension its rule type measures, the same default the check editor offers.
- **Budgets** — tokens and wakes per hour, concurrent sessions, new sessions per hour, active sessions and conversation messages per
hour. Zero means the server default; the server's hard cap still applies.

Saving keeps the agent in **Draft** until an administrator starts it. An **Active** agent runs; a **Paused** agent keeps its
configuration and identity but wakes nothing; a **Disabled** agent is permanent — create a new one instead.

### What an agent does

- **Discovers and triages.** New targets in scope are queued by priority, targets with checks or a recent failure first. A target
without checks gets one *triage*: the agent reads its schema, profiles, labels and lineage consumers, drafts checks into the
suggestions queue and records what it concluded. That target then sleeps for its review interval (about 30 days) and wakes again when
checks are created on it, a check fails or its owner asks.
- **Monitors.** Covered targets are reviewed on the agent's schedule and when a check fails or errors; each wake continues the same
conversation for that target, and the agent can record verified asset facts to remember across wakes.
- **Converses with its owner.** The owner's questions join the same long-lived conversation. An agent without a configured model still
answers with a deterministic scope summary.
- **Reports.** On its schedule the agent produces a deterministic patrol report for its scope — scope health, targets and coverage,
enabled checks, executions by verdict, new and open problems, pending proposals — and appends a note to the conversation. Its two
closing sections answer what the owner asks: what the agent fixed by itself this period (work items it closed, broken down by
failure class) and what is waiting for a person (work items it escalated or froze, work still in progress, pending proposals, open
issues). Reports are generated from records, do not call a model, and have no on-demand variant.
- **Escalates.** Four things become an **attention** item: a conversation blocked on a person, a proposal still waiting after an
adoption pass, a work item the agent cannot finish with a change only a person may make, and a report that found new failures. The proposal item separates what policy skipped from what failed to write and
names the most recent write failure. A work item the agent cannot finish arrives as one concrete change: the check, the config
field, the value it would take, what kind of change that is and the agent's reason. The value has already passed the same
validation a save runs, so it will store as written, and nothing has changed until you edit the check yourself — which is why the
agent does not offer it as something it did. The owner answers or dismisses it from the agent's home; answering resumes the target's session,
and a reply can ask the agent to adopt the pending drafts, while the proposal item offers an **Approve pending drafts** action to the
agent's owner or an administrator; a person's approval does not need adoption to be enabled. The item closes once no pending draft is left,
whether the agent adopted them or a person created or rejected the last one. The owner also gets an in-app notification.
- **Shows what it is carrying.** The agent's home lists its **work items**: the checks it could not settle by itself, each with the
  failure class, the repair attempts spent and where it stands. An item that needs a person also appears in the attention list above
  it, as a proposal to apply or a decision to make; the rest are the agent's own to-do list, visible so that "what is the agent stuck
  on" has an answer that does not require reading a transcript.
- **Shows whether adoption is working.** The agent's home reports the last 24 hours — checks the agent adopted, checks a person
approved, drafts that failed to write, and drafts still pending — plus the most recent write failure. **Approve pending drafts**
creates the agent's pending drafts as your decision: the approval writes through the internal create path, so it needs no rule-write
permission of your own, the check is attributed to you and owned by the agent's owner (or by you when the agent has none), and the
check starts enabled. Neither the rule-type allowlist, the confidence threshold, the critical-asset switch, the agent's current
database scope nor the agent's daily autonomous ceiling applies. Each draft is still validated; a draft whose rule cannot be written
is reported, recorded as a failure in the last 24 hours, and stays pending.
The action is available to the agent's owner and to administrators whether or not adoption is enabled, and it narrows to one
target when opened from a proposal item. The owner can also ask the agent in conversation to adopt pending drafts: with no drafts
named the agent applies its guardrails to the target the conversation names, while drafts the owner names count as the owner's
explicit decision — bypassing the allow-list, confidence and critical-asset switch, but never the database scope, the daily
ceiling or rule validation. The daily ceiling therefore bounds what the agent adopts on its own or through that conversation
tool; a person's approval is recorded under its own origin but never spends it.
- **Can object to its own drafts.** A draft the agent distrusts — a check it expects to report structural false positives, a column
  that is usually empty — is flagged with its reason, and a flagged draft is never adopted automatically: it waits for a person, who
  reads the objection in the review queue beside the draft. Flagging is a doubt, not a refusal: you can still approve the draft.
- **Does not write freely.** An agent never approves on its own. It may adopt drafts the owner explicitly asks for — applying the
configured guardrails, or accepting the drafts the owner names — bounded by its database scope and the daily ceiling, always
through the normal check path with rule validation and the audit ledger. The agent page's **Approve pending drafts** is the
person's own decision: it turns the agent's queued drafts into checks without the guardrails or the ceiling, but with rule
validation, the audit ledger and the person's attribution. Publishing a contract, editing a check's definition, archiving or resuming a check, rejecting a draft and changing settings remain human-only; the agent's route to a definition it may not change is `propose_revision`, which asks the owner for one value and escalates the work item. The one exception is an agent delegated at **tune** level on that target: it may change a threshold or a bound itself, but only when re-judging the check's recent runs under the new value leaves every failing run failing — a change that would turn a red run green is refused and has to be proposed. Two things it may do without a person: a check that cannot run at all may be paused — the case is escalated, and a person still decides whether to resume, archive or replace it — and one kind of failure it may repair outright: a check that reports its target's structure changed gets its saved column snapshot aligned with what the registry says now, its fingerprint updated with it and the run that proves the repair planned immediately, so the work item closes on a real verdict instead of waiting for the next scheduled slot. That repair writes a value the registry owns, never a threshold or a bound. Its other writes are proposals and asset facts inside its own runtime, an adopted check is owned by
the agent's owner (or by the approver when there is none), and MCP stays read-only.

### Budgets, blocks and work sessions

An agent that spends its hourly token or wake budget is **blocked** with `token-budget` or `wake-budget` and recovers at the hour
boundary. A target whose database left the agent's scope is blocked with `out-of-scope`, and a target of a disabled agent with
`agent-disabled`; both recover on their own once the cause is cleared. Other blocks need the owner. Blocked states appear in the
attention list and on the **Work sessions** page. Its transcript is read-only for the owner; an administrator can resume a stuck
session there, and the resume action is recorded in the audit log.

### External consumers

There is no agent token and no external API through which an agent can be called or can write. Integrations that need quality data use
the **MCP** server instead: its eight read-only quality tools expose the target directory, one target's checks and problems, the problem
ledger, executions and their evidence, one object's trust signal, the lineage impact and consumption coverage. Those tools cannot
create, change or run anything. See [mcp.md](mcp.md).

## Settings

Settings → Data Quality controls the model-analysis switch and the provider profile and model the analyses use (both empty means the
first enabled model of the first enabled profile; one without the other is not accepted), failure-sample storage and count (20 by
default, at most 100),
the **check time zone** (IANA name, empty = UTC) used to resolve time variables in filters and custom SQL and a cron schedule saved
without a zone, the default schedule a target starts with, the daily execution ceiling (only scheduled scans pause at it — **Run now**
still works), the daily AI analysis ceiling (L1+L2, default 10,000), the concurrent LLM calls (default 8, range 1–64), whether raw
sample values may be sent to the model, and the **weekly digest** schedule. A read-only **Today's AI usage** card shows used/limit for
both analysis buckets and concurrency; the counters restart at zero with the process.

The **Code tables** card lists the workspace code tables used by standard-code checks and lets you create, edit (title, description,
values) and delete them; the editor accepts a pasted list or a CSV upload parsed in the browser (a header row is imported as a value, so
remove it). Deleting a table that a check or rule template still references is refused.

Settings → LLM providers configures each provider profile and each model it enables. **Context window** (0 = server default
262,144; a positive integer up to 10,000,000) is the model's combined request and response capacity; a quality agent uses it to decide when to compact
a long conversation. **Max output tokens** (0 = server default 32,768; 256–131,072) is the ceiling sent to the model for every call
using that model, and it must stay below the context window. Unused tokens are not charged. If the provider caps output lower, the
request is rejected and the failed analysis reports a provider-status diagnostic.

Settings → Quality agents (`/settings/quality-agents`) is the agent control panel: the **Pause all agents** switch, the agent list, and
the create/edit dialog for each agent, including the provider profile and model each agent runs on. A model can be pinned only when its
output budget is at least 2,048 tokens and its capacities leave room for compaction; a model too small for an agent is reported as
unavailable rather than silently swapped. The agent's home is where its owner
talks to it, reads its reports and answers its attention.

## Retention

Executions are kept 90 days, resolved issues 90 days, column profiles and the model result cache 7 days, decided or superseded
suggestions 90 days, and captured raw LLM request/response debug entries 7 days; the maintenance runner prunes them. Checks, open issues
and accepted labels are not pruned. Agent conversation transcripts (`quality_agent_message`), per-agent reports and attention rows are
**not pruned** in this release, so a workspace with many monitored targets accumulates rows.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| `ERROR(NO_READ_ONLY_SOURCE)` | The instance has no read-only data source, or it was removed. |
| `ERROR(UNSUPPORTED)` | The engine cannot express the check, a named column was dropped, or the saved configuration is no longer accepted after an upgrade. |
| A dry run fails with a parameter or SQL error | The message shows the compiled SQL and bound values; check that filters and bounds match the column's type. |
| A value is rejected as "not a label of the enum type" | The column is an enum: use one of the labels the message lists. An enum changed recently may need the instance re-synced first. |
| Checks never run | The check is paused or archived, the instance is gone, or the daily execution ceiling was reached (**Run now** still works). |
| A cron expression is rejected | It must be five fields, two occurrences at least 15 minutes apart, fire within five years, and use a valid IANA zone. |
| A cron target did not run at the expected time | The target's own time zone applies; DST gaps are skipped and downtime is not caught up. The header's next-run time is the authority. |
| An execution stays pending or ends abandoned | The connection budget was busy or the server restarted. Scheduled checks retry later in the cycle (three attempts); manual runs do not. |
| No notification for a failure | The issue was already open, the check is muted, or more than ten checks failed and one summary replaced them. |
| A model action is unavailable | Model analysis is off, or the caller lacks `.suggest` / `.explain`. With the switch on but no enabled model, the action is still offered and the call fails with `FailedPrecondition`. |
| An AI analysis is rejected as over quota | Today's AI usage shows the limit; raise the daily AI ceiling or concurrent LLM calls. The change applies without a restart. |
| A model rejects the output budget | The model's **Max output tokens** exceeds the provider cap; after one retry at 4,096, lower the budget or use another model. |
| A label suggestion cannot be accepted | Profile the columns first; suggestions need profiles fresher than seven days. |
| *Recommend checks* proposes nothing | No fresh profile, every supported check is already saved, or proposals were already decided; unsupported columns are reported as skipped. |
| A recommended threshold looks wrong | The bound is the observation plus a fixed tolerance, a starting point to edit before saving; a range check legitimately fails past today's extremes. |
| A row count change check reports `NOT_EVALUATED` on its first run | No baseline yet: the first run records it and the comparison starts with the second. |
| A segment check reports `NOT_EVALUATED` | No usable baseline, or the segment set was truncated; if the set exceeds 500, narrow the filter or group by coarser columns. |
| Suggested thresholds are unavailable | Fewer than five judged runs, an excluded check type, an exact row count, or a changed type or unit. |
| The trend chart is empty or has no band | Only judged runs are plotted; a check edited after its last judged run has no band, and a row count change is plotted without one. |
| A CSV export stops early | The selection is capped at 100,000 rows; narrow the filters and export again. |
| "Too many concurrent exports" | Four exports already stream; wait for one to finish. |
| The weekly digest did not arrive | Check the digest schedule, an **enabled** channel with **Data quality digest** ticked, and that the server was up within 24 hours. |
| The digest email has no recipients | Email channels need fixed addresses; "include the audience" cannot resolve anyone. |
| A sampled check returned no failure sample | The counts decided the verdict; the sample held no failing row. |
| A time-variable filter is rejected or looks a day off | Use one whole variable with a valid name, unit and size (at most 366 days), unquoted; it resolves in the workspace's check time zone. |
| A long timeout still ended abandoned | The 15-minute batch deadline bounds the whole scan; a per-check timeout bounds only its query. |
| An agent never wakes | The workspace runtime is paused, the agent is not Active, or its database scope is empty. |
| An agent is blocked with `token-budget` or `wake-budget` | It spent its hourly budget and recovers at the hour boundary; an administrator can raise the budget. |
| An agent is blocked with `out-of-scope`, `agent-disabled` or `needs-human` | The scope changed, the agent was disabled, or the model reported the same blocker three times; fix the cause, then answer the attention item. |
| The owner cannot talk to the agent | Only the owner may; administrators can read but not send. |
| An agent's answer is a fixed scope summary | Model analysis is off under Settings → Data Quality, or the agent's provider profile or model is unavailable (deleted, or all its models disabled); turn the switch on, or restore it under Settings → LLM providers, then edit the agent. |
| A target the agent never checked stays unchecked | Triage admission needs a model; with the analysis switch off or the agent's pair unavailable it is not admitted. Fix the provider and the target is admitted on the next scan. |
| The agent dialog offers no provider | No provider profile enables a model; create one or enable a model under Settings → LLM providers. |

## See also

- [.agents/docs/data-quality.md](../.agents/docs/data-quality.md) — maintenance reference for contributors: contracts, decisions,
failure modes, and where the code and tests live.
- [notifications.md](notifications.md) — the inbox, outbound channels and the delivery body.
- [mcp.md](mcp.md) — the read-only MCP surface, including the eight quality tools.
- [security-posture.md](security-posture.md) — the deliberate single-tenant and process-local decisions this feature inherits.
