# Application SQL — Reference

> Status: **implemented**. Maintenance reference for `backend/plugin/sqlsanitize/`, `backend/plugin/dmltarget/`, `backend/store/application*.go`, `backend/api/v1/application_sql_handler.go`, `backend/api/v1/application_service.go`, `backend/runner/lineageanalyzer/`, and the two SPA pages.
> Integrator-facing contract: [docs/application-sql-reporting.md](../../docs/application-sql-reporting.md). Related: [lineage-analyzer.md](lineage-analyzer.md) (the runner), [lineage-semantics.md](lineage-semantics.md) (what an edge means), [openlineage-lineage.md](openlineage-lineage.md) (the other external ingest path), [rate-limiting.md](rate-limiting.md) (this endpoint deliberately has no budget).

## What it is

An application reports the SQL it actually ran, with every literal already replaced by `?`. The
server sanitizes it again, refuses what it cannot analyze, stores one row per distinct statement,
remembers which application ran it and how often, and hands the statement to the lineage runner,
where it becomes a **third origin** on the canvas beside SQL analysis and OpenLineage.

```text
application ──POST /api/v1/applicationSql/report──▶ sanitize ──▶ gate (analyzer) ──▶ store
   (key)                                              │                                │
                                                      └─ refuses, WARN                 ├─▶ statement + observation + rollup
                                                                                      ├─▶ meta registry mirror
                                                                                      └─▶ queue ──▶ runner ──▶ column_lineage ──▶ graph
```

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| D1' | **Every statement becomes a graph node.** A read writes `source column → statement column`; a write adds `statement column → target column`. | Reuses the manual-SQL edge shape, which retargets the analyzer's temporary `__result__` onto the statement's own registry GUID. A read therefore reaches the canvas through its incoming edge, so the read/write distinction is not needed for node existence. |
| D2 | **Double insurance.** Applications are required to send parameterized SQL; the server re-sanitizes anyway and stores only its own output. | The original text never reaches the database, whatever the application did. The interface is a contract, not a trust boundary. |
| D3 | **Two levels.** `application_sql` is keyed by (instance, database, schema, fingerprint); `application_sql_observation` records each application's own history. | One statement run by two applications is one row and one node; "who runs this" is a second table, not a second statement. |
| D4 | **A dedicated REST endpoint**, authenticated by a key issued per application. | Applications are not browsers and not ConnectRPC clients. |
| D5 | **A deliberately aggressive fingerprint**, versioned per row. | It is what makes D6's "the storage stays small" true. |
| D6 | **No storage cap and no rate limit**; a statement that cannot be parsed is never stored. | Rows are counted by distinct statement, and a failed parse adds none, so an application's own content is the bound. |
| D7 | **A key is issued by hand and belongs to one application.** The reported identity comes from the key, never from the payload. | A report cannot claim to be another application. |
| D8 | **No audit row per request**; per-key counters instead. | `audit_log` is kept forever, so auditing machine traffic would grow it with request volume rather than with distinct statements. |
| D9 | **The server reads a zero-relation write's target itself.** | The analyzers record column derivation, so a constant `INSERT` produces no relation in any dialect and nothing else names its table. |

Choices inside those decisions, each of which a reader would otherwise re-litigate:

- **The stored text keeps its original spacing.** The analyzer names an unaliased expression column
  after its source text, and that name is stored in `column_lineage`; normalizing whitespace would
  put it in the graph. Only the canonical form is whitespace-normalized.
- **The neutral literal is `'?'` for every dialect.** A string literal is the only substitute that
  parses in every position tested; `NULL` and `0` fail MySQL's `LOAD DATA` / `JSON_TABLE PATH` and
  PostgreSQL's `INTERVAL`, and `$n` fails PostgreSQL's `COPY` and `INTERVAL`. A PostgreSQL `$n` is
  normalized to `?` on the way in, so one statement has one stored form.
- **Six normalization rules shape the statement; N4–N6 change the fingerprint alone.** N1 turns
  every literal into `?` (and a PostgreSQL `$n` into the same `?`); N2 removes comments and preserves
  all other spacing; N3 copies identifiers and keywords verbatim; N4 folds an all-placeholder `IN`
  list to one element; N5 folds a `VALUES` list of repeated placeholder groups to the first; N6
  folds a run beginning with a placeholder and holding only placeholders and operators to one. Only
  N1–N3 shape the stored text — a person reads it, so its spacing and its literal *positions* stay
  as the application wrote them. See "The rules that shape the fingerprint" below.
- **`key_digest` exists.** A design draft dropped it; without a lookup column every request would
  bcrypt-compare against every live key.
