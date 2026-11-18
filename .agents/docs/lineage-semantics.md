# Lineage semantics — Reference

Maintenance reference for what every SQL analyzer must keep producing: `backend/plugin/lineage/{model,scope,algorithm}`, the PostgreSQL and MySQL-family analyzers, and the engine-specific analyzers linked below.
Related: [lineage-analyzer.md](lineage-analyzer.md) (assembly, the runner, the MySQL-family generator), [lineage-dialects.md](lineage-dialects.md) (StarRocks, Doris, MSSQL), [lineage-graph.md](lineage-graph.md) (graph-side field lineage), [omni-upstream-defects.md](omni-upstream-defects.md) (parser/AST defects — pointers only).

## What it is

An analyzer turns one statement into **edges**. Each edge is one derivation of one target column from one source column, carrying the operations that produced it.

| Concept | Shape | Symbol |
| --- | --- | --- |
| Edge | source column, target column, `[]Transformation`, relation type, `IsTemp` | `model.ColumnRelation` |
| Output column | `OutputColumn{Alias, Sources []ColumnSource}` — no transformation of its own | `scope.OutputColumn` |
| Source + its transformation | `ColumnSource{Ref, Transform}` | `scope.ColumnSource` |
| Statement result target | `__result__` (`model.ResultTableName`); `IsTemp` is exactly "the target is this" | `model.ColumnRelation` |
| Row-set markers | `__deletion__` for a DELETE (`model.DeletionColumnName`), `__file__` for a load (`model.FileSourceName`) | `model` |

A statement that is representable but incomplete returns its edges **plus** an `UnsupportedStatementError` carrying `model.Diagnostic` gaps; the runner keeps the edges and records the gap (see [lineage-analyzer.md](lineage-analyzer.md)). Deliberate gap shapes are listed with each dialect in [lineage-dialects.md](lineage-dialects.md).

## The transformation model

`model.Transformation` is a tagged union on `Operation`. Four decisions define its shape permanently — they are contract, not implementation detail.

| # | Decision | Symbol |
| --- | --- | --- |
| 1 | A transformation travels with **the source it came from**, not with the output column: `OutputColumn.Sources []ColumnSource`, each `{Ref, Transform}`. Two sources of one output column may disagree about how they produced it — which a set operation requires. | `scope.ColumnSource` |
| 2 | One derivation is one edge, and the transformation list is **part of the edge's identity**: `x + 1 AS a` and `x + 2 AS a` are two edges. Compared structurally, never through a rendering. | `algorithm.EdgeSet`, `model.SameTransformations` |
| 3 | The list is **ordered, outermost operation first**. `RelationTypeOf` reads `transform[0]`; a set-operation arm records the chain that combines it, outermost first. | `model.RelationTypeOf`, `algorithm.ArmChain` |
| 4 | A set operation records **whether it keeps duplicate rows** in `Transformation.All` (UNION ALL vs UNION, INTERSECT ALL, EXCEPT ALL). A bit on the kind, never inferred. | `model.Transformation.All` |

Operation → relation type is one shared rule (`model.RelationTypeOf`):

| `Operation` | Relation type | Producer |
| --- | --- | --- |
| `DELETE` | indirect | a DELETE's `__deletion__` edge |
| `UNION` / `INTERSECT` / `EXCEPT` | union / intersect / except | set-operation arms |
| `AGGREGATE` | group | aggregate functions |
| `JOIN` | join | a join predicate influence |
| `FILTER`, `WINDOW`, `PROJECT`, `OPERATOR`, `CASE`, `FUNCTION`, `SORT`, `GROUP_BY` | indirect | analyzers / ingestion |
| *(empty list)* | direct | a bare column reference |

`SORT` and `GROUP_BY` exist only for the OpenLineage subtype mapping; **no SQL analyzer emits them**.

**What the model deliberately does not express.** A recursive CTE's fixpoint (the analysis is a single pass); a set operation's arm order, or where duplicates were removed; the expression structure (`Expression` is the text the UI shows and a human checks against the SQL); column-level grouping of transformations (rebuilt by the consumer); a `SORT` / `GROUP_BY` producer.

### Where it is stored and rendered

`column_lineage.transformation` is `JSONB NOT NULL DEFAULT '[]'` holding the plain `json.Marshal` of `[]model.Transformation`, so the JSON keys are the struct's tags, **not** protojson's camelCase. The API maps the list through `convertTransformations` to `v1pb.Transformation`, and the frontend renders it in `LineageTransformationCell.vue`. Adding a field is backward-compatible for stored rows — an absent key reads as the zero value — but the model, the proto, the mapping and the cell have to move together, which is what `All` did.

