# Data quality

Data quality watches the **data** in your databases, not just its schema. A rule
names a table or column and a typed check (row count, nulls, uniqueness, accepted
values, ranges, regex, string length, referential integrity, cross-column
relations, freshness, aggregates, grouped row counts, schema drift, cross-table
reconciliation, custom SQL or a CEL expression). A scheduler runs the rules, and
each execution records a status, the observed value, the thresholds it was
judged against, and (optionally) the rows that failed.

Everything is in the **Data Quality** section of the sidebar. The rule engine is
fully offline; three model-assisted features are opt-in.

## Before the first rule

1. **Give the instance a read-only data source.** Query rules run over the
   instance's `READ_ONLY` data source and never over the admin one; an instance
   without it cannot create or preview a query rule. Settings → Instances → the
   instance → Data sources → add a `READ_ONLY` account whose database grants are
   `SELECT` only. The schema-drift rule is the exception: it reads metadata and
   needs no data source.
2. **Check the permissions.** Reading rules, executions and issues is part of the
   member baseline. Creating, editing, running, acknowledging and resolving need
   `metaxisdata.dataQuality.rules.*` / `.issues.update` (workspace
   administrator or a custom role). Model-assisted paths also need `.suggest` or
   `.explain`.
3. *(Optional)* **Turn on model assistance.** Settings → Data Quality → enable
   *LLM analysis*, pick a provider profile, then decide whether failure samples
   are stored and (separately) whether raw sample values may leave the
   deployment. Everything else works with it off.

## Writing a rule

Data Quality → Rules → **New rule**. Pick the target from the metadata browser,
then choose the rule type — the list is grouped by family (quantity and timeliness,
values and relations, aggregates and reconciliation, structure and custom) and a
one-line description of the selected type sits under the control. Fill in the
parameters; the editor compiles the rule server-side and shows the SQL it will run
before you save. Saving validates the target, the columns and the parameters; a
rule that cannot compile is refused. A view or materialized view only offers the
table-level checks (row count, row count change, freshness, custom SQL, schema
drift), because those objects have no column-level type authority.

- **Schedule**: 15 minutes to 7 days. A new rule first runs on its next scheduled
  slot; use **Run now** to check it immediately.
- **Thresholds**: a count-type rule compares failed rows (or the failed
  percentage) with a warning and a failure level; an aggregate compares its value
  with a range; freshness compares the lag in seconds. Custom SQL rules always
  count failed rows — they have no row total to take a percentage of.
- **Empty data**: by default a query that returns nothing is `NOT_EVALUATED`,
  not a pass. Choose pass or fail per rule if that is the intent; row count,
  custom SQL and schema drift always produce a value, so the setting does not
  apply to them.
- **Filters** narrow the rows the rule reads (structured AND conditions only);
  write a custom-SQL rule when the condition needs more.
- **Preview** runs the compiled statement with `LIMIT 0` against the read-only
  source: it proves syntax and permission without scanning the table.
- **Schema drift** compares each column's type and nullability — plus, if you ask
  for it, order and extra columns — with the snapshot saved when the rule was
  created. Columns recorded by an older snapshot that did not store nullability
  stay unjudged until the rule is saved again. Use *Fill from target* to record
  the current definition.

## Browsing rules

Data Quality → Rules lists every rule with its target and latest execution. The
**Target scope** filter narrows the list to a subtree of the metadata tree: pick
an instance, then drill into a database, schema or table/view, and apply at any
level — choosing a table (or view) applies that table itself. The text search
matches a rule's title and description; rule type, dimension, severity and state
narrow further. Filters take effect when you press **Apply**.

## Executions and statuses

An execution is **one rule checked once**, on demand or by the scheduler. Open
Data Quality → Executions to watch them: every rule the scheduler or a manual
run started is one row, and a queued or running row is polled until it finishes.
The list filters by lifecycle state, outcome and trigger; open a row for the full
evidence behind it.

An execution's lifecycle is *pending* (planned, waiting for the runner),
*running*, or *completed*. A queued or running execution left behind by a stopped
server process is not retried: a startup sweep marks stale rows abandoned, so an
abandoned row means "the check never finished". The next scheduled slot runs the
rule again, and *Run now* checks it immediately. Once an execution completes, its
outcome is one of six statuses:

| Status | Meaning |
| --- | --- |
| `PASS` / `WARN` / `FAIL` | The check ran: within thresholds / past warning / past failure. |
| `ERROR` | The check could not run (connection, permission, timeout, an unparsable value). **This is not a data failure.** |
| `SKIPPED` | Deliberately not run: the target disappeared, the rule is archived, or the same rule was already executing. |
| `NOT_EVALUATED` | The check ran but had nothing to judge (no rows after the filter, a NULL timestamp or aggregate). |