- **`application_sql` has a foreign key to the instance but not to `db`.** An application commonly
  starts reporting before the sync has reached its database.
- **`kind` is a filter cache, not a classification.** The runner derives it and writes it back.

## Data model

| Table | Contract |
| --- | --- |
| `application` | One application. A plain row, not a registry object: it has no schema, columns or lineage. `application_id` and `name` are each unique. |
| `application_sql_api_key` | The credential. References `application(application_id)`; several live keys per application are allowed so one can be rotated. Carries `key_hash` (bcrypt), `key_digest` (SHA-256, lookup), `last_report_id`, and the per-key counters. |
| `application_sql` | One distinct statement. Unique on (instance, database, schema, fingerprint), plus `guid` and `application_sql_id`. `sql_text` is the only form of the statement stored. `kind` is 0 UNKNOWN / 1 READ / 2 WRITE. `execution_count`, `first_seen_at`, `last_seen_at` are rollups of the observations. |
| `application_sql_observation` | PK (statement, application): that application's own count and first/last sighting. |

The mirror of a statement is a `meta_registry_resource` row of type `MetaType_APPLICATION_SQL` (19)
holding `ApplicationSQLMetadata{application_sql_id, name, sql_text, instance_resource,
database_name, schema_name}`. **Every statement is mirrored, read or write** — the mirror is what
makes the object addressable by GUID (`GetMetadata`, history, Explain SQL) — while the canvas node
set stays derived from lineage edges, which only a write gets. `getNextLevelObjectType` deliberately
does not list the type, so the metadata browser tree does not grow a sub-level per statement.

The GUID is `instance;database;schema;__application_sql__/<application_sql_id>`. The identifier is
generated rather than derived from the fingerprint, so a change to the fingerprint rules can rewrite
the fingerprint without detaching the object from its lineage and history.

## The reporting contract

`POST /api/v1/applicationSql/report`, `Authorization: Bearer <key>`. It sits under `/api/v1` rather
than `/v1` because the ConnectRPC gateway owns `/v1/*`. The full payload, the response, the reason
codes and the retry semantics are in [docs/application-sql-reporting.md](../../docs/application-sql-reporting.md);
what matters here:

| Rule | Behavior |
| --- | --- |
| Identity | From the key. A payload naming an application is ignored. |
| Body | 8 MiB, at most 1000 statements, unknown JSON fields rejected. |
| Scope | `instance` must resolve, and its engine must have a registered analyzer, or the whole report is refused. `database` is required, `schema` for the PostgreSQL family. |
| Splitting | A reported item is one statement; a script is split by the analyzer's own splitter and each statement is fingerprinted on its own. The count applies to each. |
| Per item | Sanitize, then the gate; a failure refuses that item alone. |
| Retry | Repeating the last accepted `reportId` is answered from the previous outcome without counting twice. |

## Core mechanics

### The gate is the analyzer, not a parser

A statement is stored only if `lineage.Analyzer.Analyze` accepts it. A raw parser call is **not**
equivalent: StarRocks view DDL that the analyzer handles through its view-body fallback is rejected
by the bare parser, so a parser gate would refuse statements that can produce complete lineage. The
cost is one parse per statement at ingest, and the runner parses again.

### `sqlsanitize` — three forms of one statement

| Form | Content | Lives |
| --- | --- | --- |
| Request | What the application sends | The request only |
| Stored (`sql_text`) | Literals → `?`, comments removed, **spacing preserved** | The database, and the fingerprint's input |
| Analyzable | Each `?` → `'?'` | Derived on demand; never stored |

Classification reads the token's source slice (`input[Loc:End]`), not omni's token-type constants,
which are unexported in every dialect package and would have to be hardcoded. Comments never appear
as tokens, so rebuilding from spans drops them.

`tokenSpans` trims trailing whitespace off a token's end: PostgreSQL's lexer hands a string constant
the whitespace that follows it, so `status = 'new' AND total > 10` arrives as one token spanning
`'new' `. Taken at face value that ate the space and stored `= ?AND` — still valid SQL, so no parse
check could notice.

### The ingest path

```text
authenticate ─▶ read + decode ─▶ resolve instance and engine ─▶ (repeated reportId?)
per statement item:
  split ─▶ sanitize ─▶ gate ─▶ upsert statement ─▶ upsert observation ─▶ roll up ─▶ mirror
after the transaction: queue the statement for analysis
```

The statement upsert's conflict target is the identity tuple, and a conflict leaves `guid`,
`application_sql_id` and `sql_text` alone: the id is what the GUID already points at, and the text
belongs to the first reporter, since whitespace is not part of the fingerprint. The rollup is
recomputed from the observations rather than trusted from the caller.

### Classification and the table-level edge

