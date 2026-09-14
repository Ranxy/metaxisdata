# Plan: Migrate the PostgreSQL Lineage Analyzer from `bytebase/parser` (ANTLR) to `bytebase/omni`

> **Status: implemented (cutover landed).** The omni-backed analyzer replaced the
> ANTLR one, `postgresqlantlr` was deleted, and `Engine_POSTGRES` now resolves to
> the omni implementation. See **"Outcome"** below for what shipped, the
> dependency impact, the two defects the migration fixed, and the divergences
> recorded against the legacy analyzer.
>
> This plan is deliberately scoped to **`storepb.Engine_POSTGRES` only**. Unlike
> the MySQL family there is no dialect follow-up: `Engine_POSTGRES` is the only
> PostgreSQL engine in the enum (`MSSQL`, `MYSQL`, `POSTGRES`, `TIDB`,
> `MARIADB`, `OCEANBASE`, `STARROCKS`, `DORIS`), so completing this plan removed
> `bytebase/parser` from the repository entirely.

## TL;DR

Replace the ANTLR-based parser under
`backend/plugin/lineage/postgresql/analyzer.go` with
`github.com/bytebase/omni/pg` + `.../pg/ast`, **keeping our own lineage
algorithms** (scope resolution, edge generation, temp-table flattening,
`model.Transformation` semantics, special markers).

This is **not** an import swap. The two parsers expose unrelated ASTs:

- today: ANTLR parse tree — 84 distinct `pg.*Context` types, chained accessor
  methods, 4 `.GetText()` calls;
- omni: a PostgreSQL-17 recursive-descent AST (`*ast.SelectStmt`, `*ast.InsertStmt`,
  `*ast.RangeVar`, `*ast.ResTarget`, `*ast.ColumnRef`, …) that is close to
  PostgreSQL's own `parsenodes.h`, with a byte-offset `Loc{Start,End}` on every
  node and `ast.NodeLoc(node)` to read it.

**47 of 73 functions (~1315 of 1763 lines, 74%) are parser-coupled and must be
rewritten; 26 functions (~448 lines, 25%) plus the whole `scope`/`model`/`catalog`
layer port unchanged.** The rewrite is mechanical for statement/clause traversal
and *simplifying* for identifiers and the expression layer.

**Feasibility is pre-verified, not assumed.** A throwaway probe ran
`omni/pg.Parse` over the entire existing golden corpus (85 cases across 20 YAML
files): **85/85 parse**, and each maps to exactly one of the node types the new
dispatcher needs. See "Feasibility evidence".

Why this route over adopting omni's `pg/catalog` IR (the other candidate):
omni's **parser AST keeps what a catalog IR drops** — DML statements
(INSERT/UPDATE/DELETE), `ON CONFLICT`, DML `RETURNING`, set-operation shape, and
`FuncCall.Over`. It therefore preserves our current coverage instead of
regressing it. The existing PostgreSQL analyzer also never used a catalog IR for
anything but `SELECT *` expansion, which stays on our `catalog.Provide`.

**Engine scope (decided):** the new analyzer registers for
`storepb.Engine_POSTGRES` **only** — which is all it registers today.

**Parse policy (decided): hard failure on parse errors, multi-statement input
processed statement by statement.** There is no ANTLR fallback and no best-effort
parse. `omni/pg.Parse` errors surface as an analysis error recorded in
`column_lineage_version.error_message`. Multiple well-formed statements are all
analyzed (matching the legacy `stmtmulti` behavior for `MANUAL_SQL`), which is a
deliberate difference from the MySQL migration's single-statement contract — see
"Multi-statement policy".

**Deliberately deferred, and tracked (do not forget):** this migration keeps the
legacy text-based expression detection and emits `nil, nil` window
`PARTITION BY` / `ORDER BY`. The structured upgrade is **PG-FU-1** in
**"Follow-ups"**; it is intentionally out of scope here so cutover can be
byte-identical, and must not be lost when this plan is archived.

## Outcome

Landed:

- `backend/plugin/lineage/postgresql/analyzer.go` rewritten on
  `github.com/bytebase/omni/pg` + `.../pg/ast`, pinned at the already-present
  `v0.0.0-20260912023254-4574e69bb9f1`; expression helpers split into `expr.go`.
- `backend/plugin/lineage/postgresqlantlr/` (the ANTLR implementation), the
  migration-only `parity_test.go`, and the `testutil/lineage_parity_helper.go`
  harness deleted. `RegisterAnalyzeRelation` now registers **`Engine_POSTGRES`**
  from the new package.
- `backend/server/ultimate.go` and the PostgreSQL lineage integration test import
  `plugin/lineage/postgresql` again (unchanged path, new implementation).
- Corpus grew from 85 to **90 cases / 21 files**: 5 new cases in
  `21_test_parenthesized_join_lineage_table.yaml` pin the defect fix below.
- Hard-fail parse policy pinned by `hardfail_test.go`, which also pins the
  multi-statement contract; `benchmark_test.go` mirrors the MySQL benchmark set.

Dependency impact (measured after `go mod tidy`):

- `github.com/bytebase/parser` is **removed** from `go.mod`.
- `github.com/antlr4-go/antlr/v4` becomes **indirect only**: it is still required
  by `github.com/google/cel-go` (used by `backend/api/v1`), so it cannot be
  dropped — it is simply no longer a lineage dependency.
- `github.com/bytebase/omni` was already present and is unchanged; the `pg`
  packages add no new module requirements.

Defects fixed (both found by the Phase 4 differential sweep over 31 real
`pg_get_viewdef` definitions in a live PostgreSQL):

1. **Parenthesized join trees produced zero lineage.** `pg_get_viewdef` renders
   every view that joins tables as `FROM (a JOIN b ON ...)`. The legacy analyzer
   silently returned no edges for that shape, so *every stored PostgreSQL view
   with a join had no column lineage*. The omni analyzer resolves it. Pinned by
   `21_test_parenthesized_join_lineage_table.yaml` and listed as
   `knownLegacyDefects` while the parity harness still existed.
2. **Ambiguous unqualified columns resolved nondeterministically.**
   `scope.ResolveColumn` ranged a map, so a column name present in more than one
   FROM relation (e.g. a NATURAL JOIN) picked an arbitrary table. The resolver now
   iterates sorted keys, making the emitted edge deterministic for the PostgreSQL
   analyzer and for the MySQL-family analyzers that share `scope` (their corpora
   stay green).

Recorded divergences from the legacy analyzer:

1. **Unquoted identifiers fold to lowercase** (PostgreSQL semantics). The corpus
   exercises this directly: the `ON CONFLICT` special relation `EXCLUDED` becomes
   `excluded`.
2. **Multi-statement input** is analyzed statement by statement (legacy
   parity), unlike the MySQL analyzer's single-statement rejection.
3. **Parse errors hard-fail**; the legacy ANTLR path recovered silently and its
   `a.errors` branch was dead code.
4. **Parenthesized joins and `table.*` on joined relations now emit edges** where
   legacy emitted none (defect 1). This is the one place parity is intentionally
   not byte-identical, and it is in the direction of correctness.

## Why do this at all

1. `github.com/antlr4-go/antlr/v4` and `github.com/bytebase/parser` were used by
   **exactly one file** in the whole backend:
   `backend/plugin/lineage/postgresql/analyzer.go` (`grep` over
   `backend/**/*.go` confirms `mysql`/`mariadb`/`tidb` are already omni-based).
   Finishing this migration **removes `bytebase/parser` entirely**. `antlr4-go`
   cannot be removed — `google/cel-go` (used by `backend/api/v1`) still requires
   it — but it stops being a direct lineage dependency and becomes indirect.
2. omni is Bytebase's intended successor to `bytebase/parser`: pure-Go,
   hand-written, no runtime codegen, actively maintained upstream. Its
   `pg`, `pg/parser` and `pg/ast` packages import only stdlib + `bytebase/omni`,
   so **the dependency delta is zero** — omni is already pinned in `go.mod` for
   the MySQL-family analyzers. (The `pgx`/`testcontainers` entries omni's
   `go.mod` lists come from its own tests/catalog and are not reachable from the
   imported packages.)
3. omni's AST is better suited to lineage than parse-tree walking:
   `Loc{Start,End}` gives exact source spans for expression text,
   `ColumnRef.Fields` is already a flat identifier list, `RangeVar` exposes
   schema/relation/alias directly, and `FuncCall.Over` gives structured window
   info — none of which the ANTLR tree provided without manual unwinding.

## Feasibility evidence (probe, not assumption)

Run before writing this plan; the probe program was a throwaway (not committed).

### 1. Corpus parse sweep — 85/85

Every `sql:` value from
`backend/plugin/lineage/postgresql/testdata/analyze/*.yaml` was decoded with
`yaml.v3` and passed to `omni/pg.Parse`:

| Result | Count |
| --- | ---:|
| Parsed as exactly one statement | **85** |
| Parse error | 0 |
| Multiple statements | 0 |
| Total cases | 85 |

AST node produced per corpus family:

| Corpus file | omni node |
| --- | --- |
| `01`–`05`, `10`, `12`, `13`, `16`–`19` | `*ast.SelectStmt` |
| `06`, `15` | `*ast.InsertStmt` |
| `08` | `*ast.UpdateStmt` |
| `09` | `*ast.DeleteStmt` |
| `14` | `*ast.ViewStmt` |
| `07`, `20` | `*ast.CreateTableAsStmt` |

### 2. AST shape / `Loc` coverage

A second probe dumped the AST for representative statements. Confirmed:

- `ResTarget{Name, Val, Loc}` unifies the legacy `Target_star` /
  `Target_columnref` / `Target_label` split; `Val` is `*ast.ColumnRef`,
  `*ast.A_Expr`, `*ast.FuncCall`, `*ast.CaseExpr`, `*ast.CoalesceExpr`, …
- `Loc` is populated on expression nodes and **is absolute into the full input
  SQL** (verified across a two-statement input: the second statement's column
  `Loc` slices correctly out of the whole string).
- `Loc` may include trailing whitespace (`"users.name "`), so text extraction
  must `TrimSpace`.
- Set operations are a left-deep tree: `SelectStmt.Op` = `SETOP_UNION` /
  `SETOP_INTERSECT` / `SETOP_EXCEPT` with `Larg`/`Rarg` and `All`.
- `FROM` items are `*ast.RangeVar`, `*ast.RangeSubselect`, or nested
  `*ast.JoinExpr{Larg,Rarg,UsingClause,Quals}`.
- `ColumnRef.Fields` is a `*List` of `*ast.String` / `*ast.A_Star`.
- `InsertStmt.Cols` items are `*ast.ResTarget` with `Name` set;
  `OnConflictClause.TargetList` holds the `DO UPDATE SET` list.
- `UpdateStmt.TargetList` items are `*ast.ResTarget{Name, Val}`;
  `DeleteStmt.UsingClause` holds `USING`.
- `CreateTableAsStmt{Query, Into *IntoClause, Objtype, IsSelectInto}` covers
  both `CREATE TABLE AS` and `CREATE MATERIALIZED VIEW` (and `SELECT INTO`),
  so the legacy `processCreateAsStmt` + `processCreateMatviewStmt` merge.
- Identifiers are **unquoted already** (`RangeVar.Relname` = `MyTable` for
  `"MyTable"`), so the legacy `getIdentifierText` quote-stripping disappears.

### 3. Recorded divergences found by the probe

- **Unquoted identifier case folding.** omni folds unquoted identifiers to
  lowercase (`SELECT Users.ID FROM Users` → `users.id` / `users`), matching
  PostgreSQL semantics. Legacy ANTLR preserved the literal source case. Real
  view/materialized-view input comes from `pg_catalog.pg_views.definition` /
  `pg_matviews.definition` (`backend/plugin/db/pg/sync.go`), i.e.
  `pg_get_viewdef` output, which PostgreSQL has already folded — so production
  edges are unaffected; only `MANUAL_SQL` with mixed-case unquoted identifiers
  changes. Recorded in "Recorded divergences".
- Quoted identifiers are unquoted identically by both parsers (omni stores the
  value; legacy stripped the surrounding quotes), so quoted-name behavior is
  unchanged.

## Decisions

### Settled

- **Parser-only migration.** Keep `scope`, `model`, `catalog`, and the lineage
  algorithm layer. Do **not** adopt `pg/catalog` or `pg/semantic`; that route
  still requires a walker and discards DML/set-op/window detail.
- **Use the root `pg` package for parsing.** `omnipg.Parse(sql) ([]Statement,
  error)` already returns split statements with `AST`, `ByteStart/End`, and
  `Start/End` positions; `pg/parser` is not imported directly.
- **Coexist, then cut over.** The existing ANTLR analyzer stays in the tree and
  keeps serving production until parity is proven.
  `RegisterAnalyzeRelation` panics on duplicate registration, so the two
  implementations must not both register; selection is explicit (see
  "Coexistence & cutover").
