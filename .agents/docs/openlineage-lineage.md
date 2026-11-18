# OpenLineage SQL-facet lineage — Reference

> Maintenance reference for how an ingested `RunEvent` becomes lineage edges:
> `backend/plugin/openlineage/{processor,sql_facet_lineage,lineage_validation,lineage_revalidation,resolver}.go`.
> Related: [lineage-analyzer.md](lineage-analyzer.md) (the analyzers this path reuses),
> [lineage-semantics.md](lineage-semantics.md) (what the edges mean).

## What it is, and why the producer's column facet is not trusted

An Airflow SQL operator attaches a `sql` job facet **and** a `columnLineage` facet to its
OpenLineage events. That column facet is a *run-level guess*, not a statement's answer: the
extractor merges every statement of a task into one input/output set, attaches that single
facet to **every** output, and attributes an output column by finding a same-named column
anywhere in the input set. On the e2e fixture it claimed
`v_customer_360.paid_amount -> dwd_order_fact.paid_amount` for a statement that actually
reads `v_customer_payments` — alongside `_0…_12` positional placeholders and CTE aliases as
source relations. No existence-based check can catch that: both relations exist and the
column exists on both, so the claim is plausible and wrong.

Ingestion therefore **stops trusting the facet**. When an event carries SQL of its own, that
SQL runs through the statement-scoped analyzers this build already registers — the same ones
behind view definitions and manual SQL — and the statement's own column relations are stored
against the run.

## The path a COMPLETE run takes

`Processor.ProcessRunEvent` derives lineage only for a `COMPLETE` event, because that is
where the producer states what the run finally read and wrote.

| Condition | Path |
| --- | --- |
| An analyzer is wired and the event carries a `sql` facet | `sqlFacetLineage` → `analyzeEventSQL` |
| … and the SQL cannot be analyzed at all | `tableLevelLineage` (inputs × outputs, no column claims) plus a `slog.Warn` |
| No SQL facet of its own (DataX, a task with explicit facets) | `facetLineage` — the producer's own facet, unchanged |

Whatever the path, the edges pass `validateIngestedLineage` first: a column claim a known
table does not have is blanked and the edge survives as the table-level one the run really
implies.

## Re-checking edges after the fact

A relation the registry does not know is still stored and still shown. Keeping it is
deliberate — it cannot be told apart from a table created after the last schema sync, and
dropping it would lose real lineage permanently — and the graph reports it as an unresolved
relation of the type the edge claims (`lineageNodeWithMissingMeta`) rather than failing the
request with `CodeNotFound`, so one pending endpoint cannot break the whole graph.

Two sweeps then re-run ingestion validation over ingested edges, each covering one half of
the problem:

- **`RevalidateUnresolvedLineage`** (`FindColumnLineageWithUnknownEndpoint`) covers an edge
  whose endpoint is *still* unknown: object types are filled from the known side, a claim
  against that side is blanked, and what stays unknown is counted and logged rather than
  kept silent.
- **`RevalidateContradictedColumnClaims`** (`FindColumnLineageWithContradictedColumnClaim`)
  covers an edge whose endpoint *became* known: it names a column on a relation that is now
  a TABLE, and the table has no such column. This is the only pass that can still fix such
  a claim — the sync that registered the relation also took the edge out of the sweep above,
  so without it the wrong claim survived forever and waiting for the maintenance interval
  did not help.

Both replace a run's edge set as a set, so every edge of an affected run is validated
together. A schema sync that added or changed metadata signals the `lineagevalidation`
runner, which coalesces the signals of one sync round and runs the contradicted-claim pass —
a claim is fixed moments after the relation appears, not at the next maintenance interval
(6 h), so the pass deliberately runs on both paths. The guard is
`TestSchemaSyncRevalidatesIngestedLineageRealServerIntegration`, which drives a real server
through the sequence that exposed the gap: an edge against a table that does not exist yet,
then the table, then the sync. Only ingested edges are revalidated; the passes never touch
analyzer-owned edges.

## The dataset pages' column-lineage flag

The dataset list's `supports_column_lineage` and the detail drawer's badge answer "does this
deployment hold column-level lineage for this dataset", not "did the producer state one".
The flag is derived from `column_lineage`: the dataset's resolved GUID is an endpoint of an
edge that names a column on at least one side (`store.ListColumnLineageEndpoints`,
`store.ListColumnLineageColumnsByGUID`). The probe has no `meta_type` filter, so every writer
of the edge table counts — the OpenLineage paths above as well as the view/materialized-view
and manual-SQL analyzers — which is what the column-lineage page shows.

The flag is deliberately a **strict subset** of what that page can draw: a table-level edge
(both columns empty) is a real dependency but names no column, so it does not count, even
though `LineageService.GetLineage` would still list it. A row the list offers therefore
always has page content; the reverse is not promised.

