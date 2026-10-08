# Lineage semantics — Reference

> Status: **implemented**. Maintenance reference for what the SQL analyzers must keep producing: `backend/plugin/lineage/{model,scope,algorithm}`, `.../postgresql`, `.../mysql` (+ `tidb` / `mariadb`), `.../starrocks`.
> Related: [lineage-analyzer.md](lineage-analyzer.md) (assembly, runner lifecycle, generation, AST audit), [lineage-graph.md](lineage-graph.md) (graph-side field lineage), [omni-upstream-defects.md](omni-upstream-defects.md) (upstream parser/AST defects — pointers only here).

## What it is

An analyzer turns one statement into **edges**. Each edge is one derivation of one target column from one source column, carrying the operations that produced it.

| Concept | Shape | Where |
| --- | --- | --- |
| Edge | source column, target column, `[]Transformation`, relation type, `IsTemp` | `backend/plugin/lineage/model/relation.go:10` |
| Output column | `OutputColumn{Alias, Sources []ColumnSource}` — no transformation of its own | `backend/plugin/lineage/scope/types.go:156` |
| Source + its transformation | `ColumnSource{Ref, Transform}` | `backend/plugin/lineage/scope/types.go:119` |
| Statement result target | `__result__` (`model.ResultTableName`); `IsTemp` is exactly "the target is this" | `backend/plugin/lineage/model/relation.go:30` |
| Row-set markers | `__deletion__` for a DELETE, `__file__` for a load | `backend/plugin/lineage/model/relation.go:36`, `:41` |

A statement that is representable but incomplete returns its edges **plus** an `UnsupportedStatementError` carrying `model.Diagnostic` gaps; the runner keeps the edges and records the gap (see [lineage-analyzer.md](lineage-analyzer.md)). Gap shapes that are deliberate are listed per dialect below.

## The transformation model

`model.Transformation` is a tagged union on `Operation` (`backend/plugin/lineage/model/transformation.go:45`). Four decisions define its shape permanently — they are contract, not implementation detail.

| # | Decision | Code |
| --- | --- | --- |
| 1 | A transformation travels with **the source it came from**, not with the output column: `OutputColumn.Sources []ColumnSource`, each `{Ref, Transform}`. Two sources of one output column may disagree about how they produced it — which a set operation requires. | `scope/types.go:119` |
| 2 | One derivation is one edge, and the transformation list is **part of the edge's identity**: `x + 1 AS a` and `x + 2 AS a` are two edges. Compared structurally, never through a rendering. | `algorithm/edge.go:42`, `model/transformation.go:229` |
| 3 | The list is **ordered, outermost operation first**. `RelationTypeOf` reads `transform[0]`; a set-operation arm records the chain that combines it, outermost first. | `model/relation.go:91`, `algorithm/setop.go:20` |
| 4 | A set operation records **whether it keeps duplicate rows** in `Transformation.All` (UNION ALL vs UNION, INTERSECT ALL, EXCEPT ALL). A bit on the kind, never inferred. | `model/transformation.go:76`, `:110-133` |

Operation → relation type is one shared rule (`model.RelationTypeOf`, `model/relation.go:91`):

| `Operation` | Relation type | Producer |
| --- | --- | --- |
| `DELETE` | indirect | a DELETE's `__deletion__` edge |
| `UNION` / `INTERSECT` / `EXCEPT` | union / intersect / except | set-operation arms |
| `AGGREGATE` | group | aggregate functions |
| `JOIN` | join | a join predicate influence |
| `FILTER`, `WINDOW`, `PROJECT`, `OPERATOR`, `CASE`, `FUNCTION`, `SORT`, `GROUP_BY` | indirect | analyzers / ingestion |
| *(empty list)* | direct | a bare column reference |

`SORT` and `GROUP_BY` exist only for the OpenLineage subtype mapping; **no SQL analyzer emits them** (`model/transformation.go:34-40`).

### What the model deliberately does not express

