# OpenLineage SQL-facet lineage — Reference

> Status: **implemented**. Maintenance reference for how an ingested `RunEvent` becomes lineage edges: `backend/plugin/openlineage/{processor,sql_facet_lineage,lineage_validation,resolver}.go`.
> Related: [lineage-analyzer.md](lineage-analyzer.md) (the analyzers this path reuses), [lineage-semantics.md](lineage-semantics.md) (what the edges mean), [omni-upstream-defects.md](omni-upstream-defects.md).

## What it is

An Airflow SQL operator attaches a `sql` job facet **and** a `columnLineage` facet to its OpenLineage events. That column facet is a *run-level guess*, not a statement's answer: the extractor merges every statement of a task into one input/output set, attaches that single facet to **every** output, and attributes an output column by finding a same-named column anywhere in the input set.

On the e2e fixture (one task, two `INSERT … SELECT`s over four views) the facet claimed `v_customer_360.paid_amount -> dwd_order_fact.paid_amount` — a statement that actually reads `v_customer_payments` — while the other output's claim was right, alongside `_0…_12` positional placeholders and CTE aliases as source relations. No existence-based validation can catch it: both relations exist and the column exists on both, so the claim is plausible and wrong.

So ingestion **stops trusting the facet**. When an event carries SQL of its own, that SQL is run through the statement-scoped analyzers this build already registers — the same ones behind view definitions and manual SQL — and the statement's own column relations are stored against the run.

## The paths a COMPLETE run takes

`ProcessRunEvent` (`backend/plugin/openlineage/processor.go:51`) derives lineage only for a `COMPLETE` event, because that is where the producer states what the run finally read and wrote.

| Condition | Path |
| --- | --- |
| An analyzer is wired and the event carries a `sql` facet (`processor.go:64`, `:220`) | `sqlFacetLineage` (`:112`) → `analyzeEventSQL` (`sql_facet_lineage.go:66`) |
| … and the SQL cannot be analyzed at all | `tableLevelLineage` (inputs × outputs, no column claims) plus a `slog.Warn` (`processor.go:120`) |
| No SQL facet of its own (DataX, a task with explicit facets) | `facetLineage` (`:129`) — the producer's own facet, unchanged |

Whatever the path, the edges pass `validateIngestedLineage` (`lineage_validation.go`) first: a column claim a known table does not have is blanked and the edge survives as the table-level one the run really implies (`:48`, `:127`).

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Trigger | an event whose `job.facets.sql.query` decodes to non-empty text (`sql_facet_lineage.go:27`) | A producer that states column lineage without SQL keeps its own facet. |
| Dialect | the engine of the instance the anchor dataset resolves to (`:115`) | The SQL was written for the connection the statement ran on. |
| Anchor dataset | the first output, else the first input (`:49`) | A write names the connection; a read-only task has only inputs. |
| Default database/schema for unqualified names | parsed from the anchor dataset's name by the same rule that builds its GUID (`:142`, `resolver.go:478`) | One rule, so a name splits the same way wherever it is read. |
| Statement results | temp targets (`__result__`) and synthetic markers (`__file__`) are skipped (`:154`, `:187`) | They name no stored object; those edges describe a query. |
| Analysis failure | table-level edges plus a warning — never the facet's column claims (`processor.go:120`) | The run-level dependency is still real; the guess is not. |
| Column validation | unchanged, and it now also fills the source/target object type | A claim is kept only if the registry knows the object and the column. |

## Invariants

- The facet's column claims are never a fallback. Either the analyzer produced them, or the run has table-level edges.
- The analyzer is the process-wide `*lineage.Analyzer` built at startup, not a second instance: the SQL path and the view/manual-SQL path must agree on every dialect's semantics.
- A partial analysis keeps its edges and reports the gaps (`warnAnalysisGaps`, `sql_facet_lineage.go:193`); only genuinely unanalyzable SQL falls back to table level.
- What the producer's extractor keeps writing is out of scope — we record nothing upstream, we simply do not use its column claims.

## Failure modes

| Symptom | Behavior |
| --- | --- |
| SQL parses but a statement shape is unmodelled | Edges for what was modelled, a warning naming the gap; no invented source. |
| SQL unparseable, engine unregistered, or anchor dataset resolves to no instance | Table-level edges for the run, `slog.Warn` at `processor.go:120`. |
| No SQL facet at all | The producer's facet is used as before. |
| Non-`COMPLETE` event | No lineage derived (`processor.go:52`). |
| A claim names a column the registry does not have | Blanked, table-level edge kept (`lineage_validation.go:48`). |

## Where things live

| Path | Holds |
| --- | --- |
| `backend/plugin/openlineage/processor.go` | `ProcessRunEvent` (`:51`), `hasSQLFacet` (`:220`), `sqlFacetLineage` (`:112`), the unchanged `facetLineage` (`:129`). |
| `backend/plugin/openlineage/sql_facet_lineage.go` | `sqlFacetQuery` (`:27`), `anchorDataset` (`:49`), `analyzeEventSQL` (`:66`), `analysisContextFor` (`:115`), `databaseAndSchemaFromGUID` (`:142`), `mapAnalyzedRelations` (`:154`), `isSyntheticRelation` (`:187`), `warnAnalysisGaps` (`:193`). |
| `backend/plugin/openlineage/resolver.go` | `splitDatasetName` (`:478`) — the one name-splitting rule. |
| `backend/plugin/openlineage/lineage_validation.go` | The registry check that blanks unverifiable column claims and keeps the table-level edge. |
| Tests | `sql_facet_lineage_test.go` (`:15`, `:42`, `:63`, `:101`); real-server `backend/test/integration/runner/openlineage_sql_facet_service_test.go:30`; three-engine fixture `test/e2e_etl` (`run_chain.sh`, `verify_chain.sql`, `FINDINGS.md`). |
| Gate | `go test ./backend/plugin/openlineage/...`; `go test -count=1 -tags=integration -run 'OpenLineageSqlFacet' ./backend/test/integration/runner`. |