An abandoned execution has no outcome. Failed executions can be expanded for
samples and the exact SQL; the execution detail also shows the rule revision,
engine version, duration, trigger, instance and, for a manual run, who started
it. Executions are the durable record: batching rules together for scheduling is
an internal detail.

Every rule has its own page: open its title in Data Quality → Rules, or the rule
name on an execution, an issue or a table's quality tab. It answers "is this rule
healthy?" first — the latest status and its observed value, the pass rate, and a
strip of the last 20 executions in one color per status (green pass, amber
warning, red fail, dark red execution error, grey skipped/not evaluated). Below
that sits the recent execution table, whose rows open the execution behind them,
and the rule's definition (target, type, schedule, revision, tags). *Run now* and
*Edit* appear there for callers who hold the matching permission; *Run now* opens
the execution it planned.

## Problems and notifications

A rule that starts failing opens an **issue**; it is the same issue until it
recovers, and its occurrence count grows while it keeps failing. Data Quality →
Issues lets you acknowledge it, resolve it with a reason, or mute it for a while.
When a rule passes again its issue resolves itself as *auto-recovered*. Each
issue links the first and the latest execution that observed the problem.

Issues produce in-app notifications (bell icon) for the rule's owner, or for the
workspace administrators when there is no owner; a recovered issue notifies the
same audience. Execution failures always notify the administrators and cannot be
muted. When more than ten rules fail in the same scheduler pass, one summary
replaces the per-rule messages. There is no email or webhook delivery.

## Profiles, labels and suggestions

The table detail page has a **Data Quality** tab: how many rules watch the table,
their recent executions, its open issues, and per-column profiles and semantic
labels. *Profile columns* computes null counts, distinct counts, extremes,
lengths, frequent values and value shapes with two queries; *Suggest labels*
asks the model to name each column's semantic type and proposes rule drafts from
a fixed mapping. Both only produce suggestions — nothing becomes a rule until a
person accepts and saves it.

The **Suggestions** page is the review queue: label proposals (accepting one
writes the label) and rule drafts (opening one pre-fills the editor).

## Settings

Settings → Data Quality controls: the model-analysis switch and provider,
whether failure samples are stored and how many, the default schedule for new
rules, the daily execution ceiling, the daily column-analysis ceiling, the
columns per analysis, and whether raw sample values may be sent to the model.
Zero means the server default; the page shows the effective values. The daily
execution ceiling only pauses the *scheduled* scans — **Run now** still works
when it is reached.

## Retention

Executions are kept 90 days, resolved issues 90 days, column profiles and the
model result cache 7 days, and decided suggestions 90 days; the maintenance
runner prunes them. Rules, open issues and accepted labels are not pruned.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| A rule reports `ERROR(NO_READ_ONLY_SOURCE)` | The instance has no read-only data source, or it was removed. |
| `ERROR(UNSUPPORTED)` | The rule's engine cannot express it (for example regex on SQL Server, or `PERCENTILE` outside PostgreSQL/SQL Server), or a column the rule names was dropped. |
| A dry run fails with a parameter or SQL error | The message shows the compiled SQL and its bound values; check that the filter values and range/aggregate bounds match the column's type (for example a date written as a plain string, or a numeric bound for a text column). |
| Rules never run | The rule is paused or archived, the instance is gone, or the daily execution ceiling was reached (the next day resumes it; **Run now** still works). |
| An execution stays pending, or ends abandoned | The instance's connection budget stayed busy past the runner's wait, or the server restarted mid-run. Such checks are not retried automatically; use **Run now** or wait for the next scheduled slot. |
| A rule that used to run now ends `ERROR(UNSUPPORTED)` | The rule uses a configuration this build no longer accepts, for example an aggregate `COUNT` (use a row count rule instead) or a column type the engine cannot express. Re-create the rule with the supported type. |
| No notification for a failure | The issue was already open (notifications fire on transitions), the rule is muted, or more than ten rules failed in the same scheduler pass and a summary was sent instead. |
| Model buttons are disabled | Model analysis is off, no allowed provider profile exists, or the caller lacks `suggest`/`explain`. |
| A label suggestion cannot be accepted | Profile the columns first; label suggestions require profiles fresher than seven days. |

The API surface, resource names, data model and invariants are documented for
maintainers in [.agents/docs/data-quality.md](../.agents/docs/data-quality.md).

## See also

- [.agents/docs/data-quality.md](../.agents/docs/data-quality.md) — maintenance reference (contracts, decisions, failure modes).
- [security-posture.md](security-posture.md) — the deliberate single-tenant and process-local decisions this feature inherits.
