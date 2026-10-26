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

- **Schedule**: 15 minutes to 7 days. A new check first runs on its next scheduled
  slot; use **Run now** to check it immediately.
- **Thresholds**: a count-type check compares failed rows (or the failed
  percentage) with a warning and a failure level; an aggregate compares its value
  with a range; freshness compares the lag in seconds. Custom SQL checks always
  count failed rows — they have no row total to take a percentage of.
- **Empty data**: by default a query that returns nothing is `NOT_EVALUATED`,
  not a pass. Choose pass or fail per check if that is the intent; row count,
  custom SQL and schema drift always produce a value, so the setting does not
  apply to them.
- **Filters** narrow the rows the check reads (structured AND conditions only);
  write a custom-SQL check when the condition needs more. A target-level filter
  from the contract is ANDed on top.
- **Preview** runs the compiled statement with `LIMIT 0` against the read-only
  source: it proves syntax and permission without scanning the table.
- **Schema drift** compares each column's type and nullability — plus, if you ask
  for it, order and extra columns — with the snapshot saved when the check was
  created. Columns recorded by an older snapshot that did not store nullability
  stay unjudged until the check is saved again. Use *Fill from target* to record
  the current definition.

Checks can also start from a model draft: draft in the target workspace's AI
copilot, or open **Data Quality → Suggestions** and use **Draft with AI** to pick
one or more tables and describe the check once. A queued draft appears inline in
its target's workspace, where it can be accepted or adjusted in the check dialog;
multi-target drafting runs as a background job, so you do not have to keep the
page open.

## Browsing targets and checks

Data Quality → **Targets** is the directory: one row per table or view that has
checks, with its check count, covered columns, latest result, open issues,
pending AI drafts and last run. **Start monitoring** opens the metadata picker
and lands on the chosen target's workspace ready to add its first check. Open a target to get its **workspace**: every check
on that table, grouped into dataset-level checks, cross-column checks and one
section per column, with the latest result and a strip of each check's recent
executions beside it. The workspace can run all of the target's checks at once,
add or edit a check without walking the target picker again, show which columns
have no check yet, and draft checks with the AI copilot on the right — drafts
appear inside the section they belong to until you create them as checks.

The Checks, Issues and Suggestions lists keep their filters in the address bar,
so refreshing or sharing a link returns to the same view.

Data Quality → **Checks** is the cross-target directory for audit and search:
every check with its target and latest execution, narrowed by the same target
scope, text search, check type, dimension, severity, state and owner filters.
It is read-only by design — a check is created and edited inside its target. Its
**Target scope** filter follows the metadata tree: pick an instance, then drill
into a database, schema or table/view, and apply at any level. Filters take
effect when you press **Apply**.

## Contracts and target-level settings

A target's **contract** is the target-level layer above its checks: defaults for
authoring and a record of what has been published. It lives on the same target
workspace, under the health summary.

- **Target filter** — conditions every check on the table or view is checked with.
  They are ANDed with each check's own filter at compile time, so changing the
  contract filter changes the next execution without rewriting any check. Written
  custom-SQL checks are not rewritten and stay outside the contract filter.
- **Authoring defaults** — a default schedule and default severity seed the New
  check dialog. They never rewrite an existing check or reschedule it.
- **Publish** — recording the live checks and settings as a version. Publishing
  does not gate execution: checks keep running as they are saved. It keeps a
  history with what changed since the previous version (added, changed, removed
  checks); republishing unchanged state is a no-op that returns the existing
  version. Publishing needs `metaxisdata.dataQuality.contracts.publish`; editing
  the settings needs `rules.update`.

## Executions and statuses

An execution is **one check checked once**, on demand or by the scheduler. Open
Data Quality → Executions to watch them: every check the scheduler or a manual
run started is one row, and a queued or running row is polled until it finishes.
The list filters by lifecycle state, outcome and trigger; open a row for the full
evidence behind it.

An execution's lifecycle is *pending* (planned, waiting for the runner),
*running*, or *completed*. A queued or running execution left behind by a stopped
server process is not retried: a startup sweep marks stale rows abandoned, so an
abandoned row means "the check never finished". The next scheduled slot runs the
check again, and *Run now* checks it immediately. Once an execution completes, its
outcome is one of six statuses:

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
schedule, revision, tags, filter, empty behavior). *Run now*, *Edit*,
*pause/resume* and *Delete* appear there for callers who hold the matching
permission.

## Problems and notifications

A check that starts failing opens an **issue**; it is the same issue until it
recovers, and its occurrence count grows while it keeps failing. Data Quality →
Issues lets you acknowledge it, resolve it with a reason, or mute it for a while.
When a check passes again its issue resolves itself as *auto-recovered*. Each
issue links the first and the latest execution that observed the problem.

Issues produce in-app notifications (bell icon) for the check's owner, or for the
workspace administrators when there is no owner; a recovered issue notifies the
same audience. Execution failures always notify the administrators and cannot be
muted. When more than ten checks fail in the same scheduler pass, one summary
replaces the per-check messages. There is no email or webhook delivery.

## Profiles, labels and suggestions

The table detail page has a **Data Quality** tab: how many checks watch the table,
their recent executions, its open issues, and per-column profiles and semantic
labels. *Profile columns* computes null counts, distinct counts, extremes,
lengths, frequent values and value shapes with two queries; *Suggest labels*
asks the model to name each column's semantic type and proposes check drafts from
a fixed mapping. Both only produce suggestions — nothing becomes a check until a
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

The **Suggestions** page is the review center. Suggestions arrive from two places:

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
- **Suggest labels** on a table's Data Quality tab: profile the columns first,
  then let the model name their semantic types and propose the deterministic check
  types those labels imply. The pass reports what it did, and its proposals appear in
  that tab's review card immediately.