| Not expressed | Why |
| --- | --- |
| A recursive CTE's fixpoint | the analysis is a single pass; a fixpoint would change every analyzer for one statement shape |
| A set operation's arm order, or where duplicates were removed | the chain says which operations combined the arm, not the de-duplication point |
| Expression structure | `Expression` is text — the thing the UI shows and a human checks against the SQL |
| Column-level grouping of transformations | an edge is one source's path; "these columns feed this output" is rebuilt by the consumer |
| A `SORT` / `GROUP_BY` producer | reserved values for the OpenLineage subtype mapping only |

### Where it is stored and rendered

`column_lineage.transformation` is `JSONB NOT NULL DEFAULT '[]'` holding the plain `json.Marshal` of `[]model.Transformation` (`backend/migrator/migration/LATEST.sql:306`, `backend/store/column_lineage.go:109`), so the JSON keys are the struct's tags, **not** protojson's camelCase. The API maps the list through `convertTransformations` (`backend/api/v1/lineage_service.go:379`) to `v1pb.Transformation` (`proto/v1/v1/lineage_service.proto:104`), and the frontend renders it in `frontend/src/components/metadata/LineageTransformationCell.vue`. Adding a field is backward-compatible for stored rows — an absent key reads as the zero value — but the model, the proto, the mapping and the cell have to move together, which is what `All` did.

## Edge identity and the direct ⟺ no-transformation invariant

The edge set deduplicates on *(source identifier, source column, target identifier, target column)* plus `model.SameTransformations` compared field by field (`algorithm/edge.go:13-51`). Identifiers are compared as **values**, never joined into a string: a separator inside an identifier cannot collide with two identifiers around it.

The relation type is **computed, not stored**: `buildLineageEdge` sets `RelationType: model.RelationTypeOf(transform)` (`scope/types.go:208`), and `RelationTypeOf` returns `direct` only for an empty list. So `relation_type == direct ⟺ no transformation` is structural rather than a convention.

The corpus side of the invariant is a test: `RequireFullEdgeAnnotations` (`backend/plugin/lineage/testutil/lineage_test_helper.go:337`) makes every expectation state `relation_type`, `is_temp` and both field names, and asserts `relation_type != direct ⟺ transformations non-empty` (`:369-374`); an `AGGREGATE` must also state `group_keys`. Every dialect calls it from `TestCorpusIsFullyAnnotated` (e.g. `backend/plugin/lineage/postgresql/analyze_test.go:15`).

## Per-dialect semantics

### PostgreSQL

**Scope model.** A `scope.Scope` has three namespaces and a parent chain that *is* the query level (`backend/plugin/lineage/scope/scope.go:12-30`):

| Namespace | Contents | Reached by |
| --- | --- | --- |
| Relations | `[]*TableRef` keyed by `(qualifier, alias-or-name)` — a slice, so `db1.t` and `db2.t` coexist | `AddTable`, `FindRelation` (`:57`, `:82`) |
| CTE definitions | `map[string]*CTEDefinition` in the scope that declares them | `AddCTE`, `FindCTE` (`:69`, `:109`) |
| Read-only definitions | `Scope.definitions` — a scope that resolves another scope's CTEs but none of its relations | `NewScopeWithDefinitions` (`:47`) |

The two CTE paths are deliberately asymmetric: `FindCTE` (a FROM entry) crosses into `definitions`; `findCTEQualifier` (a column qualifier `c.x`) does not (`scope/scope.go:109`, `:132`). That is PostgreSQL's rule — a CTE is nameable from a FROM list and nowhere else.

**Clause visibility, measured on PostgreSQL 16.15** (pinned by the corpus and unit tests):

| Clause / reference | Can name the statement's own relations | Can name a CTE as `c.x` |
| --- | --- | --- |
| `ON CONFLICT … DO UPDATE SET` | no — the INSERT source is its own query level | no |
| `INSERT … RETURNING` | no — same reason | no |
| `UPDATE … RETURNING` / `DELETE … RETURNING` | yes (own `FROM` / `USING`) | no |
| `EXCLUDED.x` | only inside the conflict clause's own expressions | — |
| Data-modifying CTE output | — | exposes **only** its `RETURNING` columns; without `RETURNING` it is not referenceable |

