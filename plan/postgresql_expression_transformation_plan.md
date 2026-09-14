# Plan: PostgreSQL structured expression transformation detection (PG-FU-1)

> **Status: implemented.** The text scanners are gone, classification is
> structural, and the corpus/unit tests pin the new behavior. See **"Outcome"**
> for the numbers and the verified change set.
>
> This was the deferred follow-up recorded as **PG-FU-1** in
> `plan/postgresql_omni_parser_migration_plan.md` and the code marker
> `// TODO(PG-FU-1)` in `backend/plugin/lineage/postgresql/expr.go`.
>
> Scope: **PostgreSQL only** (`backend/plugin/lineage/postgresql`). No proto,
> store-schema or frontend change was required — the `Transformation` message
> already carries every field this plan populates.

## TL;DR

The PostgreSQL analyzer still classifies expressions by **scanning the source
text** (`analyzeExpressionOperator` + `isExpressionDerivedText` in `expr.go`).
That was kept deliberately during the ANTLR→omni migration so cutover could be
byte-identical. It has three costs, all measured below:

1. **False positives.** A string literal is misclassified by its contents:
   `'CASE WHEN x'` → `CASE`, `'a(b'` → `FUNCTION`, `'2020-01-01'` → `OPERATOR`.
   Each also fabricates a `t.* → result.col` lineage edge.
2. **Lost detail.** Function names and arguments are always empty; window
   `PARTITION BY` / `ORDER BY` are always `nil`; every operator is the single
   opaque string `ARITHMETIC` (`a + b`, `a / b`, `data->>'k'` are
   indistinguishable).
3. **Text-scan artefacts.** Detection uses a hand-maintained name list scanned in
   list order, so `COALESCE(SUM(x), 0)` is reported as `AGGREGATE SUM` and
   `SUM(x) OVER (...)` as `AGGREGATE` (losing the window clauses). `CAST(x AS t)`
   and `x::t` — the same node type — are classified differently because only the
   first contains `(`.

This plan replaces both functions with **structural AST classification** over the
omni `pg/ast`, mirroring the detector the MySQL analyzer already has
(`detectAggregateFunction` / `detectWindowFunction` / `detectFunctionCall` /
`extractWindowClauses`). The output becomes strictly more informative and
consistent, and — because `Transformation` is persisted and rendered in the UI —
this is a user-visible improvement, not a refactor.

**This is not a no-op change.** It intentionally changes emitted
`Transformation` content, and for a bounded set of expression shapes it also
changes `relation_type` and even the edge set (constant targets lose a fabricated
`t.*` edge). Those changes are enumerated in "Behaviour changes", pinned by the
corpus + a new unit-test table, and gated by a **before/after snapshot diff**
(parity against the deleted legacy analyzer is no longer available).

## Outcome

Landed in `backend/plugin/lineage/postgresql/expr.go` (+ the call sites in
`analyzer.go`):

- `isExpressionDerived` — explicit structural allow-list; `A_Const`,
  `ColumnRef`, `ParamRef`, `A_Star` and `SQLValueFunction` (e.g. `CURRENT_DATE`)
  are not derived.
- `isTableWideExpression` — the source-less `table.*` fallback now applies only to
  a `FuncCall` governing node, so constant casts/arrays/rows cannot fabricate an
  edge (see "Source-less wildcard fallback").
- `classifyExpression` + `classifyFuncCall` + helpers (`funcName`,
  `funcArgTexts`, `windowClauses`, `operatorToken`, `boolOpToken`,
  `nullTestToken`, `booleanTestToken`) replace `analyzeExpressionOperator`,
  `isExpressionDerivedText`, `findFunctionInExpression`, `isCaseExpression` and
  `containsArithmeticOperator`. The `windowFunctions` and `arithmeticOperators`
  variables were deleted (`Over != nil` decides window; operator tokens come from
  the AST).

Verification actually run:

