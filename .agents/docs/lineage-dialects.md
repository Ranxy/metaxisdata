# Lineage dialect analyzers — Reference

Maintenance reference for the engine-specific SQL analyzers under `backend/plugin/lineage/`.
Related: [lineage-semantics.md](lineage-semantics.md) (the transformation model and the rules every analyzer must keep producing), [lineage-analyzer.md](lineage-analyzer.md) (assembly, the runner lifecycle, the MySQL-family generator, the AST coverage audit).

Per-dialect behavior is documented because it is not a matter of taste: each engine hands the analyzer a different AST, and omni's grammar for each engine has its own holes, so what an analyzer can model is decided by those two facts. A rule that belongs to one dialect reads as a bug when it is mistaken for a shared one, so each section below records what the parser forces, the decisions that follow from it, and what is deliberately left as a reported gap rather than a guess. The MySQL family (MySQL, TiDB, MariaDB) is not covered here: TiDB and MariaDB have no traversal of their own, and the shared body is described in [lineage-analyzer.md](lineage-analyzer.md).

## Corpus

Every dialect corpus compares its expectations exactly (`subset` is not used anywhere), so each case pins the statement's full edge set. Current size: **135 suites / 1,114 cases**.

| Dialect | Suites | Cases |
| --- | --- | --- |
| PostgreSQL | 42 | 258 |
| MySQL (shared body for TiDB and MariaDB) | 26 | 231 |
| StarRocks | 22 | 192 |
| Doris | 23 | 239 |
| MSSQL | 16 | 149 |
| TiDB (own suite on the shared corpus) | 3 | 20 |
| MariaDB (own suite on the shared corpus) | 3 | 25 |

## StarRocks

### What the engine forces

- omni exposes three query bodies only as raw text: a derived table (`TableRef.Subquery`), a CTAS body (`CreateTableStmt.AsSelect`) and an expression subquery. The analyzer keeps a stack of `source{text, tokens}`, pushes a child source for each raw body instead of remapping offsets, and rebuilds expression text from the current source's tokens (`rawsql.go`). A raw body that fails to re-parse is a hard analysis error, not a partial result.
- A StarRocks materialized-view definition is the whole `SHOW CREATE MATERIALIZED VIEW` statement, so `buildSQL` passes a definition that already opens with `CREATE` through unchanged; wrapping it would produce `AS CREATE MATERIALIZED VIEW ...`. A bare view body, however, reaches omni's `CREATE VIEW` production, which accepts only a plain `SELECT`: a `WITH`, a set operation or a parenthesized body is extracted after the top-level `AS` and parsed as a top-level query (`viewbody.go`, `extractViewDDL`).
- A VARIANT column's subcolumns are read through functions taking a JSONPath — `variant_query(v, '$.user.name')`, `get_variant` — and StarRocks 4.1 rejects both the dotted form (`Column 'v.user.name' cannot be resolved`) and a subscript on a VARIANT column (`cannot subscript type VARIANT`).

### Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| VARIANT is read by function, not by path | The function form records the whole column as the output's source through the generic expression walk; the Doris path reading is deliberately not mirrored | A mirror would record edges for statements this engine rejects (`variant_access_test.go`) |
| A raw query body is analyzed from a pushed source | Re-parse the body in a fresh scope with its own text and tokens on the stack | The body is text, so offsets cannot be remapped; a body that does not re-parse fails the statement, because edges derived from an unread body would be silently wrong |
| A materialized-view definition is used as stored | No wrapping and no body extraction | The definition is already a complete `CREATE MATERIALIZED VIEW`; wrapping it yields `AS CREATE ...` |
| A view body omni cannot read in place is extracted | `extractViewDDL` finds the first depth-0 `AS` after `VIEW` and parses its body as a top-level query | Covers the `WITH`, set-operation and parenthesized bodies the `CREATE VIEW` production rejects |
| A catalog qualifier is dropped | `[catalog.]database.table` is analyzed as `database.table` | The metadata registry has no catalog dimension |
| MERGE and an unmodelled query expression are gaps, not failures | `not modelled:` diagnostics beside the edges found | The same decision as PostgreSQL's; a coverage gap must not discard real lineage |

