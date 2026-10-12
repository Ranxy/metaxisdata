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
- **Five normalization rules shape the statement; N4 and N5 change the fingerprint alone.** N1 turns
  every literal into `?` (and a PostgreSQL `$n` into the same `?`); N2 removes comments and preserves
  all other spacing; N3 copies identifiers and keywords verbatim; N4 folds an all-placeholder `IN`
  list to one element; N5 folds a `VALUES` list of repeated placeholder groups to the first. Only
  N1–N3 shape the stored text — a person reads it, so its spacing and its literal *positions* stay
  as the application wrote them. **Nothing else folds**, and that is a rule rather than an omission:
  see "The rules that shape the fingerprint" below.
- **`key_digest` exists.** A design draft dropped it; without a lookup column every request would
  bcrypt-compare against every live key.
- **A statement the lexer cannot walk to its end is refused.** Every lexer here stops on a
  malformed token instead of reporting one, and only bytes inside a token are inspected, so
  accepting such a statement would copy a stretch no rule ever looked at into the store. In
  PostgreSQL an unterminated string is not a token at all, so `SELECT 'a` would have stored the
  literal. This is the lexer-level half of D6: a statement that cannot be tokenized is a
  statement that cannot be sanitized.
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
set stays derived from lineage edges, which only a write gets.

The type is browsable like manual SQL: `getNextLevelObjectType` lists it under the database for the
MySQL family and under the schema for PostgreSQL, so a statement appears beside the objects of the
scope it ran in, and the metadata detail renders its definition, its history and its edges. Two
things bound it. The registry row carries no counts: `kind`, `execution_count` and the per-application
breakdown live in `application_sql`, which changes with every report, and a mirror that carried them
would write a history row per report into a ledger that is kept forever — the detail links to the
Application SQL page for them instead. And the level is paginated like any other, so a database
holding thousands of statements shows a page of them rather than all of them.

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
| Body | 8 MiB, exactly one JSON object, at most 1000 statements, `count` at least 1, unknown JSON fields rejected. |
| Scope | `instance` must resolve, and its engine must have a registered analyzer, or the whole report is refused. `database` is required, `schema` for the PostgreSQL family. |
| Splitting | A reported item is one statement; a script is split by the analyzer's own splitter and each statement is fingerprinted on its own. The count applies to each. |
| Per item | Sanitize and gate **every** statement of the item first, then store the item in one transaction. A failure refuses that item alone and stores none of it; its neighbours are unaffected. |
| Counters | The response counts entries; the key's `statement_*_count` counters count the statements inside them, so a script entry counts once per statement. |
| Retry | Repeating the `reportId` of a report that landed whole is answered from the previous outcome without counting twice. A report with any refusal is not remembered, so its id stays usable for resending the refused entries. The check reads the key row before anything is written, so two copies of one `reportId` arriving at once are both processed — a retry is assumed to be sequential, which is what a client that did not see the answer does. |

## Core mechanics

### The gate is the analyzer, not a parser

A statement is stored only if `lineage.Analyzer.Analyze` accepts it. A raw parser call is **not**
equivalent: StarRocks view DDL that the analyzer handles through its view-body fallback is rejected
by the bare parser, so a parser gate would refuse statements that can produce complete lineage. The
cost is one parse per statement at ingest, and the runner parses again.

"Accepts it" means the same thing at ingest as it does in the runner, which is why an
`UnsupportedStatementError` is **not** a refusal: the analyzer returns the edges it did find beside
the gap, and the runner stores both — the edges on the statement and the gap on its lineage version.
Refusing at ingest would throw away lineage the graph can draw. Only an error that carries no
analysis at all (a hard parse failure) refuses the statement.

### `sqlsanitize` — three forms of one statement

| Form | Content | Lives |
| --- | --- | --- |
| Request | What the application sends | The request only |
| Stored (`sql_text`) | Literals → `?`, comments removed, **spacing preserved** | The database, and the fingerprint's input |
| Analyzable | Each `?` → `'?'` | Derived on demand; never stored |

Classification reads the token's source slice (`input[Loc:End]`), not omni's token-type constants,
which are unexported in every dialect package and would have to be hardcoded. Comments never appear
as tokens, so rebuilding from spans drops them.

