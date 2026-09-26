# Plan: derive SQL-facet lineage with our own analyzer

> **Status: implemented.** The behaviour is covered by hermetic unit tests, a
> real-server integration test, and the three-engine fixture in `test/e2e_etl`
> (see `test/e2e_etl/FINDINGS.md`).

## TL;DR

An Airflow SQL operator attaches a `sql` job facet and a `columnLineage` facet to
its OpenLineage events. That column facet is a **run-level guess**, not a
statement's answer: the extractor merges every statement of a task into one
input/output set, attaches that single facet to *every* output, and attributes an
output column by finding a same-named column anywhere in the input set.

Measured on the e2e fixture (one task, two `INSERT … SELECT`s over four views),
the stored lineage said

```
v_customer_360.paid_amount -> dwd_order_fact.paid_amount      (wrong: the SQL reads v_customer_payments)
v_customer_360.paid_amount -> dwd_customer_360.paid_amount    (right: that statement does read v_customer_360)
```

— the same claim, right for one output and wrong for the other, plus `_0…_12`
positional placeholders and CTE aliases as source relations. No existence-based
validation can catch the wrong claim: both relations exist, the column exists on
both, so it is *plausible* and *wrong*, which is worse than visibly bogus.

This plan stops trusting the facet. When an event carries a `sql` facet,
ingestion runs that SQL through the analyzers this build already registers — the
same statement-scoped analyzers that back view definitions and manual SQL — and
stores the statement's own column relations against the run. When the SQL cannot
be analyzed, ingestion stores only the run-level dependencies (inputs × outputs,
no column claims) and says so.

## Decisions

| Decision | Choice |
| --- | --- |
| Trigger | an event whose `job.facets.sql.query` decodes to non-empty text |
| Dialect | the engine of the instance the anchor dataset resolves to |
| Anchor dataset | the first output, else the first input |
| Default database/schema for unqualified names | parsed from the anchor dataset's name, by the same rule that builds its GUID |
| Statement results | temp targets (`__result__`) and synthetic markers (`__file__`) are skipped: they name no stored object |
| Analysis failure | fall back to table-level edges (inputs × outputs) + a warning; the facet's column claims are never used |
| Column validation | unchanged: registry digest lookups still drop claims a known table does not have, and now also fill the source/target object type |
| Events without a `sql` facet | unchanged facet path (DataX and any producer that states its own column lineage) |

## Steps

1. Thread the process-wide `*lineage.Analyzer` (built once in `server.go`) into
   the OpenLineage handler and processor instead of building a second one.
2. `sqlFacetQuery` (decode the facet) and `splitDatasetName` (one rule shared
   with `buildGUID`, so a name splits the same way wherever it is read).
3. `mapAnalyzedRelations` (pure: relations → stored edges) and
   `analyzeEventSQL` (engine/context resolution + analysis).
4. `ProcessRunEvent` picks one of three paths: analyzer edges, table-level
   fallback, or the untouched facet path.
5. Tests: hermetic unit tests for the decode/split/mapping/type filling, plus a
   real-server integration test where an `INSERT … SELECT` joins two views that
   share a column name and the stored source must be the one the SQL reads.
6. Re-verify on the fixture: `dwd_order_fact.paid_amount` must come from
   `v_customer_payments`, `dwd_customer_360.paid_amount` from `v_customer_360`,
   no `_N` placeholders, no CTE phantom relations.

## Verification

```bash
go test ./backend/plugin/openlineage/ ...
go test -count=1 -tags=integration -run 'OpenLineageSqlFacet' ./backend/test/integration/runner
cd test/e2e_etl && ./run_chain.sh && PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f verify_chain.sql
```

## Out of scope

The Airflow provider's extractor keeps producing the run-level facet; we simply
stop using its column claims and record nothing upstream.
