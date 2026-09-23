# Plan: MySQL lineage analyzer — correctness, coverage, performance and debt remediation

> **Status: implemented and remediated.** The implementation is commit
> `a90e1e2`. A review of it found that several "shipped" rows below were true
> only for the MySQL family, and that the test harness could not detect the
> difference; the follow-up commits fixed those, migrated every dialect corpus
> to exact matching and aligned PostgreSQL and StarRocks. See §10 for the
> remediation, the decisions the corpus now freezes, and what is still open.
> The audit below is kept as the rationale of record.
>
> This document is the output of a read-only audit of
> `backend/plugin/lineage/mysql` and the shared `scope` / `model` / `catalog` /
> `testutil` code it depends on.
>
> Scope: `backend/plugin/lineage/mysql`, plus the shared packages that decide its
> behavior (`backend/plugin/lineage/scope`, `model`, `catalog`,
> `testutil`, `lineage.go`). `mariadb` / `tidb` are byte-identical copies of the
> mysql analyzer (see §5.1) so most fixes must be applied there too; `postgresql`
> and `starrocks` are referenced only to show what a fix looks like when the
> sibling already has one.
>
> Constraint: `github.com/bytebase/omni` is a pinned upstream dependency and is
> **not** to be modified, patched, vendored or reported anywhere. Every omni
> defect found by this audit is recorded locally in
> [`docs/omni_upstream_defects.md`](../docs/omni_upstream_defects.md) so it can be
> handed upstream later, by a human, if that is ever wanted.

## TL;DR

The analyzer is structurally sound — a well-factored scope/edge/transformation
pipeline with a hard-fail parse policy and a golden corpus — but the audit found
**high-impact correctness bugs, silent-empty-lineage holes, a test harness that
cannot detect either, quadratic scaling, and a layer of one-migration-old
comments and dead fields.**

Highest-value items, in order:

1. **Aliased `SELECT *` / `t.*` emits no lineage at all.** The wildcard source is
   built with the real table name while the scope is keyed by alias, so
   resolution fails and every edge is dropped: `CREATE VIEW v AS SELECT p.* FROM posts p`
   records nothing. `mysqldump`-style view definitions alias their tables, so
   this is the production-common shape. The corpus's only aliased `SELECT *` case
   (`16/star_join`) asserts nothing (§4.1) and hides it.
2. **Set-operation arms after the first are silently dropped** whenever the union
   feeds a real target (view / CTAS / `INSERT ... SELECT`) or is nested in a CTE
   or derived table. `CREATE VIEW v AS SELECT a.id FROM a UNION SELECT b.id FROM b`
   records `a.id → v.id` and loses `b.id → v.id`. StarRocks already fixed exactly
   this with `resolveOutputColumns`; MySQL never got the fix.
3. **Expression/scalar subqueries resolve in the wrong scope** and produce
   fabricated sources: `SELECT (SELECT MAX(salary) FROM employees) AS m FROM departments`
   records `departments.salary → m`. Worse, the corpus (`scalar_subquery`) asserts
   this wrong edge, so the bug is frozen as "expected behavior".
4. **Unqualified columns bind to the alphabetically-first table without checking
   whether it has the column**, so `SELECT y FROM a JOIN b` records `a.y` even
   when `y` only exists in `b`, and an unknown column is silently attached to the
   first table instead of being dropped.
5. **The test harness only checks "every expected edge is present"** — extra and
   wrong edges pass; `expected_edges: []` does not actually assert emptiness
   (so the `star_join` case asserts nothing); and transformation *content* is not
   assertable at all.
6. **Quadratic analysis cost**: expression text reconstruction re-scans the whole
   token stream per expression, so overhead over parse grows from 2.4× (1 column)
   to 8.3× (1000 columns). A binary search over the already-ordered token list
   fixes the dominant term.

Everything else — dead fields, stale "legacy ANTLR" comments, the 3× dialect
duplication, the `CatelogProvide` typo — is real but mechanical.

## Outcome

Implemented on `backend/plugin/lineage/{mysql,mariadb,tidb,scope,model,catalog,testutil}`
plus one stale comment in `backend/api/v1/lineage_service_analyze.go`. The
mariadb/tidb analyzers were regenerated from the mysql analyzer, so the three
copies remain byte-identical apart from the dialect package clause, the two omni
import paths and the registered engine. Upstream omni was not touched.

| Item | What shipped |
| --- | --- |
| §2.1 aliased wildcard | Wildcard source refs are marked `Resolved` (the StarRocks pattern), so `SELECT *` / `t.*` over an aliased table resolves. Aliased-star cases added with and without catalog. |
| §2.2 set-op arms | `resolveOutputColumns` ported from StarRocks: each arm resolves its sources in its own scope, and the operation emits once at the root. `mergeSetOpOutputColumns` always records the set operation, `determineRelationType` maps it, and `OperationIntersect`/`OperationExcept` (+ relation types) were added. |
| §2.3 subqueries | `collectExprColumns` no longer descends into subqueries; `SubqueryExpr`, `InExpr.Select` and `ExistsExpr.Select` are analyzed in a pushed scope and their output sources merged as resolved. `scalar_subquery` corrected; correlated-subquery case added. |
| §2.4 unqualified columns | `Analyzer.resolveColumn` uses memoized catalog metadata to pick the table that has the column when several base tables are in scope and all have metadata; otherwise the shared deterministic rule is kept. Cases added for disambiguation and for dropping an unknown column. |
| §2.5 single-table DELETE | The target table is registered before resolving, so WHERE columns carry a real source table. |
| §2.6 classification | AST-based: `isPlainColumnRef` for direct projections, `BinaryExpr.Op`/`UnaryExpr.Op` for operators, `CaseExpr` for CASE, and the synthetic wildcard source is gated on a real aggregate call. Constant/`NOW()` expressions now emit no edges; quoted identifiers with operator characters stay direct. |
| §2.7 window/function | `fc.Over != nil` is checked before the name sets; the `"OVER"` substring test is gone. `SUM(x) OVER (...)` is WINDOW, `cover(x)` is FUNCTION, `my_udf(x) OVER ()` is WINDOW. |
| §2.8 `VALUES(col)` | Maps to the inserted value's sources instead of a fabricated source column. |
| §2.9 result edges | Suppressed for INSERT/REPLACE/CREATE TABLE AS/CREATE VIEW via one `realTarget` flag (replacing `inInsertReplaceContext`); the duplicate `__result__` expectations were removed from the dialect corpora. |
| §2.10 leading `WITH` DML | An explicit analysis error instead of a phantom table. |
| §2.11 insert overflow | Output columns beyond the insert column list are dropped. |
| §2.12 remainder | `TABLE t` emits a wildcard edge; `VALUES` and `INSERT ... SET` are correctly no-ops; `INSERT ... TABLE t` is handled; infix expression spans are widened to the earliest child offset, which also fixes the recorded expression text, inferred aliases and operator kind. |
| Performance | `exprTextOf` binary-searches the token stream; `nodeLoc` caches the Loc field index; edge dedup uses a struct key; catalog lookups are memoized per analysis; classification is one walk. Overhead over parse is now roughly constant (6.1× at 100 columns, 6.2× at 1000) instead of growing 6.5×→8.3×, and absolute overhead at 1000 columns fell from 2.80 ms to 1.97 ms. `BenchmarkAnalyzeColumnScaling` added. These are the audit's first measurements; §10.3 records the same benchmark before and after the follow-up work. |
| Test harness | `exact_edges: true` exhaustiveness, `expected_edges: []` now asserts zero edges, and `transformations:` asserts operation/function/expression/op_type/condition/arguments/group_keys/partition_by/order_by. Validator unit tests (including "a wrong expectation fails") added; `MinEdges`/`SkipEdgeValidation` removed. |
| Tests | New `17_test_regression_lineage_table.yaml` (26 cases) and engine-registration tests for MYSQL/MARIADB/TIDB. |
| Debt | Removed `OutputColumn.Expression`, `TableRef.Columns` and `CTEDefinition.DefiningScope`; `Analyzer.errors` is now used for unrepresentable shapes; `determineRelationType` is a direct switch; `CatelogProvide` → `CatalogProvide` + `GetCatalogProvide` under an `RWMutex`; `catalog.provideImpl` reuses `AnalysisContext.Complete`; materialized-view lookups report no metadata instead of dependency columns; stale "legacy ANTLR"/migration comments, the bare `nolint:revive`, the empty testdata directories and the unused `unparam`-visible helpers were cleaned up. |