- **Parity is proven on the existing golden corpus.** All **85 cases across 20
  YAML files** under `backend/plugin/lineage/postgresql/testdata/analyze/` must
  produce byte-identical normalized edge sets before cutover. The YAML suite's
  own `ValidateExpectedEdges` is a subset check (it never asserts the absence of
  extra edges), so a separate full-set parity harness is required.
- **Correctness wins over byte-parity where the legacy analyzer is wrong or
  nondeterministic.** Two such defects were found and fixed (parenthesized join
  trees; ambiguous-column map iteration) instead of being reproduced; both are
  recorded in "Outcome" and pinned by the corpus. Every other case is
  byte-identical.
- **Hard failure on parse errors; no ANTLR fallback.** The old ANTLR analyzer is
  a parity oracle during development only, then deleted. A parse error is an
  analysis error, not a silent partial result.
- **Multi-statement input is analyzed statement by statement.**
  *(Confirmed with the maintainer.)* See "Multi-statement policy" — this
  preserves legacy `MANUAL_SQL` behavior and differs deliberately from the MySQL
  migration.
- **Window-transformation content stays `nil, nil`** for `PARTITION BY` /
  `ORDER BY` in the first cut (exactly what legacy emitted). *(Confirmed with the
  maintainer: accepted as the migration behavior, on the condition that the
  structured upgrade is written down so it is not forgotten.)* The upgrade is
  **not** part of this migration and is tracked as a mandatory follow-up in
  **"Follow-ups" → PG-FU-1**; this plan must not be marked "done" without that
  section still present in the document.
- **Unsupported-but-parsed statement kinds are a no-op**, matching legacy.
- **Pin omni to the existing commit** `v0.0.0-20260912023254-4574e69bb9f1`;
  no `go.mod`/`go.sum` change is expected for the migration itself.

### Out of scope (separate work)

- **Adopting a catalog IR / type resolution.** `SELECT *` expansion stays on
  `catalog.Provide` (`expandWildcardWithCatalog`, unchanged).
- **Changing transformation semantics, markers, or relation-type derivation.**
  `__result__`, `__deletion__`, `*`, `determineRelationType`, and
  `combineTransformations` are algorithm-layer and unchanged. (`scope` gained a
  determinism fix only — see "Outcome"; its resolution rules are otherwise
  untouched.)
- **PostgreSQL-family dialects.** None exist in the engine enum; Redshift et al.
  are not registered engines.
- **Improving legacy expression detection.** `analyzeExpressionOperator`'s
  text/function-name scanning is preserved to guarantee transformation-content
  parity; upgrading it to structured `FuncCall`/`WindowDef`/`CaseExpr` analysis
  is **deferred and tracked**, not dropped — see **"Follow-ups" → PG-FU-1**.

## Current coupling (measured)

`backend/plugin/lineage/postgresql/analyzer.go`: **1763 lines, 73 functions**.

| Bucket | Functions | Lines | Work |
| --- | ---: | ---: | --- |
| Parser-coupled (signature or body references `pg.*Context` / `antlr`) | 47 | ~1315 (74%) | **Rewrite** |
| Parser-agnostic (scope/edge/transform/text helpers) | 26 | ~448 (25%) | **Keep** |
| `backend/plugin/lineage/scope/` | — | 156 | **Keep** |
| `backend/plugin/lineage/model/`, `catalog/` | — | — | **Keep** |

Coupling detail: 84 distinct `pg.*Context` types, 4 `.GetText()` calls.
(Contrast with MySQL's 58 concrete types / 18 interfaces / 80 accessors /
33 `.GetText()` calls — PostgreSQL's ANTLR grammar is context-typed rather than
accessor-heavy, so the rewrite is dominated by context unwrapping.)

**Keep unchanged (26 funcs):** `Analyze`, `NewAnalyzer` (drop the `tokens`
field), `mergeUnionOutputColumns`, `generateEdges`,
`generateEdgesForDataModification`, `generateEdgeFromSource`,
`traceThroughTableLineage`, `traceThroughTableLineageToTarget`,
`flattenTempSourceLineage`, `appendFlattenedLineage`, `inferColumnAlias`,
`findFunctionInExpression`, `isCaseExpression`, `containsArithmeticOperator`,
`pushScope`/`popScope`/`currentScope`, `markTempTable`, `isTempTable`,
`isTableTempInCurrentScope`, `addRelation`, `expandWildcardWithCatalog`,
`NewLineageEdge`, `determineRelationType`, `combineTransformations`,
`normalizeExpressionText`.

**Rewrite (47 funcs):** `AnalyzeRelations`, every `process*` statement/clause/
table/target function, the `extract*` readers, `getIdentifierText`,
`getParseTreeText`, and the expression entry points
(`extractColumnsFromExpr`, `visitExprForColumns`, `isExpressionDerived`,
`analyzeExpressionOperator`).

## Target architecture

### Package layout

```
backend/plugin/lineage/postgresql/          # NEW implementation (omni-backed)
    analyzer.go                             # rewritten traversal (same Analyze signature)
    expr.go                                 # omni expression helpers (text, column walk, detect*)
    analyze_test.go                         # same YAML suite via RunLineageYAMLTestSuites
    analyze_test_helper.go
    hardfail_test.go                        # parse-policy unit test
    benchmark_test.go                       # optional, mirrors mysql
    testdata/analyze/*.yaml                 # KEEP unchanged, 85 cases / 20 files (source of truth)
backend/plugin/lineage/postgresqlantlr/     # OLD implementation, moved verbatim (temporary)
    analyzer.go
    analyze_test.go / analyze_test_helper.go
backend/plugin/lineage/testutil/            # + temporary parity helper
```

Moving the old code to `postgresqlantlr` (rather than leaving it in
`postgresql`) keeps the public package path stable and makes the eventual
deletion a single directory removal. `postgresqlantlr` is a **development-only
parity oracle** and is never shipped once Phase 5 lands. The golden `testdata/`
stays under `postgresql/` and is read by both implementations during parity.

### Coexistence & cutover

`RegisterAnalyzeRelation` panics on a duplicate engine. So:

1. During Phases 0–4, the **new** `postgresql` package does **not** call
   `RegisterAnalyzeRelation`; `postgresqlantlr` registers `Engine_POSTGRES` and
   serves production. Temporarily, the blank imports that currently wire
   PostgreSQL lineage must point at the old package:
   - `backend/server/ultimate.go`
   - `backend/test/integration/runner/schemasync_lineage_postgres_service_test.go`

   Parity tests import both packages directly.
