# backend/plugin/lineage/AGENTS.md

Table- and column-level lineage analyzers. The root [AGENTS.md](../../../AGENTS.md) and [backend/AGENTS.md](../../AGENTS.md) still apply; this file wins for anything under `backend/plugin/lineage/`.

## How Analyzers Are Wired

- Each dialect exports a `Registration`; the supported set is assembled explicitly in `engines/` and built into one `lineage.Analyzer` at startup. Nothing self-registers from `init`.
- Registered dialects: MySQL, TiDB, MariaDB, PostgreSQL, StarRocks. Doris is **not** registered.
- A partial analysis reports structured `model.Diagnostic` gaps beside the edges it did find. Do not turn a gap into a silent drop or a hard failure — downstream callers rely on diagnostics to distinguish "no lineage" from "could not analyze".

## Generated MySQL-Family Analyzers

TiDB and MariaDB have no traversal of their own. The shared body lives only in `mysql/analyzer.go` below the `// == MYSQL-FAMILY SHARED BODY ==` sentinel, and `tidb/analyzer_body_gen.go` and `mariadb/analyzer_body_gen.go` are generated from it.

- Edit the shared body in `mysql/analyzer.go`; never hand-edit the generated `analyzer_body_gen.go` files.
- After editing below the sentinel, regenerate:

```bash
go generate ./backend/plugin/lineage/mysql
```

## Design Docs

- `plan/lineage_analyze_plan.md`, `plan/lineage_analyzer_wiring_plan.md` — analyzer design and startup wiring.
- `plan/lineage_mysql_family_generation_plan.md`, `plan/mysql_family_dialect_lineage_plan.md` — the shared-body generation and dialect coverage.
- `plan/lineage_package_architecture_review.md`, `plan/lineage_ast_field_coverage_audit.md`, `plan/lineage_transformation_model.md` — architecture, AST coverage, and the transformation model.
