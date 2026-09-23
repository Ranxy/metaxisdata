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
| §2.4 unqualified columns | `Analyzer.resolveColumn` uses memoized catalog metadata to pick the table that has the column when several base tables are in scope and all have metadata; otherwise the shared deterministic rule is kept. Cases added for disambiguation and for dropping an unknown column. §10.11 later made the rule plural — every relation the catalog confirms owns the name is reported — and gave the search its enclosing scope. |
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
| `1dd0d87` | Every MySQL-family expectation asserts `relation_type`, `is_temp` and each transformation's operation, and `RequireFullEdgeAnnotations` makes that bar a test rather than a convention (§10.5). |
| `f5802d3` | A MySQL-family edge is identified by its database instead of its never-populated schema, so a cross-database join no longer loses an edge (§10.6). |
| `dc68314` | A relation is identified by its qualifier plus its name in the shared scope, so a qualified reference, a `*` expansion and a CTE each see the relation they mean; all three analyzer families updated together (§10.7). |

Corpus state: **347 cases across the five dialects, all matched exactly, with
no expectation that can match an arbitrary source or target and no `subset`
case.** The MySQL family additionally asserts `relation_type` and `is_temp` on
every edge and the operation of every transformation, enforced by a test rather
than by convention (§10.5). `go test ./...`, `golangci-lint run` and the
real-server integration suite are green.

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
8. An expression subquery is classified as `PROJECT` in every engine: the
   subquery's aggregate belongs to the subquery's own query specification, so the
   enclosing select item or assignment is a projection of it. The MySQL family
   agreed only from §10.9.

### 10.2 Still open after remediation

Not ours to fix:

- StarRocks cannot parse a derived table's alias list (`(SELECT …) AS d (x)`), so
  the rename §10.14 added cannot apply there. An omni StarRocks parser gap.
- The MariaDB parser stops at two levels of parenthesized join nesting — the shape
  mysqldump emits for a view — where the MySQL parser accepts any depth. An omni
  gap, recorded in `knownParserGaps` with a test that fails once it is fixed.

Ours, and the largest item left:

- PostgreSQL and StarRocks carry neither `Transformation.GroupKeys` (§10.9) nor
  predicate influences (§10.10), and their corpora are not wired to
  `RequireFullEdgeAnnotations`, so an expectation there can silently stop asserting
  a field. Extending both engines is the next pass.

Decided, and deliberately not defects:

- `ORDER BY` / `DISTINCT` / `LIMIT` are not recorded, `GROUP BY` carries keys but
  no edge, and an `UPDATE` / `DELETE` `WHERE` is not recorded (§10.10). The API
  reports every relation type the model holds, but no producer emits
  `RELATION_TYPE_UNKNOWN` (§10.14).
- An unqualified name no described relation owns goes to the first relation whose
  columns are unknown; for two relations that were never synced that is still the
  scope's name order (§10.12). Two undescribed relations are out of scope by
  decision.
- A materialized view has no output-column list in the store proto, so a wildcard
  over one falls back to `*`. Closing that needs a proto field plus a sync per
  engine, and it is a StarRocks/PostgreSQL shape today.
- Several statements in one MANUAL_SQL text are rejected: they have no single
  result shape, and their `__result__` columns would collide on one object.
- `JSON_TABLE`'s argument columns are not recorded: a function in FROM would need
  per-function semantics for the columns it produces.
- An unaliased expression column keeps its expression text rather than the name
  PostgreSQL gives it (`?column?`), which is useless as a target column (§10.14).
- `SetOutputColumn` has one caller, and that caller's index comes from the same
  scope's output columns, so its out-of-range guard cannot fire (§10.15). The
  parser-independent core behind the three MySQL-family copies was not extracted;
  §10.15 adds a test that keeps the copies honest instead.
- The `map[string]bool` aggregate set is deliberate: the omni AST carries no "is
  aggregate" flag, so a name set is required, and it reads better at its three
  membership tests than the `map[string]struct{}` form would.

### 10.3 Committed-benchmark measurements

`BenchmarkAnalyzeColumnScaling` / `BenchmarkAnalyzeShapes`, same machine, `-benchmem`,
`NewAnalyzer` + `AnalyzeRelations` per iteration. The audit's own first
measurements were taken with a throwaway probe; what is committed here is the
reference.

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

### 10.5 Field-level assertion completion

Exact matching pins *which* edges exist but not their other fields: an
expectation that omits `relation_type` accepts any value, so a relation-type
regression was invisible on 115 of the 244 shared edges. Every expectation in the
three MySQL-family corpora (the shared corpus runs under MySQL, MariaDB and TiDB)
now asserts:

- `relation_type` on every edge — was 53% of the 244 shared edges; the MariaDB
  and TiDB dialect corpora were already complete.
- `is_temp` on every edge — was 66%.
- `operation`, plus the non-empty `function_name`, `op_type`, `condition`,
  `partition_by` and `order_by`, on every transformation — was 28 of the 107
  transformation entries in the shared corpus, all 107 now.