2. Phase 5 flips it: the new `postgresql` package registers
   **`Engine_POSTGRES`**; `postgresqlantlr` is deleted outright and the two
   blank imports revert to `lineage/postgresql`.

This is the same seam `plan/lineage_analyze_plan.md` designed
(`GetAnalyzeRelation` / `RegisterAnalyzeRelation`), so no runner, store, proto or
frontend change is required.

### Parity harness

Extend `backend/plugin/lineage/testutil/` with a comparison runner (this helper
was added for the MySQL migration and removed at its cutover; re-add it
temporarily):

```go
// Runs every YAML case through both analyzers and fails on any normalized
// difference in (source, target, relation_type, is_temp, transformation set).
func RunLineageParityFromYAMLDir(t *testing.T, dir string,
    oldFn, newFn AnalyzeFunc)
```

Normalization rules (must be explicit, because ordering is not contractual):

- sort edges by `(target db/schema/table/column, source db/schema/table/column)`;
- compare `IsTemp` and `RelationType` exactly;
- compare `Transformation` as a **set**, normalizing each to a comparable key over
  `(Operation, FunctionName, OpType, Expression, Condition, sorted Arguments,
  sorted GroupKeys, sorted PartitionBy, sorted OrderBy)`;
- treat `nil` and empty `[]model.Transformation` (and nil/empty slices inside a
  transformation) as equal;
- on mismatch, print the first N differing edges with both transformations.

The parity test lives in `postgresql/parity_test.go` (new package), importing
`postgresqlantlr` for the old function and reading the shared
`postgresql/testdata/analyze` directory. There is no import cycle: only the new
package's test file imports the old package.

### Parse-entry / parse-error policy

New `AnalyzeRelations` entry point:

```go
stmts, err := omnipg.Parse(a.sql)
if err != nil {
    // Hard failure. Wrap and return; AnalyzeRelations never emits a partial
    // result. The runner turns this into column_lineage_version.error_message
    // via its existing storeError path, so the failure is recorded and visible
    // rather than silently emptying the object's lineage.
    return nil, errors.Wrap(err, "failed to parse PostgreSQL SQL")
}
for _, s := range stmts {
    if s.Empty() {
        continue
    }
    switch stmt := s.AST.(type) {
    case *ast.SelectStmt:        a.processSelectStmt(stmt)
    case *ast.InsertStmt:        a.processInsertStmt(stmt)
    case *ast.UpdateStmt:        a.processUpdateStmt(stmt)
    case *ast.DeleteStmt:        a.processDeleteStmt(stmt)
    case *ast.ViewStmt:          a.processViewStmt(stmt)
    case *ast.CreateTableAsStmt: a.processCreateTableAsStmt(stmt)
    default:
        // Unsupported statement kinds produce no lineage, matching the legacy
        // analyzer which silently ignored them.
    }
}
if len(a.errors) > 0 {
    return nil, errors.Errorf("analysis errors: %s", strings.Join(a.errors, "; "))
}
return a.edges, nil
```

There is no ANTLR fallback and no best-effort mode. `postgresqlantlr` exists only
as a parity oracle and is deleted at Phase 5.

### Multi-statement policy

Legacy `processStmtBlock` iterated `Stmtmulti().AllStmt()` and processed **every**
statement on the same analyzer (shared root scope, accumulated edges), so
`SELECT a FROM t1; SELECT b FROM t2` yielded edges from both. The new analyzer
preserves that by looping over `[]pg.Statement`. This is the one intentional
difference from the MySQL migration, which rejects multi-statement input because
`omni/mysql/parser` splits `DELIMITER`/compound input where a partial analysis
would be misleading; PostgreSQL has no such construct, and `MANUAL_SQL` is a real
multi-statement surface, so rejecting it would regress lineage for existing
objects. A unit test pins the behavior (both statements contribute edges).

### Text extraction (replaces `.GetText()`)

```go
// exact source slice for a node span; Loc is absolute into the parsed sql
func (a *Analyzer) exprText(loc ast.Loc) string {
    if loc.Start < 0 || loc.End < 0 || loc.End > len(a.sql) || loc.Start >= loc.End {
        return ""
    }
    return strings.TrimSpace(a.sql[loc.Start:loc.End])
}

func (a *Analyzer) exprTextOf(n ast.Node) string { return a.exprText(ast.NodeLoc(n)) }
```

