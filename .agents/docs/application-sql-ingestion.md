# Application SQL — Reference

> Maintenance reference for `backend/plugin/sqlsanitize/`, `backend/plugin/dmltarget/`,
> `backend/store/application*.go`, `backend/api/v1/application_sql_handler.go`,
> `backend/api/v1/application_service.go`, `backend/runner/lineageanalyzer/`, and the SPA pages.
> Integrator-facing contract: [docs/application-sql-reporting.md](../../docs/application-sql-reporting.md).
> Related: [lineage-analyzer.md](lineage-analyzer.md) (the runner), [lineage-semantics.md](lineage-semantics.md)
> (what an edge means), [openlineage-lineage.md](openlineage-lineage.md) (the other external ingest
> path), [rate-limiting.md](rate-limiting.md) (this endpoint deliberately has no budget).

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
- **Only N1–N3 shape the stored text; N4 and N5 change the fingerprint alone.** N1 replaces every
  literal with `?`; N2 removes comments and preserves all other spacing; N3 copies identifiers and
  keywords verbatim. A person reads the stored text, so its spacing and literal positions stay as
  the application wrote them. See "The rules that shape the fingerprint".
- **A statement the lexer cannot walk to its end is refused.** Every lexer here stops on a
  malformed token instead of reporting one, and only bytes inside a token are inspected, so
  accepting it would copy a stretch no rule ever looked at into the store — in PostgreSQL an
  unterminated string is not a token at all, so `SELECT 'a` would have stored the literal. This is
  the lexer-level half of D6.

## Data model

| Table | Contract |
| --- | --- |
| `application` | One application. A plain row, not a registry object: it has no schema, columns or lineage. `application_id` and `name` are each unique. |
| `application_sql_api_key` | The credential. Several live keys per application are allowed so one can be rotated. Carries `key_hash` (bcrypt), `key_digest` (SHA-256 lookup), `last_report_id` and the per-key counters. |
| `application_sql` | One distinct statement. Unique on (instance, database, schema, fingerprint), plus `guid` and `application_sql_id`. `sql_text` is the only form of the statement stored. `kind` is 0 UNKNOWN / 1 READ / 2 WRITE, a filter cache the runner derives. `execution_count`, `first_seen_at`, `last_seen_at` are rollups of the observations. |
| `application_sql_observation` | PK (statement, application): that application's own count and first/last sighting. |

A statement's mirror is a `meta_registry_resource` row of type `MetaType_APPLICATION_SQL`. **Every
statement is mirrored, read or write** — the mirror is what makes the object addressable by GUID
(`GetApplicationSQL`, history, Explain SQL) — while the canvas node set stays derived from lineage
edges, which only a write gets. The GUID is
`instance;database;schema;__application_sql__/<application_sql_id>`: the identifier is generated
rather than derived from the fingerprint, so a rule change can rewrite the fingerprint without
detaching the object from its lineage and history.

`getNextLevelObjectType` lists the type beside the objects of the scope it ran in, under the database
and schema levels. The detail shows its scope, facts, history and edges; the statement itself is
read in the shared `SchemaDefinitionDialog`, with a trigger that says statement rather than schema.
The registry row carries no counts — `kind`, `execution_count` and the per-application breakdown
change with every report, so a mirror carrying them would write a history row per report into a
ledger kept forever — and the detail reads them with one extra call, gated on `applicationSqls.get`.

## The reporting contract

`POST /api/v1/applicationSql/report`, `Authorization: Bearer <key>`, on a route outside the
ConnectRPC gateway. The payload, response, reason codes, limits and retry semantics are owned by
[docs/application-sql-reporting.md](../../docs/application-sql-reporting.md). Two rules matter for
maintenance: an entry's statements are gated before any is stored, then stored in one transaction,
so a retry cannot count a half-written entry twice; and a report is remembered as the last one only
once it landed whole, which is what makes resending the refused entries safe.

## Core mechanics

### The gate is the analyzer, not a parser