- **Before/after snapshot diff** (the plan's primary gate): corpus + 31 real
  `pg_get_viewdef` definitions through the analyzer at HEAD and after the change.
  Result: **54 changed lines across 21 sections — 0 edges added, 0 edges removed,
  11 `relation_type` flips, 43 content enrichments.** Every changed line maps to a
  row in the change tables below; no unexplained diff.
- **Unit tests:** `expr_classify_test.go` (`TestIsExpressionDerived`,
  `TestClassifyExpression` — 30 expressions, `TestSourceLessFallback`).
- **Corpus:** new `22_test_expression_transformation_lineage_table.yaml`
  (11 cases) and tightened `relation_type`/`has_transform` assertions in
  `01_test_select_lineage_table.yaml`. Corpus is now **101 cases / 22 files**.
- Build, full hermetic suite, `golangci-lint`, release build and the real-server
  PostgreSQL integration test.

The `relation_type` flips, all intended:

| Section | Change | Cause |
| --- | --- | --- |
| `01` expression (`\|\|`) | DIRECT → INDIRECT | A_Expr now derived |
| `01` cast (`::`) | DIRECT → INDIRECT | TypeCast now derived |
| `03` correlated subquery | GROUP → INDIRECT | SubLink → PROJECT (decision 6) |
| `08` UPDATE with subquery | GROUP → INDIRECT | SubLink → PROJECT |
| `10` multiple window functions | GROUP → INDIRECT | windowed aggregate → WINDOW (Over-first) |

The 43 content-only changes: 15 gained a function name/args (`CONCAT`,
`COALESCE`, `CAST`, `UPPER`, `now`, …), 16 window functions gained
`PartitionBy`/`OrderBy`, 4 replaced `ARITHMETIC` with the source token, 6 moved
`FUNCTION`→`OPERATOR` (a parenthesised arithmetic/concat expression whose
governing node is `A_Expr`), and 2 were `PROJECT`→`OPERATOR` in an `UPDATE SET`.

## Why do this at all

### It is user-visible

`Transformation` is not internal. `backend/runner/lineageanalyzer` stores it per
edge (`BatchReplaceColumnLineage`), `backend/api/v1/lineage_service.go` exposes it
via `convertTransformations` (all nine fields), and
`frontend/src/components/metadata/LineageTransformationCell.vue` renders a summary
plus a details dialog for `functionName`, `arguments`, `groupKeys`, `partitionBy`,
`orderBy`, `opType`, `condition` and `expression`. Today a PostgreSQL window
column shows `WINDOW: ROW_NUMBER` with **empty** partition/order detail, and a
function column shows `FUNCTION` with no name.

`relation_type` is likewise rendered as a DIRECT/INDIRECT badge in
`TableLineageSection.vue`.

### The current detector is measurably wrong

Measured on the committed analyzer (this table is the probe output; see
Appendix A for the probe method):

| Target expression | Top-level AST node | Current output | Current `relation_type` |
| --- | --- | --- | --- |
| `'CASE WHEN x'` | `A_Const` | `CASE` | INDIRECT (+ fabricated `t.*→s` edge) |
| `'a(b'` | `A_Const` | `FUNCTION` (no name) | INDIRECT (+ fabricated `t.*→s` edge) |
| `'2020-01-01'` | `A_Const` | `OPERATOR ARITHMETIC` | INDIRECT (+ fabricated `t.*→s` edge) |
| `data->>'k'` | `A_Expr` `->>` | `OPERATOR ARITHMETIC` | INDIRECT |
| `SUM(x) OVER (PARTITION BY a ORDER BY b)` | `FuncCall` (Over) | `AGGREGATE SUM`, clauses lost | GROUP |
| `ROW_NUMBER() OVER (PARTITION BY a ORDER BY b)` | `FuncCall` (Over) | `WINDOW ROW_NUMBER`, clauses lost | INDIRECT |
| `COALESCE(a, b)` | `CoalesceExpr` | `FUNCTION` (no name/args) | INDIRECT |
| `GREATEST(a, b)` | `MinMaxExpr` | `FUNCTION` (no name/args) | INDIRECT |
| `COALESCE(SUM(x), 0)` | `CoalesceExpr` | `AGGREGATE SUM` (inner function wins) | GROUP |
| `lower(name)` | `FuncCall` | `FUNCTION` (no name/args) | INDIRECT |
| `CAST(created_at AS date)` | `TypeCast` | `FUNCTION` (no name) | INDIRECT |
| `created_at::date` | `TypeCast` | *(none)* | DIRECT |
| `a = b` | `A_Expr` `=` | *(none)* | DIRECT |
| `first_name \|\| ' ' \|\| last_name` | `A_Expr` `\|\|` | *(none)* | DIRECT |
| `a IS NULL` | `NullTest` | *(none)* | DIRECT |
| `a[1]` | `A_Indirection` | *(none)* | DIRECT |

### It diverges from the MySQL analyzer for no reason

The MySQL/MariaDB/TiDB analyzers already use structural detection over their omni
ASTs: they read `FuncCallExpr.Name`, `.Args`, `.Star`, `.Distinct`, `.Over`, and
`extractWindowClauses` fills `PartitionBy`/`OrderBy`. PostgreSQL has the *better*
AST (libpg_query-shaped, with `AggFilter`, `AggOrder`, `AggWithinGroup`) yet
reports less. This plan closes that gap.

## Decisions

### Settled (confirmed with the maintainer)

- **Classification is structural; text scanning is deleted.**
  `analyzeExpressionOperator` and `isExpressionDerivedText` are replaced by an
  AST walk over `pg/ast`; `findFunctionInExpression`,
  `isCaseExpression`, `containsArithmeticOperator` and the `arithmeticOperators`
  list are removed.
- **The top-level expression node governs classification.** *(Confirmed.)*
  PostgreSQL's parser does not materialise parentheses as nodes, so the outermost
  operator/function/case is unambiguous. This replaces "scan a name list in list
  order", fixes `COALESCE(SUM(x),0)` and `SUM(x) OVER (...)`.
- **`over != nil` means WINDOW, checked before the aggregate set.** A windowed
  aggregate is a window, not a group-by aggregate; this also corrects its
  `relation_type` from GROUP to INDIRECT.
- **Constant targets are not derived; their fabricated `t.*` edges are removed.**
  *(Confirmed: remove, category 3.)*
- **Operators use the source token** (`"+"`, `"-"`, `"*"`, `"/"`, `"||"`,
  `"->>"`, `"="`, `">"`, `"IN"`, `"BETWEEN"`, …). *(Confirmed.)* This matches the
  lineage proto comment and deliberately does not match MySQL's `ADDITION`-style
  words (see follow-ups).
- **`CAST` / `COALESCE` / `GREATEST` / `LEAST` / `NULLIF` are `FUNCTION`s with a
  synthesised name and argument texts.** *(Confirmed.)*
- **`SubLink` (scalar subquery) is `PROJECT`.** *(Confirmed.)*
- **No backfill.** *(Confirmed: the project is pre-launch, so existing
  `column_lineage_version` rows are left alone; the change applies to freshly
  analyzed objects.)*
- **Only PostgreSQL changes.** Aligning MySQL/MariaDB/TiDB with the same
  precedence and operator vocabulary is a separate follow-up (see
  "Cross-dialect consistency").
- **No proto or store change.** `Transformation` already has every field; the
  JSON shape is unchanged, so previously stored rows stay readable.
- **Keep one transformation per expression.** Combined chains (CTE/subquery
  flattening via `combineTransformations`) are unchanged.

### Out of scope

- **Aggregate `GROUP BY` keys.** `GroupKeys` stays `nil` (as MySQL does today);
  the analyzer tracks neither `GROUP BY` nor `HAVING`.
- **`FILTER` / `WITHIN GROUP` / `DISTINCT` / `ORDER BY` modifiers** on a
  function: parsed but not surfaced in `Transformation` (no field for them).
- **Unifying dialect vocabularies** (`op_type` words vs tokens).
- **Changing `model.Transformation`, the lineage proto, or the UI.**

## Target design

### Derived predicate

```go
// isExpressionDerived reports whether a target expression derives from its
// sources via an operation (i.e. its governing node is an operation, not a leaf).
func isExpressionDerived(node pgast.Node) bool
```

Returns `true` iff the **governing (top-level) node** is one of: `FuncCall`,
`CaseExpr`, `CoalesceExpr`, `MinMaxExpr`, `NullIfExpr`, `TypeCast`, `SubLink`,
`A_Expr`, `BoolExpr`, `NullTest`, `BooleanTest`, `A_ArrayExpr`, `RowExpr`,
`A_Indirection`, `GroupingFunc`, `CollateClause`, `NamedArgExpr`.
Returns `false` for `ColumnRef`, `A_Const`, `ParamRef`, `A_Star`, `SQLValueFunction`
(`CURRENT_DATE`), and any other leaf-ish node.

It is an **explicit allow-list, not a "not a leaf" default**. That distinction
matters: an unrecognised source-less node (`SELECT CURRENT_DATE AS d FROM t`)
must stay non-derived, otherwise the `len(sourceColumns)==0` fallback would
*fabricate* a `t.* → d` edge. Top-level decisiveness is safe because PostgreSQL
does not materialise parentheses as nodes, so an operation can never be wrapped in
a non-operation node.

This stops literals from masquerading as transformations (`'CASE WHEN x'`,
`'a(b'`, `'2020-01-01'`).

### Source-less wildcard fallback

`processExpressionTarget` keeps its existing fallback — a derived expression with
no detected column reference is attributed to every table in scope as `table.*`.
That is what gives `SELECT COUNT(*) FROM t` its `t.* → result.c` edge. Left
ungated, the wider (structural) predicate would **fabricate new** `t.*` edges for
constant operations, because `pg_get_viewdef` renders a constant as
`'active'::text` — a `TypeCast`, which is in the allow-list.

The fallback is therefore gated on the governing node being a `*FuncCall`:

```go
if isDerived && len(sourceColumns) == 0 && isTableWideExpression(rt.Val) {
    // COUNT(*), now(), … depend on the relation without naming a column
}
```

- `COUNT(*)`, `now()`, `array_agg(x)` → fallback (unchanged).
- `'active'::text`, `ARRAY[1,2]`, `(1,2)`, `(SELECT count(*) FROM o)` → no fallback,
  no fabricated edge (consistent with the confirmed constant decision).
- `CURRENT_DATE` → not derived at all (not in the allow-list).

Discovered while capturing the Phase 0 baseline (`omni_sweep.v_const`): without
this gate the change would have *added* a `users.* → v_const.flag` edge.

### Classification

```go
// classifyExpression maps the governing node to a transformation. ok is false
// for leaves (caller emits no transformation).
func (a *Analyzer) classifyExpression(node pgast.Node) (model.Transformation, bool)
```

| Governing node | Result |
| --- | --- |
| `*FuncCall` with `Over != nil` | `WINDOW(upper(last(Funcname)), exprText, partitionBy, orderBy)` |
| `*FuncCall` with aggregate name | `AGGREGATE(upper(last(Funcname)), exprText, nil)` |
| `*FuncCall` otherwise | `FUNCTION(last(Funcname), exprText, argTexts)` |
| `*CoalesceExpr` | `FUNCTION("COALESCE", exprText, argTexts)` |
| `*MinMaxExpr` | `FUNCTION(op == IS_GREATEST ? "GREATEST" : "LEAST", exprText, argTexts)` |
| `*NullIfExpr` | `FUNCTION("NULLIF", exprText, argTexts)` |
| `*TypeCast` | `FUNCTION("CAST", exprText, [argText])` |
| `*CaseExpr` | `CASE(exprText)` |
| `*A_Expr` | `OPERATOR(operatorToken(expr), exprText)` |
| `*BoolExpr` | `OPERATOR("AND"/"OR"/"NOT", exprText)` |
| `*NullTest` / `*BooleanTest` | `OPERATOR("IS NULL"/"IS NOT NULL"/…, exprText)` |
| `*SubLink`, `*A_Indirection`, anything else | `PROJECT(exprText)` |

Helpers:

```go
func firstFuncCall(node pgast.Node) *pgast.FuncCall
func funcName(fc *pgast.FuncCall) string        // last Funcname segment (schema-stripped)
func (a *Analyzer) funcArgTexts(fc *pgast.FuncCall) []string
func (a *Analyzer) windowClauses(w *pgast.WindowDef) (partitionBy, orderBy []string)
func operatorToken(expr *pgast.A_Expr) string   // last Name segment; AEXPR_IN/LIKE/… map to their keyword
```

Notes:

- `Funcname` is a possibly-qualified `*List`, so `pg_catalog.count(*)` reports
  `COUNT` (last segment). Aggregate/window names are upper-cased to preserve
  today's `COUNT`/`SUM` output and match the MySQL analyzer; scalar function
  names keep the AST value (PostgreSQL folds unquoted names to lower case, so
  `lower` stays `lower`).
- `WindowDef.PartitionClause` → each expression's `exprText`;
  `WindowDef.OrderClause` (a list of `*SortBy`) → each `SortBy.Node`'s
  `exprText` (direction deliberately dropped, matching
  `mysql/extractWindowClauses`).
- `operatorToken` returns the source operator (`"+"`, `"-"`, `"*"`, `"/"`, `"||"`,
  `"->>"`, `"="`, `">"`, …). For the non-`AEXPR_OP` kinds it returns the keyword
  form (`"IN"`, `"LIKE"`, `"BETWEEN"`, `"IS DISTINCT FROM"`, …). This matches the
  proto comment (`op_type`, e.g. `"+"`, `"="`); it deliberately does **not**
  match MySQL's `ADDITION`/`SUBTRACTION` words (see follow-ups).

### Call sites

`processExpressionTarget` currently calls `isExpressionDerivedText(exprText)`
then `analyzeExpressionOperator(rt.Val)`. It becomes:

```go
isDerived := isExpressionDerived(rt.Val)
// ... existing no-source wildcard fallback for derived expressions ...
if isDerived {
    if transform, ok := a.classifyExpression(rt.Val); ok {
        outputCol.Transform = []model.Transformation{transform}
    }
}
```

## Behaviour changes

Three categories, all intentional. The tables are illustrative (the corpus does
not contain every shape); the authoritative change set is the 54-line snapshot
diff summarized in "Outcome". Each row is pinned by a test.

### 1. Content-only (edges and `relation_type` unchanged)

| Expression | Before | After |
| --- | --- | --- |
| `ROW_NUMBER() OVER (PARTITION BY a ORDER BY b)` | `WINDOW ROW_NUMBER`, `pb=[] ob=[]` | `WINDOW ROW_NUMBER`, `pb=[a] ob=[b]` |
| `SUM(x) OVER (...)` | `AGGREGATE SUM` (GROUP) | `WINDOW SUM`, `pb/ob` filled (INDIRECT) |
| `COALESCE(a,b)` | `FUNCTION ""` | `FUNCTION COALESCE`, `args=[a b]` |
| `GREATEST(a,b)` | `FUNCTION ""` | `FUNCTION GREATEST`, `args=[a b]` |
| `lower(name)` | `FUNCTION ""` | `FUNCTION lower`, `args=[name]` |
| `CAST(x AS date)` | `FUNCTION ""` | `FUNCTION CAST`, `args=[x]` |
| `a + b`, `a - b`, `a * b`, `a / b`, `-a` | `OPERATOR ARITHMETIC` | `OPERATOR "+"/"-"/"*"/"/"` |
| `data->>'k'` | `OPERATOR ARITHMETIC` | `OPERATOR "->>"` |
| `pg_catalog.count(*)` | `AGGREGATE COUNT` | `AGGREGATE COUNT` (schema stripped) |
| `count(DISTINCT a)` | `AGGREGATE COUNT` | `AGGREGATE COUNT` |

### 2. `relation_type` changes (edge endpoints unchanged)

| Expression | Before | After |
| --- | --- | --- |
| `created_at::date` | DIRECT, no transform | INDIRECT, `FUNCTION CAST` (now consistent with `CAST(...)`) |
| `a = b`, `a > b`, … | DIRECT, no transform | INDIRECT, `OPERATOR "="/">"` |
| `first_name \|\| ' ' \|\| last_name` | DIRECT, no transform | INDIRECT, `OPERATOR "\|\|"` |
| `a IS NULL` | DIRECT, no transform | INDIRECT, `OPERATOR "IS NULL"` |
| `a[1]` | DIRECT, no transform | INDIRECT, `PROJECT` |
| `COALESCE(SUM(x),0)` | AGGREGATE → GROUP | FUNCTION COALESCE → INDIRECT |

These are corrections: a comparison, cast, concatenation or null test *is* a
derivation.

### 3. Edge-set changes (fabricated constants removed)

| Expression | Before | After |
| --- | --- | --- |
| `'CASE WHEN x'` | edge `t.* → result.s`, `CASE` | no edge (constant) |
| `'a(b'` | edge `t.* → result.s`, `FUNCTION` | no edge (constant) |
| `'2020-01-01'` | edge `t.* → result.s`, `OPERATOR ARITHMETIC` | no edge (constant) |
| `'x'` | no edge | no edge (unchanged) |

This is the only change that removes edges. It removes a **false** lineage claim
("`s` comes from every column of `t`") for constant targets. **Confirmed: remove
the fabricated edges** (the content-only alternative is not taken). Note that once
the predicate is an explicit allow-list, unrecognised source-less nodes such as
`CURRENT_DATE` stay non-derived too, so no `t.*` edge is fabricated for them
either.

### `relation_type` derivation is untouched

`determineRelationType` is unchanged: AGGREGATE→GROUP, UNION→UNION,
DELETE/other→INDIRECT, no transformation→DIRECT. Every `relation_type` change in
this plan is therefore a *consequence* of the derived predicate or the
AGGREGATE↔WINDOW correction, never of a new mapping.

## Impact and rollout

- **Storage.** `column_lineage.transformation` is `json.Marshal` of
  `model.Transformation` (`backend/store/column_lineage.go`); the field set is
  unchanged, so old and new rows coexist with no migration.
- **API/UI.** No change; the frontend already renders the newly populated fields.
- **Re-analysis is gated by metadata hash.** `queueAll` re-analyzes an object only
  when `column_lineage_version.meta_hash` differs
  (`backend/runner/lineageanalyzer/analyzer.go:171`). Existing PostgreSQL objects
  therefore keep their old/absent transformation detail until their definition
  changes. **No backfill is performed** (confirmed: the project is pre-launch), so
  the improvement simply applies to every object from its next analysis onward.
- **No frontend/i18n change.**

## Verification

Parity against the old ANTLR analyzer no longer exists (it was deleted at
cutover). Verification is therefore built around an explicit **before/after
snapshot diff**, so no change is silent.

1. **Baseline snapshot (before touching code).** Capture, for the whole corpus
   plus a live `pg_get_viewdef` sweep, a normalized list of
   `(source, target, relation_type, is_temp, transformation…)` from the committed
   analyzer (HEAD `16d4dd8`). Keep it as a development artefact.
2. **After snapshot + diff.** Re-run and diff. Every line must match a row in
   "Behaviour changes"; anything else is a bug and blocks the change. This is the
   replacement for the removed parity harness.
3. **Unit table test** `expr_classify_test.go`: for each expression in the
   matrices above, assert the `Transformation` (operation, function name, args,
   partition/order, op type) directly against `classifyExpression` — the corpus
   only asserts `has_transform`/`relation_type`.
4. **Corpus.** Add `22_test_expression_transformation_lineage_table.yaml` with
   `has_transform` and `relation_type` for the affected shapes, and add the missing
   assertions to `01`/`16`/`17`/`10` where behaviour changes.
5. **Live sweep.** Recreate the migration's `omni_sweep` schema (30+ views covering
   joins, windows, aggregates, casts, concatenation, constants) and diff before/after.
