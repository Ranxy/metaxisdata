# Local record: `github.com/bytebase/omni` MySQL parser/AST defects

> **Purpose.** A local record of defects found in the pinned upstream dependency
> while auditing `backend/plugin/lineage/mysql`. **Nothing here is to be sent,
> filed, posted or otherwise communicated upstream, and the module is not to be
> modified, patched, forked or vendored.** The file exists so the knowledge is not
> lost and so the analyzer's workarounds are justified in-repo. If upstream is
> ever contacted, that is a human decision made outside this record.
>
> Module: `github.com/bytebase/omni v0.0.0-20260912023254-4574e69bb9f1`
> (`go.mod`). Packages inspected: `mysql/parser` (`Parse`, `Tokenize`, `Split`)
> and `mysql/ast`.

## How this was gathered

Every claim below was produced by running throwaway probes (a scratch module and
a temporary `zz_probe_*.go` test inside the mysql package, both deleted
afterwards) and reading the omni source in the module cache. No repository or
module file was modified. Build note for anyone reproducing this: the default
`~/.cache/go-build` may be read-only in this environment; use
`GOCACHE=/tmp/gocache-probe`.

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
from the operator's operands) rather than the parent span. See
`plan/mysql_lineage_optimization_plan.md` §2.6 and §3.1.

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

- The assertion `interface{ LocStart() int; LocEnd() int }` fails for **all 48**
  concrete node types probed, including every type the analyzer touches.
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
on the per-expression hot path. See `plan/mysql_lineage_optimization_plan.md` §3.2.

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
  A statement-text prefix check is the only local signal. Recorded downstream in
  `plan/mysql_lineage_optimization_plan.md` §2.10.
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

## 6. MariaDB parser gap (already recorded elsewhere)

`omni/mariadb` accepts a parenthesised join tree nested at most two levels; the
third level fails with `expected SELECT, TABLE, VALUES, or '('`, while
`omni/mysql` accepts any depth. `mysqldump` emits three-level trees for views, so
affected MariaDB views hard-fail analysis. This is recorded in
`plan/mysql_family_dialect_lineage_plan.md` and pinned by
`backend/plugin/lineage/mariadb/analyze_test.go` (`knownParserGaps`). Note that
nothing asserts the skip list is still needed, so if the parser is fixed the four
cases stay skipped silently.

## 7. Checked and *not* defects

Recording these so nobody re-investigates them as bugs:

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

## 8. Summary for the analyzer

The parser is good enough for AST-walk-based lineage: column edges are correct
even where `Loc` is not. Everything **text-derived** from `Loc` is wrong for
infix expressions, `BETWEEN` and `IN` — that is, for every arithmetic, comparison
or logical expression. The two mitigations the analyzer owns are (a) classify
from AST node types/operators, not text, and (b) reconstruct text from child
spans. Neither requires touching omni.

Both mitigations are now implemented in `backend/plugin/lineage/mysql` (and its
mariadb/tidb copies): `exprTextOf` widens an infix node's span to the earliest
child offset, `isInfixNode` identifies the affected node types, and
`analyzeExpressionOperator` decides the operation kind from `BinaryExpr.Op`,
`UnaryExpr.Op` or the `CaseExpr` node rather than from a substring. This file
remains the record of the upstream defect itself, which is still unfixed.

## 9. MySQL-family dialect copies: AST gaps and a grammar/engine mismatch

Found while fixing the A4–A10 findings of the AST field coverage audit
(`plan/lineage_ast_field_coverage_audit.md`, `plan/postgresql_lineage_package_review.md`
§0i). None of these is a defect in the MySQL parser; they are the reason the shared
traversal needs a per-dialect accessor, and they are recorded here so the next
reader does not mistake an accessor for dead weight.

### 9.1 TiDB: `SelectStmt` has no VALUES query primary

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

### 9.2 MariaDB: `InsertStmt` has no row alias

`omni/mariadb/ast.InsertStmt` has no `RowAlias`/`ColAliases` field and
`omni/mariadb/parser` rejects the syntax — correctly, because MariaDB has no row
alias form (it is MySQL 8.0.19 syntax; `INSERT … VALUES (…) AS new ON DUPLICATE KEY
UPDATE b = new.a` executes on MySQL 8.3 and is a syntax error on MariaDB 11.8.9).
The analyzer reads the form through `rowAliasNames(stmt)` (real accessor for
mysql/tidb, empty for mariadb) and the shared corpus case is listed in
`backend/plugin/lineage/mariadb/analyze_test.go`'s `knownParserGaps`.

### 9.3 MariaDB parser and MariaDB engine disagree about `VALUES`

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

### 9.4 StarRocks: no `SelectStmt.WindowClause`, and an engine-rejected inline table

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