The runner derives the kind, and it is the only place the read/write rule lives:

| Shape | Kind | Edges |
| --- | --- | --- |
| Any relation with a real target | WRITE | `source → statement`, `statement → target` |
| Only temporary targets (a read) | READ | `source → statement` |
| **No relation at all** + `dmltarget` names a target | WRITE | one table-level `statement → target` |
| No relation at all + no target (`SELECT 1`, `SET`, `TRUNCATE`) | READ | none — not drawn |

Zero-relation writes are constant `INSERT`s in every form and a `DELETE` that references no column.
Everything else is already answered: an `UPDATE` records `t.* -> t.a` even for a constant
assignment, a multi-table `UPDATE`/`DELETE` references columns, and every DDL statement has a real
target. A table-level edge must carry a non-nil `Transformation` — the column is `NOT NULL`, so a
nil slice serializes to `null` and fails the whole batch.

### The canvas category

Relations are coloured by which writer stored them: `sql`, `openlineage`, `applicationsql`, or
`mixed` when an edge bundles several. `relationOrigin` reads the relation's meta type. The accent is
`--lineage-appsql` (amber, dashed `1 3`), declared in both themes in `main.css`.

A statement node's GUID ends in a generated identifier, so the page resolves a label for it: on
first sight of an `APPLICATION_SQL` GUID it calls `GetApplicationSQL` once and hands the statement's
first line to `lineageAssetView` as a name override. Only statement nodes cost a request, and a
failed lookup is only logged.

### The rules that shape the fingerprint (N4–N6)

These shape the canonical form and nothing else — not the lineage, not the stored text. They exist
because one statement in an application's code otherwise becomes one row per shape of its literals:
an `IN` list of varying length, a batch insert of varying size, a constant written differently. That
is what makes D6's bound — rows counted by distinct statement — true of the application's *source
code* rather than of its execution pattern.

| Rule | Folds | Example |
| --- | --- | --- |
| N4 | an all-placeholder `IN` / `NOT IN` list, to one element | `IN (?, ?, ?)` → `IN (?)` |
| N5 | a `VALUES` list of repeated placeholder groups, to the first | `VALUES (?, ?), (?, ?)` → `VALUES (?, ?)` |
| N6 | a run beginning with a placeholder and holding only placeholders and operators, to one | `? + ?` → `?`, so `1 + 1`, `2` and `5 - 3` are one statement |

**N6 must begin at the placeholder, and that is not a detail.** A run that may begin at an operator
swallows the identifier before it: `WHERE a - 1 > 0` stores as `WHERE a - ? > ?`, whose run would be
`- ? > ?` and would fold to `WHERE a ?` — colliding with statements that share nothing but the
identifier. Beginning at the placeholder leaves `WHERE a - ?`. N4 and N5 have no such trap, because
their lists are delimited.

The rules stay inside the shape they fold, which is the property that matters more than folding
enough:

| Kept apart | By |
| --- | --- |
| `IN (a, b)` from `IN (a, b, c)` | only an all-placeholder list folds; a column list names different columns |
| `IN (?, b)` from `IN (?, ?)` | a mixed list is left alone |
| `VALUES (?)` from `VALUES (?, ?)` | the first group keeps its arity |
| `a = ?` from `a = ? AND b = ?` | a keyword ends a run |
| `f(?)` from `f(?, ?)` | a comma and a parenthesis end a run |
| `x = NULL` from `x = ?` | `NULL` is a keyword, and says something a placeholder does not |

Every folding is idempotent — `fold(fold(x)) == fold(x)` — or a statement reported twice would be
two rows. `fold_test.go` asserts both halves: what must merge, and what must not.

## Invariants

| Rule | Breakage |
| --- | --- |
| Only `sqlsanitize.Sanitize`'s output is stored | the original text, with its literals, reaches the database |
| The gate is `lineage.Analyzer.Analyze`, not a dialect parser | statements the analyzer accepts are refused |
| A refusal stores nothing | rows accumulate that can never produce lineage |
| The reported identity comes from the key | a report can claim to be another application |
| A conflict on the statement upsert never rewrites `guid`, `application_sql_id` or `sql_text` | the object detaches from its lineage and history |
| `sqlsanitize` reads token source slices, not token-type constants | a dependency on unexported upstream numbers, failing silently when they change |
| A table-level edge carries a non-nil `Transformation` | the whole edge batch fails on a `NOT NULL` column |
| Every statement is mirrored; only writes get edges | the metadata browser tree grows a sub-level per statement, or reads vanish from the canvas |
| Changing any rule that shapes the stored text or the canonical form bumps `sqlsanitize.Version` and runs the re-fingerprint pass | one statement splits into two rows permanently |
| The `db` table is not a foreign key of `application_sql` | reporting fails until the schema sync reaches the database |