The run-dataset references still keep `has_column_lineage` from the ingested facet, and
`openlineage_dataset.column_lineage_ref_count` still counts those references. They are no
longer the flag's source, but they are its fallback: when the resolver cannot identify a
dataset (a degraded instance or credential store fails `ListInstances`), the row has no real
GUID to probe, and it falls back to the facet signal instead of silently reporting "no
lineage" and emptying the filtered page. The per-field `column_lineage_ready` marker is
derived from the same edges: the target columns of the dataset's incoming edges and the
source columns of its outgoing ones.

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Trigger | an event whose `job.facets.sql.query` decodes to non-empty text | A producer that states column lineage without SQL keeps its own facet. |
| Dialect | the engine of the instance the anchor dataset resolves to | The SQL was written for the connection the statement ran on. |
| Anchor dataset | the first output, else the first input | A write names the connection; a read-only task has only inputs. |
| Default database/schema for unqualified names | parsed from the anchor dataset's name by the same rule that builds its GUID (`splitDatasetName`) | One rule, so a name splits the same way wherever it is read. |
| Statement results | temp targets (`__result__`) and synthetic markers (`__file__`) are skipped | They name no stored object; those edges describe a query. |
| Analysis failure | table-level edges plus a warning — never the facet's column claims | The run-level dependency is still real; the guess is not. |
| Column validation | unchanged, and it now also fills the source/target object type | A claim is kept only if the registry knows the object and the column. |

## Invariants

- The facet's column claims are never a fallback. Either the analyzer produced them, or the
  run has table-level edges.
- The analyzer is the process-wide `*lineage.Analyzer` built at startup, not a second
  instance: the SQL path and the view/manual-SQL path must agree on every dialect's
  semantics.
- A partial analysis keeps its edges and reports the gaps (`warnAnalysisGaps`); only
  genuinely unanalyzable SQL falls back to table level.
- What the producer's extractor keeps writing is out of scope — we record nothing upstream,
  we simply do not use its column claims.
- A stored edge is never silently dropped for being unverifiable; it is degraded to its
  table-level form and re-checked later.

## Failure modes

| Symptom | Behavior |
| --- | --- |
| SQL parses but a statement shape is unmodelled | Edges for what was modelled, a warning naming the gap; no invented source. |
| SQL unparseable, engine unregistered, or anchor dataset resolves to no instance | Table-level edges for the run, `slog.Warn`. |
| No SQL facet at all | The producer's facet is used as before. |
| Non-`COMPLETE` event | No lineage derived. |
| A claim names a column the registry does not have | Blanked, table-level edge kept (`validateIngestedLineage`). |
| A MySQL-wire dataset name carries three parts | `splitDatasetName` reads it as database.schema.table, so a producer that prefixes a qualified table with its connection's database (`e2e_ods.e2e_ads.v_x`) lands on an `<instance>;<database>;<schema>;<table>` GUID no sync registers — the instance still resolves, so nothing warns. The e2e fixture names the database once, in the reader URL, which is the workaround a producer with the same shape needs. |
| `openlineage-sql` cannot parse `REFRESH MATERIALIZED VIEW` or `CREATE MATERIALIZED VIEW … WITH DATA` | The database executes them, so the task is green and only lineage inference failed: the run records an `extractionError` facet instead of those datasets. The run detail page names each failed statement and its reason (`extractOpenLineageExtractionErrors`), so the loss is visible; the object's own lineage still comes from its stored definition, so only the run's input/output set is thinner. `DROP MATERIALIZED VIEW … CASCADE` started parsing in 1.53.0; the other two have no upstream support. |
| One event carries the whole SQL script | The extractor still merges every statement of a task into one input/output set and one facet. The analyzer splits the script itself, so column lineage is unaffected, but the run record reports one set, and a producer that states lineage *without* SQL keeps the old facet semantics. |

## Where things live

| Path | Holds |
| --- | --- |
| `backend/plugin/openlineage/processor.go` | `NewProcessor`, `ProcessRunEvent`, `sqlFacetLineage`, `facetLineage`, `tableLevelLineage`, `hasSQLFacet`, `warnMissingSQLLineage` |
| `backend/plugin/openlineage/sql_facet_lineage.go` | `sqlFacetQuery`, `anchorDataset`, `analyzeEventSQL`, `analysisContextFor`, `databaseAndSchemaFromGUID`, `mapAnalyzedRelations`, `isSyntheticRelation`, `warnAnalysisGaps` |
| `backend/plugin/openlineage/resolver.go` | `Resolver`, `splitDatasetName` (the one name-splitting rule), `buildGUID`, `isMySQLLike` |
| `backend/plugin/openlineage/lineage_validation.go` | `validateIngestedLineage` / `filterIngestedLineage` — the registry check that blanks unverifiable column claims and keeps the table-level edge |
| `backend/plugin/openlineage/lineage_revalidation.go` | `RevalidateUnresolvedLineage`, `RevalidateContradictedColumnClaims`, `affectedRuns` |
| `backend/plugin/openlineage/lineage_revalidation.go` consumers | `backend/runner/maintenance/` (6 h sweeps) and the `lineagevalidation` runner (sync-triggered pass) |
| Tests | `sql_facet_lineage_test.go`, `lineage_revalidation_test.go`, `backend/test/integration/runner/openlineage_sql_facet_service_test.go`, `backend/test/integration/runner/openlineage_lineage_revalidation_service_test.go`; five-engine fixture `test/e2e_etl` |
| Gate | `go test ./backend/plugin/openlineage/...`; `go test -count=1 -tags=integration -run 'OpenLineageSqlFacet' ./backend/test/integration/runner` |