The queue groups suggestions by target table and filters by status, kind, source
and target scope. Suggestions produced by a finished drafting job appear like any
other and are selected on the current page when the job completes. Select rows on
the current page to work in bulk (up to 200 per submit):

- **Check drafts** — *Create checks* opens one dialog where you set a schedule,
  severity, dimension and owner for the whole selection; expand a row to edit its
  title, severity or parameters. One submit creates them all, and each check is reported
  separately: a draft that cannot compile is reported with its error and stays
  pending while the rest are created. Creating a check accepts its draft.
  *Ignore drafts* records a rejection instead.
- **Column labels** — *Accept labels* writes the semantic labels immediately;
  *Reject labels* discards them. Accepting a label does not touch data.

### The queue does not accumulate duplicates

Suggestions are proposals about a fact, and the queue keeps **one pending proposal
per proposal identity**: one per column for a label, one per check type plus the
columns it covers for a check draft. Clicking *Suggest labels* or *Draft with AI*
again therefore never adds a second copy: a check-draft proposal that is still
pending is **refreshed** in place (the row keeps its identity, and its *refreshed*
time moves), while a column that already has a pending label proposal is skipped
before the model is called. The result toast reports what happened — created,
refreshed, skipped — and names the columns it left out (up to six, with a count
for the rest).

A generation pass also reconciles proposals with reality:

- Columns that already have a semantic label are left out, and a pending label
  proposal for such a column is closed as *Superseded*.
- Check drafts already covered by a saved check are not queued, and a pending draft
  they cover is closed as *Superseded*.
- Proposals you already accepted or rejected are not proposed again. *Regenerate*
  (on the table's Data Quality tab and in the drafting workbench) re-analyzes those
  columns and checks; it still refreshes rather than duplicates.
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
whether failure samples are stored and how many, the default schedule for new
checks, the daily execution ceiling, the **daily AI analysis ceiling (L1+L2,
default 10,000)**, the daily column-analysis ceiling, the **concurrent LLM calls
(default 8, range 1–64)**, the columns per analysis, and whether raw sample
values may be sent to the model. A read-only **Today's AI usage** card shows
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
pruned.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| A check reports `ERROR(NO_READ_ONLY_SOURCE)` | The instance has no read-only data source, or it was removed. |
| `ERROR(UNSUPPORTED)` | The check's engine cannot express it (for example regex on SQL Server, or `PERCENTILE` outside PostgreSQL/SQL Server), or a column the check names was dropped. |
| A dry run fails with a parameter or SQL error | The message shows the compiled SQL and its bound values; check that the filter values and range/aggregate bounds match the column's type (for example a date written as a plain string, or a numeric bound for a text column). |
| Checks never run | The check is paused or archived, the instance is gone, or the daily execution ceiling was reached (the next day resumes it; **Run now** still works). |
| An execution stays pending, or ends abandoned | The instance's connection budget stayed busy past the runner's wait, or the server restarted mid-run. Such checks are not retried automatically; use **Run now** or wait for the next scheduled slot. |
| A check that used to run now ends `ERROR(UNSUPPORTED)` | The check uses a configuration this build no longer accepts, for example an aggregate `COUNT` (use a row count check instead) or a column type the engine cannot express. Re-create the check with the supported type. |
| No notification for a failure | The issue was already open (notifications fire on transitions), the check is muted, or more than ten checks failed in the same scheduler pass and a summary was sent instead. |
| Model buttons are disabled | Model analysis is off, no allowed provider profile exists, or the caller lacks `suggest`/`explain`. |
| An AI drafting job failed | Open the **AI drafting jobs** panel and expand the target. A job-level error means the provider became unavailable or the daily analysis quota ran out after it started; a target-level diagnostic names the failure class. *Output limit* means the model hit the output-token budget before finishing its JSON: retry with a narrower intent or fewer columns (the budget is the provider profile's Max output tokens, default 32,768). Provider errors carry the provider/model and stop reason but not the provider's body; the server retries once at 4,096 tokens when a model rejects the requested budget, and if it is still rejected, lower the profile's Max output tokens or use a model that supports that output budget; quote the **correlation ID** to an operator, who can find the matching server log line (and, with runtime debug on, the raw request/response within seven days). Queued suggestions from earlier targets are kept. |
| An AI drafting job stays queued or running | The server processes one job at a time; a queued job starts on the next runner tick (about five seconds). If the server restarted mid-job, the job is marked failed on startup and is not resumed — submit it again. |
| An AI analysis is rejected as over quota | Settings → Data Quality → Today's AI usage shows used/limit and concurrency. Raise **Daily AI analyses** (L1+L2, max 1,000,000) or **Concurrent LLM calls** (1–64); the change applies to the next analysis without a restart. |
| A model rejects the output budget | The provider profile's **Max output tokens** is above what that model accepts. The server first retries that request once at 4,096 tokens; if it is still rejected, lower the profile budget (Settings → LLM providers) or use a model with a larger output cap. The failure shows a provider-status diagnostic. |
| A label suggestion cannot be accepted | Profile the columns first; label suggestions require profiles fresher than seven days. |

The API surface, resource names, data model and invariants are documented for
maintainers in [.agents/docs/data-quality.md](../.agents/docs/data-quality.md).

## See also

- [.agents/docs/data-quality.md](../.agents/docs/data-quality.md) — maintenance reference (contracts, decisions, failure modes).
- [security-posture.md](security-posture.md) — the deliberate single-tenant and process-local decisions this feature inherits.