Verification: `go test ./... -count=1` green, `golangci-lint run
--allow-parallel-runners` reports 0 issues, `gofmt -l` clean.

**Still open (deferred, not defects):**

- `Transformation.GroupKeys` is still never populated (GROUP BY keys are not read).
- Whether `WHERE` / join `ON` / `ORDER BY` / `GROUP BY` / `HAVING` columns should
  be recorded as influences remains a product decision (Phase 3). Today only
  DELETE's `WHERE` is, and a subquery's filter columns are deliberately excluded
  to stay consistent with "a SELECT's WHERE is not lineage".
- The public API collapses relation types to DIRECT/INDIRECT, so the newly
  produced UNION/INTERSECT/EXCEPT/GROUP values are internal until the proto is
  widened.
- Materialized views still have no output-column list in the store proto, so a
  wildcard over an MV falls back to `*`.
- The three mysql-family analyzers remain copies kept in sync by regeneration;
  the parser-independent core was not extracted (§5.1).
- `scope.GetTables` / `GetCTEs` still return the internal maps (§5.4).

## 1. Method and baseline

Read-only audit. Evidence was gathered in three ways: (a) reading the package and
its shared dependencies, (b) running throwaway probes in the `mysql` package
(deleted afterwards) to capture actual analyzer output, and (c) a systematic
probe of the pinned `omni` `mysql/parser` + `mysql/ast`
(`v0.0.0-20260912023254-4574e69bb9f1`) for `Loc` semantics and parse
acceptance. Two further audits — a cross-dialect gap comparison against the
PostgreSQL and StarRocks analyzers, and an inventory of the golden corpus and
harness — are folded into §4 and §6. No repository file was modified except this
plan and [`docs/omni_upstream_defects.md`](../docs/omni_upstream_defects.md).

Baseline (pre-change, on the audit machine):

| Check | Result |
| --- | --- |
| `go test ./backend/plugin/lineage/... -count=1` | green |
| Corpus | 73 cases / 180 expected edges across 16 YAML files; every case sets `expected_edges`; no case uses `expect_error` |
| Corpus assertion strength | expectations iterate only the expected list; 16 edges omit `from_table`, 21 omit `from_field`, 14 omit both; `relation_type` on 47/180, `has_transform` 42/180, `is_temp` 79/180 |
| `relation_type` values asserted in the corpus | `direct` 157, `indirect` 55, `group` 21; **no `union`/`join`/`intersect`/`except`/`unknown`** |
| Wide-select scaling (self-measured, 200 iters) | n=1: parse 2.6 µs / analyze 6.3 µs; n=100: 37 µs / 239 µs; n=1000: 384 µs / **3.19 ms** |
| `BenchmarkAnalyzeShapes/wide_100_cols` | 236 µs, 1661 allocs |
| `BenchmarkAnalyzeWildcardWithCatalog/with_catalog` | 126 µs vs 3.3 µs without catalog |

The corpus is the contract today, but §4 shows several of its expectations were
captured from the implementation rather than derived from semantics, so "corpus
is green" currently means "nothing changed", not "nothing is wrong".

## 2. Priority 0 — correctness defects

Each item states the defect, the observed behavior (probe output), the root
cause, and the fix. Line numbers are `mysql/analyzer.go` unless stated.

**Most of these are shared, not MySQL-specific.** `mariadb` and `tidb` are
byte-identical copies, so §2.1–§2.3, §2.6, §2.7 and §2.10 apply verbatim to them;
`postgresql` shares §2.1, §2.2, §2.4 and §2.8. Two siblings already contain the
fix, which makes most of this a port rather than a new design:

- StarRocks: `wildcardSourceRef` (§2.1) and `resolveOutputColumns` (§2.2), plus
  it is the only analyzer that uses `ColumnRef.Resolved` or an error-accumulation
  path at all.
- PostgreSQL: an AST-based derived classification and a gated source-less
  fallback (§2.6), `fc.Over != nil` first (§2.7), `TABLE t` lowering (§2.12),
  `excluded` handling for upserts, and CTEs inside `INSERT`/`UPDATE`/`DELETE`
  (§2.10 — PostgreSQL handles all three, so its design is the reference).

### 2.1 Aliased `SELECT *` / `t.*` loses all lineage

**Observed** (no catalog required)

| SQL | Emitted |
| --- | --- |
| `SELECT * FROM employees` | `employees.id → __result__.id`, `employees.name → …` |
| `SELECT * FROM employees e` | **nothing** |
| `SELECT p.* FROM posts p JOIN users u ON p.user_id = u.id` | **nothing** |
| `CREATE VIEW v AS SELECT p.* FROM posts p` | **nothing** |
| `SELECT * FROM (SELECT id FROM posts) x` | `posts.id → __result__.id` (derived tables are fine) |
| `SELECT e.id FROM employees e` | `employees.id → __result__.id` (qualified columns are fine) |

**Root cause** — `processStar` (503–516), `processTableWildcard` (519–533) and
`expandWildcardWithCatalog` (1475–1496) build the source ref as
`{Schema: tableRef.Schema, Table: tableRef.Table, Column: …}` — the *real* table
name — but `Scope.AddTable` keys the scope by **alias** (`scope/scope.go:32-38`),
so `generateEdges`' `sp.ResolveColumn({Table: "posts"})` fails
`FindTable("posts")` (the key is `p`) and the loop `continue`s. Tables without an
alias work only because key and table name coincide. Derived tables work because
`processDerivedTable` stores the alias in `TableRef.Table`.