The asymmetry that makes the model necessary: a subquery written inside one of these clauses **is** its own query level, so its FROM list can name a statement CTE even when the clause beside it cannot — `ON CONFLICT … SET b = (SELECT y FROM c)` succeeds while `SET b = c.y` fails. That is exactly the `FindCTE` vs `findCTEQualifier` split.

Code: `processInsertSource` analyzes an INSERT's query in its own scope and returns it (`backend/plugin/lineage/postgresql/analyzer.go:1209`); `processOnConflict` detaches from the statement's relations but links its definitions (`:1261`, `:1272`); `processReturning` **replaces** the scope's output columns with the RETURNING list, and empties them when there is none (`:1337`).

**Known deviation (kept by decision).** `ResolveColumnRefs` still resolves `c.x` against a CTE definition when the CTE is not registered as a relation (`scope/scope.go:209`), so illegal SQL such as `SELECT c.a FROM stage` yields lineage here — a superset of what PostgreSQL accepts. Only the scope unit tests pin it.

**Expression classification** (`backend/plugin/lineage/postgresql/expr.go`), structural over the omni AST:

| Rule | Code |
| --- | --- |
| `isExpressionDerived` is an explicit allow-list of governing nodes; leaves (`A_Const`, `ColumnRef`, `ParamRef`, `A_Star`, `SQLValueFunction`) stay non-derived | `:204` |
| The source-less `table.*` fallback applies only when the governing node is a `FuncCall`, so a constant cast/array/row cannot fabricate an edge | `:233` |
| The **top-level node governs**; PostgreSQL materialises no parenthesis node, so this is decisive | `:240` |
| `Over != nil` means WINDOW and is checked **before** the aggregate set — a windowed aggregate is a window, not a GROUP | `:380` |
| Operators use the **source token** (`"+"`, `"->>"`, `"IN"`, …); `CAST`/`COALESCE`/`GREATEST`/`LEAST`/`NULLIF` are named `FUNCTION`s; `SubLink`, `A_Indirection`, `A_ArrayExpr`, `RowExpr`, … are `PROJECT` | `:528`, `:240-291` |
| `windowClauses` follows `OVER w` into the `WINDOW` clause chain and drops sort direction | `:501` |
| An unaliased output column is named the way PostgreSQL names it | `:648` |

The last rule matters mainly for `MANUAL_SQL`: `pg_get_viewdef` always emits `AS` aliases, so a synced VIEW / MATVIEW target column already matches (engine fact, corpus `postgresql/testdata/analyze/36_test_output_target_lineage_table.yaml`).

**Still-in-effect decisions:**

- A **top-level DML's `RETURNING` produces no `__result__` edges** — the returned rows are a client-facing result and the lineage is the write it performs. Data-modifying CTEs are fully modelled (`postgresql/analyzer.go:1334`).
- **UPDATE produces no predicate influence edges.** Its `WHERE` / `FROM` predicates are collected and deliberately dropped; DELETE models its row set as `x -> target.__deletion__` with a `DELETE` transformation (`postgresql/analyzer.go:1278-1287`, `:1519`).
- The `ON CONFLICT … DO UPDATE … WHERE` clause is not modelled — it is an UPDATE's row set by the same rule, and `15_test_on_conflict_lineage_table.yaml` asserts "with WHERE == without WHERE".
- A recursive CTE registers **itself with no lineage** inside its own body, so neither a source nor a join predicate leaks to a base table sharing the name (`postgresql/analyzer.go:461`).

### MySQL family

`backend/plugin/lineage/mysql/analyzer.go` holds the shared body; `tidb` and `mariadb` are generated from it (generator: [lineage-analyzer.md](lineage-analyzer.md)). The frozen product decisions — each asserted by the corpus and commented in the cases that depend on it:

| Decision | Detail |
| --- | --- |
| Window clauses | a window function's `PARTITION BY` / `ORDER BY` columns are sources of the windowed output column, relation type indirect |
| Source-less aggregate | `COUNT(*)` depends on the whole relation (`table.*`); a literal, `NOW()` or a cast of a constant records **no edge** |
| Constant assignment | `UPDATE t SET col = <constant>` records `table.* -> t.col` — the row is rewritten even though no column is read (`analyzer.go:1471-1481`) |
| Set operations | the edge carries `union` / `intersect` / `except` as its relation type, output columns are named by the first arm, and `All` distinguishes the ALL forms |
| Unqualified disambiguation | catalog metadata is consulted for base tables and trusted both ways: every described owner is reported (a coalesced `USING` / `NATURAL JOIN` column names both sides); a scope whose relations are all described and own nothing lets the search continue outward; an **undescribed** relation answers a name no described relation owns, preferred over one the catalog ruled out; a single-relation scope keeps the name-order rule even when its metadata lacks the column |
| Identifier case | preserved verbatim (`A.ID` stays `A.ID`) |
| Unaliased output column | named the way the engine names it: a quoted identifier contributes its unquoted name; any other expression contributes its raw source text with the original spacing |
| Expression subquery | classified `PROJECT` in every engine — the inner aggregate belongs to the subquery's own query specification |

Implementation: `resolveUnqualified` walks outward and stops at the first scope that provides the name, with the unknown-relation and single-relation fallbacks above (`scope/scope.go:242-288`).

**Deliberately not recorded:**

| Shape | Behavior |
| --- | --- |
| `ORDER BY` / `DISTINCT` / `LIMIT` | no edge (`SORT` is reserved, not produced) |
| `UPDATE`'s `WHERE` / `JOIN` | no influence edge; those clauses are walked only for their subqueries |
| `DELETE`'s `WHERE` | **is** recorded: `x -> target.__deletion__` with a `DELETE` transformation (`mysql/analyzer.go:1517`) |
| `GROUP BY` | carries keys in an `AGGREGATE` transformation; no edge of its own |
| `JSON_TABLE` argument columns | not recorded — a FROM function would need per-function semantics |
| The `UNKNOWN` relation type | has no producer; `RelationTypeOf` cannot return it (only the API maps it) |

**Failure modes worth knowing:** an unexpanded `*` does not push edges forward — `AnsweringLineage` answers a named column first and falls back to `*` only when none matches (`model/relation.go:53`); an undescribed relation is preferred as the owner of a name no described relation owns; several statements in one `MANUAL_SQL` text are rejected because they have no single result shape.

**Corpus contract.** Every dialect corpus compares its expectations **exactly** — `subset` is not used anywhere — and every dialect asserts `relation_type`, `is_temp`, both field names and each transformation's operation through `RequireFullEdgeAnnotations`, not by convention. Current size:

| Dialect | Suite files | Cases |
| --- | --- | --- |
| PostgreSQL | 40 | 250 |
| MySQL (corpus shared with TiDB and MariaDB) | 24 | 225 |
| StarRocks | 20 | 185 |
| TiDB (dialect suite, on top of the shared corpus) | 1 | 15 |
| MariaDB (dialect suite, on top of the shared corpus) | 1 | 20 |
| **total** | **86** | **695** |

### StarRocks

**Raw-text model.** omni exposes three query bodies only as text — a derived table (`TableRef.Subquery`), a CTAS body (`CreateTableStmt.AsSelect`) and an expression subquery (`SubqueryExpr`). The analyzer keeps a stack of `source{text, tokens}`: descending into a raw body pushes a child source instead of remapping offsets, and expression text is rebuilt whitespace-free within the current source (`backend/plugin/lineage/starrocks/rawsql.go:13`, `:51`). A raw body that fails to re-parse is a hard analysis error.

**Materialized views.** StarRocks stores the whole `SHOW CREATE MATERIALIZED VIEW` output in the MV definition, so `buildSQL` passes a definition through unchanged when it already starts with `CREATE` (`backend/runner/lineageanalyzer/analyzer.go:443`, `:468`). omni's `CREATE VIEW` grammar rejects a `WITH`, a `UNION` and a parenthesized body, so `viewbody.go` extracts the body after the top-level `AS` and parses it as a standalone query (`starrocks/viewbody.go:28`; pinned by `starrocks/viewbody_test.go:12`).