## Edge identity and the direct ⟺ no-transformation invariant

The edge set deduplicates on *(source identifier, source column, target identifier, target column)* plus `model.SameTransformations` compared field by field (`algorithm.EdgeSet`). Identifiers are compared as **values**, never joined into a string: a separator inside an identifier cannot collide with two identifiers around it.

The relation type is **computed, not stored**: `buildLineageEdge` sets `RelationType` from `model.RelationTypeOf(transform)`, and `RelationTypeOf` returns `direct` only for an empty list. So `relation_type == direct ⟺ no transformation` is structural rather than a convention.

The corpus side of the invariant is a test: `testutil.RequireFullEdgeAnnotations` makes every expectation state `relation_type`, `is_temp` and both field names, asserts `relation_type != direct ⟺ transformations non-empty`, and requires `group_keys` on an `AGGREGATE`. Every dialect calls it from `TestCorpusIsFullyAnnotated`.

## Engine-measured clause visibility (PostgreSQL)

Measured on PostgreSQL 16.15 and pinned by the corpus:

| Clause / reference | Rule |
| --- | --- |
| `ON CONFLICT … DO UPDATE SET` | the INSERT source is its own query level, so the clause names neither that source's relations nor a CTE by qualifier |
| `INSERT … RETURNING` | same — the INSERT source cannot be named |
| `UPDATE … RETURNING` / `DELETE … RETURNING` | may name the target and the relations of its own `FROM` / `USING`; a CTE is nameable only through those |
| `EXCLUDED.x` | visible only inside the conflict clause's own expressions |
| Data-modifying CTE output | exposes **only** its `RETURNING` columns; without `RETURNING` it is not referenceable |

The asymmetry that makes the scope model necessary: a subquery written inside one of these clauses **is** its own query level, so its FROM list can name a statement CTE even when the clause beside it cannot — `ON CONFLICT … SET b = (SELECT y FROM c)` succeeds while `SET b = c.y` fails. That is exactly the `FindCTE` vs `findCTEQualifier` split in `scope.Scope`: a FROM entry crosses into a scope's read-only CTE definitions, a column qualifier does not.

Code: `processInsertSource` analyzes an INSERT's query in its own scope and returns it; `processOnConflict` detaches from the statement's relations but links its definitions; `processReturning` **replaces** the scope's output columns with the RETURNING list, and empties them when there is none.

**Known deviation (kept by decision).** `ResolveColumnRefs` still resolves `c.x` against a CTE definition when the CTE is not registered as a relation, so illegal SQL such as `SELECT c.a FROM stage` yields lineage here — a superset of what PostgreSQL accepts. Only the scope unit tests pin it.

### Expression classification (PostgreSQL)

Classification is **structural over the AST, never a scan of the source text** (`isExpressionDerived`, `classifyExpression`):

- `isExpressionDerived` is an explicit allow-list of governing nodes; leaves (`A_Const`, `ColumnRef`, `ParamRef`, `A_Star`, `SQLValueFunction`) stay non-derived, so a source-less constant cannot fabricate an edge.
- The source-less `table.*` fallback applies only when the governing node is a `FuncCall` (`isTableWideExpression`), which is what licenses `COUNT(*)` depending on the whole relation.
- The **top-level node governs**: PostgreSQL materializes no parenthesis node, so parentheses never wrap an operation and the top-level switch is decisive.
- `Over != nil` means WINDOW and is checked **before** the aggregate set — a windowed aggregate is a window, not a GROUP.
- Operators use the **source token** (`"+"`, `"->>"`, `"IN"`, …); `CAST`, `COALESCE`, `GREATEST`, `LEAST` and `NULLIF` are named `FUNCTION`s; `SubLink`, `A_Indirection`, `A_ArrayExpr`, `RowExpr` and the other structural wrappers are `PROJECT`.
- `windowClauses` follows `OVER w` into the `WINDOW` clause chain and drops sort direction.
- An unaliased output column is named the way PostgreSQL names it. This matters mainly for `MANUAL_SQL`: `pg_get_viewdef` always emits `AS` aliases, so a synced VIEW / MATVIEW target column already matches.

**Still-in-effect decisions:**