**Why it matters** — `SELECT *` over an aliased table is the norm in real view
definitions (`mysqldump` emits ``from ((`posts` `p` join …)``), so such a view
silently stores **zero** lineage. The only aliased `SELECT *` corpus case is
`16/star_join`, whose `expected_edges: []` is vacuous under the current harness
(§4.1), so the suite cannot see it.

**Fix** — build the wildcard source ref so it survives resolution: either use the
scope key (alias) as `Table`, or follow StarRocks's `wildcardSourceRef`
(`starrocks/analyzer.go:748-751`) and set `Resolved: true` alongside the real
schema/table name. Add corpus cases for aliased `SELECT *` and `t.*` with and
without catalog, and replace the vacuous `star_join` expectation.

### 2.2 Set-operation arms after the first are dropped (nested or real target)

**Observed**

| SQL | Emitted (wrong) | Should be |
| --- | --- | --- |
| `CREATE VIEW v AS SELECT a.id FROM a UNION SELECT b.id FROM b` | `a.id→v.id` (+ 2 temp `__result__` edges) | also `b.id→v.id` |
| `INSERT INTO dst (id) SELECT a.id FROM a UNION SELECT b.id FROM b` | `a.id→dst.id` | also `b.id→dst.id` |
| `SELECT s.id FROM (SELECT a.id FROM a UNION SELECT b.id FROM b) s` | `a.id→__result__.id` | also `b.id→__result__.id` |
| `WITH c AS (SELECT a.id FROM a UNION SELECT b.id FROM b) SELECT c.id FROM c` | `a.id→__result__.id` | also `b.id` |

**Root cause** — `processSetOperation` (320–347) analyzes arms ≥ 1 in a
throwaway `tempScope` parented to `baseScope.Parent()`, then merges only
`OutputColumn`s (`mergeUnionOutputColumns`, 350–374). The merged `ColumnRef`s
carry only `Table`/`Column`; when the enclosing CTE/derived/DDL processing later
calls `sp.ResolveColumn` in the base scope, the arm's tables are no longer
visible, `err != nil` and the code does `continue` — the arm's lineage vanishes.

The `scope.ColumnRef.Resolved` flag exists for exactly this situation and
StarRocks sets it (`starrocks/analyzer.go:1098-1114`, `resolveOutputColumns`),
but MySQL and the other three dialects never do.

**Fix** — port `resolveOutputColumns` into `processSetOperation`: resolve each
arm's output-column source refs against the scope the arm was analyzed in, set
`Resolved: true`, and feed those into `mergeUnionOutputColumns`. Resolution in
`scope.ResolveColumn` already short-circuits on `Resolved` (scope.go:92). Add
corpus cases for all four shapes above.

### 2.3 Expression/scalar subqueries are walked but resolved in the outer scope

**Observed**

| SQL | Emitted (wrong) |
| --- | --- |
| `SELECT (SELECT MAX(salary) FROM employees) AS m FROM departments` | `departments.salary → m` (corpus case `scalar_subquery` asserts this) |
| `SELECT a.id, (SELECT b.y FROM b WHERE b.id = a.id) AS y FROM a` | `a.id → y`; `b.y` dropped entirely |
| `SELECT (SELECT MAX(b.y) FROM b WHERE b.id = a.id) AS m FROM a` | `a.id → m`; `b.y` dropped |

**Root cause** — `collectColumns` (1141–1153) uses `nodes.Inspect` and recurses
into `SubqueryExpr`/`SubqueryStmt` expression nodes, collecting the inner query's
column refs as if they belonged to the outer expression. There is no
subquery-scope handling in the expression path; the inner SELECT is never
analyzed on its own.

**Fix** — two steps, in order:
1. **Stop the bleeding**: make `collectColumns` not descend into subquery nodes
   (treat `*nodes.SubqueryExpr` as an opaque operand), and mark the output column
   as derived. This removes fabricated edges immediately.
2. **Add real support**: analyze the subquery in its own pushed scope, then
   flatten it into the outer expression the way StarRocks flattens expression
   subqueries, or at minimum resolve its refs against that scope. Correlated
   subqueries need the outer scope visible as a parent.

Update the `scalar_subquery` expectation to the semantically correct source
(`employees.salary`) as part of this change — do not preserve the wrong edge.

### 2.4 Unqualified columns bind to the first table without a column check

**Observed** — `SELECT y FROM a JOIN b ON a.id = b.id` emits `a.y` although `y`
exists only in `b`. `SELECT c FROM ...` where no table has `c` still emits
`<first-table>.c`.

**Root cause** — `scope.ResolveColumn` (scope.go:121–141) builds the sorted table
key list and returns on the first iteration with no membership test, and neither
`TableRef.Columns` nor the catalog is consulted. `TableRef.Columns` is populated
for derived tables/CTEs and *never read anywhere* (see §5.2).

**Fix**
1. Populate `TableRef.Columns` for base tables from the catalog when available
   (`expandWildcardWithCatalog` already has the metadata; cache it, see §3.4).
2. In `ResolveColumn`, prefer tables whose `Columns` contain the name; fall back
   to the existing sorted-first rule only when no table has column metadata, so
   determinism is preserved for the metadata-less case.
3. When the column is unknown to every table that has metadata, return an error
   (the caller then drops the edge) instead of fabricating a source.

This is a shared-package change: `postgresql`, `starrocks`, `tidb`, `mariadb`
inherit it. Keep the sorted fallback so existing corpus output is unchanged
except where metadata now disambiguates.

### 2.5 Single-table `DELETE` resolves WHERE columns against an empty table

**Observed** — `DELETE FROM users WHERE status = 'inactive'` emits
`.<empty table>.status → users.__deletion__`. The corpus case `simple DELETE`
asserts only `from_field` (no `from_table`), which is why it passes; the
`DELETE with subquery` case (`10_test_delete_lineage_table.yaml`) is worse — it
asserts only `to_table`/`to_field` while all three of its edges carry an empty
source table. In the runner this becomes a `column_lineage` row whose
`source_guid` is `<instance>;<db>;<schema>;` — an empty object name that matches
nothing.

**Root cause** — `processDeleteStatement` (966–1042) adds only `stmt.Using`
tables to the scope; for a single-table `DELETE FROM t` the target in
`stmt.Tables` is never registered, so `sp.ResolveColumn` fails and the code
falls back to `resolved = &condCol` with an empty `Table`.

**Fix** — register `stmt.Tables` as scope table refs (single-table form) before
resolving, or fall back to the resolved `actualTargetTable` when
`ResolveColumn` fails. Add an assertion on `from_table` in the corpus.

### 2.6 Text-based derived classification misfires (quoted identifiers and constants)

**Observed** (text heuristics: `isExpressionDerivedText` 1156–1165 plus the
source-less fallback in `processSelectExpr` 549–557)

| SQL | Emitted |
| --- | --- |
| ``SELECT `created-at` FROM t`` | `OPERATOR/SUBTRACTION` transformation, `relation_type=indirect` |
| ``SELECT `case_id` FROM t`` | `PROJECT` transformation, `relation_type=indirect` |
| ``SELECT `we(ird)` FROM t`` | `PROJECT` transformation, `relation_type=indirect` |
| `SELECT '2020-01-01' AS s FROM t` | **`t.* → __result__.s`** — fabricated dependency on every column |
| `SELECT 'a-b' AS s FROM t` | **`t.* → __result__.s`** — fabricated |
| `SELECT DATE_SUB(NOW(), INTERVAL 1 YEAR) AS d FROM t` | **`t.* → __result__.d`** — fabricated |

**Root cause** — two coupled heuristics. (a) "is this derived" is decided by
substring-matching the reconstructed text for `(`, `+`, `-`, `*`, `/`, `CASE`,
`WHEN`, so a plain column whose name contains any of those is misclassified and
its `relation_type` flips from `direct` to `indirect`. (b) Any *derived*
expression that collected **no** columns is given a synthetic source pointing at
every table in scope (549–557) — a fallback intended for table-wide aggregates
such as `COUNT(*)`. A constant string containing `-` is therefore "derived",
collects nothing, and invents a dependency on the whole table. PostgreSQL removed
exactly this (its `expr_classify_test.go` `TestSourceLessFallback`); StarRocks
still has it.

**Fix** — decide from the AST: an expression is a plain projection iff it is a
`*nodes.ColumnRef` (possibly wrapped in `ParenExpr`); everything else is derived.
`analyzeExpressionOperator` already receives the `ExprNode`, so the text
heuristic can be removed rather than patched. Replace `detectOperatorExpression`'s
string matching with `BinaryExpr.Op`/`UnaryExpr.Op`. Additionally gate the
synthetic wildcard source on a genuine table-wide expression (PostgreSQL's
`isTableWideExpression` is the model) so a constant or `NOW()`-based column
reports no table dependency. Note this is independent of the omni `Loc` defect:
fixing the heuristics does not fix truncated expression text, and vice versa.

### 2.7 Function and window classification is text- and order-based

**Observed**

| SQL | Emitted | Should be |
| --- | --- | --- |
| `SELECT cover(x) AS c FROM a` | `PROJECT` | `FUNCTION` |
| `SELECT my_udf(x) OVER () AS a FROM t` | `PROJECT` | `WINDOW` |
| `SELECT SUM(x) OVER (PARTITION BY y) AS s FROM t` | `AGGREGATE`, `relation_type=group` | `WINDOW` |
| `SELECT AVG(x) OVER () AS a FROM t` | `AGGREGATE`, `relation_type=group` | `WINDOW` |

**Root cause** — `analyzeExpressionOperator` (1169–1191) tests aggregate *before*
window, and both are keyed on the function name via `firstFuncCall`;
`detectFunctionCall` uses the literal substring `"OVER"` as its window test; and
`windowFunctions`/`aggregateFunctions` are fixed name sets. So a windowed
aggregate is reported as a plain aggregate (the corpus case
`16/window_aggregate_over` pins `relation_type: group`), an unknown-named
`OVER` function falls through to `PROJECT`, and any function whose name merely
contains `OVER` is rejected as non-function. The same false positive hits
`OVERFLOW(...)` and user-defined functions such as `cover`; `OVERLAY(...)` is not
valid MySQL (it uses `INSERT()`), but the substring test is wrong regardless.
PostgreSQL checks `fc.Over != nil` first, so every `OVER` call is `WINDOW`.

**Fix** — test `fc.Over != nil` before the aggregate/window name sets (a call
with `OVER` is never a plain aggregate), and delete the substring check. Update
`window_aggregate_over` and the sibling cases that pin `group`/`AGGREGATE` for
`OVER` calls.

### 2.8 `VALUES(col)` in upserts invents a source column

**Observed** — `INSERT INTO t2 (a, b) SELECT id, name FROM employees ON DUPLICATE KEY UPDATE b = VALUES(b)`
emits `employees.b → t2.b` (fabricated; `employees` has no `b`) in addition to
the correct `employees.name → t2.b`. Corpus case `insert_odku` asserts the
fabricated edge.

**Root cause** — `processInsertUpdateList` (772–819) runs `collectColumns` over
the assignment value and resolves `b` against the SELECT's FROM scope.

**Fix** — detect a `VALUES(col)` function call and map it to the *insert source*
of that target column (the output column whose positional target is `col`),
falling back to the wildcard source only when the mapping is unknown. Update the
`insert_odku` expectation.

### 2.9 Spurious `__result__` edges for statements that have a real target

**Observed** — `CREATE VIEW v AS SELECT a.id FROM a` emits `a.id→__result__.id`
**and** `a.id→v.id`; the same happens for `CREATE TABLE ... AS SELECT` and
`INSERT ... SELECT` (the latter is suppressed by `inInsertReplaceContext`, but
the DDL paths have no equivalent flag).

Two consumers already work around this by hand: the runner skips `IsTemp`
relations, and `lineage_service_analyze.go:171-186` re-derives `hasRealTarget`
and filters. The corpus's `from_table: __result__` expectations bake it in.

**Fix** — suppress result-edge generation at the source for DDL/DML targets
(generalise `inInsertReplaceContext` into a "has real target" context, or call a
`generateEdgesForDataModification`-style direct emitter for CREATE TABLE/VIEW).
Then delete the consumer workarounds — or keep them as defence in depth, but
stop relying on analyzer bugs to be filtered downstream.

### 2.10 `WITH` before `INSERT`/`DELETE`/`UPDATE` silently invents a table

**Observed**

| SQL | Emitted |
| --- | --- |
| `WITH c AS (SELECT id FROM a) INSERT INTO dst (id) SELECT id FROM c` | `c.id → dst.id` (table `c` does not exist) |
| `WITH c AS (...) DELETE FROM dst WHERE id IN (SELECT id FROM c)` | empty-table `id → dst.__deletion__` |

**Root cause** — omni's `InsertStmt`/`DeleteStmt`/`UpdateStmt` have no CTE field,
so the `WITH` clause is dropped at parse time (already recorded in the migration
plan for DELETE; it also affects INSERT/UPDATE). The analyzer then treats the CTE
name as a base table. Worse, "no error, phantom table" contradicts the package's
own hard-fail policy: a parseable statement yields silently wrong lineage.

**Fix** — since upstream cannot be changed, detect it: if an `InsertStmt`/
`DeleteStmt`/`UpdateStmt` is not reachable (no AST field), the only local signal
is a leading `WITH` in the statement text. Add a cheap guard: if the parsed
statement is DML and `strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "WITH")`,
return an explicit analysis error ("CTE-prefixed DML is not supported by the
parser") so the runner records `error_message` and the hourly scan stops
re-queueing — instead of persisting a phantom source. Record the underlying
upstream gap in the local defects doc.

### 2.11 Insert-column-list overflow becomes a phantom target column

**Observed** — `INSERT INTO dst (a, b) SELECT id, name, extra FROM a` emits
`a.extra → dst.extra`; `extra` is not in the insert column list and is not
written.

**Root cause** — `generateEdgesForDataModification` (608–633) only overrides the
target name while `i < len(targetColumns)`; extra output columns keep their alias.

**Fix** — when the insert has an explicit column list, drop output columns
beyond its length.

### 2.12 Smaller correctness items

- **Unaliased expressions get garbage output column names** because
  `inferColumnAlias` runs on `Loc`-truncated text: `CREATE VIEW d.v AS SELECT d.t.x - 1 FROM d.t`
  stores a target column literally named `-1` (§6 item 1). This is user-visible in the
  lineage UI and in the `column_lineage.target_column` value.
- **`INTERSECT`/`EXCEPT` are analyzed as `UNION`** (set-op kind is discarded and
  `RelationTypeDirect` is emitted). Either produce the matching relation type or
  document the collapse.
- **`TABLE t`, `VALUES ...`, `INSERT ... TABLE t`, `INSERT ... SET` produce no
  edges at all**, with no error (`SelectStmt.TableSource`/`ValuesSource` and
  `InsertStmt.TableSource`/`SetList` are unhandled). A parseable statement
  yielding silent empty lineage is the same policy hole as §2.10.
- **Top-level set operations never emit a set-operation transformation or
  relation type**: `mergeUnionOutputColumns` adds `OperationUnion` to the base
  scope's output columns, but each arm already emitted its own edges before the
  merge, so the merged columns are unused at top level. `OperationUnion` /
  `RelationTypeUnion` are consequently unreachable for MySQL, and the corpus
  asserts `relation_type: direct` for unions.
- **`COUNT(*)` with no alias** produces a literal target column named
  `COUNT(*)` (via `inferColumnAlias`).
- **The public API collapses relation types to a boolean**:
  `convertRelationType` (`backend/api/v1/lineage_service.go:308-314`) maps only
  `RelationTypeDirect` to `DIRECT` and everything else to `INDIRECT`, and
  `proto/v1/v1/lineage_service.proto:56-62` defines only those two values. So
  `RelationTypeGroup`/`Union`/`Join`/`Intersect`/`Except`/`Unknown` never reach a
  client even when the analyzer computes them, and the corpus's
  `relation_type: group` assertions test an internal value the API discards.
  Decide whether to widen the proto or to stop computing/maintaining the richer
  enum (including `determineRelationType`'s `Union`/`Group` branches and
  `testutil.parseRelationType`'s dead cases).
- **Case is preserved verbatim** (`SELECT A.ID FROM A` → `A.ID`). MySQL column
  names are case-insensitive and table names are case-sensitive per filesystem;
  whether the stored identifiers need folding is a product decision, but the
  PostgreSQL analyzer has an `identifier_case_test.go` and MySQL has no
  equivalent, so the behavior is untested here.

## 3. Performance

### 3.1 `exprText` is O(tokens) per call → quadratic analysis

`exprText` (166–184) walks `a.tokens` from index 0 and writes every token inside
`[loc.Start, loc.End)`. It is called once or more per select item, per
assignment, per function argument and per window clause. Measured overhead over
parse grows with column count (2.4× at n=1 → 8.3× at n=1000; absolute overhead
2.8 ms at n=1000), which is the signature of an O(N·E) scan.

**Fix** — tokens are returned in source order, so binary-search the first token
with `Loc >= loc.Start` instead of scanning from 0 (`slices.BinarySearchFunc` or
`sort.Search`); the inner loop then starts inside the span. Add a fast path that
returns the raw `sql[loc.Start:loc.End]` when it contains no whitespace outside
string literals. Optionally memoize per `nodes.Node` in the analyzer (an
`ExprNode` may be text-extracted several times through
`analyzeExpressionOperator`, `detectFunctionCall` args and alias inference).

### 3.2 `nodeLoc` reflects per call

`nodeLoc` (138–161) does `reflect.ValueOf` + `FieldByName("Loc")` +
`Interface()` for every node. omni's `LocStart()`/`LocEnd()` are **not**
promoted (it is a named field, not embedded — see the defects doc), so an
interface assertion is not available. Either cache `reflect.Type → field index`
in a package-level map, or replace reflection with a type switch over the
`ExprNode` types `exprTextOf` actually receives (the callers pass expressions,
not arbitrary nodes). The doc comment should also stop implying reflection is the
only *conceivable* option and instead point at the upstream defect.

### 3.3 Edge dedup allocates a formatted string per edge

`addRelation` (1457–1472) builds `fmt.Sprintf("%s.%s.%s->%s.%s.%s", …)` for
every edge and keys the set with it. A comparable struct
`{srcDB, srcTable, srcCol, tgtDB, tgtTable, tgtCol}` keeps identical semantics
with no allocation. This is on the hot path for wide wildcard expansions.

### 3.4 Redundant tree walks per expression; uncached catalog

`analyzeExpressionOperator` (1169–1191) calls `firstFuncCall` up to three times
(aggregate, window, function detectors) plus `collectColumns` and `exprText` —
four or five `Inspect` walks of the same subtree. A single walk that returns
(first function call, column refs) removes most of it.

`expandWildcardWithCatalog` (1475–1496) performs one catalog lookup per table per
wildcard, with no memoization, and `catalog.provideImpl` re-derives the
`AnalysisContext` completion logic that `AnalysisContext.Complete` already
implements (duplicated at catalog/provide.go:39-50 and 77-88). Add a per-analysis
`map[model.ObjectIdentifier]*catalog.TableMeta` cache on the `Analyzer`, and have
`provideImpl.GetTable` call `Complete`.

## 4. Test and corpus quality

### 4.1 The harness only checks a subset

`ValidateExpectedEdges` (testutil/lineage_test_helper.go:358-397) iterates the
*expected* list and requires each to match some actual relation. It never checks
that every actual relation is expected, never checks the total count, and never
checks uniqueness. Consequences: a run that emits 10 extra edges passes; a run
whose only difference is a *missing* edge passes if that edge was not listed;
`star_join` (`expected_edges: []`) passes regardless — and in fact the analyzer
emits **zero** edges there because of §2.1, so that case silently hides a total
lineage loss. Quantitatively, of the corpus's 180 expected edges, 16 omit
`from_table`, 21 omit `from_field` and 14 omit both, so those expectations match
*any* source; `relation_type` is asserted on only 47, `has_transform` on 42 and
`is_temp` on 79.

**Fix** — add an exact-set mode (default for new cases): compare a normalized
`(source, target, relation_type, is_temp, transformation-summary)` multiset and
report both missing and unexpected edges. Keep a `subset: true` escape hatch for
cases deliberately asserting a few edges of a large result, and make
`expected_edges: []` mean "no edges" as its comment already claims (currently the
non-nil-empty slice runs the validator over zero expectations).

### 4.2 Transformations are not assertable

`yamlExpectedEdge` only exposes `has_transform *bool` (lines 100-114). Operation
kind, function name, expression text, group keys, partition/order-by and
condition are unassertable — which is precisely why §2.6, §2.7, §2.8 and the
`GROUP BY` gap (§5.3) are invisible to the suite.

**Fix** — extend the YAML schema with an optional `transformations:` list
(`operation`, `function`, `expression`, `op_type`, `condition`, `partition_by`,
`order_by`, `group_keys`) and compare as a set. Add per-operation unit tests
(`expr_classify_test.go`-style, as PostgreSQL has) so classification bugs are
caught without a full corpus run.

### 4.3 Expectations captured from the implementation

Several corpus cases assert the analyzer's current (wrong) output rather than
semantics:

| Case | Asserted | Should be |
| --- | --- | --- |
| `16/scalar_subquery` | `departments.salary → m` | `employees.salary → m` |
| `16/window_aggregate_over` | `relation_type: group` for `SUM(...) OVER (...)` | `indirect` (WINDOW → the default branch of `determineRelationType`) |
| `16/insert_odku` | `employees.b → t2.b` | no such edge; `employees.name → t2.b` is the real one |
| `16/union_three_arms`, `05/simple UNION` | `relation_type: direct` | set-operation type (once §2.12 defines it) |
| `10/simple DELETE` | only `from_field`, no `from_table` | real source table on each edge |
| `16/star_join` | `expected_edges: []` | assert the expanded edges for both aliased tables — the case currently hides §2.1 entirely |
| `08`/`07` `CREATE VIEW`/`CTAS` cases | assert only the real target | the analyzer *also* emits `__result__` edges for these; the corpus neither asserts nor forbids them, so it cannot tell §2.9 apart |

Re-derive these from SQL semantics when the corresponding fix lands, and record
in the case itself why the expectation changed.

### 4.4 Coverage holes and dead harness features

Missing entirely from the corpus (verified by search): **aliased `SELECT *` /
`t.*` (the only aliased star case, `16/star_join`, is vacuous — §2.1)**; nested
set ops feeding a real target; set ops inside CTE/derived; expression and
correlated subqueries; `GROUP BY` keys in transformations; quoted identifiers
containing `-`, `(`, `*`, `/`, `case`, `when`, `over`; multi-table `e.*, d.*`;
qualified catalog wildcards (`schemas:` is never used, so
`expandWildcardWithCatalog` is only exercised unqualified and unaliased); catalog
lookups for VIEW/MATERIALIZED_VIEW; `TABLE`/`VALUES` primaries;
`INTERSECT`/`EXCEPT`; `INSERT ... SET`; `INSERT ... TABLE`; CTE shadowing a real
table; materialized views; case folding; CTE-prefixed DML; `DELETE` without
`WHERE`; `INSERT` column-list overflow; `NATURAL JOIN` / `USING` (present only in
the MariaDB dialect corpus).

Two repo-hygiene items found by the audit: stray empty directories
`backend/plugin/lineage/tidb/testdata/analyze/01_test_tidb_features_tab/` and
`backend/plugin/lineage/mariadb/testdata/analyze/01_test_mariadb_features_tab/`
(truncated-name leftovers from an earlier refactor), and `testutil` tests that
cover only `LoadLineageTestSuiteFromYAML`, never `ValidateExpectedEdges` /
`EdgeMatches` / the skip mechanism.

Dead/unused harness surface to delete or finish: `LineageTestCase.MinEdges`,
`SkipEdgeValidation`, `Debug` (never set by any YAML), the
`LineageTestSuite.Name` fallback, and `parseRelationType`'s
`join`/`intersect`/`except`/`unknown` branches (no analyzer produces those
relation types; the enum values `RelationTypeJoin/Intersect/Except/Unknown` are
likewise unreachable).

No registration test exists for MYSQL/MARIADB/TIDB, although StarRocks has one
(`starrocks/registration_test.go`). Add one per registered engine (or a single
shared test) asserting `GetAnalyzeRelation` resolves the engine and that
`DORIS`/`OCEANBASE` return `ErrorEngineNotSupported`.

## 5. Dead code, duplication, comments

### 5.1 Three copies of the analyzer

`mariadb/analyzer.go` and `tidb/analyzer.go` differ from `mysql/analyzer.go` by
31 diff lines each (package clause, two omni import paths, registered engine,
doc comment). That is ~4,450 lines carrying one algorithm with a 3× maintenance
cost: every §2 fix must be applied three times, and the golden corpus only proves
they still agree today. `starrocks/expr.go` further duplicates
`normalizeIdentifier`, `splitQualifiedIdentifier`, `inferColumnAlias`,
`normalizeExpressionText` and `collectColumns` verbatim.

**Fix (incremental, no big-bang)** — extract the parser-independent algorithm
core into a shared package that operates on primitives, not AST nodes:
identifier normalization, alias inference, transformation classification
(once §2.6 makes it AST-input-based it can take `(op, functionName, args,
partitionBy, orderBy, text)`), transformation builders, edge dedup, and the
scope/lineage flattening helpers that already only use `scope` types. The AST
traversal stays per dialect. This is the same boundary the migration plan
already identified ("parser-agnostic (30 functions, ~602 lines) — keep"), so it
is a known, low-risk seam. Track the dialects' continued byte-parity with a
test that compares their outputs over the shared corpus (the corpus already
reuses `mysql/testdata/analyze`, but each dialect is only checked against its own
expectations; a differential test would catch drift).

Within the file, `processInsertStatement` (731–750) and
`processReplaceStatement` (754–769) are near-duplicates.

### 5.2 Dead fields and dead code

| Symbol | Status | Evidence |
| --- | --- | --- |
| `Analyzer.errors` + the `len(a.errors) > 0` check (58, 81, 125-127) | **never appended to** | the error path is unreachable |
| `scope.TableRef.Columns` | write-only | never read anywhere in `lineage/` |
| `scope.OutputColumn.Expression` | write-only | never read anywhere in `lineage/` |
| `scope.CTEDefinition.DefiningScope` | write-only | set at 4 call sites, read nowhere |
| `model.Transformation.GroupKeys` | never populated | `createAggregateOperatorInfo(..., nil)` at 1235 (and in every dialect) |
| `RelationTypeJoin/Intersect/Except/Unknown` | never produced | only `testutil.parseRelationType` accepts them |
| `scope.ColumnRef.Resolved` | unused by MySQL | set only by StarRocks (§2.2 is the reason to use it) |
| `normalizeExpressionText` (1364) | near-redundant | token concatenation already removed inter-token whitespace; only strips spaces (not tabs) |

`determineRelationType` (scope/types.go:83-105) is a `for` loop whose `default`
branch returns on the first iteration, so it always reads only `transform[0]`;
the trailing "default to indirect" is unreachable. Rewrite as a direct
`if len == 0 / switch transform[0].Operation`.

`scope.ResolveColumn`'s two sorted-key loops (scope.go:134-141, 149-156) each
return on the first iteration, and the second is documented as redundant
("CTEs are also added to tables now, so this is redundant"). Delete the dead
branch or make the intent explicit.

### 5.3 Comments that are wrong or stale

| Location | Problem |
| --- | --- |
| `init()` 35–42 | claims MariaDB/TiDB are "intentionally not registered here" and that the runner records a "no lineage analyzer" skip for them. Both are registered by their own packages and blank-imported by `server/ultimate.go`; the skip only applies to OceanBase/Doris now. |
| 49, 121, 164, 1193, 1416 | "matching the legacy ANTLR analyzer" / "mirroring ANTLR's GetText()" — the ANTLR implementation was deleted; these read as if a live compatibility contract exists. Rephrase as "the analyzer's own defined behavior". |
| 1–6 (package doc) | fine, but points at the migration plan as if it were current; add "see the optimization plan for known gaps". |
| `model/transformation.go:49-50` | `OpType` documented as "like '+', '-'" but the values are `ADDITION`/`SUBTRACTION`/… |
| `scope/types.go:53-54` | `NewLineageEdge` "used during the migration from FieldEdge to ColumnRelation" — migration is long over. |
| `catalog/provide.go:124` | lowercase `// todo: fill real type and nullable info` (and the MV branch fills `Columns` from `dependency_columns`, i.e. the *source* columns, which is the wrong set for wildcard expansion). |
| `testutil/lineage_test_helper.go:56-58` | "When empty slice, expects no edges" — not implemented (§4.1). |
| `model/identifier.go:16` | bare `// nolint:revive` suppresses every revive rule for `FullName`; either remove or scope it with a reason. |
| `scope/scope.go:122-128` | references "the legacy and omni analyzers" (legacy is gone). |

### 5.4 API and naming debt

- `lineage.CatelogProvide` / `InitCatalogProvide` — "Catelog" misspelling in an
  exported identifier used by all five analyzers.
- `lineage.go`: `mux` guards `RegisterAnalyzeRelation` writes but
  `GetAnalyzeRelation` reads the map unlocked, and `InitCatalogProvide` writes the
  global with no synchronization; the pattern only works because both happen at
  startup. Either document that invariant or make the read path safe.
- `scope.GetTables()` / `GetCTEs()` return the internal maps (callers can mutate
  scope state); `SetOutputColumn` silently no-ops on an out-of-range index.
- `aggregateFunctions` / `windowFunctions` are `map[string]bool`; a
  `map[string]struct{}` or a switch avoids the map and the per-call
  `strings.ToUpper` allocation.

## 6. Upstream (`bytebase/omni`) defects — recorded locally, not reported

Full write-up with reproductions and the exact probe batteries:
[`docs/omni_upstream_defects.md`](../docs/omni_upstream_defects.md). Summary:

1. **Infix `Loc.Start` is the operator, not the node start.** Verified on 24
   binary nodes: `BinaryExpr.Loc.Start` always equals the operator offset, and
   the left operand is outside the span (`SELECT a + b` → `[9,13)` = `"+ b "`,
   left `a` at `[7,9)`). `BetweenExpr`, `InExpr` and the comparison/logical
   operators behave the same; `UnaryExpr` is correct, which proves it is a defect
   and not a convention. Consequences for the analyzer: truncated
   `Transformation.Expression` (`-1` for `a.x - 1`), wrong operator kind
   (`a.x - 1` → `PROJECT` because the text starts with `-`), and — worst —
   garbage output column names for unaliased expressions
   (`CREATE VIEW d.v AS SELECT d.t.x - 1 FROM d.t` yields a column literally named
   `-1`). `Loc.End` is the next token's start, so spans carry trailing
   whitespace. `CreateViewStmt`/`Limit`/`OnCondition` spans similarly drop their
   leading keyword.
2. **`LocStart()`/`LocEnd()` are not promoted.** `mysql/ast/node.go` claims the
   `Loc` methods give "any struct with a Loc field" a location interface, but Go
   promotes methods only from *embedded* fields and omni declares 212 named
   `Loc Loc` fields and zero embedded ones. The interface assertion fails for
   every one of the 48 node types probed, so reflection is currently the only way
   to read a Loc; there is no exported accessor. (The analyzer's `nodeLoc`
   reflection is therefore necessary — it just should be cached, §3.2.)
3. **Set-operation parse gaps**: `TABLE t UNION …` and `VALUES ROW(…) UNION …`
   are rejected while `SELECT … UNION TABLE t` parses — an asymmetry that makes
   the `TABLE`/`VALUES` primaries unrepresentable on the left of a set operation.
4. **`InsertStmt`/`DeleteStmt`/`UpdateStmt` have no CTE field**, so `WITH` before
   DML is dropped at parse time (§2.10); `CreateViewStmt.SelectText` is set and
   could be used when the view body text is needed.
5. **Parser test blind spot**: omni's own `TestLocAudit` only checks
   `End > Start`, so it cannot catch the class in item 1.
6. MariaDB three-level parenthesized join trees (already recorded in
   `plan/mysql_family_dialect_lineage_plan.md`); MariaDB `knownParserGaps` also
   has no test asserting the skip list is still needed, so a parser fix would
   silently stay skipped.

The analyzer must not depend on these being fixed; every item above has a local
workaround in §2.

## 7. Work plan

Ordered so that each phase is independently mergeable and the corpus grows before
behavior changes.

### Phase 0 — Make the harness capable of detecting the bugs

- Extend the YAML schema with `transformations:` and an exact-set assertion mode;
  make `expected_edges: []` assert emptiness.
- Add `from_table` assertions to the DELETE corpus cases; add negative/extra-edge
  detection and run it over the existing corpus to see what currently passes only
  because it is unchecked (expect `star_join` and several `__result__` cases to
  fail, then fix or explicitly re-scope them).
- Add regression cases for: aliased `SELECT *`/`t.*` (with and without catalog),
  nested set-op to a real target (view/CTAS/INSERT), set op in a CTE/derived
  table, expression + correlated subquery, quoted identifiers with
  `-`/`(`/`case`, constant/`NOW()`-based expressions, windowed aggregates,
  `VALUES(col)` upsert, single-table DELETE source table, insert column-list
  overflow, `TABLE`/`VALUES`/`INSERT ... SET`.
- Add `mysql/registration_test.go` (mirror StarRocks) and a differential
  dialect-parity test over the shared corpus.
- **Exit**: new cases fail for the documented reasons; `go test ./...` still
  green otherwise; no production behavior changed.

### Phase 1 — P0 correctness

- §2.1 fix wildcard source refs so aliased `SELECT *`/`t.*` resolves (use the
  scope key or set `Resolved: true` as StarRocks does); mirror into
  mariadb/tidb/postgresql.
- §2.2 port `resolveOutputColumns` (and mirror into mariadb/tidb).
- §2.3 stop `collectColumns` descending into subqueries, then add subquery-scope
  analysis; correct the `scalar_subquery` expectation.
- §2.4 catalog-backed column disambiguation in `scope.ResolveColumn` +
  `TableRef.Columns` population.
- §2.5 register DELETE target tables in scope.
- §2.6 AST-based derived/operator classification; remove the text heuristics;
  gate the synthetic wildcard source on genuine table-wide expressions.
- §2.7 `fc.Over != nil` checked before aggregate/window name sets; drop the
  `"OVER"` substring test; update the OVER corpus expectations.
- §2.8 `VALUES(col)` mapping.
- §2.9 suppress result edges for DDL targets; simplify the two consumers.
- §2.10 explicit error for CTE-prefixed DML.
- §2.11 drop insert-column-list overflow.
- §2.12 decide and document `INTERSECT`/`EXCEPT`, `TABLE`, `VALUES`,
  `INSERT ... TABLE/SET` (either support them or return an explicit
  "unsupported shape" error), and decide whether top-level set ops should carry
  the set-operation relation type/transformation.
- **Exit**: all Phase 0 cases green with semantically derived expectations;
  `make test-integration-mysql` green; runner records `error_message` instead of
  phantom lineage for the newly-rejected shapes.

### Phase 2 — Performance

- §3.1 binary-search `exprText` + whitespace fast path + optional memoization.
- §3.2 cached `nodeLoc` (type→field-index map or type switch).
- §3.3 struct edge key.
- §3.4 single expression walk; catalog memoization; `provideImpl` reuses
  `AnalysisContext.Complete`.
- **Exit**: `BenchmarkAnalyzeShapes/wide_100_cols` and the self-measured
  n=1000-column case improve materially with allocs down; add a scaling
  benchmark that fails on super-linear growth (or at least reports it) so a
  regression is visible.

### Phase 3 — Correctness/coverage gaps that need product decisions

- §5.3 `GROUP BY` keys into aggregate transformations (`SelectStmt.GroupBy` is
  already in the AST and ignored); requires deciding whether the JSONB
  transformation payload may change.
- Update/DELETE influence semantics: should `WHERE`, join `ON`, `ORDER BY`,
  `GROUP BY`, `HAVING` columns be recorded? Today only DELETE's `WHERE` is, and
  UPDATE's `WHERE` is not. Decide once for all engines.
- Case folding policy for MySQL identifiers.
- Catalog materialized-view columns: the store proto has no output-column list
  for MVs (`proto/store/store/database.proto:522`, and its `triggers` field
  carries a copy-pasted "ordered list of columns" comment), so wildcard
  expansion over an MV can only use dependency columns. Either add MV output
  columns to the sync/store/proto or document the limitation.

### Phase 4 — Debt and duplication

- §5.2 delete the dead fields and the unreachable error path; rewrite
  `determineRelationType`.
- §5.3 fix the wrong comments; §5.4 fix `CatelogProvide`, the locking story, the
  map-returning getters, the `map[string]bool` sets, the bare `nolint`.
- §5.1 extract the parser-independent algorithm core into a shared package and
  reduce mysql/mariadb/tidb to dialect traversal + shared core.

## 8. Verification

1. `go test ./backend/plugin/lineage/... -count=1` (hermetic) green.
2. `go test ./... -count=1` green; `golangci-lint run --allow-parallel-runners`
   clean; `gofmt -l` clean.
3. `make test-integration-mysql` and `make test-integration` (real MySQL; covers
   schema sync → lineage end-to-end).
4. `go test ./backend/plugin/lineage/mysql/ -run '^$' -bench Benchmark -benchmem`
   before/after, recorded in the plan or a comment.
5. Manual diff on a real deployment for a sample of views after Phase 1 (the
   migration plan's step 6): expect changed rows only for the cases this plan
   declares as fixes, and no change for the rest.
6. Runner check: analyze a MANUAL_SQL / VIEW whose statement is
   CTE-prefixed DML and confirm `column_lineage_version.error_message` is set and
   no phantom `column_lineage` row is written.

## 9. Risks and non-goals

- **Behavior changes are user-visible.** Phase 1 changes stored lineage for
  existing objects. That is the point, and it should be announced in the
  changelog. This deployment has no historical data to migrate, so no forced
  re-analysis is needed; a deployment that already stored lineage would need one,
  because the metahash-based skip does not re-analyze unchanged SQL.
  `scope.ColumnRef.Resolved`-style behavior in §2.4 was implemented inside the
  MySQL-family analyzers rather than the shared resolver, so PostgreSQL and
  StarRocks are unaffected. **Superseded by §10:** the rule now lives in
  `scope.ResolveColumn` and every dialect inherits it, so the follow-up changes
  do alter PostgreSQL and StarRocks output.
- **`scope` is shared.** Removing the dead `scope` fields did touch PostgreSQL
  and StarRocks, but their analyzers were left behaviorally unchanged and their
  corpora still pass. A future change to `scope.ResolveColumn` itself would
  affect all dialects and must re-run every corpus.
- **The corpus is currently the contract.** §4.3 changes it. Every changed
  expectation must be justified from SQL semantics in the case itself, never
  adjusted to make a fix pass.
- **Non-goals**: modifying, vendoring, forking or reporting `bytebase/omni`;
  changing the lineage proto/store schema except where Phase 3 explicitly says
  so; rewriting the analyzers around omni's `catalog.Query` IR (rejected by the
  migration plan for good reasons); adding new engines.

## 10. Follow-up remediation

A review of `a90e1e2` found that several Outcome rows were true only for the
MySQL family, that the harness could not detect the difference, and that the
new "explicit error" policy had not been wired to the runner. The follow-up
work, in order:

| Commit | Change |
| --- | --- |
| `b101bab` | A deterministic analysis error now records the current hash, so the hourly scan stops re-queueing a statement the analyzer cannot represent, and the object's stale lineage is cleared. Before, `storeError` wrote a NULL hash and the object was re-analyzed (and re-failed) forever. |
| `92b8e99` | Catalog-backed column disambiguation moved into `scope.ResolveColumn`, carried by a lazy `ColumnLookup` on the table reference. The CTE, derived-table and expression-subquery paths and the PostgreSQL/StarRocks analyzers now disambiguate identically; before, the same query inside a CTE bound to a different table. |
| `7a37e45` | The harness rejects unknown YAML keys (`KnownFields`) and compares `transformations` exactly instead of by containment. |
| `bc0ffa0`, `ad81bd2`, `9718b87` | Every dialect corpus migrated to exact matching; expectations the migration exposed were re-derived from SQL semantics. |
| `6665f61` | Exact matching became the default; `exact_edges` was removed and `subset: true` is the escape hatch. |
| `2498533` | PostgreSQL: `realTarget` suppresses the duplicate `__result__` edges (§2.9); an expression subquery is analyzed in its own scope (§2.3); set-operation arms are merged and emitted once with a UNION/INTERSECT/EXCEPT relation type, and INTERSECT keeps both arms (§2.2); aliased wildcards and the table-wide fallback resolve, and `table.*` expands with catalog metadata (§2.1). |
| `c8960e0` | StarRocks: the same set-operation handling (§2.2); the source-less wildcard fallback is gated on a real aggregate and "derived" is decided from the AST, so a constant or a quoted identifier containing an operator character no longer fabricates a `table.*` edge (§2.6). |
| `6e64894` | Regression cases for the shapes this plan promised but the corpus never covered. |
| `ee3d7da`, `ebc6871` | PostgreSQL keeps both INTERSECT arms; the harness drops its dead `debug` flag, the MariaDB/TiDB registration tests gain the unsupported-engine negative, and `knownParserGaps` is pinned by a test. |
| `89a6984` | An unaliased output column is named the way the engine names it, so a view's target column matches; the recorded DELETE condition no longer loses spaces inside a string literal (§10.4). |

Corpus state: **347 cases across the five dialects, all matched exactly, with
no expectation that can match an arbitrary source or target and no `subset`
case.** `go test ./...`, `golangci-lint run` and the real-server integration
suite are green.

### 10.1 Decisions the corpus now freezes

These are product decisions rather than consequences of a fix. Each is asserted
by the corpus and inlined as a comment in the cases that depend on it, so
changing one is a deliberate, visible corpus change.

1. A window function's `PARTITION BY` and `ORDER BY` columns are sources of the
   windowed output column, with relation type `indirect`.
2. A source-less aggregate (`COUNT(*)`) depends on the whole relation
   (`table.*`); a literal, `NOW()`, or a cast of a constant depends on no column
   and records no edge.
3. `UPDATE t SET col = <constant>` records `table.* -> t.col`: the row is
   rewritten even though no column is read.
4. A set-operation edge carries `union` / `intersect` / `except` as its relation
   type and transformation, and the output columns are named by the first arm.
   `convertRelationType` (`backend/api/v1/lineage_service.go`) still maps every
   non-`DIRECT` value to `INDIRECT`, so the richer value is analyzer-internal
   until the proto is widened.
5. Unqualified-column disambiguation needs metadata for *every* relation in
   scope. One relation without metadata drops the whole scope back to the
   deterministic key-order rule; a single-relation scope keeps that rule even
   when its metadata lacks the column, so stale metadata cannot drop a real
   column.
6. Identifier case is preserved verbatim in the MySQL family (`A.ID` stays
   `A.ID`).
7. An unaliased output column is named the way the engine names it: a quoted
   identifier contributes its unquoted name (`` `created-at` `` gives
   `created-at`, verified against a live MySQL 8.4 view), and any other
   expression contributes its raw source text with the original spacing
   (`x - 1`, not `x-1`), so the stored target column matches the column the
   view exposes.
8. An expression subquery is classified as `PROJECT` in PostgreSQL but from the
   first function call inside the subquery in the MySQL family; the edge sources
   agree, the transformation does not.

### 10.2 Still open after remediation

- `Transformation.GroupKeys` is still never populated (GROUP BY keys are not
  read), and whether `WHERE` / join `ON` / `ORDER BY` / `HAVING` columns should
  be recorded as influences remains undecided.
- The public API still collapses relation types to DIRECT/INDIRECT.
- Materialized views still have no output-column list in the store proto, so a
  wildcard over an MV falls back to `*`.
- The three MySQL-family analyzers remain copies kept in sync by regeneration;
  the parser-independent core was not extracted.
- `scope.GetTables` / `GetCTEs` still return the internal maps, and
  `SetOutputColumn` still no-ops silently on an out-of-range index.
- The `schemas:` catalog key is only meaningful for PostgreSQL: the MySQL
  family addresses a SQL qualifier as the database, so a `schemas:` entry never
  matches there.
- MySQL `NATURAL JOIN` / `USING` is covered only by the MariaDB dialect corpus.
- An unaliased expression column is named differently by the remaining
  dialects: PostgreSQL calls it `?column?` (verified against a live PostgreSQL
  16) while the analyzer stores the expression text, and StarRocks has not been
  checked against a live server. Only the MySQL-family naming was corrected
  (§10.4); the stored target column does not match the engine's column for
  those two engines.
- The `map[string]bool` function-name sets and `parseRelationType`'s `join` /
  `unknown` cases are cosmetic leftovers.
- The plan's own first-measurement performance figures were taken with a
  throwaway probe; the committed benchmark is the reference (see §10.3).

### 10.3 Committed-benchmark measurements

`BenchmarkAnalyzeColumnScaling` / `BenchmarkAnalyzeShapes`, same machine, `-benchmem`,
`NewAnalyzer` + `AnalyzeRelations` per iteration:

| Columns | before `a90e1e2` (HEAD at the time) | after `a90e1e2` | after the follow-up |
| --- | --- | --- | --- |
| 1 | 4.76 µs / 43 allocs | 6.44 µs / 41 allocs | 5.36 µs / 40 allocs |
| 10 | 25.9 µs / 203 | 29.7 µs / 174 | 28.4 µs / 164 |
| 100 | 237 µs / 1661 | 239 µs / 1362 | 242 µs / 1262 |
| 500 | 1353 µs / 8076 | 1175 µs / 6576 | 1136 µs / 6076 |
| 1000 | 3267 µs / 16091 | 2406 µs / 13087 | 2383 µs / 12087 |

The quadratic term is gone (100→1000 grows 13.8× before, 10.1× after), allocation
counts fall ~19% at scale, and the follow-up work removed a per-call key sort.
Small column counts pay a few hundred nanoseconds for the extra scope checks.

### 10.4 Second MySQL pass: output column naming and condition text

A re-check against a live MySQL 8.4 (`CREATE VIEW` then
`information_schema.columns`) found two remaining MySQL defects, both in the
MySQL family:

1. **An unaliased output column was named from the token-concatenated text, not
   the way the engine names it.** `SELECT x - 1` stored `x-1` where MySQL exposes
   `x - 1`; `CONCAT('a', 'b')` stored `CONCAT('a','b')` where MySQL exposes
   `CONCAT('a', 'b')`; `` `created-at` `` stored a column literally named
   `` `created-at` `` where MySQL exposes `created-at`. The stored
   `column_lineage.target_column` therefore never matched the view's real column
   and the edge could not join to what schema sync recorded. The alias now comes
   from the AST column name for a bare column reference and from the raw source
   span for anything else. Verified column by column against the live server:
   `x - 1`, `CONCAT('a','b')` (no-ops, so no edge), `created-at`, `case_id`,
   `COUNT(*)` all match.

2. **`normalizeExpressionText` removed spaces inside string literals.**
   `DELETE FROM t WHERE name = 'John Doe'` recorded the condition as
   `name='JohnDoe'`. The token concatenation is already whitespace-free, so the
   MySQL family now uses the reconstructed text directly and StarRocks collapses
   whitespace instead of deleting it; a literal keeps its content.

Both are pinned by corpus cases (`17/unaliased expression keeps the engine
column name`, `17/DELETE condition keeps literal spaces`, and the updated
`17/quoted identifiers with operator characters are direct`).

Checked and *not* MySQL defects, so nobody re-investigates them:

- `SELECT CONCAT('a','b') FROM t` produces no edge. The column depends on no
  table column, which is the §2.6 decision, not a missing source.
- `SELECT COUNT(*) FROM t` names its output `COUNT(*)`, which is what MySQL
  exposes for that expression.
- Identifier case, `NATURAL JOIN` coverage and the `schemas:` catalog key are
  recorded above as decisions or open items, not defects.
