# Local record: `github.com/bytebase/omni` parser/AST defects

> **Purpose.** A local record of defects found in the pinned upstream dependency
> while auditing the lineage dialects. **Nothing here is to be sent,
> filed, posted or otherwise communicated upstream, and the module is not to be
> modified, patched, forked or vendored.** The file exists so the knowledge is not
> lost and so the analyzer's workarounds are justified in-repo. If upstream is
> ever contacted, that is a human decision made outside this record.
>
> Module: `github.com/bytebase/omni v0.0.0-20261014102304-038210857601`
> (`go.mod`). Packages inspected: `mysql/parser` (`Parse`, `Tokenize`, `Split`) and
> `mysql/ast`; `mssql/parser` (`Parse`, `Tokenize`, `NewLexer`) and `mssql/ast`
> (§9); `mariadb/parser`, `tidb/parser` and `starrocks/parser` for the dialect
> notes in §8.

## How this was gathered

Every claim below was produced by running throwaway probes (a scratch module and
a temporary `zz_probe_*.go` test inside the mysql package, both deleted
afterwards) and reading the omni source in the module cache. No repository or
module file was modified. Build note for anyone reproducing this: the default
`~/.cache/go-build` may be read-only in this environment; use
`GOCACHE=/tmp/gocache-probe`.

The notes in §10 and §11 come from re-running one probe program per dialect family
against both pins — the module cache holds the old version, so a scratch module can
`require` either and the two outputs diff — and from reading the source diff between
the two module trees. A note that names a pin says where that behaviour was observed.

The analyzer consumes `Loc` in two ways — `exprTextOf` reconstructs expression
text by concatenating the tokens whose spans fall inside `[Loc.Start, Loc.End)`
(`backend/plugin/lineage/mysql/analyzer.go:265`) and `nodeLoc` reads the `Loc`
field by reflection (`:219`). Both are affected by items 1 and 2.

## 1. Infix nodes set `Loc.Start` to the operator, not the node start

Verified on 24 binary nodes across 9 statements. `Loc.Start` is always the
operator's offset, and the left operand lies **outside** the span:

| SQL | Observed `Loc` / slice | Node start |
| --- | --- | --- |
| `SELECT a + b FROM t` | `[9,13)` = `"+ b "` (op `+` at `[9,10)`) | `a` at `[7,9)` |
| `SELECT a.x - 1 AS v FROM t` | `[11,15)` = `"- 1 "` | `a.x` at `[7,11)` |
| `SELECT a + b + c FROM t` | outer `[13,17)` = `"+ c "`, inner `[9,13)` = `"+ b "` | `a` at `[7,9)` |
| `SELECT a * (b + c) FROM t` | `[9,19)` = `"* (b + c) "` | `a` at `[7,9)` |
| `SELECT (a + b) * c FROM t` | `[15,19)` = `"* c "` (drops `(a+b)` at `[7,15)`) | `(` at `[7,8)` |
| `SELECT a FROM t WHERE a = 1 AND b > 2` | `AND` `[28,37)` = `"AND b > 2"`; `=` `[24,28)` = `"= 1 "`; `>` `[34,37)` = `"> 2"` | respective left operands |
| `SELECT a BETWEEN 1 AND 2 FROM t` | `[9,25)` = `"BETWEEN 1 AND 2 "` | `a` at `[7,9)` |
| `SELECT a IN (1,2) FROM t` | `[9,17)` = `"IN (1,2)"` | `a` at `[7,9)` |
| `SELECT a NOT IN (SELECT b FROM t2) FROM t` | `[21,46)` = `"NOT IN (SELECT b FROM t2)"` | `a` at `[19,21)` |

Details:

- `Loc.Start` is `opStart` captured explicitly in `mysql/parser/expr.go`
  (e.g. `Loc{Start: opStart, End: p.pos()}`, lines ~164, ~185, ~2070) and is used
  identically for `BETWEEN`/`IN` and keyword containers — so it is *deliberately
  coded*, an undocumented "span starts at the operator" convention rather than an
  accident.
- `Loc.End` is `p.pos()` = the current token's start (`parser.go:170`), i.e. the
  start of the next **unconsumed** token. It includes the right operand and often
  trailing whitespace (`"+ b "` ends where `c` begins).
- **`UnaryExpr` is correct** (`SELECT -x` → `[7,10)` = `"-x "`; `NOT a` →
  `[7,13)`; `-(a+b)` → `[7,16)`). The contrast confirms infix anchoring is a
  defect, not a global convention.
- Same pattern in other nodes: `CreateViewStmt` `[7,32)` = `"VIEW v AS SELECT a FROM t"`
  (drops the leading `CREATE `); `Limit` `[22,24)` = `"10"` for
  `SELECT a FROM t LIMIT 10` (drops `LIMIT`; with `OFFSET` → `"10 OFFSET 5"`);
  `OnCondition` drops the leading `ON`.

**Why it is a defect, not a convention.** omni's own `Loc` doc says only "source
location range (byte offsets)" with no operator exception; omni's PostgreSQL
scenario file uses `sql[node.Loc.Start:node.Loc.End]` matching the expected source
text as its verification criterion; and MySQL's `TestLocEndExcludesTrailingComments`
asserts exact statement spans.

**Downstream impact observed through the real analyzer:**

- `CREATE VIEW d.v AS SELECT d.t.x - 1 AS v FROM d.t` →
  `Transformation{Operation: PROJECT, Expression: "-1"}`; expected `"d.t.x-1"`.
