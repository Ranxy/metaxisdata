# Plan: TiDB and MariaDB lineage dialects (MySQL-family analyzers)

> **Status: implemented.** TiDB and MariaDB analyzers landed as their own
> packages, modeled on the omni-backed MySQL analyzer. OceanBase is dropped.

## TL;DR

The MySQL analyzer (`backend/plugin/lineage/mysql`) is built on
`github.com/bytebase/omni`'s MySQL parser + typed AST. This plan adds the two
remaining MySQL-family dialects, **one package each**, because omni ships a
separate parser and AST per dialect:

| Engine | Package | omni parser + AST | Corpus |
| --- | --- | --- | --- |
| `MYSQL` | `backend/plugin/lineage/mysql` | `omni/mysql/{parser,ast}` | 73/73 |
| `TIDB` | `backend/plugin/lineage/tidb` | `omni/tidb/{parser,ast}` | 73/73 |
| `MARIADB` | `backend/plugin/lineage/mariadb` | `omni/mariadb/{parser,ast}` | 69/73 (4 skipped) |
| `OCEANBASE` | — | — | **dropped, no analyzer** |

## Decisions

- **One package per dialect.** omni's dialect ASTs already diverge (MariaDB adds
  `Returning`/`ForPortionOf`/temporal `SystemTime`; TiDB omits
  `TableSource`/`ValuesSource`/`Quantifier`), so each package is the place for
  dialect-specific handling. A single shared analyzer parameterised over the AST
  is not expressible in Go without an adapter/IR layer, which the MySQL migration
  deliberately avoided.
- **Dialect copies, not a generator.** `tidb/analyzer.go` and
  `mariadb/analyzer.go` are copies of `mysql/analyzer.go` differing only in the
  package clause, the two omni import paths, and the registered engine. The
  traversal is source-compatible because every AST difference is an addition or
  removal of fields the analyzer does not read (verified type by type).
- **The shared golden corpus is the sync guard.** All three packages run the same
  statements from `backend/plugin/lineage/mysql/testdata/analyze/` (73 cases).
  Behavioral drift between the dialects fails a test rather than a code review.
- **Parser gaps are explicit, never silent.** Where a dialect's omni parser
  cannot parse a corpus statement, the package names it in a `knownParserGaps`
  map passed to `testutil.RunLineageTestSuitesFromYAMLDirSkipping`, with a comment
  explaining why.
- **OceanBase has no analyzer.** `Engine_OCEANBASE` resolves to
  `ErrorEngineNotSupported`; the runner records a deliberate per-object skip.
  OceanBase is out of scope by decision, not pending work.

## Findings

### AST compatibility (what made the copies possible)

Type-by-type comparison of `mysql/ast` against `tidb/ast` and `mariadb/ast` for
every node the analyzer touches showed only additive/removable differences:

- `SelectStmt`: TiDB lacks `TableSource`/`ValuesSource` (unused by the analyzer).
- `InsertStmt`/`DeleteStmt`/`CreateTableStmt`: MariaDB adds `Returning`,
  `ForPortionOf`, period columns (unused).
- `UpdateStmt`: MariaDB adds `ForPortionOf` (unused).
- `LoadDataStmt`: TiDB/MariaDB lack the Aurora S3 fields (unused).
- `SubqueryExpr`: TiDB lacks `Quantifier` (unused).
- `OrderByItem`: TiDB lacks `Direction`; only `Expr` is read.
- `TableRef`: MariaDB adds `SystemTime` (unused).

No field the analyzer reads is renamed or removed, so the traversal compiles and
behaves identically on all three.

### MariaDB parser gap: join trees nested three or more parentheses deep

omni's MariaDB parser accepts a parenthesised join tree nested up to **two**
levels; the third level fails with
`expected SELECT, TABLE, VALUES, or '('`. omni's MySQL parser accepts any depth:

```
SELECT * FROM (t1 JOIN t2 ON ...)                          mysql=ok  mariadb=ok
SELECT * FROM ((t1 JOIN t2 ON ...))                        mysql=ok  mariadb=ok
SELECT * FROM (((t1 JOIN t2 ON ...)))                      mysql=ok  mariadb=FAIL
SELECT * FROM (((t1 JOIN t2 ON ...)) LEFT JOIN t3 ON ...)  mysql=ok  mariadb=FAIL
```

This matters in production: **mysqldump emits three-level parenthesised join
trees for views**, so such MariaDB views hard-fail lineage analysis (per the
hard-fail parse policy) until omni's parser is fixed. The four affected corpus
cases are listed in `backend/plugin/lineage/mariadb/analyze_test.go`
(`knownParserGaps`) so the gap is visible and testable, not hidden by a narrowed
corpus.

## Verification

1. `go test ./backend/plugin/lineage/... -count=1` — TiDB 73/73, MariaDB 69/73
   (4 recorded gaps), MySQL unchanged.
2. `go test ./... -count=1` (hermetic) green.
3. `golangci-lint run --allow-parallel-runners` clean; `gofmt -l` clean.
4. Release build.
5. Registration: `backend/server/ultimate.go` blank-imports all four lineage
   packages, so `MYSQL`/`TIDB`/`MARIADB` resolve to their analyzers and
   `OCEANBASE` resolves to `ErrorEngineNotSupported`.

## Follow-ups

- **Report the MariaDB nested-paren gap upstream** to `github.com/bytebase/omni`.
  When fixed, drop the corresponding entries from `knownParserGaps` and the corpus
  count returns to 73/73.
- **Dialect-specific lineage** (not needed for parity): MariaDB `RETURNING`,
  application-time `FOR PORTION OF`, system-versioned `FOR SYSTEM_TIME`.
- **Benchmarks** for the two dialects (the MySQL package has them) if dialect
  performance needs tracking.
- **OceanBase** stays unsupported until explicitly re-scoped.
