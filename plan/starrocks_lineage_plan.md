# Plan: StarRocks Column-Level Lineage Analyzer (omni-backed)

> **Status: in progress — P0 through P4 landed.** `buildSQL` pass-through, the
> package foundation, SELECT (plain, CTE, derived tables, set operations,
> expression subqueries), the DDL targets (CREATE VIEW / MATERIALIZED VIEW /
> CTAS) and the DML statements (INSERT / UPDATE / DELETE / COPY / LOAD) are in
> the tree and lint-clean; the engine is not registered yet, which is P5.
> Design verified against `github.com/bytebase/omni`
> `v0.0.0-20260912023254-4574e69bb9f1` and a live StarRocks 4.1 container; both
> blockers in "Findings" are reproduced, not theoretical.
>
> Related: `plan/mysql_omni_parser_migration_plan.md` (the pattern this
> follows), `plan/mysql_family_dialect_lineage_plan.md` (why a dialect gets its
> own package), `plan/starrocks_doris_sync_plan.md` (the driver and its
> "lineage not registered" gap).

## TL;DR

Add `backend/plugin/lineage/starrocks`, built on
`github.com/bytebase/omni/starrocks/{parser,ast}`, registering
`storepb.Engine_STARROCKS` through the existing
`lineage.RegisterAnalyzeRelation` seam. The `scope` / `model` / `catalog` layers
are reused unchanged.

**This is a genuine port, not a dialect copy.** omni's StarRocks AST is
structurally different from its MySQL AST: `SelectStmt` has no `SetOp` field
(set operations are a separate `SetOpStmt` node), the select list is
`Items []*SelectItem` (not `TargetList []ExprNode`), `TableRef.Name` is an
`*ObjectName` (not `Name`+`Schema` strings), and three query bodies are only
available as **raw text** (`TableRef.Subquery`, `CreateTableStmt.AsSelect`,
`SubqueryExpr`). Source-compatible copies — the technique
`plan/mysql_family_dialect_lineage_plan.md` used for TiDB/MariaDB — do not
apply.

Two integration defects must land with it, or StarRocks lineage fails on the
most common objects (both reproduced below):

1. `buildSQL` in `backend/runner/lineageanalyzer/analyzer.go` double-wraps
   StarRocks materialized-view definitions, producing an unparseable statement.
2. omni's `CREATE VIEW … AS` grammar rejects `WITH`, `UNION` and parenthesized
   view bodies — all valid, common StarRocks view definitions.

## Decisions (settled)

| Decision | Choice |
| --- | --- |
| Engine scope | `STARROCKS` only. `DORIS` keeps its deliberate `ErrorEngineNotSupported` skip and gets a separate plan. |
| Capability | Parity with `mysql/analyzer.go`: SELECT / CTE / derived table / set operation / window + CTAS / INSERT / UPDATE / DELETE + transformations. |
| Package layout | One dialect package, `backend/plugin/lineage/starrocks`, on `omni/starrocks`. |
| View-body parser gap | Work around it in the dialect package (extract the body after the top-level `AS`, parse it as a standalone query). The gap is recorded in this document only — **no upstream omni contact**. |
| MV definition wrapping | `buildSQL` passes a definition through when it is already a complete `CREATE …` statement. |
| Parse policy | Hard failure, matching the MySQL analyzer: unparseable SQL is an error with no partial result, recorded in `column_lineage_version.error_message`. |
| DORIS | Not in this plan. Decide after StarRocks lands. `omni/doris` exists and its AST is near-identical, so a follow-up package would be cheap. |
| Integration tests | Not added now. Coverage stays hermetic (YAML corpus + hard-fail); a live StarRocks check is a development-time activity only. |
| Upstream contact | No issue, PR or report is filed against `github.com/bytebase/omni`. Every parser gap found is written down here instead. |

## Findings (verified)

### F1 — StarRocks stores the full MV DDL, so `buildSQL` produces a doubled statement

`backend/plugin/db/starrocks/sync.go` reads
`information_schema.materialized_views.MATERIALIZED_VIEW_DEFINITION` straight
into `MaterializedViewMetadata.Definition`. On a live StarRocks 4.1 that column
is the **entire** `SHOW CREATE MATERIALIZED VIEW` output:

```sql
CREATE MATERIALIZED VIEW `mv1` (`id`, `cnt`, `total`)
DISTRIBUTED BY HASH(`id`)
REFRESH ASYNC
PROPERTIES ("replicated_storage" = "true", "replication_num" = "1")
AS SELECT id, count(*) AS cnt, sum(amount) AS total FROM lineage_test.t1 GROUP BY id;
```