- `CREATE VIEW d.v AS SELECT d.t.x - 1 FROM d.t` (no alias) → the inferred output
  column is named **`-1`** (`t.x -> __result__.-1`, `t.x -> v.-1`), because
  `inferColumnAlias` runs on the truncated text.
- `SELECT d.t.x + d.t.y AS v` → `Expression: "+d.t.y"`; `IN (1,2)` →
  `"IN(1,2)"`; `x * (y + 1)` → `"*(d.t.y+1)"`; `UPDATE t SET a = a.x - 1` →
  `"-1"`.
- The text-based operator detector the analyzer used at the time was misled by the
  truncated text: `"-1"` starts with `-`, so its SUBTRACTION branch
  (`!strings.HasPrefix(text, "-")`) was skipped and `a.x - 1` became `PROJECT`,
  while the `+` form yielded `OPERATOR`. The analyzer now decides the operator
  from `BinaryExpr.Op`/`UnaryExpr.Op` (`detectInfixOperator`).
- Column *edges* are unaffected: `collectColumns` walks the AST, so `t.x -> v.v`
  is still emitted. This is corrupt expression text, corrupt inferred aliases and
  wrong transformation kinds — not missing edges.

**Downstream workaround (in this repo).** Do not trust `Loc.Start` for infix
nodes. Classify operators from `BinaryExpr.Op`/`UnaryExpr.Op` instead of the
text, and reconstruct text from the minimum of the children's `Loc.Start` (or
from the operator's operands) rather than the parent span. Both are implemented in
`backend/plugin/lineage/mysql/analyzer.go` (`exprTextOf`, `isInfixNode`,
`detectInfixOperator`).

## 2. `LocStart()` / `LocEnd()` are not promoted; no exported Loc accessor

`mysql/ast/node.go` defines value methods on `Loc`:

```go
// This method enables field promotion: any struct with a Loc field
// automatically gets LocStart() without explicit implementation.
func (l Loc) LocStart() int { return l.Start }
func (l Loc) LocEnd() int   { return l.End }
```

That comment is **false in Go**: methods are promoted only through *embedded*
fields, never named fields, and omni declares `Loc` as a named field
(`type SelectStmt struct { Loc Loc; … }`, `type ColumnRef struct { Loc Loc; … }`).
A minimal reproduction:

```go
type Loc struct{ Start, End int }
func (l Loc) LocStart() int { return l.Start }
type Named struct{ Loc Loc }     // omni's shape
type Embedded struct{ Loc }      // what promotion requires
// interface{ LocStart() int } is satisfied by Embedded, never by Named.
```

- The assertion `interface{ LocStart() int; LocEnd() int }` fails for **all 53** concrete
  node types probed in the re-run, including every type the analyzer touches.
  Structurally general: `mysql/ast` declares **212 named `Loc Loc` fields and
  zero embedded `Loc` fields**.
- `ast.List` has no `Loc` field at all (`ast/node.go:44`), so reflection yields a
  zero `Loc`; the analyzer's `nodeLoc` handles that correctly.
- There is no `Locatable` interface, no per-node `GetLoc`, and no exported Loc
  accessor anywhere in `mysql/ast`. (`mysql/catalog/exec.go` rolls its own
  unexported reflection helper, `stmtLocStart`; by contrast `cassandra/ast`
  defines an explicit `GetLoc() Loc` on every node.)

**Consequence.** Reflection — or an exhaustive type switch — is currently the only
way to read a MySQL node's `Loc`; the analyzer's `reflect`-based `nodeLoc` is
necessary, not lazy. It should still be cached (type → field index) because it is
on the per-expression hot path, so `nodeLoc` caches it by type
(`backend/plugin/lineage/mysql/analyzer.go:268`).

## 3. Set-operation parse gaps, and one over-acceptance

Real gaps (valid MySQL 8.0.19+ that omni rejects):

| SQL | omni error |
| --- | --- |
| `TABLE t UNION TABLE s` / `TABLE t UNION SELECT 1` | `syntax error at or near "UNION" (line 1, column 9)` |
| `TABLE t EXCEPT TABLE s` | `syntax error at or near "EXCEPT"` |
| `TABLE t INTERSECT SELECT 1` | `syntax error at or near "INTERSECT"` |
| `VALUES ROW(1,2) UNION VALUES ROW(10,15)` / `… UNION SELECT 1` | `syntax error at or near "UNION" (line 1, column 17)` |
| `VALUES ROW(1,2) INTERSECT …` / `… EXCEPT …` | `syntax error at or near "INTERSECT"/"EXCEPT"` |

Asymmetry: `SELECT 1 UNION TABLE t` and `SELECT 1 UNION VALUES ROW(1,2)` both
parse — only a `SELECT` may be the left operand. This means a `TABLE` / `VALUES`
query primary cannot appear on the left of a set operation at all.

Over-acceptance: `SELECT a FROM t MINUS SELECT b FROM s` parses, although MySQL
has no `MINUS` — the same set-operator path that rejects `TABLE t UNION …`
accepts a non-MySQL keyword.

## 4. `WITH` before DML is dropped; other structural notes

- `InsertStmt`, `DeleteStmt` and `UpdateStmt` have **no CTE field**. `WITH c AS (…)
  INSERT/DELETE/UPDATE …` therefore parses (the statement itself is accepted) but
  the CTE is discarded, and a downstream analyzer that uses the AST cannot see it.
  A statement-text prefix check is the only local signal. The analyzer therefore
  skips the statement and reports it as a not-modelled gap rather than inventing a
  source table for the CTE name.