Use it everywhere `GetText()`/`getParseTreeText` was used. This matches legacy
`getParseTreeText`, which took `tokens.GetTextFromInterval(...)` and trimmed —
i.e. the source substring, internal whitespace preserved. (Note the deliberate
difference from the MySQL analyzer, whose `exprText` removes inter-token
whitespace to mirror ANTLR `GetText()`; PostgreSQL's legacy path preserved it.)
The `tokens *antlr.CommonTokenStream` field is dropped entirely — `sql` plus
`Loc` is sufficient, and `omni/pg/ast` exposes `NodeLoc` directly (no reflection
needed, unlike MySQL).

## Work breakdown

### Statement / clause mapping (illustrative; all 47 funcs follow this shape)

| Current (ANTLR) | New (omni `pg/ast`) |
| --- | --- |
| `parser.Root()→Stmtblock→Stmtmulti→Stmt` dispatch | `omnipg.Parse` → `[]pg.Statement`; type switch on `s.AST` |
| `Stmt.Selectstmt()` | `*ast.SelectStmt` |
| `Stmt.Insertstmt()` / `Insert_target` / `Insert_rest` / `Insert_column_list` | `InsertStmt{Relation *RangeVar, Cols []*ResTarget, SelectStmt}` |
| `Stmt.Updatestmt()` / `Relation_expr_opt_alias` / `Set_clause_list` | `UpdateStmt{Relation, TargetList []*ResTarget, FromClause, WhereClause}` |
| `Stmt.Deletestmt()` / `Using_clause` / `Where_or_current_clause` | `DeleteStmt{Relation, UsingClause, WhereClause}` |
| `Stmt.Viewstmt()` / `Opt_column_list` / `Columnlist` | `ViewStmt{View, Aliases, Query, Replace}` |
| `Stmt.Creatematviewstmt()` + `Stmt.Createasstmt()` | `CreateTableAsStmt{Query, Into *IntoClause, Objtype, IsSelectInto}` |
| `Select_no_parens` / `Select_clause` / `Select_with_parens` | `*ast.SelectStmt` (parens are collapsed by the parser) |
| `Simple_select_intersect` / `Simple_select_pramary` | `SelectStmt` leaf: `TargetList`, `FromClause`, `WhereClause`, `ValuesLists` |
| UNION/INTERSECT/EXCEPT (`AllSimple_select_intersect`) | `SelectStmt.Op` (`SetOperation`) + `Larg`/`Rarg` + `All` → `flattenSetOpArms` |
| `With_clause` / `Cte_list` / `Common_table_expr` | `WithClause.Ctes []*CommonTableExpr{Ctename, Aliascolnames, Ctequery}` |
| `From_clause` / `From_list` / `Table_ref` | `SelectStmt.FromClause` (`*List` of `*RangeVar`/`*JoinExpr`/`*RangeSubselect`) |
| `Relation_expr` + `Qualified_name` + `Opt_alias_clause` | `*ast.RangeVar{Catalogname, Schemaname, Relname, Alias}` |
| `Select_with_parens` as derived table | `*ast.RangeSubselect{Lateral, Subquery, Alias}` |
| `Joined_table` | `*ast.JoinExpr{Jointype, Larg, Rarg, UsingClause, Quals}` |
| `Opt_target_list` / `Target_list` / `Target_el` (`Target_star` / `Target_columnref` / `Target_label`) | `SelectStmt.TargetList` (`*ast.ResTarget{Name, Val, Loc}`); star = `*ColumnRef` whose last field is `*A_Star` |
| `Columnref` + `Indirection` (attr shift) | `*ast.ColumnRef.Fields` (`*String` / `*A_Star`) → `columnRefFromFields` |
| `Opt_on_conflict` / `Set_clause_list` | `InsertStmt.OnConflictClause.TargetList []*ResTarget` |

### Expression layer (the part that gets simpler)

| Current | New |
| --- | --- |
| `visitExprForColumns(node antlr.ParseTree)` — recursive `GetChild(i)` | `collectColumns(node ast.Node)` — type switch over `ColumnRef`, `A_Expr`, `FuncCall`, `CaseExpr`, `CoalesceExpr`, `TypeCast`, `SubLink`, `BoolExpr`, `NullTest`, `BooleanTest`, `RowExpr`, `ArrayExpr`, `A_ArrayExpr`, `A_Indirection`, `NamedArgExpr`, `CollateClause`, `MinMaxExpr`, `NullIfExpr`, `GroupingFunc`, … (~45 lines) |
| `extractColumnRef` (Colid + Indirection shift loop) | `columnRefFromFields(cr.Fields)`: last `*String` → `Column`, preceding `*String` → `Table`, `*A_Star` → `Column = "*"`; schema discarded exactly as today |
| `extractQualifiedName` (Colid + Indirection) | `RangeVar{Schemaname, Relname}` (3-part → drop `Catalogname`, matching today) |
| `extractTargetAlias` (Collabel / `GetText()`) | `ResTarget.Name` |
| `extractAlias` (`Table_alias_clause`) | `Alias.Aliasname` |
| `getIdentifierText` (strip surrounding quotes) | read `String.Str` / `RangeVar.Relname` / `ResTarget.Name` directly — omni already unquotes |
| `getParseTreeText(node)` | `exprTextOf(node)` / `exprText(node.Loc)` |
| `isExpressionDerived(expr pg.IA_exprContext)` | `isExpressionDerivedText(text)` (unchanged text rules) |
| `analyzeExpressionOperator(expr pg.IA_exprContext)` | `analyzeExpressionOperator(node ast.Node)` — same text scan, fed by `exprTextOf` |
| `findFunctionInExpression` / `isCaseExpression` / `containsArithmeticOperator` | unchanged |

Expression detection is intentionally **not** upgraded to structured
`FuncCall`/`WindowDef`/`CaseExpr` analysis in this migration; doing so would
change `Transformation` content (e.g. window `PartitionBy`/`OrderBy`) and break
the byte-identical parity goal. This deferral is deliberate and **recorded as a
tracked follow-up** — see **"Follow-ups" → PG-FU-1**; do not silently ship the
first cut as the final state.

To keep it from being forgotten in the code itself, the ported functions carry
an explicit marker (removed only when PG-FU-1 lands):

```go
// detectWindowFunction keeps the legacy text scan and passes nil partition/order.
// TODO(PG-FU-1): extract WindowDef.PartitionClause / .OrderClause from FuncCall.Over.
func (a *Analyzer) detectWindowFunction(...) { ... }
```

## Phases

### Phase 0 — Move + parity harness + spike (day one)

- Confirm omni is pinned at the recorded commit and `go mod tidy` is a no-op.
- Move the current implementation to `postgresqlantlr` unchanged; leave
  `postgresql` as a stub that does not register. Temporarily point the
  `ultimate.go` and PG integration-test blank imports at `postgresqlantlr`.
- Land `RunLineageParityFromYAMLDir` plus normalization in `testutil`.
- Implement `exprText`/`exprTextOf`/`columnRefFromFields`/`collectColumns` and
  port **one** family end-to-end: `SELECT` target list over a single table with
  alias/star (YAML `01`).
- **Exit:** parity test passes for `01_test_select`; `go build ./...` and
  `golangci-lint` clean; exactly one `Engine_POSTGRES` registration.

### Phase 1 — SELECT core

- FROM / JOIN (`JoinExpr`), derived tables (`RangeSubselect`), subqueries (in
  `TargetList`/`WhereClause` via `SubLink`), CTE (`WithClause`/`CommonTableExpr`),
  set operations (`SelectStmt.Op`), window functions, `SELECT *` and `table.*`,
  correlated subqueries.
- Wire `scope` + `catalog.Provide` exactly as today
  (`expandWildcardWithCatalog` unchanged).
- **Exit:** parity green on `01`–`05`, `10_window`, `11_catalog`, `18_lateral`,
  `19_distinct`; `12_relation_types`, `13_is_temp_table` unchanged behavior
  (relation types and temp filtering are produced by the algorithm layer, so
  they must match without edits).

### Phase 2 — DML

- `INSERT` / `INSERT … SELECT` (`generateEdgesForDataModification`),
  `INSERT … ON CONFLICT DO UPDATE` (`OnConflictClause.TargetList`),
  `INSERT … RETURNING` (parsed but not a lineage source), `UPDATE` with/without
  `FROM` (`UpdateStmt.TargetList`), `UPDATE … FROM` subquery, `DELETE` with
  `USING`/subquery and `__deletion__` edges.
- **Exit:** parity green on `06_insert`, `08_update`, `09_delete`,
  `15_on_conflict`.

### Phase 3 — DDL targets

- `CREATE VIEW` / `CREATE OR REPLACE VIEW` with explicit column lists
  (`ViewStmt.Aliases`), `CREATE TABLE … AS SELECT`, `CREATE TEMP TABLE … AS
  SELECT`, `CREATE MATERIALIZED VIEW` with explicit columns (`CreateTableAsStmt`
  + `Into`). Note: the runner wraps view bodies as
  `CREATE VIEW "<name>" AS <select>` (`buildSQL` in
  `backend/runner/lineageanalyzer/analyzer.go:409`), so both bare and wrapped
  forms must work; `SELECT … INTO` (`IsSelectInto`) should behave as a SELECT
  (`__result__` edges), preserving legacy behavior.
- **Exit:** parity green on `07_create_table`, `14_view`,
  `20_select_lineage_materialized_view`.

### Phase 4 — Hardening

- **Parse-failure path**: land `hardfail_test.go` asserting (a) unparseable SQL
  returns an error and no relations, and (b) the runner records
  `column_lineage_version.error_message` for it.
- **Multi-statement test**: assert every well-formed statement contributes edges
  (legacy parity), and that a parse error anywhere hard-fails the whole input.
- **Differential sweep**: feed every stored PostgreSQL view/matview definition in
  a real deployment (or the integration fixture schema) through both analyzers
  and diff their normalized edges. This is where corpus gaps the 85 YAML cases
  miss are found — especially the identifier-case-folding divergence.
- **Corpus top-up**: for any divergence, either fix the port or add/refresh the
  YAML expectation deliberately (never mask a difference).
- **Benchmarks**: port the MySQL `benchmark_test.go` shape (corpus-wide,
  per-case, parse-only vs analyze).
- **Exit:** zero unexplained divergences on the sweep; hard-fail and
  multi-statement tests green; `make test-integration` green.

### Phase 5 — Cutover

- New `postgresql` package registers `Engine_POSTGRES` only; `postgresqlantlr`
  is deleted (code + its registration).
- Revert the temporary blank imports in `backend/server/ultimate.go` and
  `backend/test/integration/runner/schemasync_lineage_postgres_service_test.go`
  to `lineage/postgresql`.
- Remove the temporary parity harness from `testutil`.
- `go mod tidy`: `github.com/bytebase/parser` becomes unused and is dropped.
  `github.com/antlr4-go/antlr/v4` stays as an indirect requirement of
  `google/cel-go` (via `backend/api/v1`), no longer a lineage dependency.
  `bytebase/omni` stays (MySQL family).
- **Carry the follow-ups forward.** Before this plan is trimmed/archived, move
  the **"Follow-ups"** table (at minimum **PG-FU-1**, the structured
  window/aggregate/CASE detection that this migration deliberately leaves on the
  legacy text scan) into the successor maintenance doc for
  `backend/plugin/lineage/postgresql` so it survives the plan's cleanup.
- **Exit:** full Go suite green; `golangci-lint run --allow-parallel-runners`
  clean; `make build-release` builds; the new analyzer's own YAML suite is the
  single source of truth; `bytebase/parser` is absent from `go.mod`; the
  follow-ups have an owner/tracker (not just this heading).

## Relevant files

Landed state (`Action` = what actually happened).

| Path | Action |
| --- | --- |
| `backend/plugin/lineage/postgresql/analyzer.go` | REWRITTEN on omni (was moved to `postgresqlantlr/` during Phases 0-4) |
| `backend/plugin/lineage/postgresql/expr.go` | ADDED (text/column/detection helpers) |
| `backend/plugin/lineage/postgresql/analyze_test.go`, `analyze_test_helper.go` | RECREATED against the new analyzer |
| `backend/plugin/lineage/postgresql/testdata/analyze/*.yaml` | KEEP + 1 new file → **90 cases / 21 files** |
| `backend/plugin/lineage/postgresql/hardfail_test.go`, `benchmark_test.go` | ADDED |
| `backend/plugin/lineage/postgresql/parity_test.go` | ADDED then DELETED at cutover |
| `backend/plugin/lineage/postgresqlantlr/` | ADDED (moved old impl) then DELETED at cutover |
| `backend/plugin/lineage/testutil/lineage_parity_helper.go` | ADDED then DELETED at cutover |
| `backend/plugin/lineage/scope/scope.go` | CHANGED: deterministic unqualified-column resolution (see "Outcome") |
| `backend/plugin/lineage/model/`, `catalog/` | UNCHANGED |
| `backend/plugin/lineage/lineage.go` | UNCHANGED (registration seam) |
| `backend/server/ultimate.go` | temporary import swap, reverted at cutover |
| `backend/test/integration/runner/schemasync_lineage_postgres_service_test.go` | temporary import swap, reverted at cutover |
| `backend/runner/lineageanalyzer/analyzer.go` | UNCHANGED (reference: `buildSQL`, wrapping) |
| `go.mod` / `go.sum` | `bytebase/parser` REMOVED; `antlr4-go` now indirect (via `cel-go`); `omni` unchanged |

## Verification

## Verification (results)

All steps below were actually run at cutover.

1. **Parity (primary):** `RunLineageParityFromYAMLDir` over the legacy-comparable
   cases — **zero normalized diffs**, except the 5 `knownLegacyDefects` cases that
   are the parenthesized-join fix. The harness was then removed.
2. **New analyzer's own YAML suite:** **90/90** cases green
   (`go test ./backend/plugin/lineage/postgresql/ -count=1`).
3. **Hermetic suite:** `go build ./...` and `go test ./... -count=1` green.
4. **Integration (real PostgreSQL + MySQL via testcontainers):**
   `TestPostgresSchemaSyncAndLineageRealServerIntegration` **PASS** — schema sync →
   lineage analysis end-to-end through the new analyzer.
5. **Lint/build:** `golangci-lint run --allow-parallel-runners` → **0 issues**;
   `gofmt -l` clean.
6. **Differential sweep (Phase 4):** 31 real `pg_get_viewdef` definitions from a
   live PostgreSQL → 25 exact matches, 6 divergences all explained by the
   parenthesized-join defect (legacy emitted 0 edges; new emits the correct set).
7. **Malformed SQL:** `TestHardFailOnUnparseableSQL` green (error + no relations).
8. **Multi-statement:** `TestMultiStatementAnalyzed` and
   `TestParseErrorFailsWholeInput` green.
9. **Engine scope / registration:** `Engine_POSTGRES` resolves to the omni
   analyzer; `backend/server/ultimate.go` and the integration test import the same
   package, so registration happens exactly once.
10. **Benchmarks:** `BenchmarkAnalyzeCorpus`, `BenchmarkAnalyzeCase`,
    `BenchmarkParseCase`, `BenchmarkAnalyzeShapes`,
    `BenchmarkAnalyzeWildcardWithCatalog` all run green.
11. **Dependency removal:** `github.com/bytebase/parser` is absent from `go.mod`
    and no `backend/**/*.go` imports `antlr4-go` or `bytebase/parser`;
    `antlr4-go` remains listed as an indirect requirement of `google/cel-go`.
12. **Not run here:** the manual before/after `column_lineage` diff against a
    deployed instance (needs a deployment). The in-repo sweep in step 6 covers the
    same ground using real `pg_get_viewdef` output.

## Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- |
| Rewrite regresses an edge case the YAML corpus does not cover | Medium | High | Differential sweep over real stored view/matview definitions (Phase 4); keep cutover behind the registry seam until parity + sweep are clean |
| Identifier case folding changes edges (omni lowercases unquoted identifiers) | Medium | Low–Medium | Accepted as more correct (matches PostgreSQL storage; `pg_views.definition` is already folded). Covered by the sweep; documented in "Recorded divergences" |
| Hard failure drops lineage for SQL ANTLR previously recovered from (notably `MANUAL_SQL`) | High | Medium | Accepted by decision; the error is recorded in `column_lineage_version.error_message` and the object is skipped rather than silently wrong. Covered by a unit test; watch the error rate after cutover |
| Multi-statement behavior diverges from the MySQL migration | Medium | Medium | Explicit decision with legacy-parity rationale; unit test; documented |
| Window transformation content changes if structured `Over` is used | Low | Low | Keep `nil, nil` partition/order in the first cut; structured extraction is explicitly deferred to **PG-FU-1** (tracked in "Follow-ups", marker in code) |
| Coexistence wiring breaks registration (duplicate panic or missing engine) | Medium | Medium | Follow the MySQL precedent exactly; assert exactly one registration; integration test covers the real path |
| `Loc` assumptions wrong for a node kind (e.g. missing `Loc`) | Low | Medium | `exprText` guards `Start/End < 0` and falls back to empty; probe confirmed coverage for all corpus shapes; sweep catches the rest |
| Effort overrun (mechanical volume) | Medium | Medium | Phase-level exit criteria on small YAML subsets; each phase is independently mergeable |

## Open questions

1. **`SELECT … INTO`**: confirm treating `CreateTableAsStmt{IsSelectInto:true}`
   as a SELECT (`__result__` edges) matches legacy for `MANUAL_SQL`. Resolve
   during Phase 3 with a corpus case; it is an implementation detail, not a
   scope question.
2. **Upstream contribution**: should the DML/set-op/window-preserving walker be
   fed back to omni (e.g. a production `analysis` package for PostgreSQL), rather
   than keeping the walker in-repo? Long-term this could shrink our code, as
   raised in the MySQL plan. Tracked as PG-FU-2 below.

Resolved during plan review (kept here so the decision is not re-litigated):

- **Multi-statement `MANUAL_SQL`** — process every parsed statement (legacy
  parity), *confirmed with the maintainer*. Pinned by a unit test in Phase 4.
- **Window / aggregate / CASE detection** — keep the legacy text scan for this
  migration; a *confirmed, tracked* upgrade is PG-FU-1.

## Follow-ups

Deferred work that this plan explicitly hands off. These are **not** optional
nice-to-haves to be forgotten after cutover: PG-FU-1 is the reason the migration
keeps `nil, nil` window clauses, and it must remain visible in this document (or
in the successor maintenance doc) until it is either implemented or explicitly
dropped by a later decision.

| ID | Follow-up | Why it is deferred | Where it lands |
| --- | --- | --- | --- |
| **PG-FU-1** | Replace text/function-name scanning in `analyzeExpressionOperator` / `isExpressionDerived` with structured AST detection: `FuncCall{Name, Args, AggDistinct, AggStar, Over}` (incl. window `WindowDef.PartitionClause`/`OrderClause`), `*ast.CaseExpr`, `*ast.CoalesceExpr`, `A_Expr{Kind, Name}` for operators. | It deliberately **changes `Transformation` content** (window `PartitionBy`/`OrderBy` become non-nil; function names/args become structured), which would break the byte-identical parity goal this migration is built on. The parity harness is removed at Phase 5, so this must be a separate change validated against the golden corpus + a sweep. | **LANDED.** `plan/postgresql_expression_transformation_plan.md` (implemented; marker `TODO(PG-FU-1)` removed). |
| **PG-FU-2** | Feed the DML/set-op/window-preserving walker back upstream to `github.com/bytebase/omni` as a production PostgreSQL `analysis` package, shrinking the in-repo walker. | Out of scope for a parity migration; depends on upstream appetite (raised in `plan/mysql_omni_parser_migration_plan.md` as well). | Upstream contribution / separate plan. |
| **PG-FU-3** | If the Phase 4 sweep shows any real mixed-case-unquoted `MANUAL_SQL` divergence from omni's case folding, decide whether to document-only or add a normalization layer. | Expected to be a non-issue for synced views/matviews (definitions come from `pg_get_viewdef`), which is why it is accepted rather than fixed now. | Revisit only if the Phase 4 sweep finds production impact. |

## Appendix A — Probe methodology (how the numbers were produced)

- **Corpus sweep:** a throwaway Go program decoded each
  `testdata/analyze/*.yaml` with `gopkg.in/yaml.v3`, passed every `sql:` to
  `pg.Parse`, and classified the result (`ok` / parse error / multi-statement)
  and the returned `AST` type. Result: 85 ok, 0 failed.
- **AST shape probe:** a throwaway program parsed ~15 representative statements
  and printed `ResTarget`/`RangeVar`/`JoinExpr`/`ColumnRef`/`FuncCall`/
  `CaseExpr`/`InsertStmt`/`UpdateStmt`/`DeleteStmt`/`ViewStmt`/
  `CreateTableAsStmt` fields plus `sql[loc.Start:loc.End]` slices, confirming
  field names and `Loc` coverage/absolute offsets.
- **Case-folding / quoting probe:** parsed mixed-case and quoted identifiers and
  read the resulting `String.Str` / `RangeVar` values.
- **Coupling measurement:** the file was split by `^func ` and a function was
  classified parser-coupled if its signature or body matched `\bpg\.|\bantlr\.`;
  `grep -o` produced the context-type inventory and `.GetText()` count.

## Appendix B — omni PostgreSQL AST quick reference

```go
// github.com/bytebase/omni/pg
func Parse(sql string) ([]Statement, error)
type Statement struct {
    Text      string // includes trailing semicolon if present
    AST       ast.Node
    ByteStart int; ByteEnd int       // absolute byte offsets
    Start     Position; End Position // 1-based line:column
}
func (s *Statement) Empty() bool

// github.com/bytebase/omni/pg/ast  (all nodes carry Loc{Start,End}, -1 if unknown)
func NodeLoc(n Node) Loc
func ListSpan(l *List) Loc
type List struct { Items []Node }   // String, Integer, Float, Boolean, ...
type String struct { Str string }   // identifier, already unquoted

type SelectStmt struct { DistinctClause, IntoClause, TargetList, FromClause *List
    WhereClause, HavingClause Node; GroupClause, WindowClause, SortClause *List
    ValuesLists *List; LimitOffset, LimitCount Node
    WithClause *WithClause
    Op SetOperation; All bool; Larg, Rarg *SelectStmt
    Loc Loc }
type InsertStmt struct { Relation *RangeVar; Cols, ReturningList *List
    SelectStmt Node; OnConflictClause *OnConflictClause; WithClause *WithClause; Loc Loc }
type UpdateStmt struct { Relation *RangeVar; TargetList *List; WhereClause Node
    FromClause *List; ReturningList *List; WithClause *WithClause; Loc Loc }
type DeleteStmt struct { Relation *RangeVar; UsingClause *List; WhereClause Node
    ReturningList *List; WithClause *WithClause; Loc Loc }
type ViewStmt struct { View *RangeVar; Aliases *List; Query Node; Replace bool; Loc Loc }
type CreateTableAsStmt struct { Query Node; Into *IntoClause; Objtype ObjectType
    IsSelectInto bool; IfNotExists bool; Loc Loc }
type IntoClause struct { Rel *RangeVar; ColNames *List; ViewQuery Node; SkipData bool; Loc Loc }
type RangeVar struct { Catalogname, Schemaname, Relname string; Alias *Alias; Loc Loc }
type Alias struct { Aliasname string; Colnames *List; Loc Loc }
type ColumnRef struct { Fields *List; Loc Loc }        // *String and/or *A_Star
type ResTarget struct { Name string; Indirection *List; Val Node; Loc Loc }
type A_Expr struct { Kind A_Expr_Kind; Name *List; Lexpr, Rexpr Node; Loc Loc }
type FuncCall struct { Funcname, Args *List; AggOrder *List; AggFilter, Over Node
    AggWithinGroup, AggStar, AggDistinct, FuncVariadic bool; Loc Loc }
type WindowDef struct { Name, Refname string; PartitionClause, OrderClause *List
    FrameOptions int; StartOffset, EndOffset Node; Loc Loc }
type CaseExpr / CoalesceExpr / MinMaxExpr / NullIfExpr / TypeCast / SubLink /
     BoolExpr / NullTest / BooleanTest / RowExpr / ArrayExpr / A_ArrayExpr /
     A_Indirection / NamedArgExpr / CollateClause / A_Star      // all with Loc
type WithClause struct { Ctes *List; Recursive bool; Loc Loc }
type CommonTableExpr struct { Ctename string; Aliascolnames *List
    Ctematerialized int; Ctequery Node; Loc Loc }
type JoinExpr struct { Jointype JoinType; IsNatural bool; Larg, Rarg Node
    UsingClause *List; JoinUsing, Alias *Alias; Quals Node; Loc Loc }
type RangeSubselect struct { Lateral bool; Subquery Node; Alias *Alias; Loc Loc }
type OnConflictClause struct { Action int; Infer *InferClause; TargetList *List
    WhereClause Node; Loc Loc }
```

## Appendix C — Recorded divergences from the legacy analyzer

### Defects fixed (intentional, not parity)

1. **Parenthesized join trees returned zero edges.** `pg_get_viewdef` renders a
   view that joins tables as `FROM (a JOIN b ON ...)`. The legacy analyzer
   silently returned no lineage for that shape, so every stored PostgreSQL view
   with a join had empty column lineage. The omni analyzer resolves it.
   - Evidence: Phase 4 differential sweep over 31 real definitions in a live
     PostgreSQL — 6 views (`v_join`, `v_left`, `v_natural`, `v_cross`,
     `v_lateral`, `v_tablestar`) returned `legacy=0 / new=4..16` edges; all 25
     other definitions matched exactly.
   - Pinned by `testdata/analyze/21_test_parenthesized_join_lineage_table.yaml`
     (5 cases); while the parity harness existed these were listed in
     `knownLegacyDefects`, and the legacy package skipped them in its own suite.
2. **Ambiguous unqualified columns resolved nondeterministically.**
   `scope.ResolveColumn` ranged a `map[string]*TableRef`, so a column present in
   more than one FROM relation (e.g. `NATURAL JOIN`) picked an arbitrary table,
   making the emitted edge unstable run to run. The resolver now iterates sorted
   keys. This is shared code, so MySQL-family analyzers changed with it and their
   corpora stayed green.

### Parity-preserving divergences

3. **Unquoted identifiers fold to lowercase.** omni applies PostgreSQL's
   case-folding (`MyTable` → `mytable`); legacy ANTLR preserved source case. The
   corpus exercises this directly through the `ON CONFLICT` special relation
   `EXCLUDED` → `excluded`, so the parity comparison was made identifier
   case-insensitive. Production impact is limited to `MANUAL_SQL` with
   mixed-case unquoted identifiers, because synced view/matview bodies come from
   `pg_get_viewdef` and are already folded.
4. **Multi-statement contract.** New: every statement is analyzed (legacy
   parity). This deliberately differs from the MySQL analyzer's
   single-statement rejection.
5. **Window transformation content.** `PARTITION BY`/`ORDER BY` remain `nil`,
   exactly as legacy emitted; the omni AST could supply them, but that is
   PG-FU-1 rather than part of a parity migration.
6. **CTAS / matview / `SELECT INTO` unify** under `CreateTableAsStmt`; the
   analyzer merges the legacy `processCreateAsStmt` and
   `processCreateMatviewStmt` and special-cases `IsSelectInto`.
7. **Parse errors now hard-fail.** Legacy removed ANTLR error listeners and
   recovered silently; `a.errors` was never appended to anywhere in
   `analyzer.go`, so the "analysis errors" branch was dead code. A statement omni
   cannot parse now yields an explicit error recorded in
   `column_lineage_version.error_message`.
8. **Identifier unquoting is structural.** Legacy stripped surrounding double
   quotes from `GetText()`; omni stores the unquoted value. Net behavior is the
   same, but the quote-stripping helper disappears.