The whitespace-normalized `expression` text is still asserted only where it
already was: it is an internal rendering, and freezing it on every edge would
make the corpus churn on any formatting change. `arguments` carries the same
reconstructed text, so it is not asserted. `group_keys` became assertable, and
required on every `AGGREGATE`, in §10.9.

`testutil.RequireFullEdgeAnnotations` makes the bar enforceable instead of a
one-off cleanup. It fails a case whose expectation leaves `relation_type` or
`is_temp` unasserted, whose relation type and transformation coverage disagree
(a direct edge carries no transformation, any other type carries at least one),
or whose transformation omits `operation`. `TestCorpusIsFullyAnnotated` calls it
for MySQL, MariaDB and TiDB. PostgreSQL (258 edges: 29% relation type, 21%
is_temp) and StarRocks (180 edges: 75% / 74%) are not wired yet, so their corpora
still accept a field change silently — the guard can be enabled there once those
corpora are annotated the same way.

### 10.6 Third MySQL pass: edge identity across databases

Completing the annotations exposed a defect the corpus could not see, because no
case joined two same-named tables from different databases.

**Fixed — the dedup key ignored the database.** `addRelation` built its
`columnEdgeKey` from `Table.Schema`, but a MySQL-family relation stores its SQL
qualifier in `Table.Database` (`NewLineageEdge` never sets `Schema`), so every
key's database was empty. Two edges that differed only by database collided and
the second was dropped:

```sql
SELECT a.id, b.id FROM db1.t a JOIN db2.t b ON a.id = b.id  -- produced 1 edge, not 2
```

The key now reads `Table.Database`. Pinned by
`17/aliased same-name tables in two databases keep both edges` and
`17/the same qualified column twice is one edge`, the latter keeping the
"identical edge is still deduplicated" half honest.

**Fixed in §10.7 — a qualified reference was resolved by table name only.**
`scope.ResolveColumn`'s qualified branch called `FindTable(colRef.Table)` and then
reported *the found table's* qualifier; `colRef.Schema` was never compared. The
scope also keyed its table map by the bare table name (or alias), so when two
relations shared a name the later registration overwrote the earlier one. The
result was a silently wrong source, not a dropped edge:

```sql
SELECT db1.t.a FROM db1.t JOIN db2.t ON db1.t.id = db2.t.id
-- stored edge: db2.t.a -> __result__.a   (the SQL asked for db1.t.a)
```

The aliased form above was correct because the aliases gave the two relations
distinct keys; only same-name relations without aliases collided. Fixing it meant
giving a relation an identity of qualifier plus name, which the shared `scope`
package now does for all three analyzer families.

### 10.7 Relation identity across qualifiers

The §10.6 open item is fixed. Its root cause was that a relation had no identity
beyond the name a query used, so a name-keyed fix would have cured only the first
of four symptoms. `scope` now models the identity explicitly:

- `RelationKey{Qualifier, Name}` is the database (MySQL family, StarRocks) or
  schema (PostgreSQL) plus the alias, or the table name when there is none.
- `Scope` holds relations in a slice, so same-named relations from different
  qualifiers coexist instead of overwriting each other; `AddTable` replaces only
  an identical key, which keeps a repeated registration idempotent.
- `FindRelation(key)` matches the key exactly first, then accepts a relation
  registered without a qualifier — the analyzer does not know which database an
  unqualified FROM clause resolves in, so a qualified reference to it stays
  resolvable.
- `Tables()` / `CTEs()` return ordered slices instead of the internal map, so
  every relation in scope is expanded and no order depends on map iteration.
- `ResolveColumnRef` also returns the relation a reference resolved to, so the
  ~25 "is the source a query-local relation?" call sites stop looking the name up
  again and guessing; `scope.RelationKeyOf` maps a stored endpoint back to its
  key for the temp-table checks.
- The reference side keeps its qualifier where it used to drop it: the MySQL
  family's `collectExprColumns` and `collectUpsertSources`, and PostgreSQL's
  `columnRefFromFields`, which discarded a schema qualifier by design before.
- A CTE is query-local and is never qualified, so a CTE is bound only when the
  reference carries no qualifier, and the temp-table filters treat only an
  unqualified endpoint as query-local.

Four consequences, each verified by probe before and after, and identical in all
three families:

| Consequence | Before | After |
| --- | --- | --- |
| wrong source | `SELECT db1.t.a FROM db1.t JOIN db2.t …` gives `db2.t.a` | `db1.t.a` |
| dropped edge | `SELECT a.id, b.id FROM db1.t a JOIN db2.t b …` gives 1 edge | 2 edges |
| a star expands one relation | `SELECT * FROM db1.t JOIN db2.t …` gives 1 edge | 2 edges |
| a CTE captures a real table | `WITH t AS (…) SELECT db1.t.name FROM db1.t` gives 0 edges | 1 edge |

