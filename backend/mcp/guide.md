# Querying metaxisdata over MCP

This deployment serves its metadata registry, its lineage and its data-quality
signals as read-only tools. They answer from what the schema sync, the lineage
runner and the quality runner actually analyzed, so prefer them over reading SQL
files by hand.

## How objects are addressed

An object is named the way a person would name it:

```json
{"instance": "prod-mysql", "database": "shop", "schema": "public", "name": "orders"}
```

Every value may be a name or the id a listing returned, and `schema` is only
needed on engines that have a named schema level. A `guid` from an earlier result
works too and short-circuits the rest — but you never have to build one: the
server resolves names itself, because the identifier layout differs per engine.

When a name matches nothing, or several things, the tool refuses and returns
`details.candidates` instead. That is a hint about the next call, not a failure to
work around: pick from the candidates, or narrow the reference.

## Which tool answers what

| Question | Tool |
| --- | --- |
| Which instances are registered? | `list_instances` |
| Which databases does an instance have? | `list_databases` |
| Where does this name live? | `search_metadata` |
| What is under this database or schema? | `list_metadata` |
| What does this table look like? | `get_metadata` |
| What is this object's definition? | `get_ddl` |
| Is this table healthy, should I trust it? | `get_quality_signal` |
| Which tables do quality checks watch, and which need attention? | `list_quality_targets` |
| What are this table's checks, health and unresolved problems? | `get_quality_target` |
| What is in the quality problem ledger? | `list_quality_issues` |
| What have the checks run, and how did they verdict? | `list_quality_executions` |
| What is the evidence behind one run's verdict? | `get_quality_execution` |
| What is downstream of this table, and what upstream may explain a failure? | `get_quality_impact` |
| How much of the data other assets read is watched? | `get_quality_consumption_coverage` |
| What does this SQL statement read and write? | `analyze_sql` |
| What feeds this table, or what does it feed? | `get_lineage_graph` |
| What may I read? | `whoami` |

`search_metadata` is the entry point when you have only a name: it returns each
match's `guid`, `metaType` and `parentGuid`, which are what the other tools take.

## Analyzing a statement

A statement's unqualified names cannot say which instance they belong to, so
`analyze_sql` takes the scope explicitly:

```json
{"sql": "INSERT INTO daily (amount) SELECT amount FROM orders",
 "scopes": [{"instance": "prod-mysql", "database": "shop"}],
 "depth": 2}
```

Send more than one scope and the statement is analyzed once per scope,
independently: results are grouped by scope and never merged, because merging
scopes that may live on different instances would invent relationships the
registry does not have. Leave the scope out and the tool answers with the
databases that exist rather than guessing one.

The answer splits a statement's relations into two fields:

- `relations` — the real edges, into objects that exist in the registry;
- `temporaryRelations` — edges whose target exists only inside the statement. For
  a bare `SELECT` these are the answer to "where do these output columns come
  from": `targetColumn` is the query's output alias.

`diagnostics` reports what the analyzer could not represent — a statement shape it
does not model, or a reference it could not resolve — beside the relations it did
resolve, so a partial analysis never reads as a complete one. A statement that does
not parse at all fails the scope instead.

`depth` (0–10, default 0) also expands each resolved target into a lineage graph,
attached to its scope as `graphs`.

## Reading a lineage graph

`get_lineage_graph` is column-level; the table-level view is the collapse of those
edges, so an object whose definition yields no column-level relation — a
`SELECT count(*)`, or something never synced — does not appear at all. `direction`
picks `up` (where data comes from), `down` (what it feeds) or `both`, `depth`
(1–10, default 3) says how far to walk, and `column` keeps only the edges touching
one column.

## Reading a quality signal

`get_quality_signal` reports the data-quality view of one object — the same trust
signal the workspace's metadata browser shows. Ask it when the question is "is this
table healthy / should I trust it".

A table with no check at all is **not monitored**: the answer is `monitored: false`,
not a failure, because the object exists and nothing watches it. A table whose
checks all exist but are paused answers with `allPaused: true` and
`enabledRuleCount: 0`, and its verdict fields are omitted: the checks are history,
and nothing is being verified until one is resumed. A monitored object reports:

- `worstStatus` — the worst latest result across its checks, as the enum name
  (`QUALITY_STATUS_PASS`, `QUALITY_STATUS_WARN`, `QUALITY_STATUS_FAIL`,
  `QUALITY_STATUS_ERROR`, `QUALITY_STATUS_SKIPPED` or
  `QUALITY_STATUS_NOT_EVALUATED`), omitted before any check has run;
- `lastRunTime` — when its checks last ran;
- `qualityScore` — 0–100 over its enabled checks' judged results, omitted until one
  is judged;
- `recentPassRate` with `recentExecutionCount` — the pass share of its newest
  completed runs, and how many verdicts that share covers;
- `coveredColumnCount` of `totalColumnCount` — column coverage: current registry
  columns that an enabled check reads, out of the columns the target has now;