**The port-time "deliberate divergences from MySQL" are closed.** The divergences the StarRocks plan recorded were MySQL-family defects that the MySQL remediation has since fixed; both dialects now assert the same result, so a difference in these shapes is a bug rather than a decision:

| Shape | MySQL corpus that now pins it |
| --- | --- |
| `WITH c (a, b) AS (…) SELECT c.a, c.b FROM c` honours the declared column list | `mysql/testdata/analyze/04_test_c_t_e_lineage_table.yaml:91` |
| `SELECT * FROM t x` / `SELECT x.* FROM t x` produces edges | `mysql/testdata/analyze/17_test_regression_lineage_table.yaml:6`, `:40` |
| A set operation nested in a view / derived table / CTE keeps every arm | `mysql/testdata/analyze/17_test_regression_lineage_table.yaml:337`, `:357`, `:377` |
| A scalar subquery resolves in its own scope and classifies as `PROJECT` | `mysql/testdata/analyze/16_test_extended_forms_lineage_table.yaml:254` |
| A DDL body emits only target edges, no duplicate `__result__` edges | `mysql/testdata/analyze/08_test_create_view_lineage_table.yaml:5` |

**Scope contract.** `ColumnRef.Resolved` marks a reference already resolved against a scope a later lookup cannot see; `ResolveColumnRefs` returns it unchanged (`scope/scope.go:183`), pinned by `TestScope_ResolveColumn_ResolvedPassThrough` (`scope/scope_test.go:502`). StarRocks introduced it, and all dialects now set it where a query-local or arm-local reference must survive into an enclosing scope (`starrocks/analyzer.go:1009`, `mysql/analyzer.go:942`, `postgresql/analyzer.go:1090`).

**Reported as gaps rather than failures:** `MERGE INTO` and an unmodelled query expression raise `not modelled:` diagnostics and keep the surrounding edges (`starrocks/analyzer.go:207`, `:248`; `starrocks/analyzer_test.go:112`). **Not modelled at all:** `SELECT * REPLACE`, `GROUP BY ALL`, cross-catalog references (the registry has no catalog dimension). **`DORIS` stays unregistered** by decision (`backend/plugin/lineage/engines/engines.go:10`).

## Cross-dialect naming and edge rules

Rules that hold across all dialects; changing one is changing all of them.

| Rule | Code |
| --- | --- |
| The relation type is computed from the transformation list, never stored per producer | `scope/types.go:208`, `model/relation.go:91` |
| A qualifier means a database (MySQL family, StarRocks) or a schema (PostgreSQL) — different identifier fields | `scope/types.go:175`, `:183` |
| A query-local relation is never an edge endpoint: its lineage is traced to base tables with transformations combined along the way | `algorithm/temp.go:14`, `:34`, `:92` |
| A query-local name never shadows a real relation of the same name — temp-ness is derived from the resolved relation, not a name set | `algorithm/temp.go:92` |
| Set-operation arms merge positionally; each arm keeps its own transformations, combining chain outermost first | `algorithm/setop.go:42` |
| Predicate influences belong to the scope whose rows they decide and are inherited by its consumer; an unconsumed scope's predicates are dropped | `algorithm/influence.go:31`, `:75`, `:87` |
| A predicate edge's target column is empty — it decides rows, not a column's value | `algorithm/influence.go:122-144` |
| `HAVING` may name a select-list alias; the alias resolves to that output column's own sources | `algorithm/influence.go:154` |
| `*` answers only when no named column does | `model/relation.go:53` |
| Every corpus edge states its relation type, temp flag and transformation operations | `testutil/lineage_test_helper.go:337` |

## Open items

