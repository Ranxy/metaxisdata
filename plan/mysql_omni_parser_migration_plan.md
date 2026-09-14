# Plan: Migrate the MySQL Lineage Analyzer from `bytebase/parser` (ANTLR) to `bytebase/omni`

> **Status: implemented (cutover landed).** The omni-backed analyzer replaced the
> ANTLR one, `mysqlantlr` was deleted, and `Engine_MYSQL` now resolves to the omni
> implementation. See "Outcome" below for what shipped, the dependency impact, and
> the divergences recorded against the legacy analyzer.
>
> This plan is deliberately scoped to **`storepb.Engine_MYSQL` only**. The other
> MySQL-family dialects (MariaDB, TiDB, OceanBase) and PostgreSQL are explicitly
> out of scope and will be handled by separate plans — see "Out of scope".

## TL;DR

Replace the ANTLR-based parser under `backend/plugin/lineage/mysql/analyzer.go`
with `github.com/bytebase/omni/mysql/parser` + `.../mysql/ast`, **keeping our own
lineage algorithms** (scope resolution, edge generation, temp-table flattening,
`model.Transformation` semantics, special markers).

This is **not** an import swap. The two parsers expose unrelated ASTs:

- today: ANTLR parse tree — 58 concrete `mysql.*Context` types, 18 context
  interfaces, 80 accessor methods, 33 `.GetText()` calls;
- omni: typed struct AST (`*ast.SelectStmt`, `*ast.JoinClause`, …) with
  byte-offset `Loc` for every node.

**53 of 83 functions (~1709 of ~2331 lines, 73%) are parser-coupled and must be
rewritten; 30 functions (~602 lines, 26%) plus the whole `scope`/`model` layer
port unchanged.** The rewrite is mechanical for statement/clause traversal and
*simplifying* for the expression layer.

Why this route over adopting omni's `catalog.Query` IR (the other candidate):
omni's **parser AST keeps what its catalog IR drops** — DML statements (INSERT /
REPLACE / UPDATE / DELETE / LOAD DATA / CTAS) and `FuncCallExpr.Over`
(window `PARTITION BY` / `ORDER BY`). It therefore preserves our current
coverage instead of regressing it.

**Engine scope (decided):** the new analyzer registers for
`storepb.Engine_MYSQL` **only**. MariaDB, TiDB and OceanBase are *not* pointed at
`omni/mysql` — their SQL is migrated later against their own parsers — so after
cutover those engines have no lineage analyzer until a follow-up plan lands. The
runner already degrades gracefully: `GetAnalyzeRelation` returns
`ErrorEngineNotSupported`, and `analyzeObject` records a deliberate skip with
`engine X has no lineage analyzer; analysis skipped`
(`backend/runner/lineageanalyzer/analyzer.go:276`), so the hourly scan stops
re-queueing those objects instead of erroring.

**Parse policy (decided): hard failure.** There is no ANTLR fallback and no
best-effort parse. `omni/mysql/parser.Parse` is strict/all-or-nothing; a
statement it cannot parse surfaces as an analysis error recorded in
`column_lineage_version.error_message`. `mysqlantlr` is deleted at cutover.

## Outcome

Landed:

- `backend/plugin/lineage/mysql/analyzer.go` rewritten on
  `github.com/bytebase/omni/mysql/parser` + `.../mysql/ast`, pinned at
  `v0.0.0-20260912023254-4574e69bb9f1` (the newest version available offline in
  the module cache at implementation time).
- `backend/plugin/lineage/mysqlantlr/` (the ANTLR implementation) deleted, along
  with the migration-only `parity_test.go`, `sweep_test.go` and the
  `testutil/parity.go` harness. The
  `RegisterAnalyzeRelation` seam now registers **`Engine_MYSQL` only**.
- `backend/server/ultimate.go` and the MySQL lineage integration test import
  `plugin/lineage/mysql` again (unchanged path, new implementation).
- Corpus grew from 51 to **73 golden cases**: 22 extended forms (window
  aggregate-OVER, three-arm UNION, CTE chain, derived-table join, scalar
  subquery, table `*`, comma join, `ON DUPLICATE KEY UPDATE`, multi-table
  UPDATE/DELETE, `LOAD DATA` with columns and with `SET`) captured from the
  parity-proven analyzer and committed as
  `testdata/analyze/16_test_extended_forms_lineage_table.yaml`.