### Deliberate gaps

- `MERGE INTO` and a query expression the traversal does not model are `not modelled:` diagnostics (`dispatch`, `processQueryNode`), pinned by `analyzer_test.go` `TestUnmodelledStatementsAreGaps`.
- `LOAD` models the file source and the target column list only; a `SET`/`WHERE` in its data description carries no column-level lineage (`processLoadStatement`).
- A table-valued function's own body is not read: a function in FROM becomes a query-local relation built from the columns its arguments read, so lineage through it stops at the call and the function is never an edge endpoint.

### Tests

- `backend/plugin/lineage/starrocks/testdata/analyze/` (22 suites / 192 cases): `TestAnalyzeYAML`, `TestCorpusIsFullyAnnotated`.
- `viewbody_test.go` `TestExtractViewDDL`; `variant_access_test.go` `TestVARIANTIsReadByFunctionOnStarRocks`; `hardfail_test.go` `TestHardFailOnUnparseableSQL` and `TestRejectsMultiStatement`; `analyzer_test.go` `TestExprTextReconstruction` and `TestUnmodelledStatementsAreGaps`.

## Doris

### What the engine forces

- omni's Doris AST is a sibling of the StarRocks AST, not the same grammar. A parenthesized query or a repeated `ORDER BY`/`LIMIT` group is `GroupedQuery{Query}` where StarRocks has `ParenSelect{Sel}`; a table-valued function is `TableRef.Func` where StarRocks has a `TableFunctionRef`; `TableRef.LateralViews` exists only here; `InsertStmt` has no `ByName`, `CreateTableStmt` no `CTASColumns`, and `UpdateStmt`/`DeleteStmt` no `With`. The traversal is therefore a port of the StarRocks one with those deltas (package comment in `analyzer.go`).
- Doris stores a view or materialized-view body with every column fully qualified by catalog and database: `internal`.`db`.`t`.`c`. The registry has no catalog dimension, so the leading segment is dropped and the relation is read in the same database (`columnRefFromObjectName`, `tableRefFromObjectName`); the end-to-end chain reads the stored definitions back this way.
- An async materialized view is not a kind of its own in `information_schema.tables` — Doris reports it as `BASE TABLE` — and its body is reachable only through `mv_infos("database"=?)`'s `QuerySql`, which is a bare query body. The runner therefore always wraps it as `CREATE MATERIALIZED VIEW <name> AS <body>`, unlike StarRocks, whose stored definition is the whole statement (`getMaterializedViews` in `backend/plugin/db/starrocks/sync.go`, `buildSQL` in `backend/runner/lineageanalyzer/build_sql.go`).
- An object is addressed by a meta GUID with one segment per identifier part, `instance;database;schema;name`, and a part the engine lacks is an empty segment. Doris has no schema, so its edges must read `<instance>;<database>;;<name>`; a producer that writes a schema segment shifts every part and lands on a GUID no schema sync registered. The end-to-end Doris hop is the check for this: its gate forbids an edge with a schema segment.
- omni's Doris grammar rejects four shapes the engine runs (checked against 4.1.4): `LATERAL VIEW` over a derived table, `SELECT ... INTO OUTFILE`, a VARIANT path that mixes bracket and dot spelling, and a `WITH` clause standing before INSERT/UPDATE/DELETE.

### Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| The catalog segment of a qualified name is dropped | `catalog.database.table` is read as `database.table`, in the analysis database | The registry has no catalog dimension; an edge has to address the GUID the sync registered |
| A `LATERAL VIEW` over a derived table is recovered, not failed | Hoist the derived table into a CTE of the same name and re-parse; refuse when any other relation in the statement holds that name | The engine runs it and omni stops at `LATERAL`; a derived table read once and a CTE read once are the same relation. A hoisted name outranks a base table, so a refused rewrite keeps the hard failure instead of changing what a name means |
| A grouping set contributes its columns as keys | `ROLLUP`, `CUBE` and `GROUPING SETS` flatten to the columns of every set, deduplicated | A grouping set is a set of groupings and the model has only a key list; the grouped expression (`ROLLUP(a,b)`) is not a key a consumer can act on. PostgreSQL and MSSQL flatten them the same way; Doris also drops a key the query repeats |
| A dotted VARIANT path is read only with catalog evidence | Bracket and dot forms name the same column; the dotted form resolves as a path only when the registry says the base column is `variant`, and a mixed spelling is rewritten to the bracket form while expression naming keeps the original text | The engine returns the same value for both forms. The rewritten text must not become a column name (the engine names an unaliased output after the query text), and `a[1].b` is not rewritten because the bracket form means something else there |
| A `WITH` clause before DML is recovered where it can be | An INSERT's clause moves in front of the query it writes; an UPDATE's or DELETE's is dropped only when the statement reads none of its names; a clause the statement does read keeps the hard failure | The engine runs all three; omni does not. An unused CTE writes nothing, while a used one has no valid placement to move to |
| A `SELECT ... INTO OUTFILE` sink is stripped | Parse the query before the sink; report no diagnostic | A sink carries no column-level lineage, so the projection edges are the statement's lineage — the result the StarRocks analyzer gives for the same statement |
| A qualified column is answered by the catalog | `scope.SetVerifyQualifiedColumns` is on in the root scope; a described relation that does not own the column turns the reference into a gap, and query-local relations are never asked | The engine answers `Unknown column 'nope' in 't'` the same way for a qualified and an unqualified name, so a qualified reading is the same question with a relation pinned to it. A query-local column list is the analyzer's own account, so a wrong one may not create a gap |

### Deliberate gaps

- `MERGE INTO` and an unmodelled query expression are `not modelled:` diagnostics beside the edges, never a failure (`TestUnmodelledStatementsAreGaps`).
- `LOAD`'s `SET`/`WHERE` carries no column-level lineage; only the target and its column list are read (`processLoadStatement`, the same decision as StarRocks).
- A VARIANT path whose base is a query-local relation has no type evidence, so the reference is reported unresolved rather than read as a path or dropped (`TestAnUnexplainableVARIANTBaseIsAGap`).
- The qualified-column check costs a stored column list that lags the engine: `SELECT t3.name FROM t3` loses its edge while the unqualified spelling keeps the single-relation fallback. The registry is what the graph renders from, so an edge to a column it does not have is the worse answer (`TestTheRegistryAnswersForAQualifiedColumn`).

### Tests

- `backend/plugin/lineage/doris/testdata/analyze/` (23 suites / 239 cases): `TestAnalyzeYAML`, `TestCorpusIsFullyAnnotated`.
- `lateral_test.go` `TestALateralViewOverADerivedTableKeepsItsLineage`; `generator_lineage_test.go` `TestLateralViewGeneratorLineage` and `TestCTASTargetColumnsAreTheDeclaredNames`.
- `variant_path_test.go` `TestVARIANTSubcolumnPathLineage` and `TestAVARIANTReadNeedsCatalogEvidence`; `mixedpath_test.go` `TestAMixedVARIANTpathKeepsItsLineageAndItsName`.
- `qualified_column_test.go` `TestTheRegistryAnswersForAQualifiedColumn`; `withdml_test.go` `TestAWithBeforeDMLKeepsItsLineage`; `hardfail_test.go` `TestHardFailOnUnparseableSQL` and `TestRejectsMultiStatement`.
- End to end: `test/e2e_etl/doris/*.sql`, `test/e2e_etl/dags/e2e_07_pg_to_doris.py` and `e2e_08_doris_transform.py`, with the Doris-hop queries in `test/e2e_etl/verify_chain.sql`.

## MSSQL

### What the engine forces

