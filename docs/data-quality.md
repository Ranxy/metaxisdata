# Data quality

Data quality watches the **data** in your databases, not just its schema. A rule
names a table or column and a typed check (row count, nulls, uniqueness, accepted
values, ranges, regex, string length, referential integrity, cross-column
relations, freshness, aggregates, grouped row counts, schema drift, cross-table
reconciliation, custom SQL or a CEL expression). A scheduler runs the rules, and
each run records a status, the observed value, the thresholds it was judged
against, and (optionally) the rows that failed.

Everything is in the **Data Quality** section of the sidebar. The rule engine is
fully offline; three model-assisted features are opt-in.

## Before the first rule

1. **Give the instance a read-only data source.** Query rules run over the
   instance's `READ_ONLY` data source and never over the admin one; an instance
   without it cannot create or preview a query rule. Settings → Instances → the
   instance → Data sources → add a `READ_ONLY` account whose database grants are
   `SELECT` only. The schema-drift rule is the exception: it reads metadata and
   needs no data source.
2. **Check the permissions.** Reading rules, runs and issues is part of the
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
choose the rule type, and fill in the parameters; the editor compiles the rule
server-side and shows the SQL it will run before you save. Saving validates the
target, the columns and the parameters; a rule that cannot compile is refused.

- **Schedule**: 15 minutes to 7 days. A new rule first runs on its next scheduled
  slot; use **Run now** to check it immediately.
- **Thresholds**: a count-type rule compares failed rows (or the failed
  percentage) with a warning and a failure level; an aggregate compares its value
  with a range; freshness compares the lag in seconds.
- **Empty data**: by default a query that returns nothing is `NOT_EVALUATED`,
  not a pass. Choose pass or fail per rule if that is the intent.
- **Filters** narrow the rows the rule reads (structured AND conditions only);
  write a custom-SQL rule when the condition needs more.
- **Preview** runs the compiled statement with `LIMIT 0` against the read-only
  source: it proves syntax and permission without scanning the table.

## Runs, results and statuses

A run is one instance's batch of rules, triggered manually or by the scheduler.
Open Data Quality → Runs to watch it; a queued or running run is polled until it
finishes. Every result carries one of six statuses:

| Status | Meaning |
| --- | --- |
| `PASS` / `WARN` / `FAIL` | The check ran: within thresholds / past warning / past failure. |
| `ERROR` | The check could not run (connection, permission, timeout, an unparsable value). **This is not a data failure.** |
| `SKIPPED` | Deliberately not run: the target disappeared, the rule is archived, or the same rule was already executing. |
| `NOT_EVALUATED` | The check ran but had nothing to judge (no rows after the filter, a NULL timestamp or aggregate). |

A run's own status is the worst result, and its six-state tallies are shown
beside it. Failed results can be expanded for samples and the exact SQL.

## Problems and notifications

A rule that starts failing opens an **issue**; it is the same issue until it
recovers, and its occurrence count grows while it keeps failing. Data Quality →
Issues lets you acknowledge it, resolve it with a reason, or mute it for a while.
When a rule passes again its issue resolves itself as *auto-recovered*.

Issues produce in-app notifications (bell icon) for the rule's owner, or for the
workspace administrators when there is no owner; a recovered issue notifies the
same audience. Execution failures always notify the administrators and cannot be
muted. A run in which more than ten rules fail sends one summary instead of one
message per rule. There is no email or webhook delivery.

## Profiles, labels and suggestions

The table detail page has a **Data Quality** tab: how many rules watch the table,
their recent results, its open issues, and per-column profiles and semantic
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
Zero means the server default; the page shows the effective values.

## Retention

Results are kept 90 days with their run, resolved issues 90 days, column
profiles and the model result cache 7 days, and decided suggestions 90 days; the
maintenance runner prunes them. Rules, open issues and accepted labels are not
pruned.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| A rule reports `ERROR(NO_READ_ONLY_SOURCE)` | The instance has no read-only data source, or it was removed. |
| `ERROR(UNSUPPORTED)` | The rule's engine cannot express it (for example regex on SQL Server, or `PERCENTILE` outside PostgreSQL/SQL Server), or a column the rule names was dropped. |
| Rules never run | The rule is paused or archived, the instance is gone, or the daily execution ceiling was reached (the next day resumes). |
| No notification for a failure | The issue was already open (notifications fire on transitions), the rule is muted, or the run crossed the storm threshold and sent a summary instead. |
| Model buttons are disabled | Model analysis is off, no allowed provider profile exists, or the caller lacks `suggest`/`explain`. |
| A label suggestion cannot be accepted | Profile the columns first; label suggestions require profiles fresher than seven days. |

The API surface, resource names, data model and invariants are documented for
maintainers in [.agents/docs/data-quality.md](../.agents/docs/data-quality.md).

## See also

- [.agents/docs/data-quality.md](../.agents/docs/data-quality.md) — maintenance reference (contracts, decisions, failure modes).
- [security-posture.md](security-posture.md) — the deliberate single-tenant and process-local decisions this feature inherits.