`isLiteral` is the same kind of heuristic: a leading quote, a leading digit or a dot, a `$` that is
not a numbered parameter, or a letter-led token that quotes (`x'AB'`). `.5` is why the leading-dot
case is spelled out — a number that starts with a dot is still a number, and before it was
recognized the literal survived into the stored text.

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
label to `lineageAssetView` as a name override. The root's own type is seeded before the relations
arrive, which is what makes that lookup skip it, so the root asks for its label separately — it is
the one node always on screen. Only statement nodes cost a request, and a failed lookup is only
logged.

The label itself is `statementLabel`: every run of whitespace collapsed to a single space, bounded to
120 runes. It is computed twice — `backend/store/application_sql.go` fills the mirror's `name`, and
`frontend/src/lib/statementLabel.ts` serves the two places that hold the text rather than the mirror
row (the Application SQL list, and the graph's per-node lookup). The two implementations have to
agree: they are the same statement read through a breadcrumb and through a canvas node. Taking the
first line instead, as it did at first, labels a multi-line statement `WITH recent AS (` or
`INSERT INTO orders (` and labels a statement that opens with a comment by the keyword after it.

`TableLineageSection` renders the same writer as a column value, and a relation's meta GUID is the
object whose definition the analyzer read — so on that object's own page the writer is the page
itself. The cell says so (`lineageGraph.originCurrentObject`) instead of printing the generated GUID
back at the reader. On *other* pages a reported statement is still named by that GUID, because only
the canvas resolves a label; giving the table the same lookup is the open piece.

### The rules that shape the fingerprint (N4–N5)

These shape the canonical form and nothing else — not the lineage, not the stored text. They exist
because a *list* in an application's code changes length from execution to execution: an `IN` list
whose length depends on how many values were bound, a batch insert whose size depends on how many
rows were batched. That is what makes D6's bound — rows counted by distinct statement — true of the
application's *source code* rather than of its execution pattern.

| Rule | Folds | Example |
| --- | --- | --- |
| N4 | an all-placeholder `IN` / `NOT IN` list, to one element | `IN (?, ?, ?)` → `IN (?)` |
| N5 | a `VALUES` list of repeated placeholder groups, to the first | `VALUES (?, ?), (?, ?)` → `VALUES (?, ?)` |

**Nothing else folds, and a constant is not folded at all.** A constant becomes `?` and stays where
it is, so the operator around it and the number of placeholders the statement carries are both part
of its identity. The rule was not always this narrow: a "constant run" folding used to collapse a
run of placeholders and operators into one `?`, which meant `price * 100 >= 5000` and
`price * 100 <= 2000` were one row, and `id = ? - ?` — two bound values — was one row with `id = ?` —
one. Both are silent data corruption of exactly the kind D5 exists to prevent, one statement's text,
count and kind attributed to another. It was removed; what it merged is in
`testdata/06_operators.yaml`, pinned the other way round.

This limit is the accepted practice in this space rather than a local preference: the reference
fingerprinter (`query.Fingerprint` in Percona's `go-mysql`, the query ID of `pt-query-digest`) states
its whole principle as *"we only replace numbers, quoted strings, and value lists"*. It also folds
value lists, and it folds them more aggressively than N4/N5 do — it does not look inside the
parentheses, so `IN (a, b)` and `IN (1, 2, 3)` are one fingerprint there, and it lowercases the whole
statement. Those two choices are worth *not* copying: for a digest of a slow-query log a coarse
grouping is cheap, while for a registry that attaches lineage and counts to the row it is not, and
lowercasing would merge `"Foo"` with `"foo"` in the PostgreSQL family, where those are different
columns.

#### How close the two rule sets are, and what a state machine costs

Both were run over the same 414 statements (the lineage corpus plus this package's own suites, MySQL)
and compared after removing the typography they differ in: theirs lowercases and copies the source
spacing, ours keeps the case and spaces every token.

| Agreement | Statements | What it is |
| --- | --- | --- |
| same shape | 333 | the two rule sets agree |
| their `(?+)` marker | 24 | the same fold, written differently |
| they fold what we keep | 29 | our deliberate narrowing: their list collapse ignores what is inside the parentheses |
| different | 24 | below |

The 24 are where a lexer and a hand-written state machine part ways. Eight lose information that
ours keeps: `'it''s'` and `"a""b"` fingerprint as `??`, because the doubled-quote escape is not
handled; `SELECT 1 /* a /* b */ */` keeps a `*/`, because block comments are not nested; a word
immediately before a comment is dropped, so `SELECT a/* c */FROM t` is `select from t` and the column
is gone; `.5` fingerprints as `.?`; `1_000` is not a number at all. Four truncate a malformed
statement — an unterminated string, or a backtick — instead of refusing it, which turns a reported
statement into whatever came before the break. Ten are choices we disagree with: `NULL` becomes `?`,
a leading sign is dropped, `ORDER BY ... ASC` is removed, and `$1` keeps its parameter index, so a
driver's `$1` and a written `?` never meet. Two are PostgreSQL dollar-quoting read by a MySQL-only
state machine, where neither implementation is right.

Performance, on the 225-statement MySQL lineage corpus: theirs is 2.5us per statement (553us,
33 MB/s) and ours is 7.5us (1.68ms, 11 MB/s) — 3x, widening to 7-8x on token-dense statements (a
1000-row batch insert: 296us against 2.24ms), because it allocates per token: 10 allocations for that
statement against our 28000, all of them inside the omni lexer. One pass of theirs costs about what
one omni lex pass costs us, and the trade is that we pay for three passes and hand back three
artifacts — the spacing-preserving text, the canonical form and the analyzable form — in three
dialects, with the lexing done by a real lexer instead of a state machine.

These numbers came from a throwaway module that required both implementations side by side, since
this repository does not depend on the reference one. They are an ordering rather than a benchmark
the build runs; re-deriving them means recreating that module.

The rules stay inside the shape they fold, which is the property that matters more than folding
enough:

| Kept apart | By |
| --- | --- |
| `IN (a, b)` from `IN (a, b, c)` | only an all-placeholder list folds; a column list names different columns |
| `IN (?, b)` from `IN (?, ?)` | a mixed list is left alone, and so is a subquery |
| `VALUES (?)` from `VALUES (?, ?)` | the first group keeps its arity |
| `value * ? >= ?` from `value * ? <= ?` | the operator is part of the statement |
| `id = ?` from `id = ? - ?` | so is how many values it binds |
| `f(?)` from `f(?, ?)` | a comma and a parenthesis end a group |
| `x = NULL` from `x = ?` | `NULL` is a keyword, and says something a placeholder does not |

Every folding is idempotent — `fold(fold(x)) == fold(x)` — or a statement reported twice would be
two rows. `fold_test.go` asserts both halves: what must merge, and what must not.

Three asymmetries between the dialects follow from the shared rules running over tokens the lexers
disagree about. They are pinned in `testdata/09_dialects.yaml` and beside the cases they affect:

| Difference | Effect |
| --- | --- |
| PostgreSQL lexes `?` as an operator character, so `-?` and `+?` come back as single operator tokens | a signed constant is spaced differently in the two: `x = -1 + 2` is `x = - ? + ?` in the MySQL family, where `-` and `?` are two tokens, and `x = -? + ?` in PostgreSQL, where they are one. Fingerprints are scoped to an instance, so the two forms never meet in one table. |
| StarRocks tokenizes a `/*+ … */` hint as code rather than as a comment | the hint is stored and fingerprinted with the statement — but its body is tokenized like any expression, so a literal inside it is still rewritten. MySQL and PostgreSQL drop it as a comment. |
| MySQL tokenizes a `/*! … */` executable comment into unusable fragments | the statement is refused, like any statement the lexer cannot walk to its end. |

### The corpus

`sanitize_test.go` and `fold_test.go` are the hand-written cases for one behavior each.
`testdata/*.yaml` is the contract at statement scale: 220 statements in nine suites, each run under
every dialect unless it names the ones it is valid in (`engine: postgres`, `engine: [mysql,
starrocks]`).

A case pins `text` and `canonical` exactly and nothing else, and both are required. Pinning both is
what states the deduplication contract with no second mechanism: two cases whose canonical forms are
written identically must merge, and cases whose canonical forms differ must not, so a folding suite
reads as a list of equivalence classes — and a case cannot assert only the safe half of it.

The runner adds what has to hold for every statement whatever its case asserts: no token is a
literal, every byte between tokens is whitespace, the fingerprint is the canonical form's hash,
re-sanitizing the stored text is a no-op, and folding the canonical form again is a no-op. The
loader refuses the two YAML spellings that would silently weaken a case — a ` #` in a plain scalar
is a comment, and trailing whitespace is stripped — and refuses two cases covering the same
statement under the same dialect.

| Suite | Rule |
| --- | --- |
| `01_literals` | N1, every literal form the three lexers emit, and the numbered-parameter rewrite |
| `02_comments` | N2, including the forms that are *not* comments: a string, a quoted identifier, a StarRocks hint, a MySQL executable comment |
| `03_identifiers` | N3, quoting and case, and where a double quote or a backtick changes meaning |
| `04_in_lists` | N4, and every list that must keep its shape |
| `05_values` | N5, batch size against group arity |
| `06_operators` | the operators and placeholder counts that are part of a statement's identity, including the two pairs that used to share a row |
| `07_statement_shapes` | realistic application statements, so a rule change has to answer for one |
| `08_refusals` | what must not be stored — text that is not valid UTF-8 is refused too, but YAML cannot carry an invalid byte, so `sanitize_test.go` pins that one |
| `09_dialects` | the asymmetries above, written out

## Invariants

| Rule | Breakage |
| --- | --- |
| Only `sqlsanitize.Sanitize`'s output is stored | the original text, with its literals, reaches the database |
| Only a statement the lexer walks to its end is sanitized | a stretch no rule inspected — an unterminated PostgreSQL string holds a literal — is copied into the store |
| Only text that is valid UTF-8 is sanitized | an invalid byte is copied into `sql_text` and the write fails there, as a `STORE_FAILED` the caller is told to retry forever |
| No log line carries a statement the sanitizer could not rewrite | the one input that still holds its literals reaches the log |
| One entry's statements are stored in one transaction | a retry of the entry counts the statements that did land a second time |
| The gate is `lineage.Analyzer.Analyze`, not a dialect parser | statements the analyzer accepts are refused |
| A refusal stores nothing | rows accumulate that can never produce lineage |
| The reported identity comes from the key | a report can claim to be another application |
| A conflict on the statement upsert never rewrites `guid`, `application_sql_id` or `sql_text` | the object detaches from its lineage and history |
| `sqlsanitize` reads token source slices, not token-type constants | a dependency on unexported upstream numbers, failing silently when they change |
| A table-level edge carries a non-nil `Transformation` | the whole edge batch fails on a `NOT NULL` column |
| Every relation endpoint's meta type is resolved from the registry, the table-level edge's target included | the API answers with an untyped target, and every client that routes by type links to a list instead of the object |
| Every statement is mirrored; only writes get edges | a reported statement has no object to hang lineage or history on, or reads vanish from the canvas |
| The mirror carries no counts, and every level of the tree is paginated | a row changes on every report, so the forever-kept registry history grows with traffic rather than with statements |
| Changing any rule that shapes the stored text or the canonical form bumps `sqlsanitize.Version` | one statement splits into two rows, and a deployment holding the old ones has nothing to tell it apart |
| The `db` table is not a foreign key of `application_sql` | reporting fails until the schema sync reaches the database |

`Version` is **1**, and that is not a placeholder: the foldings were added while this feature was
still unreleased, so they are part of the first rule set rather than a second one with rows to
migrate. Nothing has been deployed, so there is no historical fingerprint to reconcile and no
migration logic to write — building one now would be machinery for a past that does not exist.

What `Version` is for is the *next* change, after this ships: rewriting a rule splits one statement
into rows fingerprinted under the old one, and a deployment that holds rows needs to find out. The
column is the hook for that, and it costs one `INT2`. Writing the pass itself belongs to whoever
makes that change, against a deployment that actually has the rows.

## Failure modes

| Symptom | Behavior |
| --- | --- |
| No key, an unknown key, a revoked key, a stopped application | 401, nothing stored, and no log line: the key is the credential, and a rejected one is not worth a line per request |
| Malformed payload, unknown instance, engine without an analyzer | 4xx, nothing stored |
| Body over 8 MiB, more than 1000 statements | 413 / 400, nothing stored |
| One statement of an item fails sanitize or the gate | the whole item is `REJECTED` with the reason, and the store is never asked, so none of the item's statements is counted; its neighbours are stored, and a WARN carries the application, engine, reason and, when the statement has a sanitized form, its first line |
| The analysis fails after a successful ingest | the version row carries the error and the edges are cleared; the statement row stays |
| Recording the report outcome fails after the entries were stored | the statements are stored and counted, but the report is not remembered as the last one, so a retry of it counts again; a WARN names the failure |
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
| SPA | `frontend/src/pages/ApplicationManagementPage.vue` (under `/settings/applications`), `ApplicationSQLPage.vue`, `src/components/application/ApplicationSelect.vue`, `src/components/metadata/ApplicationSQL{List,MetadataDetail}.vue`, `src/api/application.ts`, `src/lib/lineageOrigin.ts`, `src/lib/statementLabel.ts` |
| Permissions | `backend/common/permission/permission.json` (`applications.*`, `applicationSqls.*`), `backend/store/predefined_roles.go` |

Tests and their gates:

| Layer | Test | Gate |
| --- | --- | --- |
| Sanitizer | `sqlsanitize/{sanitize,token,fold,neutral,corpus,yaml}_test.go` and `sqlsanitize/testdata/*.yaml` | `go test ./backend/plugin/sqlsanitize/` |
| Target | `dmltarget/target_test.go` | `go test ./backend/plugin/dmltarget/` |
| Store | `store/application{,_sql}_pure_test.go` | `go test ./backend/store/` |
| Classification | `runner/lineageanalyzer/classify_test.go` | `go test ./backend/runner/lineageanalyzer/` |
| Endpoint | `api/v1/application_sql_handler_test.go` | `go test ./backend/api/v1/` |
| Graph edge shape | `api/v1/lineage_service_graph_test.go` (`TestBuildLineageGraphDrawsTableLevelEdges`) | same |
| Whole chain | `test/integration/runner/application_sql_service_test.go` | `go test -tags=integration -run ApplicationSQLReport ./backend/test/integration/runner` |
| SPA | `lineageOrigin.test.ts`, `lineageAsset.test.ts`, `permissions.test.ts`, `ApplicationSQLFacts.test.ts`, `TableLineageSection.test.ts`, `statementLabel.test.ts` | `pnpm --dir frontend biome:check && lint && i18n && type-check && test run` |
| Cost | `sqlsanitize/benchmark_test.go`, `sqlsanitize/ingest_benchmark_test.go`, `sqlsanitize/allocation_budget_test.go` | `go test ./backend/plugin/sqlsanitize/ -bench . -benchmem` |

Two sanitizer suites guard a rule change from opposite ends. `TestSanitizedStatementsKeepTheirLineage`
is the acceptance gate: it runs 642 statements from the repository's own lineage corpus and requires
that sanitizing never makes an analyzable statement unanalyzable, with 0 refusals and 100%
idempotency. `TestSanitizeYAMLCorpus` is the contract: 220 statements whose stored text and canonical
form are pinned exactly, so a change has to be stated as a deliberate edit to a written expectation
rather than discovered as a moving fingerprint.

The sanitizer's cost is measured rather than assumed, and the measurement is the argument for not
optimizing it: `BenchmarkIngestCost` puts it beside the analyzer it feeds, and over the lineage corpus
the whole sanitizer — the stored text, the canonical form and the analyzable form, three lexer passes
— is about a quarter of what the ingest path spends per statement, while the analyzer's own parse is
roughly three times the sanitizer. A per-statement table and the layer split are in
`BenchmarkSanitize` and `BenchmarkSanitizeLayers`; the counts that must not creep are in
`TestSanitizeAllocationBudget`, which budgets allocations rather than nanoseconds because they do not
depend on the machine running the test.

Known gaps: the two SPA pages have no runtime coverage (`pnpm audit:ui`
needs a browser on the DevTools port); both corpora are hand-written statements rather than real ORM
output, so how much the foldings merge *enough* has never been measured against real application SQL
— only that they never merge what they should not. The benchmarks measure the shapes those corpora
contain, which are views and small statements: a real ORM's token-dense queries are represented only by
the synthetic shapes in `benchmark_test.go`.