`buildSQL` unconditionally wraps a definition as `CREATE MATERIALIZED VIEW
<name> AS <definition>`, yielding `CREATE MATERIALIZED VIEW \`mv1\` AS CREATE
MATERIALIZED VIEW …`. Parsing that with omni fails:

```
syntax error at or near CREATE   (loc 34–40)
```

This is latent today only because no analyzer is registered, so the runner
records a skip before analysis. Registering the analyzer without fixing
`buildSQL` would hard-fail every StarRocks materialized view.

By contrast, `information_schema.views.VIEW_DEFINITION` is the bare query body
(and preserves a leading `WITH`), which is what `buildSQL` expects.

Doris differs again: `mv_infos().QuerySql` returns the bare query body.

### F2 — omni's `CREATE VIEW` grammar is missing `WITH`, `UNION` and parentheses

`starrocks/parser/view.go:parseCreateView` calls `parseSelectStmt()` directly,
which parses only a plain `SELECT`. The top-level statement path
(`parseWithStatement` + `parseSetOpTail`) supports the rest. Measured results:

| Statement | omni result |
| --- | --- |
| `CREATE VIEW v AS SELECT …` | OK |
| `CREATE VIEW v AS WITH c AS (…) SELECT …` | **FAIL** `syntax error at or near WITH` |
| `CREATE VIEW v AS SELECT … UNION ALL SELECT …` | **FAIL** `syntax error at or near UNION` |
| `CREATE VIEW v AS (SELECT …)` | **FAIL** `syntax error at or near (` |
| `ALTER VIEW v AS SELECT … UNION …` | **FAIL** |
| bare `WITH … SELECT … UNION …` | OK |
| `INSERT INTO t2 SELECT … UNION ALL SELECT …` | OK |
| full `CREATE MATERIALIZED VIEW … AS WITH … SELECT …` | OK |
| `CREATE TABLE t AS WITH … SELECT … UNION …` | OK (raw-text capture) |

All of the failing forms are valid StarRocks: a live `CREATE VIEW` with a CTE
round-trips through `SHOW CREATE VIEW` and `VIEW_DEFINITION` with the `WITH`
clause intact, as does a `UNION ALL` view.

### F3 — Capabilities the port can rely on

- `parser.Parse(input) (*ast.File, []ParseError)`; `ast.File.Stmts []Node`.
- `parser.Split(input) []Segment{Text, ByteStart, ByteEnd}` — usable to reject
  multi-statement input.