- A **top-level DML's `RETURNING` produces no `__result__` edges** — the returned rows are a client-facing result and the lineage is the write it performs. Data-modifying CTEs are fully modelled.
- **UPDATE produces no predicate influence edges.** Its `WHERE` / `FROM` predicates are collected and deliberately dropped; DELETE models its row set as `x -> target.__deletion__` with a `DELETE` transformation.
- The `ON CONFLICT … DO UPDATE … WHERE` clause is not modelled — it is an UPDATE's row set by the same rule.
- A recursive CTE registers **itself with no lineage** inside its own body, so neither a source nor a join predicate leaks to a base table sharing the name.

## MySQL family

`mysql/analyzer.go` holds the shared body; `tidb` and `mariadb` are generated from it (generator: [lineage-analyzer.md](lineage-analyzer.md)). The frozen product decisions — each asserted by the corpus:

| Decision | Detail |
| --- | --- |
| Window clauses | a window function's `PARTITION BY` / `ORDER BY` columns are sources of the windowed output column, relation type indirect |
| Source-less aggregate | `COUNT(*)` depends on the whole relation (`table.*`); a literal, `NOW()` or a cast of a constant records **no edge** |
| Constant assignment | `UPDATE t SET col = <constant>` records `table.* -> t.col` — the row is rewritten even though no column is read |
| Set operations | the edge carries `union` / `intersect` / `except` as its relation type, output columns are named by the first arm, and `All` distinguishes the ALL forms |
| Unqualified disambiguation | catalog metadata is consulted for base tables and trusted both ways: every described owner is reported (a coalesced `USING` / `NATURAL JOIN` column names both sides); a scope whose relations are all described and own nothing lets the search continue outward; an **undescribed** relation answers a name no described relation owns, preferred over one the catalog ruled out; a scope holding a single relation answers a name no scope owns even when its metadata lacks the column |
| Identifier case | preserved verbatim (`A.ID` stays `A.ID`) |
| Unaliased output column | named the way the engine names it: a quoted identifier contributes its unquoted name; any other expression contributes its raw source text with the original spacing |
| Expression subquery | classified `PROJECT` in every engine — the inner aggregate belongs to the subquery's own query specification |

Implementation: `Scope.resolveUnqualified` walks outward and stops at the first scope that provides the name, with the unknown-relation and single-relation fallbacks above.

**Deliberately not recorded:**

| Shape | Behavior |
| --- | --- |
| `ORDER BY` / `DISTINCT` / `LIMIT` | no edge (`SORT` is reserved, not produced) |
| `UPDATE`'s `WHERE` / `JOIN` | no influence edge; those clauses are walked only for their subqueries |
| `DELETE`'s `WHERE` | **is** recorded: `x -> target.__deletion__` with a `DELETE` transformation |
| `GROUP BY` | carries keys in an `AGGREGATE` transformation; no edge of its own |
| A function in FROM other than `JSON_TABLE` | carries no lineage; `JSON_TABLE` is a query-local relation whose declared columns depend on the document expression's columns |
| The `UNKNOWN` relation type | has no producer; `RelationTypeOf` cannot return it (only the API maps it) |

**Failure mode worth knowing:** an unexpanded `*` does not push edges forward — `model.AnsweringLineage` answers a named column first and falls back to `*` only when none matches.

## Per-dialect detail

StarRocks, Doris and MSSQL are documented in [lineage-dialects.md](lineage-dialects.md), which records what each AST and parser forces and also holds the corpus size and per-dialect split. Every dialect corpus compares its expectations exactly, so each case pins the statement's full edge set.

## Cross-dialect naming and edge rules

Rules that hold across all dialects; changing one is changing all of them.