A statement is stored only if `lineage.Analyzer.Analyze` accepts it. A raw parser call is **not**
equivalent: StarRocks view DDL that the analyzer handles through its view-body fallback is rejected
by the bare parser, so a parser gate would refuse statements that can produce complete lineage. The
cost is one parse per statement at ingest, and the runner parses again.

"Accepts it" means the same thing at ingest as in the runner, which is why an
`UnsupportedStatementError` is **not** a refusal: the analyzer returns the edges it found beside the
gap, and the runner stores both. Only an error carrying no analysis at all (a hard parse failure)
refuses the statement.

### `sqlsanitize` — three forms of one statement

| Form | Content | Lives |
| --- | --- | --- |
| Request | What the application sends | The request only |
| Stored (`sql_text`) | Literals → `?`, comments removed, **spacing preserved** | The database, and the fingerprint's input |
| Analyzable | Each `?` → `'?'` | Derived on demand; never stored |

Classification reads the token's source slice, not omni's token-type constants, which are unexported
in every dialect package and would have to be hardcoded. `isLiteral` is the same kind of heuristic:
a leading quote, digit or dot, a `$` that is not a numbered parameter, or a letter-led token that
quotes. `tokenSpans` trims trailing whitespace off a token's end, because PostgreSQL's lexer hands a
string constant the whitespace after it; taken at face value that once stored `= ?AND`.

### The ingest path

```text
authenticate ─▶ read + decode ─▶ resolve instance and engine ─▶ (repeated reportId?)
per statement item:
  split ─▶ sanitize ─▶ gate ─▶ upsert statement ─▶ upsert observation ─▶ roll up ─▶ mirror
after the transaction: queue only the statements the upsert created for analysis
```

The upsert's conflict target is the identity tuple, and a conflict leaves `guid`, `application_sql_id`
and `sql_text` alone: the id is what the GUID already points at, and the text belongs to the first
reporter, since whitespace is not part of the fingerprint. The rollup is recomputed from the
observations rather than trusted from the caller, and a replay is not re-queued because the upsert
never rewrites its `sql_text`.

### Classification and the table-level edge

The runner derives the kind, and it is the only place the read/write rule lives. It writes the kind
only after the edges commit: the kind is a filter cache over the edges, so a stale kind is better
than one that claims a classification the graph does not have.

| Shape | Kind | Edges |
| --- | --- | --- |
| Any relation with a real target | WRITE | `source → statement`, `statement → target` |
| Only temporary targets (a read) | READ | `source → statement` |
| **No relation at all** + `dmltarget` names a target | WRITE | one table-level `statement → target` |
| No relation at all + no target (`SELECT 1`, `SET`, `TRUNCATE`) | READ | none — not drawn |

Zero-relation writes are constant `INSERT`s in every form and a `DELETE` that references no column;
everything else is already answered — an `UPDATE` records `t.* -> t.a` even for a constant
assignment, and every DDL statement has a real target. A statement the analyzer cannot parse never
reaches this package: the ingest gate refuses it first (`PARSE_ERROR`). T-SQL adds shapes of its own
(`MERGE`, `SELECT … INTO`) and refuses a target that is not a registry object (a `@table` variable,
a `#temp` table) rather than naming one the registry can never hold. A table-level edge must carry
a non-nil `Transformation` — the column is `NOT NULL`, so a nil slice fails the whole batch.

A statement that parses but has no lineage to record is not drawn, and the analysis says why: a
shape the analyzer cannot see (`EXEC`, a routine body) becomes a gap on the version row, so a blank
statement is explained; `SELECT 1`, `SET` and `TRUNCATE` stay silent. A zero-relation write target is
named by the statement, so the classifier resolves it through the analyzer's `CanonicalIdentifier`:
`INSERT INTO orders` for the object the registry holds as `Orders` becomes an edge into that object,
not into a GUID the registry does not have. The engine's rule is declared once in its registration
(`EngineRegistration.FoldIdentifierNames`).

### The canvas category