- `parser.Tokenize(input) ([]Token, []LexError)`; `parser.KeywordToken(name)`.
- `ast.NodeLoc(node)` / `ast.Inspect(node, fn)` — no reflection needed (the
  MySQL port needed `reflect` because omni's MySQL nodes expose no `NodeLoc`).
- Full asynchronous-MV DDL parses, including `DISTRIBUTED BY` / `REFRESH ASYNC`
  / `ON SCHEDULE` / `PROPERTIES`.
- Derived tables and CTAS keep the query as raw text with an absolute
  `TextStart`: `TableRef.Subquery{SQLText, TextStart}` and
  `CreateTableStmt.AsSelect{RawText, TextStart}`. `TextStart` is the offset of
  the text in the outer statement.
- `CREATE TABLE … AS <anything>` is lenient because the body is captured raw.

### F4 — Remaining parser gaps (avoid in the corpus; not blockers)

`SELECT * REPLACE (…)`, `GROUP BY ALL`, and
`CREATE VIEW … SECURITY NONE …` (the latter matters only if a user pastes
`SHOW CREATE VIEW` output into MANUAL_SQL).

### F5 — `omni/starrocks/analysis` is not sufficient

`analysis.GetQuerySpan` returns access tables and a "best-effort" output-column
list. It has no target-table mapping, no transformations, and no CTE/subquery
flattening, so it cannot express our `model.ColumnRelation`. It is useful as a
reference for raw-text/CTE scoping only; the walker stays in-repo, as with
MySQL.

## Target architecture

### Package layout

```
backend/plugin/lineage/starrocks/
    analyzer.go            # dispatch + SELECT/FROM/CTE/set-op + DDL/DML targets
    expr.go                # exprText, collectColumns, transformation detection
    rawsql.go              # raw-text re-parse + source stack
    viewbody.go            # CREATE/ALTER VIEW, CREATE MV "AS body" extraction (F2)
    analyze_test.go
    analyze_test_helper.go
    hardfail_test.go
    testdata/analyze/*.yaml
```

Only `storepb.Engine_STARROCKS` is registered. The runner already degrades
cleanly for `DORIS`.

### Registration seam

Unchanged. `backend/server/ultimate.go` gains
`_ "github.com/Ranxy/metaxisdata/backend/plugin/lineage/starrocks"`.
`backend/api/v1/explain_sql_service.go` shares `GetAnalyzeRelation`, so Explain
SQL inherits StarRocks support automatically.

### Source context (the raw-text model)

omni downgrades three query bodies to text. A small `source` value carries the
text, its tokens, and its offset:

```go
type source struct {
    text   string          // SQL text at this nesting level
    tokens []parser.Token  // parser.Tokenize(text)
    base   int             // offset in the whole statement (0 at top level)
}
```

- Top level: `source{text: sql, base: 0}`.
- Descending into `TableRef.Subquery`, `RawQuery`, or `SubqueryExpr`: push a
  child source (`text = RawText`, `base = TextStart`); pop when done.
- `exprText` reconstructs whitespace-free expression text **within the current
  source** by concatenating token slices in the span, mirroring the MySQL
  analyzer's ANTLR-equivalent output. `base` is used only for diagnostics, so
  no absolute-offset remapping is needed.
- A raw-text body that fails to re-parse is a hard analysis error.

### Identifier mapping

`ObjectName.Parts` is normalized by segment count. The metadata registry has no
catalog dimension, so a catalog qualifier is dropped (recorded as a known
limitation):

| SQL form | Parts | Mapping |
| --- | --- | --- |
| `col` | 1 | `Column=col` (resolved by scope) |
| `tbl.col` | 2 | `Table=tbl, Column=col` |
| `db.tbl.col` | 3 | `Schema=db`, `Table=tbl, Column=col` |
| `cat.db.tbl.col` | 4 | catalog dropped → same as 3 |
| table `db.tbl` / `cat.db.tbl` | — | `TableRef{Schema: db, Table: tbl}` |

`scope.NewLineageEdge` maps the SQL "schema" qualifier onto
`ObjectIdentifier.Database`, and `catalog.GetTable` consumes `Schema` as the
database, so StarRocks (`database.table`, no schema concept) lines up with the
existing wildcard-expansion path with no changes.

### Statement dispatch

`AnalyzeRelations()` counts `parser.Split` segments (exactly one required),
parses, and dispatches on `file.Stmts[0]`:

| AST node | Handling |
| --- | --- |
| `*ast.SelectStmt` | query (including `With`) |
| `*ast.SetOpStmt` | flatten arms, process each, merge output columns positionally (`mergeUnionOutputColumns`) |
| `*ast.ParenSelect` | unwrap `.Sel` |
| `*ast.CreateViewStmt` / `*ast.AlterViewStmt` | target = view; `Columns []*ViewColumn` overrides output aliases |
| `*ast.CreateMTMVStmt` | target = MV; same column override |
| `*ast.CreateTableStmt` | `AsSelect != nil` → CTAS target; `CTASColumns` overrides aliases; `Like` carries no lineage |
| `*ast.InsertStmt` | `Query != nil` → positional column mapping; `Values` → no lineage; `ByName` per below |
| `*ast.UpdateStmt` | `From` into scope, then `Assignments` |
| `*ast.DeleteStmt` | `Using` into scope, `Where` columns → `__deletion__` |
| `*ast.CopyIntoStmt` / `*ast.LoadDataStmt` | `__file__` → target table (optional phase) |
| anything else | no lineage, no error (matching MySQL) |

FROM entries are `*ast.TableRef` / `*ast.JoinClause`; `*ast.InlineTable` and
`*ast.TableFunctionRef` carry no physical-table lineage (same as MySQL's
function-in-FROM).

### Expression and transformation layer

- `collectColumns(expr)` walks with `ast.Inspect` for `*ast.ColumnRef` and
  applies the identifier mapping above.
- Aggregate / window / function / case / operator detection reuses the MySQL
  text heuristics, fed by `exprText`. Window `PARTITION BY` / `ORDER BY` read
  the structured `FuncCallExpr.Over.PartitionBy` / `.OrderBy` instead of MySQL's
  string hack — more accurate, same `model.WindowTransformation` shape.
- Expression subqueries (`Scalar`, `IN`, `EXISTS`) re-parse their `RawText` and
  are analyzed like a derived table; their resolved real source columns are
  flattened onto the enclosing output column. This is deliberately **more
  correct** than MySQL's current behavior, which resolves a scalar subquery's
  inner columns against the outer FROM (see the `scalar_subquery` case in
  `mysql/testdata/analyze/16_test_extended_forms_lineage_table.yaml`). The
  StarRocks corpus pins the correct semantics and the divergence is documented.
- `SELECT * EXCEPT (cols)`: with catalog metadata, expand then subtract; without
  it, fall back to the wildcard edge.
- `INSERT … BY NAME`: without catalog metadata the target column is the output
  alias. Documented approximation.

### F2 workaround (`viewbody.go`)

When parsing fails **and** the statement begins with `CREATE`/`ALTER` `VIEW` or
`MATERIALIZED VIEW`:

1. Tokenize; find the first `AS` keyword token at paren depth 0. (A CTE's inner
   `AS` always follows it; `'… AS …'` is one string token; `REFRESH ASYNC` is a
   single `ASYNC` token.)
2. `body := sql[asTok.Loc.End:]`; `parser.Parse(body)` uses the **top-level**
   query path, which accepts `WITH`, set operations and parentheses.
3. If the body is itself a complete `CREATE … VIEW …` (the F1 doubled form),
   recurse one level; otherwise treat it as the query. The target name is the
   last part of the DDL's `ObjectName`; explicit column names come from
   `ViewColumn`.

This makes every F2 form analyzable while the parser gap is open, and
also covers view DDL pasted into MANUAL_SQL.

### F1 fix (`buildSQL`)

```go
// A definition that is already a complete statement is used as-is: StarRocks
// stores SHOW CREATE MATERIALIZED VIEW output verbatim, so wrapping it again
// would produce "CREATE MATERIALIZED VIEW x AS CREATE MATERIALIZED VIEW ...".
if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(definition)), "CREATE ") {
    return definition, definition, nil
}
```

Engine-agnostic, no special-casing, no change to stored-data semantics, and
covered by `TestBuildSQL`.

## Work breakdown

| Phase | Work | Exit criteria | Status |
| --- | --- | --- | --- |
| P0 | `buildSQL` pass-through + test; package skeleton, `source`, `exprText`, `collectColumns`; plain SELECT end-to-end | `TestBuildSQL` green; package compiles; hermetic tests green | **landed** |
| P1 | SELECT core: CTE, derived tables (raw text), set operations, `source` push/pop, temp-table flattening | corpus 03–05 green | **landed** |
| P2 | Expression layer hardening: expression subqueries (scalar / `IN` / `EXISTS`), wildcard expansion over aliased relations, `relation_type`/`is_temp` coverage; fix a set operation nested in a temp table dropping its non-first arms | corpus 16 green (15 cases); `relation_type`/`is_temp` consistent with the shared algorithm layer | **landed** |
| P3 | DDL targets: CREATE VIEW / MV / CTAS + `viewbody.go` | `WITH` / `UNION` / parenthesized view corpus green | **landed** |
| P4 | DML: INSERT (incl. Overwrite/ByName), UPDATE, DELETE, COPY/LOAD | DML corpus green | **landed** |
| P5 | Register in `ultimate.go`; corpus completion; live StarRocks end-to-end check | `go test ./...` green; lint/build green; real view/MV produce edges | pending |

### P0 landed (what is actually in the tree)

| Artifact | What it does |
| --- | --- |
| `runner/lineageanalyzer/analyzer.go` | `isCompleteStatement` pass-through in `buildSQL`, plus two `TestBuildSQL` cases (StarRocks full MV DDL passthrough, StarRocks view still wrapped) |
| `lineage/starrocks/analyzer.go` | parse + single-statement rejection + hard-fail; dispatch; SELECT traversal; edge generation and dedup |
| `lineage/starrocks/rawsql.go` | `source` + `currentSource` + `exprText` / `exprTextOf` (whitespace-free reconstruction from tokens) |
| `lineage/starrocks/expr.go` | `ObjectName` → `scope.ColumnRef` / table mapping (catalog dropped), `collectColumns`, alias inference, aggregate/window/function/case/operator transformation detection |
| `lineage/starrocks/{analyze_test_helper,analyze_test,analyzer_test,hardfail_test}.go` | YAML-suite wiring, helper unit tests, hard-fail and multi-statement pins |
| `lineage/starrocks/testdata/analyze/01_test_select_lineage_table.yaml` | 12 hermetic SELECT cases |

### P1 landed (what is actually in the tree)

| Artifact | What it does |
| --- | --- |
| `lineage/starrocks/rawsql.go` | `pushSource` / `popSource` and `parseRawQuery`, the re-parse path for query bodies omni keeps as text |
| `lineage/starrocks/analyzer.go` | `processQueryNode`; `processCTEs` / `processCTE`; `flattenSetOpArms` / `processSetOperation` / `mergeUnionOutputColumns`; `processDerivedTable`; scope stack (`pushScope` / `popScope`); temp-table tracking; `traceThroughTableLineage`, `appendFlattenedLineage`, `flattenTempSourceLineage` |
| `lineage/starrocks/expr.go` | `combineTransformations` |
| `lineage/starrocks/testdata/analyze/{03,04,05}_*.yaml` | derived tables (5), CTEs (4) and set operations (5) |

### P2 landed (what is actually in the tree)

| Artifact | What it does |
| --- | --- |
| `lineage/scope/types.go`, `scope/scope.go` | `ColumnRef.Resolved` and the `ResolveColumn` pass-through: a reference already resolved in the scope it came from is not rebound by a later lookup in a sibling scope. Other analyzers never set it, so their behavior is byte-identical; a guard test pins that |
| `lineage/starrocks/analyzer.go` | `analyzeRawQueryScope` + `tempTableShape` (shared by derived tables and expression subqueries); `expressionSubquerySources`, wired into `processSelectExpr`; `resolveOutputColumns` in `processSetOperation`; `wildcardSourceRef` for `*` / `t.*` over aliased relations; catalog expansion marks its refs resolved |
| `lineage/starrocks/testdata/analyze/16_test_extended_forms_lineage_table.yaml` | 16 extended forms: window/aggregate-over, CASE, COALESCE, GROUP_CONCAT, COUNT(DISTINCT), aliased `*`/`t.*`, `COUNT(*)`, comma join, scalar subqueries, and set operations nested in a derived table / CTE / aliased CTE wildcard |

Full corpus is now 41 cases across 5 suites (01, 03, 04, 05, 16). `relation_type`
and `is_temp` are asserted explicitly in the extended-forms suite.

### P3 landed (what is actually in the tree)

| Artifact | What it does |
| --- | --- |
| `lineage/starrocks/viewbody.go` | `extractViewDDL`: the F2 workaround. When omni rejects a view statement, the target name, declared column list and query body are recovered from the token stream so the body can be parsed as a top-level query (which accepts `WITH`, set operations and parentheses) |
| `lineage/starrocks/analyzer.go` | `processDDLTarget` (CREATE/ALTER VIEW, CREATE MATERIALIZED VIEW), `processViewDDL` (extracted body), `processCreateTable` (CTAS from `RawSelect`), `generateEdgesForTarget`, `traceThroughTableLineageToTarget`, `viewColumnNames`, `isTableTempInCurrentScope`; `inDDLTarget` suppresses `__result__` edges inside a DDL body |
| `lineage/starrocks/viewbody_test.go` | 11 extractor cases (CTE / set-operation / parenthesized bodies, MV full DDL, `SECURITY NONE`, `COMMENT`, `ALTER VIEW`, and four non-matches) plus the CTAS `LIKE` no-lineage assertion |
| `lineage/starrocks/testdata/analyze/{07,08,17}_*.yaml` | CTAS (6), CREATE/ALTER VIEW (11) and materialized view (5) |

Full corpus is now 63 cases across 8 suites. DDL targets are asserted with
`is_temp: false`, and no `__result__` edges are emitted for a DDL body.

### P4 landed (what is actually in the tree)

| Artifact | What it does |
| --- | --- |
| `lineage/starrocks/analyzer.go` | `processInsertStatement` (SELECT / OVERWRITE / BY NAME), `processUpdateStatement` + `processUpdateList`, `processDeleteStatement`, `processCopyInto`, `processLoadStatement`; `addBaseTable`; `__deletion__` and `__file__` markers re-added; `inDDLTarget` generalized to `inTargetContext` so an INSERT body is suppressed the same way a DDL body is |
| `lineage/starrocks/expr.go` | `normalizeExpressionText` (used by the UPDATE/DELETE transform text) |
| `lineage/starrocks/dml_test.go` | `INSERT ... VALUES` and `DELETE` with no `WHERE` return no edges and no error |
| `lineage/starrocks/testdata/analyze/{06,09,10,18}_*.yaml` | INSERT (8), UPDATE (4), DELETE (4), COPY/LOAD (4) |

Full corpus is now 83 cases across 12 suites. `MERGE INTO` is the only
statement kind that still fails loudly (see "Remaining gaps"); every other DML
form either produces lineage or is a statement kind that genuinely has none.

DML notes:

- `INSERT ... BY NAME` matches source column names to target column names; the
  target's catalog is not consulted, so the source alias is the target name.
- `LOAD ... SET (...)` is captured by omni only as raw text, so it contributes
  no column lineage; the file-to-column edges still do.
- An assignment or condition subquery contributes its own output sources (for
  example `DELETE FROM t1 WHERE id IN (SELECT id FROM t2 ...)` yields both
  `t1.id` and `t2.id` as `__deletion__` sources). The MySQL analyzer does not
  trace subqueries in these positions.

No statement kind is silently ignored any more: `MERGE INTO` is the one DML
form this analyzer does not model, and it fails loudly
(`analysis errors: MERGE analysis is not implemented yet`) rather than returning
no lineage, so it is visible in `column_lineage_version.error_message`. It is
pinned by `TestUnimplementedStatementsFailLoudly`. The package does **not** call
`RegisterAnalyzeRelation` yet, so production behavior is unchanged.

## Files

| Path | Action |
| --- | --- |
| `backend/plugin/lineage/starrocks/*` | ADD (analyzer, tests, corpus) |
| `backend/runner/lineageanalyzer/analyzer.go` | EDIT `buildSQL` (pass-through) |
| `backend/runner/lineageanalyzer/analyzer_test.go` | ADD StarRocks MV case |
| `backend/plugin/lineage/scope/{types,scope}.go` | EDIT `ColumnRef.Resolved` + `ResolveColumn` pass-through (P2) |
| `backend/server/ultimate.go` | ADD blank import |
| `backend/plugin/lineage/{model,catalog,lineage}.go` | UNCHANGED |
| `go.mod` | UNCHANGED (`github.com/bytebase/omni` already required) |

## Verification

1. `go test ./backend/plugin/lineage/... -count=1` — YAML corpus + hard-fail
   (hermetic).
2. `go test ./... -count=1` green; `gofmt` clean;
   `golangci-lint run --allow-parallel-runners` clean (repeat until clean).
3. `go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go`.
4. **Live StarRocks end-to-end** (local container, `localhost:9030`; CI has no
   StarRocks, so this is a development check): create a database, a plain view,
   a CTE view, a `UNION` view and an async MV; sync metadata; run the analyzer;
   assert the edge sets. This is the only way to exercise F1 and F2 for real.
5. Confirm `DORIS` still resolves to `ErrorEngineNotSupported` and the runner
   records a deliberate skip (not a crash).

## Risks and limitations

| Risk | Mitigation |
| --- | --- |
| omni `CREATE VIEW` grammar gap | `viewbody.go` workaround, with token-extraction feasibility already verified. Recorded here; no upstream contact |
| Raw-text re-parse failure (malformed subquery) | Hard failure with the reason in `column_lineage_version.error_message` (visible, never silent), matching MySQL |
| Cross-catalog references (`hive_catalog.db.tbl`) | Catalog dimension does not exist in the registry; dropped and documented. A real cross-catalog model is its own plan |
| Expression-subquery behavior differs from MySQL | Intentional (MySQL's is a bug); corpus pins the correct expectation and the difference is documented |
| No StarRocks CI coverage | Hermetic corpus is the contract; live end-to-end is a development-time check only, by decision |
| `SELECT * REPLACE` / `GROUP BY ALL` unsupported | Kept out of the corpus; recorded as known gaps |
| `MERGE INTO` has no analyzer | Fails loudly rather than silently reporting no lineage; a follow-up if a real MERGE appears in analyzed objects |

### Fixed in P2: set operation nested in a temporary table

The P1 analyzer kept **only the first arm** of a set operation nested inside a
CTE or derived table. `mergeUnionOutputColumns` merges unresolved
`scope.ColumnRef`s, and the enclosing CTE/derived table later resolved them
against a scope holding only the first arm's relations, so later arms collapsed
onto the first arm's table or failed to resolve. The MySQL analyzer still has
this gap (reproduced: `SELECT u.id FROM (SELECT id FROM t1 UNION ALL SELECT id
FROM t2) u` yields only `t1.id`).

P2 resolves each arm's references **within that arm's scope** before merging and
marks them resolved, so both arms survive:

```
SELECT u.id FROM (SELECT id FROM t1 UNION ALL SELECT id FROM t2) u
  now -> t1.id -> __result__.id  and  t2.id -> __result__.id
```

Covered by `set_operation_nested_in_derived_table` and
`set_operation_nested_in_cte` in the extended-forms corpus.

### Fixed in P2: expression subqueries

omni models a scalar / `IN` / `EXISTS` subquery as `SubqueryExpr`, a walker leaf
whose body is only `RawText`, so P1 saw no columns inside it and the enclosing
expression fell back to a wildcard reference to the outer FROM relations:

```
SELECT (SELECT MAX(salary) FROM employees) AS m FROM departments
  P1    -> departments.*  -> __result__.m   (wrong table)
  MySQL -> departments.salary -> __result__.m (wrong too: binds the inner column to the outer table)
  now   -> employees.salary -> __result__.m
```

P2 re-parses `SubqueryExpr.RawText` in its own scope (via `analyzeRawQueryScope`)
and flattens the subquery's resolved sources onto the enclosing output column,
marking them resolved. Covered by `scalar_subquery` and
`scalar_subquery_alongside_outer_columns`.

### Fixed in P2: `*` / `t.*` over an aliased relation

`processStar` emitted the relation's real table name as the source reference,
then resolution looked it up by name in a scope keyed by alias, failed, and
dropped the edge — so `SELECT * FROM t x` produced **no lineage at all**. The
MySQL analyzer still has this gap (verified: `SELECT * FROM employees e` yields
0 edges there). P2 marks a base table's `*` reference resolved (it is already
fully identified), and looks a CTE/derived table up by its scope key so the
temp-table trace still applies. Catalog-driven expansion marks its references
resolved for the same reason. Covered by `star_join_with_aliases`,
`table_star_with_alias`, `count_star_with_alias` and `cte_star_with_alias`.

### Deliberate divergences from the MySQL analyzer

| Shape | MySQL | StarRocks |
| --- | --- | --- |
| `WITH c (a, b) AS (SELECT id, name FROM users) SELECT c.a, c.b FROM c` | no edges | `users.id -> __result__.a`, `users.name -> __result__.b` |
| `SELECT * FROM t x` / `SELECT x.* FROM t x` | no edges | `t.* -> __result__.*` |
| Set operation nested in a CTE / derived table | first arm only | every arm |
| `SELECT (SELECT MAX(salary) FROM employees) AS m FROM departments` | `departments.salary` (wrong table) | `employees.salary` |
| DDL body (`CREATE VIEW` / `MATERIALIZED VIEW` / CTAS) | also emits `__result__` edges | only the target edges |
| `CREATE VIEW v AS SELECT id FROM t1 UNION ALL SELECT id FROM t2` | target edges from the first arm only | both arms |

All of these produce strictly more correct, less noisy lineage; each is pinned by
the corpus and recorded here so the difference is not mistaken for a bug. The
transform for a scalar subquery's enclosing column is `PROJECT` (the subquery is
opaque text), where MySQL infers `AGGREGATE` from the inner call — a metadata
nuance, not a missing edge.