- `CreateViewStmt.SelectText` **is** populated (`"SELECT a FROM t"`, including
  comments/WHERE), and `CreateViewStmt.Select.Loc` covers that text exactly; it
  does not depend on the caller retaining the original SQL.
- `Parse` accepts multi-statement input and returns a `List`; on the first failing
  segment it returns `(nil, err)` and discards previously parsed statements.
  Empty/whitespace/comment-only input yields `List.Len() == 0`. Offsets stay
  absolute across `DELIMITER` segments (`parseSingle(seg.Text, seg.ByteStart)`).
- `Split(sql)` returns `Segment{Text, ByteStart, ByteEnd}` with exact byte offsets,
  excluding the trailing `;`; leading whitespace stays in `Text`.
- `Tokenize(sql)` is always base-0 over the whole string and in source order,
  which matches `Parse`'s per-segment base offsets, so the analyzer's
  `Tokenize(a.sql)` + `Loc` slicing is internally consistent (including DELIMITER
  scripts).

## 5. omni's own parser tests cannot catch item 1

`mysql/parser/loc_test.go::TestLocAudit` only flags `Loc{Start >= 0, End <= Start}`.
It has no notion of "the span must cover the node's own source text", which is
exactly why the infix anchoring defect survives upstream's suite.

## 6. Checked and *not* defects

Recording these so nobody re-investigates them as bugs. Every row was re-run against
the current pin (15 rejected and 20 accepted statements) and none flipped.

- `VALUES (1,2)` and `VALUES ROW(1,2),(3,4)` are rejected — MySQL requires
  `ROW()` per row; omni accepts `VALUES ROW(1,2), ROW(3,4)`.
- `SELECT JSON_TABLE(...)` is rejected — JSON_TABLE is FROM-only in MySQL; omni
  parses `FROM JSON_TABLE(...)` as `ast.JsonTableExpr`.
- `SELECT a FROM t SOUNDS LIKE b` is rejected — invalid MySQL (the operator must
  be inside an expression); `SELECT 'x' SOUNDS LIKE 'y'` parses.
- `SELECT OVERLAY('abc' PLACING 'x' FROM 1)` is rejected — MySQL has no `OVERLAY`
  function (it uses `INSERT()`), so rejection is arguably correct even though the
  lexer reserves `kwOVERLAY`/`kwPLACING`. Not verified against a live server.