Pinned by a fully annotated `TestRelationIdentity_Table` suite added to each
family: `18` (shared by MySQL, MariaDB and TiDB), `23` PostgreSQL and `20`
StarRocks. Negative-checked by dropping the qualifier from `TableRef.Key()`: five
cases per dialect fail, and the CTE case is carried by the separate
CTE-binding rule.

Deferred here and resolved in §10.11: an *unqualified* reference that several
relations could satisfy resolved to the first relation by name (the qualifier as
tie-break) instead of reporting every owner, and the enclosing scope was
unreachable whenever the current scope had any relation at all. §10.11 replaced
both with an innermost-out search that reports every relation the catalog
confirms owns the name.

`BenchmarkAnalyzeColumnScaling` is unchanged by the rewrite: 1000 columns
2130 µs / 12086 allocs against 2383 µs / 12087 before it (same machine). Scopes
hold a handful of relations, so the linear scans the slice introduced cost the
same as the map lookups they replaced.

### 10.8 Fourth MySQL pass: catalog fidelity and join coverage

Two test-only gaps, with no analyzer behavior change; the shared corpus runs
unchanged under MySQL, MariaDB and TiDB.

1. **The harness could not express a database-qualified catalog entry.** A
   `schemas:` entry registers `ObjectIdentifier{Schema: ...}`, while the MySQL
   family looks a relation up as `{Database: tableRef.Schema, Name: ...}`
   (`mysql/analyzer.go`), so a `schemas:` entry never matched. The consequence
   was worse than a missing test: a MySQL case written with `schemas:` did not
   fail, it silently fell back to `db1.t.*`, which the corpus accepted — so the
   whole qualified-wildcard path (`processTableWildcard` →
   `expandWildcardWithCatalog`) had zero coverage.

   `yamlCatalog` now also accepts `databases:`, registering
   `{Database: name, Name: table}`. The two forms stay deliberately distinct:
   `schemas:` addresses PostgreSQL, `databases:` addresses the MySQL family and
   StarRocks, and an entry in the wrong form is a visible failure rather than a
   silent fallback. The new suite `19_test_database_catalog_table` covers a
   qualified star expanding from a `databases:` entry, a cross-database join
   expanding two same-named tables from their own databases, and a negative case
   pinning that a `schemas:` entry is not visible to a MySQL analyzer.
   `TestLoadLineageTestSuiteFromYAML` asserts both the new lookup and the
   negative. Negative-checked by dropping the `databases:` registration: both
   positive cases and the loader test fail.

2. **`NATURAL JOIN` / `USING` existed only in the MariaDB dialect corpus**, even
   though both are MySQL grammar. The shared corpus now covers them (`02`): a
   natural join contributes only its projection, because the implicit join
   column is never written; `JOIN ... USING (col)` with explicit qualifiers
   resolves each side; and an unqualified `USING` column, which is coalesced and
   names no single relation, resolves by the deterministic
   first-relation-by-name rule. That last case pins the metadata-less fallback
   rather than the ambiguity policy: without a catalog the scope cannot tell
   which relation owns the name, so the deterministic rule stands, and §10.11
   leaves it unchanged. The pre-existing MariaDB dialect case is left in place.

Corpus state: the shared MySQL-family corpus is now 128 cases (`02` gained three
join cases, `19` adds three database-catalog cases), all matched exactly and
fully annotated.

### 10.9 Fifth MySQL pass: GROUP BY keys

`Transformation.GroupKeys` was plumbed end to end from the start — the model
field, the `column_lineage.transformation` JSONB payload, the API proto's
`repeated string group_keys = 5`, and a detail row the frontend already renders —
and was always empty, because its one producer passed `nil`. This pass fills it.
No proto change, no migration and no frontend change were needed.

Decisions:

- **What is stored** — the written text of each GROUP BY item, rendered the way
  `PartitionBy` and `OrderBy` already are: inter-token whitespace removed, source
  order kept, duplicates kept, case preserved. A positional key stays `"1"` and
  an alias stays the alias, because that is what the query wrote; resolving them
  would need select-list and catalog work for a field whose contract is "what the
  query grouped by".
- **Where it is attached** — on every transformation of a select item that
  contains a group aggregate. The outermost node is often not the aggregate:
  `SUM(o.total) + 1` is an `OPERATOR` and `CASE WHEN SUM(x) …` is a `CASE`, so an
  AGGREGATE-only rule would silently drop the keys for the commonest reporting
  shapes. An item with no group aggregate records nothing, so the field never
  claims a column was aggregated when it was only projected.
- **Window functions are excluded** — a windowed aggregate is governed by its
  `OVER` clause, so `SELECT SUM(x) OVER (…) … GROUP BY y` records no keys.
- **The subquery boundary is part of the rule** — `firstFuncCall` no longer
  descends into a `SubqueryExpr`, and the aggregate check stops there too.
  Without it the enclosing query's keys would be stamped on an inner subquery's
  aggregate. This also closes §10.1 item 8: an expression subquery is now
  `PROJECT` in the MySQL family, as it already was in PostgreSQL, so the `09`
  and `16/scalar_subquery` expectations move from `group`/`AGGREGATE` to
  `indirect`/`PROJECT`.