| Rule | Symbol |
| --- | --- |
| The relation type is computed from the transformation list, never stored per producer | `model.RelationTypeOf` |
| A qualifier means a database (MySQL family, StarRocks, Doris) or a schema (PostgreSQL, MSSQL) — different identifier fields | `scope.NewLineageEdge` / `scope.NewSchemaLineageEdge` |
| A query-local relation is never an edge endpoint: its lineage is traced to base tables with transformations combined along the way | `algorithm.TraceThroughTableLineage`, `FlattenTempSources` |
| A query-local name never shadows a real relation of the same name — temp-ness is derived from the resolved relation, not a name set | `scope.TableRef.IsCTE` / `IsSubquery` |
| Set-operation arms merge positionally; each arm keeps its own transformations, combining chain outermost first | `algorithm.MergeSetOpColumns`, `ArmChain` |
| Predicate influences belong to the scope whose rows they decide and are inherited by its consumer; an unconsumed scope's predicates are dropped | `algorithm.Influences` |
| A predicate edge's target column is empty — it decides rows, not a column's value | `algorithm.Influences.Emit` |
| `HAVING` may name a select-list alias; the alias resolves to that output column's own sources | `algorithm.Influences.Resolve` |
| An identifier's case is the dialect's rule to declare, never a shared default; the same fact gates the registry's folded lookup | `scope.Scope.SetFoldNames`, `lineage.EngineRegistration.FoldIdentifierNames` |
| A qualified reference is taken on faith by default; a dialect whose engine answers it from the catalog turns the check on, and only a relation the catalog describes is asked | `scope.Scope.SetVerifyQualifiedColumns` |
| A relation's identity in an analysis is the identity the registry registered, not the name the statement wrote | `catalog.Provide.GetTable`, `lineage.Analyzer.CanonicalIdentifier` |
| `*` answers only when no named column does | `model.AnsweringLineage` |
| A `*` over a relation whose columns are known expands into one output column per column; a relation whose columns are unknown keeps one wildcard output column | `algorithm.ExpandQueryLocalWildcard` |
| A temporary relation exposes the columns its body produces, with a declared list renaming a prefix of them — never the names its lineage happens to carry | `scope.ExposedColumnNames`, `scope.OutputColumnNames` |
| Every corpus edge states its relation type, temp flag and transformation operations | `testutil.RequireFullEdgeAnnotations` |

## Known gaps

- **Expression classification is not unified across dialects.** PostgreSQL classifies the governing top-level node and names operators by source token; the MySQL family classifies a few node kinds and then falls back to the first function call in the tree, and names operators with words. So `sum(x) IS NULL` reports the inner `SUM` where PostgreSQL reports an `OPERATOR "IS NULL"`, and shared shapes differ in vocabulary (`ADDITION` vs `+`).
- **A constant ODKU write records nothing.** MySQL `INSERT … ON DUPLICATE KEY UPDATE a = 1` produces no edge, because the upsert path continues when no source column is found; a plain `UPDATE t SET a = 1` records `t.* -> t.a`, and so does PostgreSQL's `ON CONFLICT … DO UPDATE`.
- **An unanswered query-local reference is a gap only in MSSQL.** A reference that resolves to a CTE, derived table, table function, LATERAL VIEW generator or pivot whose lineage says nothing about the column is dropped silently by the shared tracers; only the MSSQL wrappers ask first (`queryLocalAnswers`). Reachable only with a reference the engine rejects, which is why it is recorded rather than engineered.
- **A described relation with an empty column list is read two ways.** `resolveInScope` treats it as described-owns-nothing while `ownsColumn` treats it as unknown; with two such relations an unqualified name can become a false gap. It needs a table or view whose stored column list a sync has not filled, so it is contrived.
- **`OutputColumn.IsDerived` has no reader.** It is written and merged across dialects but never decides anything; consumers read the transformation list instead.

## Where things live

| What | Path |
| --- | --- |
| Edge, relation type, result markers | [backend/plugin/lineage/model/](../../backend/plugin/lineage/model/) — `ColumnRelation`, `Transformation`, `RelationTypeOf` |
| Scope, namespaces, resolution | [backend/plugin/lineage/scope/](../../backend/plugin/lineage/scope/) — `Scope`, `ColumnRef`, `ColumnSource`, `ExposedColumnNames` |
| Dialect-neutral mechanisms | [backend/plugin/lineage/algorithm/](../../backend/plugin/lineage/algorithm/) — `EdgeSet`, `ArmChain`, `Influences`, the temp-relation tracers |
| PostgreSQL analyzer | [backend/plugin/lineage/postgresql/](../../backend/plugin/lineage/postgresql/) — `analyzer.go`, `expr.go`, `predicate.go`; corpus in `testdata/analyze/` |
| MySQL family | `mysql/analyzer.go` (shared body) with `tidb/`, `mariadb/` generated; corpora in each `testdata/analyze/` |
| Engine set and wiring | [engines/engines.go](../../backend/plugin/lineage/engines/engines.go), built once in [backend/server/server.go](../../backend/server/server.go) |
| Unit tests worth reading | `wildcard_target_test.go` and `cte_exposure_test.go` in the package root; `postgresql/expr_classify_test.go` (`TestIsExpressionDerived`, `TestClassifyExpression`, `TestSourceLessFallback`), `postgresql/onconflict_test.go`, `scope/scope_test.go`, `scope/exposed_test.go` |
| Assembly, runner, generator | [lineage-analyzer.md](lineage-analyzer.md); engine-specific analyzers: [lineage-dialects.md](lineage-dialects.md) |