- `SELECT a FROM t GROUP BY a DESC` is rejected — MySQL removed GROUP BY
  ASC/DESC in 8.0 (WL#8693). Not verified against a live server.
- `SELECT /*!80000 a */ 1` is rejected — the versioned comment expands to
  `SELECT a 1`, invalid in MySQL too.
- `RETURNING`, `AS OF TIMESTAMP`, `NULLS LAST` and `?` placeholders —
  PostgreSQL/other-dialect syntax, out of `Parse`'s scope.

Accepted as expected: `TABLE t`; `VALUES ROW(1,2)`; `WITH … INSERT/DELETE/UPDATE`;
`INSERT INTO t TABLE s`; `INSERT INTO t SET a=1`; `NATURAL JOIN`; one/two/three
nested parenthesised joins; `ON DUPLICATE KEY UPDATE a=VALUES(a)`;
optimizer hints; window frames (`ROWS/RANGE/GROUPS BETWEEN …`); `DUAL`;
`LOCK TABLES`; `ALTER TABLE`; `FOR UPDATE/SHARE`; `DISTINCTROW`; `BINARY a`;
`INTERSECT`/`EXCEPT`/`MINUS` after a SELECT; `INTO OUTFILE`; all comment styles;
`DELIMITER`-separated procedures; row alias `INSERT … AS new ON DUPLICATE KEY
UPDATE`; `LATERAL`; `MEMBER OF`.

## 7. Summary for the analyzer

The parser is good enough for AST-walk-based lineage: column edges are correct
even where `Loc` is not. Everything **text-derived** from `Loc` is wrong for
infix expressions, `BETWEEN` and `IN` — that is, for every arithmetic, comparison
or logical expression. The two mitigations the analyzer owns are (a) classify
from AST node types/operators, not text, and (b) reconstruct text from child
spans. Neither requires touching omni.

Both mitigations are now implemented in `backend/plugin/lineage/mysql` (and its
mariadb/tidb copies): `exprTextOf` widens an infix node's span to the earliest
child offset, `isInfixNode` identifies the affected node types, and
`analyzeExpressionOperator` (`analyzer.go:1912`) decides the operation kind from
the typed AST — `CaseExpr`, `detectInfixOperator`, or the first function call with
its `Over` clause — never from a substring. This file remains the record of the
upstream defect itself, which is still unfixed.

## 8. MySQL-family dialect copies: AST gaps and a grammar/engine mismatch

Found while auditing AST field coverage across the MySQL-family analyzers (see
`lineage-analyzer.md` for the audit method). None of these is a defect in the MySQL parser; they are the reason the shared
traversal needs a per-dialect accessor, and they are recorded here so the next
reader does not mistake an accessor for dead weight.

### 8.1 TiDB: `SelectStmt` has no VALUES query primary

`omni/tidb/ast.SelectStmt` has no field for a VALUES query primary, and
`omni/tidb/parser` rejects the shape outright:

| SQL | `omni/mysql` | `omni/tidb` | MySQL 8.3 | TiDB |
| --- | --- | --- | --- | --- |
| `VALUES ROW((SELECT MAX(x) FROM other))` | parses | parses | executes | not checked |
| `SELECT * FROM (VALUES ROW(1), ROW((SELECT MAX(x) FROM other))) v(a)` | parses | `expected identifier (line 1, column 16)` | executes | not checked |

Consequence: the MySQL-family analyzer reads a VALUES query primary through
`valuesQueryPrimary(stmt)`, which returns `stmt.ValuesSource` for mysql/mariadb and
`nil` for tidb (the helper is declared in each dialect's own header, before the
shared body marker). The TiDB copy lists the statement in `knownParserGaps`
(`backend/plugin/lineage/tidb/analyze_test.go`), so the shared corpus stays honest
instead of being narrowed.

### 8.2 MariaDB: `InsertStmt` has no row alias

`omni/mariadb/ast.InsertStmt` has no `RowAlias`/`ColAliases` field and
`omni/mariadb/parser` rejects the syntax — correctly, because MariaDB has no row
alias form (it is MySQL 8.0.19 syntax; `INSERT … VALUES (…) AS new ON DUPLICATE KEY
UPDATE b = new.a` executes on MySQL 8.3 and is a syntax error on MariaDB 11.8.9).
The analyzer reads the form through `rowAliasNames(stmt)` (real accessor for
mysql/tidb, empty for mariadb) and the shared corpus case is listed in
`backend/plugin/lineage/mariadb/analyze_test.go`'s `knownParserGaps`.

### 8.3 MariaDB parser and MariaDB engine disagree about `VALUES`

| SQL | omni/mariadb | MariaDB 11.8.9 engine |
| --- | --- | --- |
| `VALUES ROW(1), ROW(2)` | parses | `ERROR 1064` (no `ROW()` form) |
| `VALUES (1), (2)` | `unexpected token` | executes |
| `SELECT * FROM (VALUES ((SELECT MAX(x) FROM other))) v(a)` | `unexpected token` | executes |

So the MariaDB grammar accepts a form the engine rejects and rejects the form the
engine accepts. The analyzer's behavior follows the parser: `VALUES ROW(…)` is
analyzed (and, for the subquery rows, produces lineage) while the MariaDB spelling
hard-fails. Neither outcome is a silent wrong answer, but the mismatch means the
mariadb dialect's reachable syntax is not what a MariaDB user would write. Worth
re-checking if the parser is ever aligned with the engine.

### 8.4 StarRocks: no `SelectStmt.WindowClause`, and an engine-rejected inline table

Two observations from the same batch, recorded for completeness:

- `omni/starrocks/ast.SelectStmt` has no `WindowClause` field. A named window has
  only `WindowSpec.Name` (the reference) and no definition site, so `OVER w` cannot
  be resolved in the StarRocks dialect at all — unlike PostgreSQL's
  `SelectStmt.WindowClause` + `WindowDef.Refname`. The MySQL-family dialects do
  have the field, and the analyzer now follows it.
- StarRocks 4.1 rejects an inline table whose row holds a subquery
  (`SELECT * FROM (VALUES ((SELECT MAX(x) FROM other))) v(a)` →
  `Required field 'node_type' was not present!`, a planner internal error), so the
  "FROM-side VALUES" residual recorded in the audit is an engine limitation rather
  than dropped lineage. Literal rows (`FROM (VALUES (1,2)) v(a)`) execute and name
  their columns `column_0`, `column_1` — the same 0-based placeholders the MySQL
  engines use.

## 9. MSSQL (T-SQL) parser and AST

Gathered while writing `backend/plugin/lineage/mssql` against the same pin, with a
throwaway probe package (`.probe-mssql/`, deleted afterwards: 87 numbered statements
plus 105 extra shapes) and by reading `mssql/parser` and `mssql/ast` in the module
cache. The dialect's own record of what it does about each item is
[mssql-lineage.md](mssql-lineage.md).

### 9.1 Two keywords are consumed as a table alias

| SQL | Observed | Consequence |
| --- | --- | --- |
| `FROM t1 NATURAL JOIN t2` | `JoinClause{Left: TableRef{Object: "t1", Alias: "NATURAL"}, Condition: nil}` | `t1` is addressed as `NATURAL`, so `t1.a` resolves to nothing; the join has no condition at all |
| `MERGE INTO tgt USING src ON …` | `MergeStmt.Target` is a `TableRef` with `Alias: "USING"` | the target is addressed as `USING`, so `tgt.a` resolves to nothing |

`MergeStmt.SourceAlias` is never populated either: an aliased source keeps its alias
on `TableRef.Alias`. Both mis-parses are silent — no error, no marker anywhere in the
AST. The dialect decides on the token stream instead (`relationAlias` /
`aliasIsFollowingClause` in `analyzer.go`): NATURAL is a clause only when `JOIN`
follows it, USING only when the row source it introduces follows, comments are not
tokens and a `WITH (…)` table hint is skipped whole. The distinction matters because
neither word is a T-SQL reserved keyword — only an ODBC and a future one — so
`FROM t NATURAL`, `FROM t USING` and `MERGE INTO [tgt] AS [USING]` are valid
statements whose alias has to be kept.

### 9.2 Many nodes carry `Loc.End == -1`, and the statement text depends on `Loc`

Measured with `sql[Loc.Start:Loc.End]` on each node:

| Node | `Loc` |
| --- | --- |
| set-operation wrapper `SelectStmt`, and its `Larg` | `End == -1` (the `Rarg` and an inner SELECT are exact) |
| `AliasedTableRef`, `SubqueryExpr` | `End == -1` |
| every name list — `*List` of `*String`: INSERT `Cols`, CTE `Columns`, view `Columns`, alias `Columns`, PIVOT `InValues` | `NoLoc` |
| `SetExpr.Column` (the UPDATE SET target) | `End == -1` |

The names are still readable (`.Str`, the typed fields), and expression nodes are
exact — `sql[7:12]` of `a + 1 AS b` is `a + 1`. Two consequences: any code that
renders a node's text must handle an invalid `Loc` (`mssql/ast.NodeToString` is a
debug comparison format, **not** a SQL deparser, and cannot be used for it), and
`Statement.Text`, which is derived from the previous statement's end offset, is
empty for a statement that follows a node with no `Loc.End`.