Relations are colored by which writer stored them: `sql`, `openlineage`, `applicationsql`, or
`mixed` when an edge bundles several. `relationOrigin` reads the relation's meta type. The accent is
`--lineage-appsql` (amber, dashed `1 3`), declared in both themes in `main.css`.

A statement node's GUID ends in a generated identifier, so the page resolves a label for it: on
first sight of an `APPLICATION_SQL` GUID it calls `GetApplicationSQL` once and hands the statement's
label to `lineageAssetView` as a name override; the root's own type is seeded before the relations
arrive, which is why the root asks for its label separately. A failed lookup is only logged.

The label is `statementLabel`: whitespace runs collapsed to one space, bounded to 120 runes. It is
computed twice — `backend/store/application_sql.go` fills the mirror's `name`, and
`frontend/src/lib/statementLabel.ts` serves the places that hold the text rather than the mirror row
(the Application SQL list and the graph's per-node lookup) — and the two have to agree, because they
are the same statement read through a breadcrumb and through a canvas node.

`TableLineageSection` names the writer on a relation's row: on the object's own page it says
`lineageGraph.originCurrentObject` rather than printing the generated GUID, and on other pages it
still shows that GUID, because only the canvas resolves a label.

### The rules that shape the fingerprint (N4–N5)

N4 and N5 shape the canonical form and nothing else — not the lineage, not the stored text. They
exist because a *list* in an application's code changes length from execution to execution: an `IN`
list whose length depends on how many values were bound, a batch insert whose size depends on how
many rows were batched. That is what makes D6's bound — rows counted by distinct statement — true of
the application's *source code* rather than of its execution pattern.

| Rule | Folds | Example |
| --- | --- | --- |
| N4 | an all-placeholder `IN` / `NOT IN` list, to one element | `IN (?, ?, ?)` → `IN (?)` |
| N5 | a `VALUES` list of repeated placeholder groups, to the first | `VALUES (?, ?), (?, ?)` → `VALUES (?, ?)` |

**Nothing else folds, and a constant is not folded at all.** A constant becomes `?` and stays where
it is, so the operator around it and the number of placeholders the statement carries are both part
of its identity. A "constant run" folding used to collapse `price * 100 >= 5000` and
`price * 100 <= 2000` into one row, and `id = ? - ?` into `id = ?` — one statement's text, count and
kind attributed to another, the silent corruption D5 exists to prevent. It was removed, and
`testdata/06_operators.yaml` pins the distinction. The rules stay inside the shape they fold: only an
all-placeholder list folds, so `IN (a, b)` and `IN (a, b, c)` stay apart, and a mixed list, a
subquery, `VALUES (?)`'s arity and `NULL` are left alone. Every folding is idempotent, and
`fold_test.go` asserts both halves: what must merge and what must not.

### The corpus

`sanitize_test.go` and `fold_test.go` hold the hand-written cases for one behavior each.
`testdata/*.yaml` is the contract at statement scale: 242 cases in ten suites, each run under every
dialect unless it names the ones it is valid in. A case pins `text` and `canonical` exactly and
nothing else, and both are required — that states the deduplication contract with no second
mechanism: canonical forms written identically must merge, and ones that differ must not, so a
folding suite reads as a list of equivalence classes.

The runner adds what has to hold for every statement whatever its case asserts: no token is a
literal, every byte between tokens is whitespace, the fingerprint is the canonical form's hash,
re-sanitizing the stored text is a no-op, and folding the canonical form again is a no-op. The
loader also refuses the two YAML spellings that would silently weaken a case (a ` #` in a plain
scalar is a comment; trailing whitespace is stripped) and duplicate coverage.

| Suites | Rule |
| --- | --- |
| `01_literals`, `02_comments`, `03_identifiers` | N1–N3, every literal form the lexers emit, and the forms that are *not* comments |
| `04_in_lists`, `05_values`, `06_operators`, `07_statement_shapes` | N4/N5, the operators and placeholder counts that are part of a statement's identity, and realistic statements |
| `08_refusals`, `09_dialects`, `10_mssql` | what must not be stored; the asymmetries below; the T-SQL branch of `tokenSpans` |

Four asymmetries follow from one rule set running over tokens the lexers disagree about. PostgreSQL
lexes `?` as an operator character, so a signed constant is spaced differently (`x = - ? + ?`
against `x = -? + ?`); fingerprints are scoped to an instance, so the forms never meet. StarRocks
and Doris tokenize a `/*+ … */` hint as code, so the hint is stored and fingerprinted while it is
dropped elsewhere. MySQL tokenizes a `/*! … */` executable comment into unusable fragments, so the
statement is refused. T-SQL keeps `N'…'`, `[…]`, `"…"` and `#tmp` whole, so the dialect has its own
branch in `tokenSpans`.

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

`Version` is **1**: the foldings were added while this feature was still unreleased, so they are part
of the first rule set rather than a second one with rows to migrate. Its purpose is the *next* rule
change: rewriting a rule splits one statement into rows fingerprinted under the old one, and a
deployment that holds rows needs the column to find out which.

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

## Code map

| Concern | Path |
| --- | --- |
| Sanitizer and zero-relation target | `backend/plugin/sqlsanitize/` — `sanitize.go`, `token.go`, `fold.go`; `backend/plugin/dmltarget/` — `target.go` |
| Store | `backend/store/` — `application.go`, `application_api_key.go`, `application_sql.go` |
| Endpoint and service | `backend/api/v1/application_sql_handler.go`, `application_service.go`; route in `backend/server/grpc_routes.go` |
| Runner | `backend/runner/lineageanalyzer/` — `queueAll` (`analyzer.go`), `buildSQL` (`build_sql.go`), `classifyApplicationSQL` (`classify.go`) |
| Schema | `backend/migrator/migration/LATEST.sql` and `0.1/0019##application_sql.sql` |
| SPA | `frontend/src/pages/{ApplicationManagementPage,ApplicationSQLPage}.vue`, `src/components/application/ApplicationSelect.vue`, `src/components/metadata/ApplicationSQL{List,MetadataDetail}.vue`, `src/api/application.ts`, `src/lib/{lineageOrigin,statementLabel}.ts` |
| Permissions | `backend/common/permission/permission.json` (`applications.*`, `applicationSqls.*`) |

Test pointers: `sqlsanitize/{sanitize,fold,neutral,corpus,yaml,mssql_token}_test.go` and
`testdata/*.yaml`; `dmltarget/target_test.go`; `store/application{,_sql}_pure_test.go`;
`runner/lineageanalyzer/classify_test.go`; `api/v1/application_sql_handler_test.go` and
`lineage_service_graph_test.go`; the whole chain in
`test/integration/runner/application_sql_service_test.go` (gate: `go test -tags=integration -run
ApplicationSQLReport ./backend/test/integration/runner`); SPA in `lineageOrigin.test.ts`,
`lineageAsset.test.ts`, `permissions.test.ts`, `ApplicationSQLFacts.test.ts`,
`TableLineageSection.test.ts`, `statementLabel.test.ts`, `SchemaDefinitionDialog.test.ts`
(gate: `pnpm --dir frontend biome:check && lint && i18n && type-check && test run`).

Two sanitizer suites guard a rule change from opposite ends.
`TestSanitizedStatementsKeepTheirLineage` is the acceptance gate: it walks the repository's lineage
corpus — 135 suites / 1,114 cases across every dialect, of which the test runs the four parser
families with a registered analyzer — and requires that sanitizing never makes an analyzable
statement unanalyzable, with 0 refusals and 100% idempotency. `TestSanitizeYAMLCorpus` is the
contract: 223 cases whose stored text and canonical form are pinned exactly, so a change has to be
stated as a deliberate edit rather than discovered as a moving fingerprint. The cost is measured
rather than assumed: `BenchmarkIngestCost` puts the sanitizer beside the analyzer it feeds,
`BenchmarkSanitize` and `BenchmarkSanitizeLayers` split the layers, and
`TestSanitizeAllocationBudget` budgets allocations because they do not depend on the machine.