**The re-fingerprint pass** (not implemented; required when N1–N3 change): recompute every old-version
row's fingerprint, merge rows that collide on the new one, union their observations, and delete the
losers with their mirrors and lineage. Nothing in the code does this today.

## Failure modes

| Symptom | Behavior |
| --- | --- |
| No key, an unknown key, a revoked key, a stopped application | 401, nothing stored, `slog.Warn` |
| Malformed payload, unknown instance, engine without an analyzer | 4xx, nothing stored |
| Body over 8 MiB, more than 1000 statements | 413 / 400, nothing stored |
| One statement fails sanitize or the gate | that item is `REJECTED` with a reason; its neighbours are stored, and a WARN carries the application, engine, reason and the SQL's first line |
| The analysis fails after a successful ingest | the version row carries the error and the edges are cleared; the statement row stays |
| A statement has no relation and is not a write | it stays in the registry and the list, and never appears on the canvas |
| An application is deleted | its keys and observations cascade; statements no other application reports are deleted with their mirror, lineage and version |
| A statement is deleted by hand | the same, for that statement |
| A node's label lookup fails | the node draws from its own GUID, which is the pre-existing behavior for every object |

## Where things live

| Concern | Path |
| --- | --- |
| Sanitizer, fingerprint, analyzable form | [backend/plugin/sqlsanitize/](../../backend/plugin/sqlsanitize/) — `sanitize.go`, `token.go`, `fold.go` |
| Zero-relation write target | [backend/plugin/dmltarget/](../../backend/plugin/dmltarget/) — `target.go` |
| Store | [backend/store/](../../backend/store/) — `application.go`, `application_api_key.go`, `application_sql.go` |
| Ingest endpoint | [backend/api/v1/application_sql_handler.go](../../backend/api/v1/application_sql_handler.go); route in `backend/server/grpc_routes.go` |
| Management service | [backend/api/v1/application_service.go](../../backend/api/v1/application_service.go), `proto/v1/v1/application_service.proto` |
| Runner | [backend/runner/lineageanalyzer/analyzer.go](../../backend/runner/lineageanalyzer/analyzer.go) — `queueAll`, `buildSQL`, `classifyApplicationSQL` |
| Schema | `backend/migrator/migration/LATEST.sql` and `0.1/0019##application_sql.sql` |
| SPA | `frontend/src/pages/ApplicationManagementPage.vue`, `ApplicationSQLPage.vue`, `src/api/application.ts`, `src/lib/lineageOrigin.ts` |
| Permissions | `backend/common/permission/permission.json` (`applications.*`, `applicationSqls.*`), `backend/store/predefined_roles.go` |

Tests and their gates:

| Layer | Test | Gate |
| --- | --- | --- |
| Sanitizer | `sqlsanitize/{sanitize,fold,neutral,corpus}_test.go` | `go test ./backend/plugin/sqlsanitize/` |
| Target | `dmltarget/target_test.go` | `go test ./backend/plugin/dmltarget/` |
| Store | `store/application{,_sql}_pure_test.go` | `go test ./backend/store/` |
| Classification | `runner/lineageanalyzer/classify_test.go` | `go test ./backend/runner/lineageanalyzer/` |
| Endpoint | `api/v1/application_sql_handler_test.go` | `go test ./backend/api/v1/` |
| Graph edge shape | `api/v1/lineage_service_graph_test.go` (`TestBuildLineageGraphDrawsTableLevelEdges`) | same |
| Whole chain | `test/integration/runner/application_sql_service_test.go` | `go test -tags=integration -run ApplicationSQLReport ./backend/test/integration/runner` |
| SPA | `lineageOrigin.test.ts`, `lineageAsset.test.ts`, `permissions.test.ts` | `pnpm --dir frontend biome:check && lint && i18n && type-check && test run` |

The corpus test in `sqlsanitize` is the acceptance gate for any change to the rules: it runs 642
statements from the repository's own lineage corpus and requires that sanitizing never makes an
analyzable statement unanalyzable, with 0 refusals and 100% idempotency.

`Version` is 2: the foldings were the first change to the rules, and **the re-fingerprint pass they
require does not exist**. Nothing has shipped, so no deployment should hold version-1 rows; one that
does will keep them beside the version-2 rows a re-report creates, until the pass is written.

Known gaps: the re-fingerprint pass; the two SPA pages have no runtime coverage (`pnpm audit:ui`
needs a browser on the DevTools port); the corpus is view definitions rather than real ORM output, so
how much the foldings merge *enough* has never been measured against real application SQL — only
that they never merge what they should not.