6. **Cross-dialect spot check.** Run the shared-shape SQL through the MySQL
   analyzer and confirm no *unintended* divergence beyond the documented ones.
7. **UI smoke.** Open a PG view's transformation details and confirm function
   name/args and window partition/order render.
8. **Standard gates.** `go test ./...`, `go test -tags integration
   ./backend/test/integration/runner -run Postgres…`, `golangci-lint`, release build.

## Phases

All phases landed in one change (see "Outcome"); the phase split below is the
review order that was followed.

### Phase 0 — Baseline + harness
- Captured the before snapshot (corpus + live sweep) from the committed analyzer.
  The classification unit table was written against the *target* behavior and run
  red first (the NULLIF and window-clause cases), then made green. ✅

### Phase 1 — Structural predicate
- Replaced `isExpressionDerivedText` with `isExpressionDerived`; wired it in
  `processExpressionTarget`; gated the wildcard fallback on
  `isTableWideExpression`. **Exit:** snapshot diff shows only category 2/3 rows —
  confirmed (0 edges added/removed). ✅

### Phase 2 — Structural classification
- Implemented `classifyExpression` + helpers; deleted the text scanners. **Exit:**
  snapshot diff shows only rows in the change tables; unit table green. ✅

### Phase 3 — Richness
- Filled function names/args, window partition/order, operator tokens, `CAST`,
  `COALESCE`/`GREATEST`/`LEAST`/`NULLIF`, `CASE`, null/boolean tests. **Exit:**
  corpus + unit table green. ✅