Suppressing `__result__` inside a DDL body is safe for the runner: for
`VIEW`/`MATERIALIZED_VIEW` it drops only `IsTemp` rows it discards anyway, and
for `MANUAL_SQL` the discarded rows were duplicates of the source columns the
non-temp target edges already contribute.

### Shared `scope` change introduced in P2

`scope.ColumnRef` gained a `Resolved` field and `Scope.ResolveColumn` returns
such a reference unchanged. It exists because a resolved table can live in a
scope that a later lookup must not search. Every other analyzer leaves the field
false, so MySQL/TiDB/MariaDB/PostgreSQL behavior is unchanged — their full
corpora still pass — and `TestScope_ResolveColumn_ResolvedPassThrough` pins the
contract.

## Decisions after review

1. **StarRocks first.** `DORIS` is deferred; revisit only after this lands.
2. **No integration tests.** The hermetic suite is the contract; the live
   StarRocks container is used for manual verification during development.
3. **No upstream omni contact.** Every parser gap is documented here (F2, F4)
   instead of being filed, so the record lives with the code.

## Open questions

None blocking. `DORIS` scope and any future gated StarRocks integration suite
are deferred decisions, not prerequisites.

## Appendix — omni StarRocks API quick reference

```go
// github.com/bytebase/omni/starrocks/parser
func Parse(input string) (*ast.File, []ParseError)   // strict
func Split(input string) []Segment                   // {Text, ByteStart, ByteEnd}
func Tokenize(input string) ([]Token, []LexError)
func KeywordToken(name string) (TokenKind, bool)
type Token struct { Kind TokenKind; Str string; Ival int64; Loc ast.Loc }

// github.com/bytebase/omni/starrocks/ast
func NodeLoc(n Node) Loc                              // no reflection needed
func Inspect(node Node, f func(Node) bool)
type Loc struct{ Start, End int }

type File struct{ Stmts []Node; Loc Loc }
type ObjectName struct{ Parts []string; Loc Loc }
type SelectStmt struct{ With *WithClause; Distinct, All bool; Items []*SelectItem
    From []Node; Where Node; GroupBy []Node; Having, Qualify Node
    OrderBy []*OrderByItem; Limit, Offset Node; Into *IntoOutfileClause; Loc Loc }
type WithClause struct{ Recursive bool; CTEs []*CTE; Loc Loc }
type CTE struct{ Name string; Columns []string; Query Node; Loc Loc }
type SelectItem struct{ Expr Node; Alias string; Aliased, Star bool
    TableName *ObjectName; ExceptColumns []string; Loc Loc }
type TableRef struct{ Name *ObjectName; Alias string; Subquery *SubqueryExpr
    TabletIDs []int64; Loc Loc }
type JoinClause struct{ Type JoinType; Left, Right Node; Natural bool; On Node
    Using []string; Hints []string; Loc Loc }
type SetOpStmt struct{ Op SetOperator; All bool; Left, Right Node
    OrderBy []*OrderByItem; Limit, Offset Node; Loc Loc }
type ParenSelect struct{ Sel Node; OrderBy []*OrderByItem; Limit, Offset Node; Loc Loc }
type CreateViewStmt struct{ Name *ObjectName; OrReplace, IfNotExists bool
    Columns []*ViewColumn; Comment string; Query Node; Loc Loc }
type AlterViewStmt struct{ Name *ObjectName; Columns []*ViewColumn; Query Node; Loc Loc }
type ViewColumn struct{ Name, Comment string; Loc Loc }
type CreateMTMVStmt struct{ Name *ObjectName; IfNotExists bool; BuildMode, RefreshMethod string
    RefreshTrigger *MTMVRefreshTrigger; Columns []*ViewColumn; Comment string
    PartitionBy *PartitionDesc; DistributedBy *DistributionDesc
    Properties []*Property; Query Node; Loc Loc }
type CreateTableStmt struct{ Name *ObjectName; IfNotExists, External, Temporary bool
    Columns []*ColumnDef; KeyDesc *KeyDesc; PartitionBy *PartitionDesc
    DistributedBy *DistributionDesc; Properties []*Property; Engine, Comment string
    Like *ObjectName; AsSelect *RawQuery; CTASColumns []string; Loc Loc }
type RawQuery struct{ RawText string; TextStart int; Loc Loc }
type InsertStmt struct{ Overwrite bool; Target *ObjectName; Label string
    Partition []string; TempPartition, PartitionStar bool; Columns []string
    ByName bool; FileTarget []*Property; Values [][]Node; Query Node; Loc Loc }
type Assignment struct{ Column *ObjectName; Value Node; Loc Loc }
type UpdateStmt struct{ With *WithClause; Target *ObjectName; TargetAlias string
    Assignments []*Assignment; From []Node; Where Node; Loc Loc }
type DeleteStmt struct{ With *WithClause; Target *ObjectName; TargetAlias string
    Partition []string; Using []Node; Where Node; Loc Loc }
type CopyIntoStmt struct{ Target *ObjectName; Source string; Files []string
    Pattern string; Properties []*Property; Loc Loc }
type LoadDataStmt struct{ Label string; DataDescs []*LoadDataDesc; /* … */ Loc Loc }
type ColumnRef struct{ Name *ObjectName; Loc Loc }
type FuncCallExpr struct{ Name *ObjectName; Args []Node; Distinct, Star bool
    OrderBy []*OrderByItem; Separator string; IgnoreNulls bool; Over *WindowSpec; Loc Loc }
type WindowSpec struct{ Name string; PartitionBy []Node; OrderBy []*OrderByItem
    Frame *WindowFrame; Loc Loc }
type SubqueryExpr struct{ RawText string; TextStart int; Loc Loc }
type CaseExpr struct{ Kind CaseKind; Operand Node; Whens []*WhenClause; Else Node; Loc Loc }
type BinaryExpr struct{ Op BinaryOp; Left, Right Node; Loc Loc }
```