- **Deliberately not recorded** — `WITH ROLLUP` (the field is a key list, and the
  flag adds subtotal rows rather than keys); the grouping of a
  grouped-but-non-aggregating output column (`SELECT o.user_id … GROUP BY
  o.user_id` leaves `o.user_id` direct, because giving it a transformation would
  break the direct ⟺ no-transformation invariant the corpus enforces); and the
  grouping of a nested query whose transformation `subquerySources` discards
  (it surfaces where the inner transformation is what reaches the edge, as in
  `SELECT d.n FROM (SELECT x, COUNT(*) AS n FROM t GROUP BY x) d`).

Harness: `group_keys` is a pointer in the YAML, so `group_keys: []` is an
assertion rather than an omission, and `TransformationMatches` checks it whenever
it was written. `RequireFullEdgeAnnotations` now requires every `AGGREGATE`
transformation to assert it. A wrapper hides the aggregate from the guard, so the
`OPERATOR` and `CASE` shapes assert their keys in the corpus instead.

Corpus: `20_test_group_by_keys_table` adds 13 cases (one and several keys,
positional and alias keys, an expression key, an operator and a `CASE` wrapping
an aggregate, `COUNT(DISTINCT …)`, a windowed aggregate, no `GROUP BY`, a scalar
subquery, a nested query's own grouping, `WITH ROLLUP`), and the 16 pre-existing
`AGGREGATE` expectations now assert their keys. The shared corpus is 141 cases.

Negative-checked: disabling the attachment fails 33 cases, restoring traversal
into a subquery fails the three subquery cases, dropping one AGGREGATE's
`group_keys` fails the annotation guard, and reverting the empty-list match fails
`TestTransformationMatchesGroupKeys`. `gofmt`, `golangci-lint`, `go test ./...`,
the build and the real-server integration suite are green; the MariaDB and TiDB
copies remain pure regenerations of the MySQL analyzer.

### 10.10 Sixth MySQL pass: row-set predicate influences

`WHERE`, `HAVING` and join `ON` decide which rows a statement produces without
their value reaching any output column. The plan left open whether to record
them; they are now recorded as a **row-level influence**: the predicate column is
the source, the statement's target object is the target, and the **target column
is empty**, because the influence is on the rows rather than on any one column.

The vocabulary and the shape come from OpenLineage rather than being invented:
its `INDIRECT` transformation type is defined as "the output column value is
impacted by the value of `inputField`, but it is not derived from it", with the
example `SELECT source AS result FROM TAB WHERE pred = true`, and its indirect
subtypes are exactly `JOIN`, `GROUP_BY`, `FILTER` and `SORT`
([spec](http://openlineage.io/docs/1.48.0/spec/facets/dataset-facets/column_lineage_facet/)).
The ingestion path already consumed `INDIRECT` and the frontend already renders
it, so the SQL analyzers were the only side not producing it.

Decisions, and what they cost:

- **Row-level target, not per-output-column.** Recording one edge per predicate
  column per output column would grow the corpus 2× and the worst case 7×
  (a 12-column, 7-predicate view: 12 edges → 96), duplicate one fact N times, and
  add nothing to the table graph, which collapses column edges anyway. One edge
  per predicate column costs 0.5×.
- **The target column is empty**, matching how the ingestion already stored a
  dataset-level reference, so no synthetic marker, no frontend change and no i18n
  key were needed.
- **`FILTER` / `JOIN` operations**, and `GROUP_BY` / `SORT` exist for the
  subtypes ingestion can carry. `determineRelationType` maps `JOIN` to
  `RelationTypeJoin`, which was a dead enum value until now and makes
  `parseRelationType`'s `join` case live; `FILTER` stays indirect, as OpenLineage
  types it. The API still collapses both to `INDIRECT` (§10.2), which is exactly
  what OpenLineage reports.
- **SELECT-based statements only.** An `UPDATE`'s `WHERE` decides which rows are
  written of the table the statement already targets, so it would be a self-edge
  of low value; `DELETE` keeps its `__deletion__` marker, which names the effect
  rather than an influence. Both are asserted by the existing corpus.
- **`ORDER BY` is not recorded**, nor `DISTINCT` / `LIMIT`. OpenLineage has a
  `SORT` subtype, so this is a scope decision rather than a claim that ordering
  has no effect; it is the one clause whose effect is row order and truncation
  rather than which rows exist. `GROUP BY` keeps its payload keys (§10.9) with no
  edge.

Implementation notes:

- Predicates are collected while the query tree is walked and emitted by
  whichever emitter runs last, so a filter inside a CTE, a derived table, a
  scalar subquery or a set-operation arm is attributed to the statement's real
  target rather than to a temporary one.
- `HAVING` is the one clause that may name a select-list alias. An unqualified
  name that matches an output column resolves to that column's own sources, so
  `HAVING comment_count > 0` over `count(c.id) AS comment_count` records
  `comments.id` instead of inventing a `comments.comment_count` that does not
  exist.
- `JOIN ... USING (col)` names a key of both sides, resolved through each side's
  relation; `ON` uses its expression.
- A row-level influence over a query-local relation is traced to its base table
  through the existing `traceThroughTableLineage` path.
- A column used by two clauses is one predicate, so it keeps one edge: `JOIN` is
  collected before `FILTER` and names the relation type.
- `Transformation.Condition` is no longer DELETE-only; it carries the predicate
  text of a `FILTER` or `JOIN` edge.

Harness: `from_field` and `to_field` became pointer-backed, so a written name —
including the empty one — is an exact assertion and an omitted one is still a
wildcard. Every existing expectation already wrote both, so the change cost no
corpus churn, and `RequireFullEdgeAnnotations` now requires both. Without it,
`to_field: ""` would have silently stopped checking the very column this pass
depends on.

Ingestion (the same change on the other side): a dataset-level column-lineage
reference now keeps its `field` as the source column, records the target column
as empty, and derives its relation type from its transformations instead of
hard-coding `DIRECT`. Before, the filter column was dropped and an indirect
relation was stored as direct.

Corpus: `21_test_predicate_influence_table` adds 12 cases (WHERE, `ON`, a column
that both joins and filters, `HAVING`, `HAVING` on an alias, `USING`, a derived
table predicate, a predicate that is also a value source, a constant predicate,
`ORDER BY` / `LIMIT`, a qualified predicate, an `INSERT ... SELECT`), and the 98
predicate edges the existing cases gained were added mechanically. The shared
corpus is 153 cases with 111 row-level edges; MariaDB and TiDB gained four
between them.

Negative-checked: disabling predicate emission fails 33 cases, emitting a
non-empty target column fails 9 of the 12 new cases (which is what proves
`to_field: ""` asserts emptiness), disabling the `HAVING` alias resolution fails
the alias case and the `08` view, disabling `USING` fails three cases, and
dropping the ingested field again fails the new ingestion test. `gofmt`,
`golangci-lint`, `go test ./...`, the build and the real-server integration suite
are green; the MariaDB and TiDB copies remain pure regenerations.

### 10.11 Seventh MySQL pass: unqualified reference resolution

An unqualified name belongs to the innermost scope that provides it, and a scope
provides it when catalog metadata says so. The resolver violated both halves, and
the two defects were measured before and after against a live MySQL 8.3.0.

**Fixed — the enclosing scope was unreachable.** `Scope.ResolveColumnRef`'s
parent lookup ran only when the current scope had no relations at all, so a
correlated reference was resolved locally instead. Two faces:

```sql
SELECT o.id FROM orders o
WHERE EXISTS (SELECT 1 FROM customers c WHERE c.id = o.id AND amount > 100)
-- stored: customers.amount   (a column that does not exist)
-- MySQL:  orders.amount      (verified: the query runs and reads the outer column)
```

With two inner relations and complete metadata the name was not found in either,
which returned an error and **dropped the edge entirely**. Both shapes now record
`orders.amount` as a `FILTER` influence.

**Fixed — several owners were a guess.** `resolveByColumnMetadata` returned the
first relation that owned the name, so a name several relations own — which for
legal SQL means a coalesced `USING` / `NATURAL JOIN` column, whose value is
`COALESCE(left, right)` — reported one side only.

The engine's own answers bound the policy. `USING` and `NATURAL JOIN` coalesce
and are legal; `SELECT id FROM a JOIN b ON a.id = b.id` and
`SELECT id FROM db1.t JOIN db2.t ON …` both fail with error 1052 (`SELECT *` over
the same join is legal) — verified on 8.3.0. A view therefore never reaches the
analyzer in that shape; the statement is still analyzable as `MANUAL_SQL`, where
reporting both candidates is strictly more informative than picking one.

Decisions:

- **Every relation the catalog confirms owns the name is reported**, one edge
  each, in the scope's name order. A coalesced join column is now right on both
  sides, which is the case the policy exists for.
- **The search is innermost-out and metadata-gated.** A scope is left for the
  enclosing one only when every relation in it has columns and none owns the
  name; an ambiguous or metadata-less scope answers locally, because an enclosing
  scope cannot resolve a name the current one may own.
- **Metadata is never used to rule out a lone candidate.** When no scope owns the
  name, the innermost scope holding exactly one relation keeps it, because a
  catalog snapshot can lag a schema change. The rule is dropped as soon as a
  scope offers several candidates, so it keeps its "only when there is no choice"
  meaning.
- **A temporary relation no longer short-circuits the scan.** A relation the
  catalog confirms owns the name wins over a CTE or derived table that merely
  might; that is a fact rather than a guess.
- **The policy covers every engine and every source-column site** — a projection,
  a CTE or derived body, a set-operation arm, a predicate influence, an
  assignment source, a `DELETE` condition, a `LOAD DATA SET` source. A resolved
  *target* column stays singular: it names one column of the row being written.

Implementation: `scope.ResolvedColumn` pairs a reference with the relation that
owns it; `Scope.ResolveColumnRefs` returns one per owner and `ResolveColumnRef`
reports the first of them, so the sites that must choose one — an `UPDATE`
assignment's left-hand side, a view's declared column name — keep their
signature. `resolveUnqualified` walks the scope chain, `resolveInScope`
classifies a single scope, and the old metadata helper is gone. No proto, store,
API or frontend change: the extra edges are ordinary `ColumnRelation`s.

Corpus: `22_test_unqualified_resolution_table` adds 12 cases to the shared
MySQL-family corpus — several owners, three owners, the metadata-less fallback,
two databases, `USING`, `NATURAL JOIN`, a CTE body, three correlated shapes, a
name no scope owns, and the metadata-less correlated case — for 165 shared cases.
PostgreSQL adds 8 (`24`) and StarRocks 9 (`21`). The `02` case that pinned the
old first-relation-by-name rule keeps its expectation, because without metadata
the rule is unchanged, and its comment now says which decision it documents.

Negative-checked: emitting only the first owner fails 5 shared cases; doing the
same in the CTE and derived builders fails the CTE case in all three engine
suites; disabling the outward walk fails 3 shared, 1 PostgreSQL and 2 StarRocks
cases; and so does emitting one owner in their funnels. `gofmt`, `golangci-lint`, `go test ./...`, the build and
the real-server integration suite are green; the MariaDB and TiDB copies remain
pure regenerations (21-line diffs).

### 10.12 Eighth pass: undescribed relations and temporary-table columns

§10.11 left one case open: a relation the catalog does not describe made its
scope undecidable, and the scope then fell back to its name order, which could
attribute a name to a relation the catalog confirms does *not* own it. Measuring
that fallback found the second half — a CTE or derived table was *always*
undescribed, so a name it owned went to a base table by name order instead of
through the temporary table's own lineage.

Two changes, both in the shared resolver plus one registration site per analyzer.
Measured with catalog `a{id}`:

| shape | before | after |
| --- | --- | --- |
| `SELECT x FROM a JOIN u ON a.id = u.id` (`u` unsynced) | `a.x` | `u.x` |
| `SELECT x FROM a JOIN (SELECT id, x FROM b) d ON a.id = d.id` | `a.x` | `b.x`, through `d` |
| `SELECT id FROM a JOIN (SELECT id, x FROM b) d ON a.id = d.id` | `a.id` only | `a.id` **and** `b.id` |
| `WITH d AS (SELECT id, x FROM b) SELECT id FROM a JOIN d …` | `a.id` only | `a.id` and `b.id` |
| `… WHERE EXISTS (SELECT 1 FROM (SELECT id, y FROM b) d WHERE amount > 1)` | no edge | `orders.amount` as a `FILTER` influence |
| `SELECT x FROM a JOIN (SELECT * FROM b) d …`, with `b{id,x}` | `a.x` | `b.x` |
| the same without `b` in the catalog | `a.x` | `b.* → __result__.x` (§10.13) |

1. **An undescribed relation is preferred over one the catalog ruled out.** If no
   described relation owns the name, the only relation that can still provide it
   is one whose columns are unknown, so the first such relation in name order
   answers. Before, name order could pick a relation already known not to own it.
2. **A temporary relation exposes the columns it names.** The analyzer attaches a
   column list to the `TableRef` it registers for a CTE or derived table: the
   declared list (`WITH d (a, b)`) when the query wrote one, otherwise the names
   the body's output exposes. The resolver then treats it like any described
   relation — it can own the name, and it can be *ruled out*, which is what lets a
   correlated reference past a derived table reach the enclosing query. No catalog
   is needed for this: a derived table's own output list is known from its SQL.

Decisions:

- **An incomplete column list is reported as unknown.** A wildcard the catalog did
  not expand, and an output without a name, both leave the list incomplete, and
  the relation keeps the fallback rather than being ruled out on partial
  information. Treating the known subset as complete would drop or misattribute
  every column the star hides. The wildcard entry in the relation's lineage still
  answers a name the list does not carry (§10.13).
- **The names come from the analyzer, not from the lineage alone.** A column with
  no source (`SELECT 1 AS one`) contributes no lineage edge, so lineage targets
  are not a column list by themselves; the declared list or the output names are.
- **Two relations nobody describes are still a guess.** The rule only decides
  between an undescribed relation and one the catalog ruled out; §10.2 keeps the
  residual case honest.

Implementation: `scope`'s `resolution` carries the undescribed relations instead
of an `undecidable` flag, `resolveInScope` no longer special-cases a temporary
relation (its column list decides), and each analyzer gained
`attachTempColumnLookup`, `tempColumnNames` and `outputColumnAliases`. No proto,
store, API or frontend change.

Corpus: each `TestUnqualifiedResolution_Table` suite gained seven cases — a
relation with no snapshot, a derived table answering through its lineage, a name
a base table and a derived table both own, a CTE, a correlated reference past a
derived table, an expanded wildcard body, and an unexpanded one asserting zero
edges. The shared MySQL-family corpus is 172 cases, PostgreSQL 130, StarRocks 119.

Negative-checked: restoring the name-order fallback fails two scope unit tests and
two corpus cases per engine, and making temporary relations undescribed again
fails three per engine — the unexpanded-wildcard case among them, because name
order then invents `a.x`. `gofmt`, `golangci-lint`, `go test ./...`, the build and
the real-server integration suite are green; MariaDB and TiDB remain pure
regenerations (21-line diffs).

### 10.13 The wildcard lineage entry answers any column

§10.12 left one shape with no edge at all: a body that never expanded its star
(`SELECT * FROM b`) left its temporary relation undescribed, and tracing a
reference through it matched nothing — the match required the requested column to
be the *target* of a lineage entry, while the entry the body does carry is
`b.* → d.*`.

`model.AnsweringLineage` now selects the entries that answer a reference: an entry
whose target names the column, or, when none does, the entries whose target is the
wildcard — a body that never expanded its star forwards every column of its
source. The column behind the wildcard stays unknown; the source table does not.
A `*` request still takes the whole lineage, which is how a wildcard expands.

| shape | before | after |
| --- | --- | --- |
| `SELECT x FROM a JOIN (SELECT * FROM b) d ON a.id = d.id`, catalog `a{id}` | no edge | `b.* → __result__.x`, and the `ON` clause's `d.id` gains `b.* → __result__` |
| `WITH c AS (SELECT * FROM t) SELECT a FROM c` | no edge | `t.* → __result__.a` |
| the same written `SELECT c.a FROM c` | no edge | `t.* → __result__.a` |
| a derived body over `b JOIN c`, star unexpanded | — | one edge per source, `b.*` and `c.*` |
| a derived body `SELECT id, * FROM b` | — | the named entry answers `id`, the wildcard answers anything else |

Decisions:

- **A named entry wins over the wildcard.** It is the precise answer, so the
  wildcard only answers when the lineage names nothing; otherwise a mixed body
  would report the same fact twice.
- **The relation stays undescribed.** Forwarding is not knowing the column list,
  so a wildcard body still leaves the relation in the fallback rather than making
  it a confirmed owner (§10.12), and two relations nobody describes are still
  resolved by name order (§10.2).
- **The marker has one definition.** `model.WildcardColumn` is what every
  analyzer's `wildcardColumn` now refers to, because the marker stopped being an
  analyzer-only convention: the model reads it too.

Implementation: `model.AnsweringLineage` replaces the inline
`columnName != wildcardColumn && edge.Target.Name != columnName` test in every
lineage walk — `traceThroughTableLineage` and `appendFlattenedLineage` in the
MySQL family, and the three equivalents in PostgreSQL and StarRocks — so both the
lookup and the construction-time flattening forward a wildcard.

Corpus: each `TestUnqualifiedResolution_Table` suite's unexpanded-wildcard case now
asserts the forwarded edge (renamed `an unexpanded wildcard body forwards the
column`) and gained `a nested temporary relation forwards a wildcard column`,
which exercises the construction-time path. The shared MySQL-family corpus is 173
cases, PostgreSQL 131, StarRocks 120.

Negative-checked: restricting the selector to named targets fails the model test
and two cases per engine, and always including the wildcard fails the
named-preference test. `gofmt`, `golangci-lint`, `go test ./...`, the build and the
real-server integration suite are green; MariaDB and TiDB remain pure regenerations
(21-line diffs).

### 10.14 Ninth pass: column lists, the API relation type and one ingestion rule

Three gaps an audit of the MySQL family turned up, each measured before the fix.

**A temporary relation's column list was ignored.** `WITH c (a) AS (SELECT name
FROM t)` and `(SELECT name FROM t) AS d (x)` produced *no edge at all*: the
relation was registered under the body's output names, so a reference to the
exposed name found nothing and the lineage target carried the body's name.

| shape | before | after |
| --- | --- | --- |
| `WITH c (a) AS (SELECT name FROM users) SELECT a FROM c` | no edge | `users.name → __result__.a` |
| the same written `SELECT c.a FROM c` | no edge | `users.name → __result__.a` |
| `SELECT x FROM (SELECT name FROM users) AS d (x)` | no edge | `users.name → __result__.x` |
| `SELECT d.x FROM (SELECT name FROM users) AS d (x)` | no edge | `users.name → __result__.x` |
| `SELECT p, q FROM (SELECT id, name FROM users) AS d (p, q)` | no edge | `users.id → p`, `users.name → q` |
| `WITH c (name) AS (SELECT name FROM users) SELECT name FROM c` | worked | unchanged |
| `WITH c (a) AS (SELECT name FROM users) SELECT name FROM c` | no edge | still no edge: `c` does not expose `name` |

`exposedColumnNames` renames a temporary relation's output positionally when the
declared list's arity matches, and both the lineage target and the column list the
scope resolves against are built from it. PostgreSQL already renamed a CTE's
columns and was missing only the derived list; StarRocks renames the CTE list and
**cannot parse** a derived table's alias list, which is an omni gap, so only the
MySQL family and PostgreSQL changed.

**The API collapsed the relation type.** `convertRelationType` reported every
non-direct relation as `INDIRECT`, so the `JOIN`, `GROUP`, `UNION`, `INTERSECT`
and `EXCEPT` edges the analyzers record were indistinguishable through the API.
The enum now carries `JOIN = 3` through `UNKNOWN = 8` — the model's own numbering,
so the stored value and the wire value agree — the conversion is total, and the
frontend labels all of them from one helper (`frontend/src/lib/relationType.ts`,
`metadataBrowser.relation*`). The OpenLineage column page's literal
`DIRECT`/`INDIRECT` badges use the same labels now, so they are translated too.

**The ingestion derived the relation type by a second rule.** `mapRelationType`
returned only `Direct`/`Indirect`, so an ingested `INDIRECT/JOIN` facet was stored
as `INDIRECT` and a `DIRECT/AGGREGATION` as `INDIRECT`, while a SQL analyzer stores
`Join` and `Group` for the same semantics. The rule now lives once, in
`model.RelationTypeOf`, and both sides use it: `scope.NewLineageEdge` and the
ingestion's `buildColumnLineage`, which no longer takes a relation type at all
because the transformations already decide it. One consequence is deliberate — an
`IDENTITY` facet is a direct edge, so it carries **no** transformation, which is
the invariant the SQL side keeps and `RequireFullEdgeAnnotations` enforces.

Corpus: the shared MySQL-family corpus gained four cases (`04` a CTE list with and
without an alias reference, `03` a derived alias list likewise) for 177 cases, and
PostgreSQL gained the two derived ones for 133. StarRocks is unchanged.

Negative-checked: dropping the CTE rename fails its two shared cases; neutralising
`exposedColumnNames` fails the two derived cases in the MySQL family and in
PostgreSQL; collapsing the API conversion fails its test; and restoring a
`PROJECT` transformation for an identity facet fails five ingestion tests.
`gofmt`, `golangci-lint`, `go test ./...`, the build and the real-server
integration suite are green, as are Biome, ESLint, the i18n audit, `vue-tsc` and
the frontend tests; MariaDB and TiDB remain pure regenerations (21-line diffs).

### 10.15 Tenth pass: the items that were ours to close

An audit of what §10.2 still listed, with each item either closed or recorded as
decided.

Closed:

- **`ALTER VIEW` is analyzed.** The MySQL family dispatched it nowhere, so
  `ALTER VIEW v AS SELECT …` produced no lineage at all, even though MySQL accepts
  it and it replaces the view's definition. Verified on 8.3 that the column-list
  form is legal and renames the column: `ALTER VIEW v (order_ref) AS SELECT id FROM
  orders` leaves the view with one column called `order_ref`. `processViewBody` now
  backs both CREATE and ALTER and the shared corpus pins both forms. It matters for
  MANUAL_SQL only, because the runner wraps a stored definition as `CREATE VIEW`.
- **A recursive CTE is right, and now pinned.** The audit called `WITH RECURSIVE`
  unhandled; measuring showed the opposite for a recursion that has a source: the
  anchor arm's columns are reported and the self-reference contributes nothing,
  because it is query-local. The shape that yields no edge —
  `SELECT 1 AS n UNION ALL SELECT n + 1 FROM counter WHERE n < 5` — is correct: the
  value comes from the literal. Two cases pin both, the second with
  `expected_edges: []`.
- **The three copies have a parity test.** `TestDialectCopiesStayInSync` asserts
  that everything from the `Analyzer` type onward is byte-identical in the MySQL,
  MariaDB and TiDB analyzers, so a hand-edit to one copy cannot drift from the
  others. That is what keeps the 21-line prologue diff the regeneration produces
  honest.
- **The unreachable error branch is gone.** `processQuerySpecification` refused a
  "query form with no select list and no FROM"; `SELECT`, `SELECT FROM t` and
  `SELECT FROM` are all parse errors, so it could never fire.

Corpus: the shared MySQL-family corpus gained four cases — `ALTER VIEW` with and
without a column list (`08`), and two recursive CTEs (`04`) — for 181 cases.

Negative-checked: removing the `ALTER VIEW` dispatch fails its two cases, and a
hand-edit to the MariaDB copy fails the parity test with the regeneration message.
`gofmt`, `golangci-lint`, `go test ./...`, the build and the real-server
integration suite are green; MariaDB and TiDB remain pure regenerations.