### Phase 4 — Rollout
- No backfill (confirmed pre-launch); the improvement applies from each object's
  next analysis. **Exit:** integration suite green; follow-ups recorded. ✅

## Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- |
| A snapshot diff row is not in the change tables (unintended change) | Medium | High | Per-row review gate; the baseline snapshot is the contract |
| Constant targets lose their `t.*` edge and a user notices | Medium | Medium | Explicit Open Question 1; the content-only alternative preserves edges |
| `relation_type` flips (DIRECT→INDIRECT, GROUP→INDIRECT) affect graph filters | Medium | Medium | Enumerated; each is a correctness fix; documented in the release note |
| Existing rows do not change until re-analysis, so the UI looks inconsistent across objects | High | Low | Decide the backfill (Open Question 5); document convergence |
| Structural rule diverges from MySQL, confusing users across engines | Medium | Medium | Cross-dialect table + follow-up to unify |
| Over-classifying a rare node (e.g. `RowExpr`) as INDIRECT | Low | Low | Fallback is `PROJECT`; snapshot + corpus catch surprises |

## Cross-dialect consistency (follow-ups)

| Aspect | PostgreSQL (this plan) | MySQL family (today) | Follow-up |
| --- | --- | --- | --- |
| Windowed aggregate `SUM(x) OVER (...)` | `WINDOW` | `AGGREGATE` | Align MySQL to `WINDOW` |
| Nested mix `abs(a)+abs(b)`, `COALESCE(SUM(x),0)` | top-level node governs (`OPERATOR +`, `FUNCTION COALESCE`) | first function call governs (`FUNCTION abs`, `FUNCTION COALESCE`) | Align MySQL to top-level-governs |
| Operator vocabulary | source token (`"+"`, `"->>"`) | word (`ADDITION`, …) | Prefer tokens repo-wide (matches the proto comment) |
| Function names | last `Funcname` segment | raw `FuncCallExpr.Name` | — |

