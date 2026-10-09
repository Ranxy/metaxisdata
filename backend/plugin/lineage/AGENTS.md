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

- [.agents/docs/lineage-analyzer.md](../../../.agents/docs/lineage-analyzer.md) — package layout, assembly, runner lifecycle, the MySQL-family generator, and the AST coverage audit method.
- [.agents/docs/lineage-semantics.md](../../../.agents/docs/lineage-semantics.md) — what each dialect must keep producing: the transformation model, per-dialect semantics, engine-measured clause visibility.
- [.agents/docs/lineage-graph.md](../../../.agents/docs/lineage-graph.md) — the field trail and field-scoped expansion on the graph page.
- [.agents/docs/omni-upstream-defects.md](../../../.agents/docs/omni-upstream-defects.md) — upstream parser/AST defects and the deliberate no-patch policy.