1. **PG-FU-4 — cross-dialect expression-classification unification.** PostgreSQL classifies the governing top-level node and emits the source operator token. MySQL handles only `CaseExpr`, `BinaryExpr`, `UnaryExpr`, `BetweenExpr` and `InExpr` before falling back to the *first function call in the tree* (`analyzeExpressionOperator`, `mysql/analyzer.go:1912-1941`, `firstFuncCall` at `:1925`), and names operators with words (`binaryOperatorName`, `:1967`). So wrapper nodes the MySQL AST carries (`IsExpr`, `LikeExpr`, `CastExpr`, `ExistsExpr`) are classified from whatever function they contain — `sum(x) IS NULL` reports the inner `SUM` where PostgreSQL reports `OPERATOR "IS NULL"` — and the shared shapes differ in vocabulary (`ADDITION` vs `+`). Recorded here because the plan that named it is retired.
2. **MySQL ODKU constant-assignment asymmetry.** PostgreSQL `ON CONFLICT … DO UPDATE SET b = 'x'` produces `t.* -> t.b`; MySQL `ON DUPLICATE KEY UPDATE a = 1` produces **zero edges**, because `len(sourceColumns) == 0` continues without the `table.*` fallback that the plain `UPDATE t SET a = 1` path has (`mysql/analyzer.go:1244-1246` vs `:1471-1481`). Decide first whether a constant write deserves a `t.*` edge, then align.
3. **Deliberately missing shapes** (not defects): StarRocks `* REPLACE`, `GROUP BY ALL`, `SECURITY NONE`, cross-catalog references; `DORIS` unregistered. Upstream parser gaps are recorded in [omni-upstream-defects.md](omni-upstream-defects.md).

## Where things live

| What | Path |
| --- | --- |
| Edge, relation type, result markers | [backend/plugin/lineage/model/](../../backend/plugin/lineage/model/) — `relation.go`, `transformation.go` |
| Scope, namespaces, resolution | [backend/plugin/lineage/scope/](../../backend/plugin/lineage/scope/) — `scope.go`, `types.go`, `exposed.go` |
| Dialect-neutral mechanisms | [backend/plugin/lineage/algorithm/](../../backend/plugin/lineage/algorithm/) — `edge.go`, `setop.go`, `influence.go`, `temp.go`, `diagnostics.go` |
| PostgreSQL analyzer | [backend/plugin/lineage/postgresql/](../../backend/plugin/lineage/postgresql/) — `analyzer.go`, `expr.go`, `predicate.go`; corpus in `testdata/analyze/` |
| MySQL family | `mysql/analyzer.go` (shared body) with `tidb/`, `mariadb/` generated; corpora in each `testdata/analyze/` |
| StarRocks analyzer | [backend/plugin/lineage/starrocks/](../../backend/plugin/lineage/starrocks/) — `analyzer.go`, `expr.go`, `rawsql.go`, `viewbody.go` |
| Engine set / wiring | [engines.go](../../backend/plugin/lineage/engines/engines.go), built once in [backend/server/server.go:107](../../backend/server/server.go#L107) |
| Invariant test | [testutil/lineage_test_helper.go:337](../../backend/plugin/lineage/testutil/lineage_test_helper.go#L337) |
| Unit tests worth reading | `postgresql/expr_classify_test.go` (`TestIsExpressionDerived`, `TestClassifyExpression`, `TestSourceLessFallback`), `postgresql/onconflict_test.go`, `starrocks/viewbody_test.go`, `scope/scope_test.go` |
| Gates | `go test ./backend/plugin/lineage/... -count=1`; `gofmt`; `golangci-lint run --allow-parallel-runners`; corpus test `TestAnalyzeYAML`, annotation test `TestCorpusIsFullyAnnotated` |
| Assembly, runner, generator, AST audit | [lineage-analyzer.md](lineage-analyzer.md) |
| Graph-side field lineage | [lineage-graph.md](lineage-graph.md) |

Non-goals: no fixpoint for recursive CTEs; no `SORT` / `GROUP_BY` producer; no DML `RETURNING` result edges; no `UPDATE` predicate influences; no catalog dimension in relation identity.