- `openIssueCount` and `enabledRuleCount` — unresolved issues, and checks that are
  actually on;
- `schemaChanged` — a check was saved against an older schema, so it may no longer
  describe the table;
- `orphaned` — the object, its instance or its database no longer resolves; the
  rules stay readable but runs are skipped.

## Reading quality problems and their evidence

The quality tools form a ladder from "how is this table" to "why did this check
fail":

- `get_quality_signal` answers about one object: is it watched, and how healthy is
  it. Start here.
- `list_quality_targets` is the directory: which tables the checks watch, with
  their worst latest result, quality score, column coverage, latest run and
  schedule. Filter it by `search` (a case-insensitive substring of the path or
  GUID), by `worst_status`, or with `has_open_issue: true` to find what needs
  attention. `worst_status` filters on the worst latest result, and that
  aggregate includes paused checks' history, so a paused target can match it and
  still come back as `allPaused: true` with no `worstStatus`: its last verdict is
  history, not a current health claim.
- `get_quality_target` opens one table: its health summary, every check with its
  configuration and latest result, its schedule, and its unresolved problems. It
  is the workspace a person would open. A watchable table with no check at all
  answers with `monitored: false` and an empty `rules` list, not an error — the
  same "not monitored is an answer" rule as `get_quality_signal`. A table whose
  checks all exist but are paused answers `monitored: true` with `allPaused: true`
  — no check is enabled (the summary omits the zero `enabledRuleCount`), and the
  `worstStatus` it omits is the paused checks' history — because nothing is being
  verified until a check is resumed.
- `list_quality_issues` is the problem ledger: one row per unresolved or resolved
  problem, with its severity, when it was first and last seen, how many runs
  observed it, and who acknowledged, muted or resolved it. An issue row's
  `target.path` is the path snapshot stored with the rule, for display only; it
  is omitted when the target no longer has a live check or the best-effort
  enrichment read fails, and `target.guid` with `target.metaType` still locate the
  target.
- `list_quality_executions` lists runs with their verdict, thresholds and
  evidence flags (`sampled`, `sampleRows`, `truncated`). It deliberately leaves
  out the failed sample rows and the executed SQL: those are one run's worth of
  data, not a page's. `observedValue` is absent when the run produced no
  observation (ERROR, SKIPPED or NOT_EVALUATED); a present 0 is a real
  observation. This is a projection, so unlike the raw protoJSON behind it, a row
  always carries its counts and booleans — `failedRows: 0` and `sampled: false`
  are real values here.
- `get_quality_execution` returns all of it: the SQL that executed, the failed
  sample rows, the thresholds the run was judged against, any baseline value and
  time, and the sampling and truncation flags. It returns the whole execution as
  protoJSON, so its nested field names follow the proto
  (`target.metaGuid`, `executedSql`, `samples[].columns[].value`, `ruleRevision`)
  rather than the projected `target.guid` shape the listing tools use. That raw
  protoJSON omits zero-valued fields: an absent `observedValue` means 0 when
  `outcome` is PASS, WARN or FAIL, and "no observation at all" when `outcome` is
  ERROR, SKIPPED or NOT_EVALUATED — read it together with `outcome`, and note a
  run that is still PENDING or RUNNING has no outcome yet either. A zero
  `failedRows`, a false `sampled` and every other zero scalar are omitted the
  same way.

The `object` filter of `list_quality_issues` and `list_quality_executions` must
name a quality check target — a table, external table, view or materialized view.
Those tools refuse a schema or column reference, because no check can watch one;
a bare `guid` is accepted because the filter then carries no type.

There is no tool that asks a model to explain a failure. The agent calling these
tools is the model: it reads the evidence — the executed SQL, the observed value,
the thresholds, the failed rows — and forms the explanation itself. Failed sample
values are real row data, returned to a caller the `executions.get` permission
already entitles.

## What a failure means

Every failure is one JSON object with a stable `code`:

| `code` | Meaning | What to do |
| --- | --- | --- |
| `invalid_argument` | a missing or malformed argument | fix the call; the message names the argument |
| `not_found` | no object matches the reference | use `details.candidates`, or search first |
| `ambiguous` | the name matches several objects | pick one from `details.candidates`, or pass its `guid` |
| `scope_required` | `analyze_sql` has no scope | pass one; `details.databases` lists what exists |
| `unsupported` | this deployment cannot answer that as asked — an engine with no lineage analyzer, for instance | try another scope or engine; the message says what is missing |
| `permission_denied` | the caller lacks the permission | report it; another tool may answer part of the question |
| `unauthenticated` | the client's authorization is gone | the client must authorize again |
| `unavailable` | the server could not be reached | retry later; it is not a bad request |
| `resource_exhausted` | rate limited | back off before retrying |
| `timeout` | the request ran out of time | retry, or ask for less at once |
| `internal` | a server fault | retry once, then report it |

Each failure also carries a `hint` written to be passed on to the user, and the
tools are annotated read-only: nothing here changes the workspace.