- A T-SQL script is split by the parser, not lexically: `mssql.Parse` is the only thing that knows where a `GO` batch ends and that a `;` inside a `CREATE PROCEDURE ... BEGIN ... END` ends a statement in the batch rather than the batch. A batch separator carries no SQL and is dropped; when a statement's text span comes back empty, the split falls back to a lexical scan so an unparseable statement is still reported. The analyzer itself accepts exactly one statement.
- A set operation is not a node of its own but fields on `SelectStmt` (`Op`, `All`, `Larg`, `Rarg`). `All` doubles as `SELECT ALL`, so it means "keeps duplicates" only when `Op` is set.
- Every query body arrives typed — a derived table is `AliasedTableRef > SubqueryExpr > *SelectStmt`, a CTE body is `CommonTableExpr.Query` — so this dialect has no raw text to re-parse and no view-body extraction. The traversal covers CTEs and the joins inside them, aggregates over windows (`OVER w` reads the named window's own keys), wildcard expansion, `CUBE`/`ROLLUP`/`GROUPING SETS` as key lists, `PIVOT`/`UNPIVOT`, and `CROSS`/`OUTER APPLY` as joins whose right side may be a table-valued function.
- The qualifier is the schema (`scope.NewSchemaLineageEdge`), and a `database.schema.table` name's database is compared against the analysis context; a name in another database is a reported gap, never read as a same-named local object. A four-part name keeps its server, which the analyzer drops on purpose.
- The wrong-order hazard: an object is named `database.schema.table`, and the producer side has to split the same three parts in the same order. A two-part producer name is read as `schema.table` with no database at all (`splitDatasetName` in `backend/plugin/openlineage/resolver.go`), while the same two-part spelling means `database.table` on the MySQL-wire engines, so a producer that writes the database where the schema goes, or omits it, resolves to a GUID no sync registered. This is the one engine in the end-to-end chain where that mistake is reachable, so the MSSQL hop checks both its copy and the view the analyzer itself produced.
- Three upstream AST facts shape the traversal: a scalar subquery, an aliased table reference and every declared name list carry no end offset, so expression text is rebuilt from nodes and names are read as fields; a typed nil survives `x == nil` and would be dereferenced by omni's walker, so every entry point normalizes it first (`nodeOrNil`); and `NATURAL`/`USING` are read as table aliases, so `relationAlias` re-checks a recorded alias against the source text.

### Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| A script is split by the parser, with a lexical fallback | `SplitStatements` parses first and takes each statement's text; text the parser rejects is cut lexically | A splitter that returned nothing for unparseable input would turn "could not analyze" into "no lineage" |
| One statement per analysis | Multi-statement and comment-only input are rejected | The split lives above the dialects; a second multi-statement policy inside a dialect would disagree about what one statement is |
| `All` is read only when `Op` is set | A plain `SELECT ALL` is not a set operation | One field carries both; reading it unconditionally turns `SELECT ALL` into `UNION ALL` |
| A three-part name's database must match the analysis context | Same database: analyzed as a schema-qualified name. Another database: a gap | The edge records a schema and fills the database from the context, so a foreign name would otherwise point at a local object of the same name |
| The server segment of a four-part name is dropped | `srv.db.dbo.t` is analyzed as `db.dbo.t` | The product registers one instance per logical database and does not model linked servers; the assumption is that one logical database is registered once, and a foreign instance exposing a same-named database would get a local edge instead of a gap |
| Identifier case is folded | `scope.SetFoldNames` is on, and a relation takes the registry's identity through the folded lookup (`canonicalRelation`) | SQL Server compares identifiers case-insensitively under its default collation while the registry is byte-exact. Folding decides which object a reference addresses, never how a column is spelled |
| `MERGE` is modelled | The ON condition is a join influence on the target, each `WHEN` clause's own condition a filter influence, and each action writes what its statement would | It is the one statement the other dialects report as a gap; this engine makes its shape visible, so nothing has to be guessed |
| A PIVOT's grouping columns are read from the catalog | The source's columns that neither the aggregate nor the `FOR` column consumes are carried through when the source's column list is known; otherwise the relation exposes the pivot columns alone | The statement never names them, so a guess would invent edges; the source is analyzed in a scope of its own so a `SELECT *` beside the pivot cannot read it as a second relation |
| An empty result must be a gap | A reference a query-local relation cannot answer, `EXEC`, `INSERT ... EXEC` and a routine body become diagnostics; a column the relation offers with no source (constants) does not | An analysis that produced nothing and said nothing is indistinguishable from "this statement has no lineage", and only a diagnostic carries the difference. A constant column has no lineage to record |

### Deliberate gaps

- A `PIVOT` over a derived table cannot enumerate the grouping column it carries through: the derived table's columns are its own query's, so the reference is reported unresolved while the pivot columns keep their edges (`pivotGroupingColumns`; the chain's `v_pivot_over_derived` fixture).
- A table-valued function is not a relation the registry can address. It is registered as a query-local relation exposing the columns the query declares for it, so without a declared list a reference to its columns is reported unresolved rather than attributed to the call (`processTableFunction`; the chain's `v_tvf_source` fixture).
- A routine body is not analyzed: `CREATE PROCEDURE` and `CREATE FUNCTION` produce a `not modelled:` diagnostic and no edges, and a procedure call or `EXEC` is a gap because the statements it runs are not in the analyzed text.
- A parenthesized view body is rejected by the pinned parser: the statement fails whole, with no edges and its error recorded — not a view that merely has no lineage (the chain's `v_parenthesised` fixture).
- `OUTPUT` is not modelled, like PostgreSQL's `RETURNING`: a data-modification statement's client-facing result set is not lineage.
- A target the registry cannot address — a table variable, a `#temp` table, a `SEMANTIC` model table — produces no edge and says so, rather than writing an edge with an empty name. A table variable used as a *source* is skipped silently, because static analysis does not know its columns and its references already surface as unresolved.

### Tests

- `backend/plugin/lineage/mssql/testdata/analyze/` (16 suites / 149 cases): `TestAnalyzeYAML`, `TestCorpusIsFullyAnnotated`.
- `analyzer_test.go` `TestSplitStatements`, `TestClauseKeywordAliasGuard`, `TestCrossDatabaseQualifierIsReported` and `TestAQueryLocalColumnWithNoSourceIsNotAGap`; `identifier_case_test.go` `TestIdentifierCaseIsFolded`; `hardfail_test.go` `TestHardFailOnUnparseableSQL` and `TestRejectsMultiStatement`.
- End to end: `test/e2e_etl/mssql/60_gap_fixtures.sql`, `test/e2e_etl/dags/e2e_05_pg_to_mssql.py` and `e2e_06_mssql_to_sr.py`, with the MSSQL-hop queries in `test/e2e_etl/verify_chain.sql`; the registered-identity behavior also has the integration test `TestMSSQLLineageRecordsTheRegisteredRelationRealServerIntegration` in `backend/test/integration/runner/`.

## Where things live

| Concern | Code |
| --- | --- |
| Registration and assembly | `backend/plugin/lineage/engines/` |
| StarRocks dialect | `backend/plugin/lineage/starrocks/{analyzer,expr,predicate,rawsql,viewbody,tokenname}.go` |
| Doris dialect | `backend/plugin/lineage/doris/{analyzer,expr,predicate,rawsql,viewbody,lateral,mixedpath,outfile,withdml,tokenname}.go` |
| MSSQL dialect | `backend/plugin/lineage/mssql/{analyzer,expr,predicate,split}.go` |
| Shared scope, catalog and edge algorithms | `backend/plugin/lineage/{scope,catalog,algorithm}/` |
| Materialized-view definitions and SQL wrapping | `backend/plugin/db/starrocks/sync.go`, `backend/runner/lineageanalyzer/build_sql.go` |
| Shared-mechanism guard | `backend/plugin/lineage/shared_analysis_test.go` (`TestSharedAnalysisStaysShared`) |
| Upstream parser and AST defects, and the no-patch policy | [omni-upstream-defects.md](omni-upstream-defects.md) |
| End-to-end chain | `test/e2e_etl/` |