### 9.3 Grammar gaps and over-acceptance (all reproduced)

| Input | Result |
| --- | --- |
| `SELECT a FROM t1 JOIN t2 USING (a)` | syntax error at or near `(` |
| `SELECT * FROM OPENROWSET(BULK 'f.txt', SINGLE_CLOB) AS t` | syntax error at or near `BULK` |
| `SELECT * FROM dbo.fn(x) result(a INT, b INT)` | syntax error at or near `INT` |
| `CREATE VIEW v AS (SELECT a FROM t)` | syntax error at or near `(` |
| `(SELECT a FROM t1) UNION ALL (SELECT b FROM t2)` | syntax error at or near `(` |
| `CREATE VIEW v WITH CHECK OPTION AS SELECT a FROM t` | syntax error at or near `WITH` (the valid order, trailing `WITH CHECK OPTION`, parses) |
| `SELECT a, COUNT(*) FROM t GROUP BY ALL` | syntax error at end of input — the form is valid (if deprecated) T-SQL, and `SelectStmt.GroupByAll` exists in the AST, so the field is unreachable from a parsed statement |
| `SELECT * FROM t WITH (NOLOCK) x JOIN u ON …` | syntax error at or near `x` — **no** alias is accepted after a table hint, with or without `AS`, while `t NOLOCK JOIN u` parses (`NOLOCK` read as the alias). The hint itself is modelled; a hint *and* an alias together is a hard failure. Found while reviewing the alias workaround, not caused by it |
| `SELECT a FROM t AS x (NOLOCK)` | syntax error at or near `(` — the other order fails too: an alias **before** the hint, which is valid T-SQL, is not accepted either. Neither order can be worked around in the dialect: the failure is in the parser |
| `SELECT a, b = 1 FROM t` | **accepted** — `ResTarget{Name: "b"}` |
| `SELECT @v = a FROM t` | **accepted** — `ResTarget{Val: SelectAssign}` |

The two `SELECT … = …` rows are recorded in the module's own
`SCENARIOS-mssql-syntax-gaps.md` as gaps; they are not. `NATURAL JOIN` (§9.1) is the mirror image: accepted, with the
wrong tree.

### 9.4 Checked and *not* defects (the AST the dialect relies on)

- Set operations are distinguishable: `UNION ALL` sets `SelectStmt.All = true` where
  `UNION` leaves it false, both with `Op = SetOpUnion`. `All` doubles as `SELECT ALL`,
  so it only means "keeps duplicate rows" when `Op != SetOpNone`.
- Every query body is **typed**: a derived table is `AliasedTableRef >
  SubqueryExpr > *SelectStmt`, an expression subquery is `SubqueryExpr`,
  `InExpr.Subquery`, `ExistsExpr.Subquery` (a bare `*SelectStmt`) or
  `SubqueryComparisonExpr.Subquery`, a CTE body is `CommonTableExpr.Query`, and the
  walker in `mssql/ast/walk_generated.go` reaches all of them. No raw-SQL string field
  exists anywhere in the probed trees — unlike StarRocks, no body has to be re-parsed.
- `AliasedTableRef.Columns` and `CommonTableExpr.Columns` are populated
  (`FROM (SELECT a FROM t) AS d(x)`, `WITH c(x) AS (…)`), and `(VALUES (1),(2)) v(x)`
  parses — three shapes `SCENARIOS-mssql-syntax-gaps.md` records as gaps.
- Bracketed identifiers lose their brackets in the node (`[dbo].[t]` →
  `Schema "dbo"`, `Object "t"`), and `db.dbo.t.a` fills
  `Database/Schema/Table/Column`; `Loc` still recovers the literal text.
- A four-part name keeps its server: `SELECT a FROM srv.db.dbo.t` parses to
  `Server "srv"`, `Database "db"`, `Schema "dbo"`, `Object "t"`. The dialect decides
  what to do with the server — it drops it, on purpose (see
  [mssql-lineage.md](mssql-lineage.md)) — rather than the parser hiding it.
- `parser.Parse` returns `(*ast.List, error)`; there is **no** `ast.File` and **no**
  `mssql.Split` or `mssql.Tokenize` (the one-shot tokenizer is `parser.Tokenize`,
  and `parser.TokenName` names a token type). `mssql.Parse` splits and parses in one
  call and is the only thing that knows where a `GO` batch ends.