- Hard-fail parse policy pinned by `hardfail_test.go`: unparseable SQL and
  multi-statement input return an error and never a partial result.

Dependency impact (forced by omni's own `go.mod` requirements, not chosen):

- `pgx/v5` 5.7.6 → 5.9.1, `go-mssqldb` 1.7.1 → 1.9.8,
  `testcontainers-go` 0.39.0 → 0.41.0, `x/crypto` 0.42.0 → 0.48.0,
  `x/net` 0.44.0 → 0.51.0, `x/text` 0.29.0 → 0.34.0, `grpc` 1.76.0 → 1.79.1,
  `grpc-gateway/v2` 2.27.2 → 2.28.0, and related genproto bumps. MVS selects
  these because omni lists them; they cannot be pinned lower while depending on
  the module. `github.com/hjson/hjson-go` was *not* added (unused by the imported
  packages).

Recorded divergences from the legacy analyzer:

1. **`WITH ... DELETE ...` loses the CTE.** omni's `DeleteStmt` has no CTE field,
   so the `WITH` clause is dropped at parse time and the legacy trace-through-CTE
   behavior cannot be reproduced. Affects `MANUAL_SQL` only; view bodies are
   always SELECT.
2. **Unqualified columns ambiguous across FROM relations resolve
   nondeterministically.** The shared `scope` package iterates a map, so
   `... JOIN ... USING (id)` plus an unqualified shared column name can pick
   either relation. This is a pre-existing bug in `scope`, identical in both
   implementations, and out of scope for the parser migration.
3. Transformation *content* may differ where the omni AST is more precise than
   the legacy ANTLR tree (notably window functions: legacy emitted `PROJECT`
   because its function detector missed the window grammar context; omni emits
   `WINDOW`). Edge identity — including `relation_type`, which both derive from
   transformation *presence* — is unchanged and was parity-verified.

## Why do this at all

1. `github.com/antlr4-go/antlr/v4` and `github.com/bytebase/parser` are used by
   **only two files** in the whole backend:
   `backend/plugin/lineage/mysql/analyzer.go` and
   `backend/plugin/lineage/postgresql/analyzer.go`. The dependency is fully
   isolated to lineage.
2. omni is Bytebase's intended successor to `bytebase/parser`: pure-Go,
   hand-written, no runtime codegen, actively maintained upstream. Its
   `mysql/parser` + `mysql/ast` packages import only stdlib +
   `bytebase/omni/...` (no transitive deps), so taking them is cheap.
3. omni's AST is better suited to lineage than our current parse-tree walking:
   `Loc{Start,End}` gives exact source spans for expression text,
   `FuncCallExpr.Over` gives structured window info, and `ColumnRef` /
   `ResTarget` / `Assignment` are already flat.

## Decisions

### Settled

- **Parser-only migration.** Keep `scope`, `model`, and the lineage algorithm
  layer. Do **not** adopt `catalog.AnalyzeSelectStmt` / `catalog.Query`; that
  route loses DML and window info and still requires writing a walker.
- **Coexist, then cut over.** The existing ANTLR analyzer stays in the tree and
  keeps serving production until parity is proven. `RegisterAnalyzeRelation`
  panics on duplicate registration, so the two implementations must not both
  register; selection is explicit (see "Coexistence & cutover").
- **Parity is proven on the existing golden corpus.** All **51 cases across 15
  YAML files** under `backend/plugin/lineage/mysql/testdata/analyze/` must
  produce byte-identical normalized edge sets before cutover.
- **MySQL engine only.** The new analyzer's `init()` registers
  `storepb.Engine_MYSQL` and nothing else. `omni/mysql` is never pointed at
  MariaDB/TiDB/OceanBase SQL; those dialects get their own migration with
  `omni/mariadb` / `omni/tidb` later.
- **Hard failure on unparseable SQL; no ANTLR fallback.** The old ANTLR analyzer
  is a parity oracle during development only, then deleted. A parse error is an
  analysis error, not a silent partial result.
- **Pin omni to a commit.** Add `github.com/bytebase/omni` at a recorded commit
  hash; do not track `main`.

### Out of scope (separate plans)

- **MariaDB, TiDB, OceanBase.** They keep sharing the current ANTLR analyzer
  only until this plan's cutover; after that they have no analyzer until their
  own migrations land. This is intentional: pointing `omni/mysql` at their SQL
  is exactly the parse-divergence risk we are avoiding.
- **PostgreSQL.** Untouched. Consequence: `bytebase/parser` and `antlr4-go`
  remain in `go.mod` for the PostgreSQL analyzer (they are used by only two
  files today, so this plan removes one of the two).

## Current coupling (measured)

`backend/plugin/lineage/mysql/analyzer.go`: **2331 lines, 83 functions**.

| Bucket | Functions | Lines | Work |
| --- | ---: | ---: | --- |
| Parser-coupled (signature or body uses `mysql.*Context` / `antlr.ParseTree`) | 53 | ~1709 (73%) | **Rewrite** |
| Parser-agnostic (scope/edge/transform/identifier helpers) | 30 | ~602 (26%) | **Keep** |
| `backend/plugin/lineage/scope/` | — | 156 | **Keep** |
| `backend/plugin/lineage/model/` | — | — | **Keep** |

Coupling detail: 58 concrete context types, 18 context interfaces, 80 distinct
accessor methods, 33 `.GetText()` calls, 51 functions taking a context parameter,
12 `All*()` list accessors.

**Keep unchanged (30 funcs):** `NewAnalyzer`, temp-table tracking
(`markTempTable`, `isTempTable`, `isTableTempInCurrentScope`),
`traceThroughTableLineage*`, `appendFlattenedLineage`,
`flattenTempSourceLineage`, `generateEdges`, `generateEdgesForDataModification`,
`addRelation`, `pushScope`/`popScope`/`currentScope`, `combineTransformations`,
`createFunctionOperatorInfo` / `createAggregateOperatorInfo` /
`createOperatorExprInfo` / `createCaseOperatorInfo` /
`createWindowOperatorInfo`, `normalizeExpressionText`, `normalizeIdentifier`,
`splitQualifiedIdentifier`, `parseQualifiedIdentifier`,
`expandWildcardWithCatalog`, `Analyze`.

**Rewrite (53 funcs):** all `process*` statement/clause/table functions, the
`get*`/`extract*`/`inferColumnAlias` readers, the parse entry in
`AnalyzeRelations`, and the expression layer (`extractColumnsFromExpression`,
`visitExprForColumns`, `extractColumnRef`, `isExpressionDerived`,
`analyzeExpressionOperator`, `detectFunctionCall`, `detectAggregateFunction`,
`detectWindowFunction`, `detectCaseExpression`, `detectOperatorExpression`,
`containsFunctionContext`, `extractWindowClauses`).

## Target architecture

### Package layout

```
backend/plugin/lineage/mysql/          # NEW implementation (omni-backed)
    analyzer.go                        # rewritten traversal (same Analyze signature)
    expr.go                            # omni expression helpers (text, column walk, detect*)
    analyze_test.go                    # same YAML suite via RunLineageYAMLTestSuites
backend/plugin/lineage/mysqlantlr/     # OLD implementation, moved verbatim (temporary)
    analyzer.go
    analyze_test.go / analyze_test_helper.go / testdata/
backend/plugin/lineage/testutil/       # + parity helper (compare two AnalyzeFunc)
```

Moving the old code to `mysqlantlr` (rather than leaving it in `mysql`) keeps the
public package path stable and makes the eventual deletion a single directory
removal. `mysqlantlr` is a **development-only parity oracle** — it is never
shipped once Phase 5 lands. The golden `testdata/` is shared: the parity test
reads it while both implementations exist, and it stays as the new `mysql`
package's own suite after cutover.

### Coexistence & cutover

`RegisterAnalyzeRelation` panics on a duplicate engine. So:

1. During Phases 0–4, the **new** `mysql` package does **not** call
   `RegisterAnalyzeRelation`; the old `mysqlantlr` package keeps registering and
   serving production. Both are reachable directly for parity tests.
2. Phase 5 flips it: the new `mysql` package registers **`Engine_MYSQL` only**;
   `mysqlantlr` is deleted outright. `TIDB`/`MARIADB`/`OCEANBASE` then resolve
   to `ErrorEngineNotSupported` and are skipped by the runner (see TL;DR) until
   their own plans land.

This is the same seam `plan/lineage_analyze_plan.md` designed
(`GetAnalyzeRelation` / `RegisterAnalyzeRelation`), so no runner, store, proto or
frontend change is required.

### Parity harness

Extend `backend/plugin/lineage/testutil/` with a comparison runner:

```go
// Runs every YAML case through both analyzers and fails on any normalized
// difference in (source, target, relation_type, is_temp, transformation set).
func RunLineageParityFromYAMLDir(t *testing.T, dir string,
    oldFn, newFn AnalyzeFunc)
```

Normalization rules (must be explicit, because ordering is not contractual):

- sort edges by `(target table, target column, source table, source column)`;
- compare `Transformation` as a set (operation + function/op/condition + sorted
  args/keys), not by slice order;
- treat `nil` and empty `[]Transformation` as equal;
- compare `IsTemp` and `RelationType` exactly.

### Parse-error policy (hard failure — decided)

New `AnalyzeRelations` entry point:

```go
list, err := mysqlparser.Parse(sql)
if err != nil {
    // Hard failure. Wrap and return; AnalyzeRelations never emits a partial
    // result. The runner turns this into column_lineage_version.error_message
    // via its existing storeError path, so the failure is recorded and visible
    // rather than silently emptying the object's lineage.
    return nil, fmt.Errorf("parse MySQL SQL: %w", err)
}
if list.Len() != 1 {
    // Parse() splits DELIMITER/compound input; AnalyzeRelations is defined for
    // exactly one statement. Reject multi-statement input explicitly.
    return nil, fmt.Errorf("expected exactly 1 statement, got %d", list.Len())
}
```

There is no ANTLR fallback and no best-effort mode. `mysqlantlr` exists only as a
parity oracle and is deleted at Phase 5. Behavior change to accept: SQL that ANTLR
recovered from (and that we previously analyzed partially and silently) now
yields an explicit error and no lineage for that object.

### Text extraction (replaces `.GetText()`)

```go
// exact source slice for a node span; omni's Loc is absolute into the parsed sql
func exprText(sql string, loc ast.Loc) string
```

Use it everywhere `GetText()` was used. This is strictly more accurate than
ANTLR's token concatenation. `mysql/deparse.Deparse` is the fallback when a
normalized rendering is wanted.

## Work breakdown

### Statement / clause mapping (illustrative; all 53 funcs follow this shape)

| Current (ANTLR) | New (omni `mysql/ast`) |
| --- | --- |
| `parser.Query().SimpleStatement()` dispatch | `list.Items[0]` type switch on `*ast.SelectStmt` / `*ast.InsertStmt` / `*ast.UpdateStmt` / `*ast.DeleteStmt` / `*ast.LoadDataStmt` / `*ast.CreateTableStmt` / `*ast.CreateViewStmt` |
| `SelectStatement → QueryExpression → QueryExpressionBody → QueryPrimary → QuerySpecification` | `*ast.SelectStmt` (+ `SetOp` / `Left` / `Right` / `SetAll`, `ParenSource`, `TableSource`, `ValuesSource`) |
| `WithClause().AllCommonTableExpression()` | `SelectStmt.CTEs []*ast.CommonTableExpr` |
| `FromClause().TableReferenceList()` | `SelectStmt.From []ast.TableExpr` (`*ast.TableRef` / `*ast.JoinClause`) |
| `TableAlias().Identifier()` | `TableRef.Alias`, `TableRef.Schema`, `TableRef.Name` |
| `ColumnRef().FieldIdentifier()` | `ast.ColumnRef{Table, Schema, Column, Star}` |
| `SelectItemList().AllSelectItem()` / `MULT_OPERATOR()` | `SelectStmt.TargetList []ast.ExprNode` (`*ast.ResTarget`, `*ast.StarExpr`) |
| `InsertQueryExpression().Fields()` | `InsertStmt.Columns []*ast.ColumnRef` |
| `InsertUpdateList().UpdateList()` | `InsertStmt.OnDuplicateKey []*ast.Assignment` |
| `UpdateList().AllUpdateElement()` | `UpdateStmt.SetList []*ast.Assignment` |
| `DuplicateAsQueryExpression()` | `CreateTableStmt.Select *ast.SelectStmt` |
| `ViewTail().ViewSelect()` + `ColumnInternalRefList()` | `CreateViewStmt.Select` + `CreateViewStmt.Columns []string` |
| `TableAliasRefList()` / `DeleteStatement` | `DeleteStmt.Tables` / `DeleteStmt.Using` |
| `LoadDataFileTail().LoadDataFileTargetList()` | `LoadDataStmt` fields |
| `ReplaceStatement` | `InsertStmt.IsReplace == true` (merge `processReplaceStatement` into `processInsertStatement` or keep a thin splitter) |
| `Subquery()/QueryExpressionParens()/DerivedTable()` | `*ast.SelectStmt` reached as `TableExpr` / expression |

### Expression layer (the part that gets simpler)

| Current | New |
| --- | --- |
| `visitExprForColumns(node antlr.ParseTree)` — recursive `GetChild(i)` | `collectColumns(expr ast.ExprNode)` — type switch over `ColumnRef`, `BinaryExpr`, `UnaryExpr`, `FuncCallExpr`, `CaseExpr`, `BetweenExpr`, `InExpr`, `CastExpr`, `SubqueryExpr`, `RowExpr`, … (~40 lines) |
| `containsFunctionContext` — matches `SimpleExprFunctionContext` / `SimpleExprRuntimeFunctionContext` / `SimpleExprSumContext` / `SumExprContext` + `AVG_SYMBOL`… | read `*ast.FuncCallExpr{Name, Args, Star, Distinct, Over}`; `IF`/`COALESCE` are ordinary `FuncCallExpr` |
| `detectAggregateFunction` (name map) | unchanged, fed by `FuncCallExpr.Name` |
| `detectOperatorExpression` (string matching) | unchanged, fed by `exprText` (optionally upgrade to `BinaryExpr.Op`) |
| `detectCaseExpression` (string matching) | unchanged (optionally `*ast.CaseExpr`) |
| `extractWindowClauses` — text hack, looks for `"PARTITIONBY"` | `FuncCallExpr.Over.PartitionBy` / `.OrderBy` — **fixes a latent bug** |
| `expr.GetText()` | `exprText(sql, node.Loc)` |

## Phases

### Phase 0 — Dependency + spike (parity harness on day one)

- Add `github.com/bytebase/omni` pinned to a commit; `go mod tidy`.
- Move the current implementation to `mysqlantlr` unchanged; leave `mysql` as a
  stub that does not register.
- Land `RunLineageParityFromYAMLDir` plus normalization in `testutil`.
- Implement `exprText` + `collectColumns` and port **one** family end-to-end:
  `SELECT` target list over a single table with alias/star (YAML `01`).
- **Exit:** parity test passes for `01_test_select`; `go build ./...` and
  `golangci-lint` clean; omni pinned and recorded.

### Phase 1 — SELECT core

- FROM / JOIN (`JoinClause`), derived tables, subqueries, CTE
  (`processCTE`, `processWithClause`), set operations (`processUnionQueries`,
  `mergeUnionOutputColumns`), window functions, `SELECT *`, correlated
  subqueries.
- Wire `scope` + `catalog.Provide` exactly as today (`expandWildcardWithCatalog`
  unchanged).
- **Exit:** parity green on `01`–`05`, `12_window`; `14_relation_types`,
  `15_is_temp_table` unchanged behavior (relation types and temp filtering are
  produced by the algorithm layer, so they must match without edits).

### Phase 2 — DML

- `INSERT` / `INSERT … SELECT` (`generateEdgesForDataModification`),
  `INSERT … ON DUPLICATE KEY UPDATE` (`processInsertUpdateList`),
  `REPLACE` (`IsReplace`), `UPDATE` (`UpdateStmt.SetList`),
  `DELETE` single + multi-table (`DeleteStmt.Tables`/`Using`, `__deletion__`
  edges), `LOAD DATA` (`__file__` edges, `processLoadDataSetClause`).
- **Exit:** parity green on `06_insert`, `09_update`, `10_delete`,
  `11_replace`, plus LOAD DATA cases if present in the corpus.

### Phase 3 — DDL targets

- `CREATE TABLE … AS SELECT` (`CreateTableStmt.Select`) and `CREATE VIEW`
  (`CreateViewStmt.Select` + explicit column list). Note: the runner wraps view
  bodies as `CREATE VIEW <name> AS <select>` (`buildSQL` in
  `backend/runner/lineageanalyzer/analyzer.go:409`), so both forms must work.
- **Exit:** parity green on `07_create_table`, `08_create_view`, `13_catalog`.

### Phase 4 — Hardening

- **Parse-failure path**: land the hard-failure behavior above and a unit test
  asserting (a) unparseable SQL returns an error and no relations, and (b) the
  runner records `column_lineage_version.error_message` for it.
- **Differential sweep**: feed every stored MySQL view definition in a real
  deployment (or the integration fixture schema) through both analyzers and diff.
  This is where corpus gaps the 51 YAML cases miss are found.
- **Corpus top-up**: for any divergence, either fix the port or add/refresh the
  YAML expectation deliberately (never mask a difference).
- **Exit:** zero unexplained divergences on the sweep; hard-failure unit test
  green; `make test-integration-mysql` green.

### Phase 5 — Cutover

- New `mysql` package registers **`Engine_MYSQL` only**; `mysqlantlr` is deleted
  (code + its registration + any duplicated testdata).
- Remove the MySQL usage of `antlr4-go` / `bytebase/parser`. **The dependencies
  stay** for the PostgreSQL analyzer.
- Confirm the graceful-degradation path for `TIDB`/`MARIADB`/`OCEANBASE`: their
  views get `engine X has no lineage analyzer; analysis skipped` recorded by
  `markAnalyzed`, and the hourly scan does not re-queue them. Note the gap in the
  follow-up dialect plans so it is tracked, not forgotten.
- **Exit:** full Go suite green; `golangci-lint run --allow-parallel-runners`
  clean; `make build-release` builds; parity harness removed and the new
  analyzer's own YAML suite is the single source of truth.

## Relevant files

| Path | Action |
| --- | --- |
| `backend/plugin/lineage/mysql/analyzer.go` | MOVE to `mysqlantlr/`, then write the new omni-backed analyzer |
| `backend/plugin/lineage/mysql/analyze_test.go`, `analyze_test_helper.go` | MOVE then re-create against the new analyzer |
| `backend/plugin/lineage/mysql/testdata/analyze/*.yaml` | KEEP (parity source of truth), 51 cases / 15 files |
| `backend/plugin/lineage/testutil/lineage_test_helper.go` | ADD parity runner + normalization |
| `backend/plugin/lineage/scope/`, `model/`, `catalog/` | UNCHANGED |
| `backend/plugin/lineage/lineage.go` | UNCHANGED (registration seam) |
| `backend/runner/lineageanalyzer/analyzer.go` | UNCHANGED (reference: `buildSQL`, wrapping) |
| `go.mod` / `go.sum` | ADD `github.com/bytebase/omni` (pinned). `bytebase/parser` + `antlr4-go` stay (PostgreSQL only) |

## Verification

1. **Parity (primary):** `go test ./backend/plugin/lineage/... -run Lineage -count=1`
   with `RunLineageParityFromYAMLDir` over all 51 cases — zero normalized diffs.
2. **New analyzer's own YAML suite:** same corpus through
   `RunLineageYAMLTestSuitesFromYAMLDir` against expected edges.
3. **Hermetic suite:** `go test ./...` (no DB) green.
4. **Integration:** `make test-integration-mysql` (real MySQL, testcontainers)
   covers schema sync → lineage analysis end-to-end. `make test-integration`
   for the broader scenario set.
5. **Lint/build:** `golangci-lint run --allow-parallel-runners` (repeat until
   clean), `go build -ldflags "-w -s" -p=16 -o ./build/metaxisdata ./backend/bin/server/main.go`.
6. **Manual diff on real data:** pick a deployment, compare
   `column_lineage` rows before/after cutover for a sample of views; expect
   identical `(source_column, target_column, relation_type, transformation)`.
7. **Malformed SQL:** unit test asserting hard failure (error + no relations for
   unparseable or multi-statement input) and that the runner records
   `column_lineage_version.error_message` rather than silently emptying lineage.
8. **Engine scope:** after cutover, assert `MYSQL` resolves to the omni analyzer
   and `TIDB`/`MARIADB`/`OCEANBASE` return `ErrorEngineNotSupported` (a
   deliberate, recorded skip — not a crash).
9. **Benchmarks:** `go test ./backend/plugin/lineage/mysql/ -run '^$' -bench Benchmark -benchmem`
   covers the whole corpus, per-case analyze vs parse-only (to separate omni
   parse cost from the lineage walk), synthetic scaling shapes (wide select,
   deep subquery, many joins) and the catalog-backed `SELECT *` expansion path.

## Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- |
| Rewrite regresses an edge case the YAML corpus does not cover | Medium | High | Differential sweep over real stored view definitions (Phase 4); keep cutover behind the registry seam until parity + sweep are clean |
| Hard failure drops lineage for SQL that ANTLR previously recovered from (notably `MANUAL_SQL`) | High | Medium | Accepted by decision; the error is recorded in `column_lineage_version.error_message` and the object is skipped rather than silently wrong. Covered by a unit test; watch the error rate after cutover |
| MariaDB/TiDB/OceanBase lose lineage at cutover | Certain | Medium | Accepted by decision: out of scope, handled by separate plans using `omni/mariadb`/`omni/tidb`. The runner already records a deliberate skip per object and stops re-queueing it |
| Window/transformation output changes (e.g. `extractWindowClauses` bug fix produces *different* output than today) | Medium | Medium | Parity harness compares transformations as a set; where omni is strictly more correct, update the YAML expectation deliberately rather than masking the diff |
| Multi-statement input (`Split`) silently analyzed partially | Low | Medium | Assert exactly one statement; explicit error otherwise |
| Effort overrun (mechanical volume) | Medium | Medium | Phase-level exit criteria on small YAML subsets; each phase is independently mergeable |

## Open questions

1. **Upstream contribution**: should the window/`Over`-preserving experience be
   fed back to omni (e.g. a production `analysis` package for MySQL), rather
   than keeping the walker in-repo? Long-term this could shrink our code.
2. **Package naming**: `mysqlantlr` (temporary) vs a build-tag switch. Naming is
   cosmetic; the registration seam is what matters.
3. **Dialect follow-up ordering**: MariaDB vs TiDB first, and whether each gets
   its own `omni/mariadb`/`omni/tidb` parser or a shared adapter. Decided in the
   respective plans, not here.

## Appendix A — Probe methodology (how the numbers were produced)

- `awk` over `analyzer.go` split by `^func ` to attribute lines to functions;
  a function counts as parser-coupled if its signature or body references
  `mysql.` or `antlr.`.
- `grep -o "mysql\.[A-Za-z]*"` for the type/accessor inventory.
- omni capabilities verified by building `./mysql/...` at HEAD `f240970c` and by
  a scratch program (since omni's MySQL lineage walker is unexported and
  test-only) that ran the same SQL through both `catalog.AnalyzeSelectStmt` and
  our analyzer. That probe showed omni's *catalog IR* loses set-op and window
  lineage, while omni's *parser AST* retains `FuncCallExpr.Over` — the reason
  this plan chooses the parser AST, not the IR.

## Appendix B — omni AST quick reference

```go
// github.com/bytebase/omni/mysql/parser
func Parse(sql string) (*nodes.List, error)   // strict; splits DELIMITER/compound
func Split(sql string) []Segment

// github.com/bytebase/omni/mysql/ast (all nodes carry Loc{Start,End})
type SelectStmt struct { CTEs; TargetList []ExprNode; From []TableExpr; Where ExprNode
    GroupBy; Having; OrderBy; WindowClause []*WindowDef
    SetOp SetOperation; SetAll bool; Left, Right *SelectStmt
    TableSource *TableStmt; ValuesSource *ValuesStmt; ParenSource *SelectStmt }
type InsertStmt struct { IsReplace bool; Table *TableRef; Columns []*ColumnRef
    Values [][]ExprNode; Select *SelectStmt; OnDuplicateKey []*Assignment }
type UpdateStmt struct { Tables []TableExpr; SetList []*Assignment; Where ExprNode }
type DeleteStmt struct { Tables []TableExpr; Using []TableExpr; Where ExprNode }
type CreateTableStmt struct { Table *TableRef; Select *SelectStmt; Like *TableRef }
type CreateViewStmt struct { Name *TableRef; Columns []string; Select *SelectStmt
    SelectText string; Algorithm string }
type LoadDataStmt struct { Table *TableRef; ... }
type JoinClause struct { Type JoinType; Left, Right TableExpr; Condition Node }
type TableRef struct { Schema, Name, Alias string }
type ColumnRef struct { Table, Schema, Column string; Star bool }
type ResTarget struct { Name string; Val ExprNode }
type Assignment struct { Column *ColumnRef; Value ExprNode }
type FuncCallExpr struct { Name string; Args []ExprNode; Star, Distinct bool
    Over *WindowDef; OrderBy []*OrderByItem; Separator ExprNode }
type WindowDef struct { Name, RefName string; PartitionBy []ExprNode; OrderBy []*OrderByItem; Frame *WindowFrame }
type CaseExpr / BinaryExpr / UnaryExpr / BetweenExpr / InExpr / CastExpr / SubqueryExpr / RowExpr / StarExpr / ParenExpr
```