These are recorded as **PG-FU-4** (cross-dialect expression-transformation
unification) unless the maintainer chooses to fold them into this plan.

## Decisions resolved during plan review

All six questions were answered by the maintainer; they are recorded in
"Decisions → Settled" above. For traceability:

1. **Constants** → remove the fabricated `t.*` edge (category 3 applies).
2. **Operator vocabulary** → source token (`"+"`, `"="`, `"->>"`, …).
3. **`CAST` / `COALESCE` / `GREATEST` / `NULLIF`** → named `FUNCTION`.
4. **Precedence** → the top-level node governs.
5. **Backfill** → none (pre-launch).
6. **`SubLink`** → `PROJECT`.

## Appendix A — Probe method

A throwaway program (not committed) parsed each expression with
`omnipg.Parse`, printed the top-level `*ResTarget.Val` type and an `Inspect`
walk of node kinds, and ran the committed `postgresql.Analyze` to print the
current `Transformation` per `__result__` edge. The "current output" and
"relation_type" columns in this plan are that output. Node facts were confirmed
from `pg/ast/parsenodes.go` and `pg/ast/enums.go`.

## Appendix B — Relevant omni AST nodes

All confirmed present in `github.com/bytebase/omni/pg/ast`:

```go
type FuncCall struct { Funcname, Args *List; AggOrder *List; AggFilter, Over Node
    AggWithinGroup, AggStar, AggDistinct, FuncVariadic bool; Loc Loc }
type WindowDef struct { Name, Refname string; PartitionClause, OrderClause *List
    FrameOptions int; StartOffset, EndOffset Node; Loc Loc }
type SortBy struct { Node Node; SortbyDir SortByDir; SortbyNulls SortByNulls; UseOp *List; Loc Loc }
type A_Expr struct { Kind A_Expr_Kind; Name *List; Lexpr, Rexpr Node; Loc Loc }
type CaseExpr / CaseWhen / CoalesceExpr / MinMaxExpr / NullIfExpr
type TypeCast struct { Arg Node; TypeName *TypeName; Loc Loc }
type BoolExpr struct { Boolop BoolExprType; Args *List; Loc Loc }   // AND/OR/NOT
type NullTest struct { Arg Node; Nulltesttype NullTestType; Argisrow bool; Loc Loc }
type BooleanTest struct { Arg Node; Booltesttype BoolTestType; Loc Loc }
type A_Indirection struct { Arg Node; Indirection *List; Loc Loc }
type SubLink struct { SubLinkType int; Testexpr Node; OperName *List; Subselect Node; Loc Loc }
// A_Expr_Kind: AEXPR_OP, AEXPR_OP_ANY, AEXPR_OP_ALL, AEXPR_DISTINCT,
//   AEXPR_NOT_DISTINCT, AEXPR_NULLIF, AEXPR_IN, AEXPR_LIKE, AEXPR_ILIKE,
//   AEXPR_SIMILAR, AEXPR_BETWEEN, AEXPR_NOT_BETWEEN, AEXPR_BETWEEN_SYM,
//   AEXPR_NOT_BETWEEN_SYM, AEXPR_OVERLAPS
// No ParenExpr node: parentheses are not materialised, so the top-level
// expression node is unambiguous.
```