- Empty, whitespace-only and comment-only input all return `(nil, nil)` from
  `mssql.Parse`, and a UTF-8 BOM is a lexer error (`syntax error at or near "\ufeffSELECT"`
  — §11.6 corrects the shorter message this file used to quote).

## 10. What the bump itself changed

Not defects: deliberate upstream changes that this repo had to absorb. Each is one of the
engines' own rules, which is why it is recorded here rather than worked around.

- **`omni/starrocks/parser.Parse` is now `(*ast.File, error)`**, was
  `(*ast.File, []ParseError)`. `ParseError` lost `Loc`/`Msg` and gained
  `Message`/`Position`/`End`; the new `ParseErrors` slice type carries `Unwrap()` and
  renders a multi-error message as `first (and N more)`. This was the only API break in
  the packages this repo imports: the three call sites
  (`backend/plugin/lineage/starrocks/analyzer.go`, `rawsql.go`,
  `backend/plugin/dmltarget/target.go`) and one test moved to the single-`error` shape, so
  a StarRocks parse failure now surfaces the parser's first message rather than
  `&errs[0]`.
- **MySQL-family block comments no longer nest** — the `mysql`, `mariadb` and `tidb`
  lexers, and the `mysql`/`mariadb` splitters, now end a comment at the first `*/`, which
  is what MySQL and MariaDB do; `/*!…*/` executable comments are still depth-counted.
  A `/* a /* b */` spelling that used to hide the rest of the statement is now a syntax
  error, and the sanitizer's stored text for that input changed with it:
  `backend/plugin/sqlsanitize/testdata/02_comments.yaml` and the comment on
  `blockCommentEnd` were updated, and the MySQL family now behaves like StarRocks, which
  always ended the comment at the first `*/`.
- **PostgreSQL** (three changes, none of which reaches us as a wrong answer): `Segment.Empty`
  counts `\f` and `\v` as whitespace and ends a `--` comment at `\r`, pinned by
  `backend/plugin/lineage/postgresql/split_test.go`; an unterminated string, quoted
  identifier, comment or dollar-quote at the *start* of a statement is now an error where
  the lexer used to hand the parser a bare EOF token; and an expression node's `Loc.End` is
  now the end of its own last token instead of the start of the next one (the analyzer
  trims what it slices, so its expression text is unchanged). The second is the sharpest of
  the three for us: an analyzer handed `SELECT 1; 'abc` used to see the truncated tail as a
  clean end of input, count one statement and analyze `SELECT 1`; it now fails whole. That is
  stricter rather than wrong — the prefix analysis was the silent partial result the
  analyzers' contract forbids — but it is user-visible on the Lineage and Explain SQL RPCs,
  which pass caller text to the analyzer unsplit.
- **StarRocks strict `Parse`** additionally returns a nil `*ast.File` for a segment that
  carries any error, and re-parses every query body it captured as raw text
  (`validateRawQueries`). Both make the parser stricter and neither is silent; the same
  release also removed a panic we were exposed to, `CREATE TABLE t AS ` (no query), which
  used to slice out of range in `starrocks/parser/create_table.go`.
- **MSSQL** attaches the CTE list a leading `WITH` introduces to the statement that
  follows it, which is what the dialect's handlers read; it rejects a `WITH` that
  introduces nothing (bare `WITH c AS (…)`, a dangling comma after `XMLNAMESPACES`) and
  one in a nested query position; and `Walk`/`Inspect` in `mssql/ast` and `pg/ast` skip a
  typed-nil node instead of dereferencing it.
- **`mssql.Position.Column` and `pg.Position.Column` are now code-point columns**, not byte
  columns. Nothing in this repo reads `Statement.Start`/`End`/`Position`.

## 11. Found while re-running this record

All of these are pre-existing: each was reproduced identically on the previous pin, so none
arrived with the bump. They were simply not recorded before.

### 11.1 MySQL: an unknown word after a table is read as its alias, and the rest parses as a second statement

§3 records `SELECT a FROM t MINUS SELECT b FROM s` as set-operator over-acceptance. It is not
a set operation at all: `MINUS` is taken as the alias of `t`, and `Parse` then returns **two**
`SelectStmt`s while `Split` reports **one** segment. An arbitrary word behaves the same —
`SELECT a FROM t GARBAGE SELECT b FROM s` also yields two items, the first spanning
`SELECT a FROM t GARBAGE` — so a statement the engine rejects (column 30) becomes two accepted
statements, one of them carrying a fabricated alias. The analyzer is not misled, because it
parses one `Split` segment at a time and rejects the segment whose parse yields two items, but
a caller that hands `Parse` a whole script can be.

### 11.2 TiDB: the FROM-side `VALUES` gap is wider than §8.1 records

`SELECT * FROM (VALUES ROW(1)) v(a)` fails with `expected identifier` (column 16) — not only
the subquery-row spelling §8.1 lists — so the missing `SelectStmt` field is not the only
reachable consequence of the gap. Related, and recorded here for the same reason: the §8.3
row-3 SQL, `SELECT * FROM (VALUES ((SELECT MAX(x) FROM other))) v(a)`, is rejected by
`omni/mysql` too (`unexpected token`, column 23), where §8.3 names only MariaDB's engine.

### 11.3 MariaDB and TiDB panic on an executable comment that is never closed

`omni/mariadb/parser` and `omni/tidb/parser` panic with `slice bounds out of range` on any
input whose `/*!` executable comment does not close. Minimal reproductions, all of them
identical on both pins:

| Input | mysql | mariadb | tidb |
| --- | --- | --- | --- |
| `/*!` | `unterminated comment` | **panic** `[5:3]` | **panic** `[5:3]` |
| `/*!a/*b*/` | `unterminated comment` | **panic** `[11:9]` | **panic** `[11:9]` |
| `SELECT 1 /*! a /* b */ FROM t` | `unterminated comment` | **panic** `[31:29]` | **panic** `[31:29]` |

The crash is in `skipWhitespaceAndComments` (`mariadb/parser/lexer.go:1920`, the same line in
both pins): the versioned-comment branch finds no closing `*/`, leaves `end == len(input)` and
then slices `input[end+2:]`. The non-nesting rewrite below it did not touch this branch.

It is reachable from the request path: `lineage.Analyze(MARIADB|TIDB, sql)` panics on `/*!`,
which is why the family body converts a parser panic into the parse failure the analyzers
promise (`parseStatement`/`tokenizeStatement` in `backend/plugin/lineage/mysql/analyzer.go`,
generated into mariadb and tidb; pinned by the shared corpus case "an unterminated
executable comment is a parse failure, not a panic"). Without that, a caller-supplied
statement would be a crash path: the process survives it — `recoverMiddleware`
(`backend/server/echo_routes.go`) answers 500 and the lineage runner recovers per object and
schedules a retry — but nothing below the request would have reported what failed.

### 11.4 `CREATE PROCEDURE` parses to `CreateFunctionStmt`

`mysql/ast` has no procedure node: `DELIMITER // CREATE PROCEDURE p() BEGIN … END//` yields a
`*CreateFunctionStmt`. A consumer switching on the statement type must not read that type as
"function only". `mssql/ast` does distinguish `CreateProcedureStmt` from `CreateFunctionStmt`.

### 11.5 `mssql/ast.NodeLoc` panics on a typed-nil node

`NodeLoc` guards only the interface nil, then dereferences the pointer in each of its
concrete-type arms: `var p *ast.SelectStmt; ast.NodeLoc(p)` panics. Nothing in a parsed
`mssql.Parse` result is a typed-nil node today, so the exposure is latent rather than
reachable — but the arms are unguarded, `mssql/ast.SpanNodes` has the same exposure through
its `NodeLoc` call, and only the fallback branch `nodeLocFromField` nil-guards. That is why
the dialect normalizes every node through `nodeOrNil` before it dereferences one.

### 11.6 Two claims in this file were imprecise

- §9.3 says `SelectStmt.GroupByAll` "is unreachable from a parsed statement". Only the bare
  `GROUP BY ALL` fails; `GROUP BY ALL a` parses and sets the field (it is not valid T-SQL,
  which is a different complaint). Unchanged on both pins.
- §9.4 quotes the BOM lexer error as `syntax error at or near "\ufeff"`; the message is
  `syntax error at or near "\ufeffSELECT"`.

## 12. Doris parser: `VARIANT<…>` schema templates are rejected

Gathered while writing `backend/plugin/lineage/doris` against the same pin, with a
throwaway probe deleted afterwards, and confirmed against the official data-type
reference (the Schema Template form is documented there, so the input is valid
Doris, not a dialect mistake). The dialect's own record is
[doris-lineage.md](doris-lineage.md).

### 12.1 A VARIANT schema template does not parse

| Input | Result |
| --- | --- |
| `CREATE TABLE t (k BIGINT, v VARIANT)` | parses (a bare VARIANT column, `NULL` modifier included) |
| `CREATE TABLE t (k BIGINT, v VARIANT<'id': INT>)` | syntax error at or near `<` |
| `CREATE TABLE t (k BIGINT, v VARIANT<properties('variant_max_subcolumns_count' = '2048')>)` | syntax error at or near `<` |
| `CREATE TABLE t2 (k BIGINT, v VARIANT<'id': INT>) AS SELECT k, v FROM events` | syntax error at or near `<` — the CTAS form loses the query's lineage too |
| `ALTER TABLE t ADD COLUMN v2 VARIANT<'x': STRING>` | syntax error at or near `<` |

`doris/parser/datatypes.go` maps `kwVARIANT` to the type name `VARIANT` and stops
there: the `<` of the schema template is left to the statement grammar, which has
no production for it. The type name itself is therefore readable — only the
template is not.

Consequences, in the analyzer's terms: a view or materialized-view body never
contains a `CREATE TABLE`, so the registered VIEW / MATERIALIZED_VIEW paths are
unaffected. A MANUAL_SQL or APPLICATION_SQL statement that carries such DDL is a
**hard parse failure**: its stored lineage is cleared and `error_message` records
the parse error, which is the documented boundary for text the parser cannot read
("failed to parse Doris SQL: syntax error at or near `<`"). A CTAS with a schema
template is the one case that loses real lineage — the target columns are known
but the statement is discarded whole.

No patch, fork or upstream report, per the policy at the top of this file; the
input is recorded here so a future reader can tell "Doris feature not modelled"
from "omni cannot read this statement".

### 12.2 A LATERAL VIEW after a derived table does not parse

| Input | Result |
| --- | --- |
| `SELECT * FROM ev LATERAL VIEW explode(ev.arr) u AS elem` | parses |
| `SELECT * FROM (SELECT arr FROM ev) d LATERAL VIEW explode(d.arr) u AS elem` | syntax error at or near `LATERAL` |

Verified against a live Apache Doris FE **4.1.4** (the engine this repository's
development environment runs): it accepts the second statement and returns rows, so
this is valid Doris SQL omni cannot read. The statement hard-fails, which is the
documented boundary for text the parser rejects, but it is a coverage loss for a
shape an application may write.

**The Doris analyzer no longer loses that statement.** `backend/plugin/lineage/doris/lateral.go`
hoists the derived table into a CTE of the same name — a derived table read once and a
CTE read once are the same relation, and the rewritten statement parses — before
re-parsing. On 4.1.4 the two forms return the same rows under the same column names
(`arr`, `elem`), and both the bare and the `AS d` alias are handled. The hoist gives the
name the rank of a CTE, which outranks a base table of the same name, so the rewrite
refuses whenever the statement holds another relation by that name; it also handles one
such derived table only, at the statement's own level. The upstream defect stands and is
not patched.

`LATERAL VIEW OUTER` and a parenthesized `AS (c1)` list are the opposite: the same
engine rejects both (`extraneous input 'OUTER'`, `extraneous input '('`), so omni's
rejection matches the engine and neither is a defect.

### 12.3 `SELECT ... INTO OUTFILE` does not parse

| Input | Result |
| --- | --- |
| `SELECT a, b FROM t INTO OUTFILE 's3://bucket/x.csv'` | syntax error at or near `INTO`, and `*ast.IntoOutfileClause` does not exist in `doris/ast` |

The StarRocks parser accepts the same statement and models it. The live Doris
4.1.4 engine **parses** it and fails later, in planning: `Please specify BROKER
properties or check your local file path` for an `s3://` path, and `Exporting
results to local disk is not allowed. To enable this feature, you need to add
`enable_outfile_to_local=true` in fe.conf` for a local path. So the syntax is
Doris's, and omni cannot read it.

**The Doris analyzer no longer loses the statement to this defect.** The clause is a
result sink that carries no column lineage, so `backend/plugin/lineage/doris/outfile.go`
strips it and re-parses the query in front of it, which is what the StarRocks analyzer
already reports for the same statement (its AST has `IntoOutfileClause` and its
traversal ignores it). The upstream defect stands and is not patched; only the recovery
is ours. Verified against the live engine: `SELECT k FROM t INTO OUTFILE '…'` and the
same behind a `WITH` clause both fail on the export policy, i.e. after the parser.

### 12.4 Mixed bracket and dot paths do not parse

| Input | Result |
| --- | --- |
| `SELECT v['user'].name FROM ev` | syntax error at or near `.` |
| `SELECT v['user']['name'].x FROM ev` | syntax error at or near `.` |
| `SELECT v.user['name'] FROM ev` | parses (and the analyzer reads it as a path) |

The live Doris 4.1.4 engine accepts the first two — `v['a'].b` and
`v['a']['b'].c` are ordinary path expressions there, and `SELECT v['user'].name FROM ev`
answers `alice`, the same value as `SELECT v['user']['name'] FROM ev` — so omni cannot read
two forms an application may write, and the analyzer hard-fails them. Only the bracket-then-
bracket and the dot-then-bracket spellings parse.

**The Doris analyzer no longer loses those statements.**
`backend/plugin/lineage/doris/mixedpath.go` rewrites a dot that follows a *string*
subscript into the bracket form of the same key, which omni parses. `a[1].b` is left
alone: it reads a field of an array element, and the bracket form does not mean that, so
that shape keeps its hard failure. Because these two spellings are textually different,
the rewritten statement also keeps the text it was written from
(`source.original`/`edits` in `rawsql.go`), and an expression is named from it — the engine
names a column after the text the query holds (`SELECT v['user'].name FROM ev` reports the
column `v['user'].name`), so a rewritten spelling must not become a name of its own. The
upstream defect stands and is not patched.

### 12.5 Checked and *not* defects

- The materialized-view refresh spellings omni rejects are ones the engine rejects
  too. Doris 4.1.4 refuses `REFRESH ASYNC` (`extraneous input 'ASYNC'`, expecting
  `AUTO`/`COMPLETE`/…), so the 2.1-era `REFRESH ASYNC START(...) EVERY(...)` form is
  not valid 4.x SQL; the accepted header is `REFRESH AUTO ON SCHEDULE EVERY 1 DAY
  STARTS '…'`, which omni parses. omni and this engine agree, so there is nothing to
  record as an omni defect — only the analyzer's own guard, which refuses to build a
  name out of such a header (see doris-lineage.md).
- `SELECT * FROM t TABLESAMPLE(1000 ROWS) REPEATABLE 1` parses, while the
  parenthesized `REPEATABLE(1)` is rejected. The engine agrees: the unparenthesized
  spelling is the valid one, so `TableRef.Sample.Seed` is reachable exactly where
  Doris allows it.
- `CREATE VIEW v DEFINER = 'u' AS ...` is rejected at `DEFINER`; the engine rejects
  it too (`mismatched input 'DEFINER' expecting {'(', 'AS', 'COMMENT', 'SELECT',
  'VALUES', 'WITH'}`). The view-body fallback refuses this header rather than
  reading `DEFINER` — an identifier token, not a keyword — as the view's name.
- A declared column list in CTAS (`CREATE TABLE t (a INT, b INT) AS SELECT …`) is
  rejected by the engine (`Should not define the entire column in CTAS`), while
  omni's Doris grammar requires types for one. The analyzer reads the names when a
  list is present (harmless — the engine would reject the statement) and otherwise
  takes the query's own output aliases, which is what a valid CTAS creates.
