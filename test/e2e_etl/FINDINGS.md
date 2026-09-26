# Findings from the first full three-engine lineage run

Scope: MySQL `e2e_ods` → PG `e2e` (`e2e_ods` → `e2e_dwd`/`e2e_ads`) → StarRocks
`e2e_ods`/`e2e_ads`, driven by Airflow DAGs `e2e_01`…`e2e_04`, with lineage
coming from three producers: the MySQL/PG/StarRocks definition analyzers, the
DataX OpenLineage provider, and the Airflow SQL operator OpenLineage extractor.

All four DAG runs are green and every hop reconciles row-for-row. The lineage
data (849 edges for the `e2e` objects, 63 OpenLineage runs, 18 analysed objects,
0 analyzer errors) was then checked against the registry.

**Status of the chain: 3 of 4 hops are internally consistent; the PostgreSQL →
StarRocks hop is attached to the wrong StarRocks GUIDs (F1), and the column-level
part of the Airflow SQL path is not trustworthy (F3/F4).**

Reproduction: the fixture was wiped with `install.sh --bootstrap` and the chain
re-run through `run_chain.sh` afterwards (4 DAGs, 26 tasks, all green, 50 s).
Row counts and every finding below reproduced byte-for-byte — same hop edge
counts (5 / 8 / 44 / 5 / 4), same 5 phantom StarRocks GUIDs, same 2 CTE
phantoms, 0 analyzer errors.

---

## F1 — BLOCKER: the StarRocks side of the DataX hop resolves to non-existent GUIDs

**Symptom.** Every PostgreSQL → StarRocks edge lands on `starrocks-dev-1;;e2e_ods;<table>`
(database *empty*, schema `e2e_ods`). The registry knows those tables as
`starrocks-dev-1;e2e_ods;;<table>`. The chain walk therefore reaches the
phantom IDs and stops; starting from the real StarRocks object finds nothing:

```
=== 3a. upstream column provenance of the REAL starrocks guid
 depth | source_guid | source_column | target_guid | target_column | relation_type
-------+-------------+---------------+-------------+---------------+---------------
(0 rows)

=== 3b. same walk reaching the PHANTOM guid the DataX event actually wrote
 0 | test-pg-1;e2e;e2e_dwd;mv_daily_sales | net_line_amount | starrocks-dev-1;;e2e_ods;mv_daily_sales | net_line_amount | 1
 1 | test-pg-1;e2e;e2e_dwd;v_order_line   | line_amount     | test-pg-1;e2e;e2e_dwd;mv_daily_sales    | net_line_amount | 4 (AGGREGATE)
 2 | test-pg-1;e2e;e2e_ods;order_items    | line_amount     | test-pg-1;e2e;e2e_dwd;v_order_line      | line_amount     | 1
 3 | mysql-dev-1;e2e_ods;;order_items     | line_amount     | test-pg-1;e2e;e2e_ods;order_items       | line_amount     | 1
```

**Root cause.** `backend/plugin/openlineage/resolver.go:288` `isMySQLLike()`
lists only `MYSQL`, `TIDB`, `MARIADB`, `OCEANBASE`, so `buildGUID()`
(`resolver.go:260`) parses a two-part dataset name for a `STARROCKS` instance as
`schema.table` instead of `database.table`. The DataX StarRocks writer plugin
emits `e2e_ods.dwd_order_fact` (the same convention the MySQL/Postgres plugins
use), which becomes `instance;;e2e_ods;dwd_order_fact`.

**Proof (controlled replay).** Re-posting the identical event with a three-part
dataset name resolves to the real GUID:

```
name = "e2e_ods..dwd_order_fact"  -> target_guid = starrocks-dev-1;e2e_ods;;dwd_order_fact   (exists in registry)
name = "e2e_ods.dwd_order_fact"   -> target_guid = starrocks-dev-1;;e2e_ods;dwd_order_fact   (does not exist)
```

**Impact.** 5 phantom nodes and 5 broken table edges; the whole StarRocks leg is
invisible from the metadata browser and from column-level provenance.
`verify_chain.sql` section 4 lists them.

**Suggested fix (one line + test).** Add `STARROCKS` (and `DORIS`, which has the
same addressing model) to `isMySQLLike`. Alternatively, make the resolver treat
"two-part name on a non-MySQL-like engine" as database.table when the instance's
own synced GUIDs use an empty schema segment. Either way, a resolver that
produces a GUID with no `meta_registry_resource` row should raise a diagnostic
instead of silently storing the edge.

---

## F2 — HIGH: `localhost` vs `127.0.0.1` silently downgrades internal tables to external datasets

**Symptom.** With the Airflow connection host set to `127.0.0.1` while the
`test-pg-1` instance is registered with host `localhost`, the SQL-operator
lineage was written as **external** datasets:

```
external:postgres://127.0.0.1:5432:e2e.e2e_dwd.v_order_base -> external:postgres://127.0.0.1:5432:e2e.e2e_dwd.dwd_order_fact
... 152 edges / 12 external datasets
```

Pointing the connection at `localhost` (and leaving the SQL untouched) made the
same edges resolve to the internal tables. No warning was emitted in either case.

**Root cause.** `resolver.go:247` `matchHostPort()` compares host strings with
`strings.EqualFold`; an alias is never normalised, and `resolveByAutoMatch`
simply falls through to "external dataset". The DataX jobs happened to use
`localhost` (so hop 1 matched) while the Airflow connection used `127.0.0.1`,
which is why only half the chain broke.

**Impact.** Self-owned tables appear as external assets, the graph shows
duplicate-looking nodes, and column lineage for that hop is orphaned. This is
the most likely failure a real user hits, because host naming is inconsistent
between JDBC URLs, Airflow connections and the instance registration form.

**Suggested fix.** Normalise loopback aliases (`localhost`, `127.0.0.1`, `::1`)
and, when a namespace host resolves to no instance but is a known alias of a
registered data source, log/emit a warning ("namespace postgres://127.0.0.1:5432
did not match instance test-pg-1 (localhost:5432); add a namespace mapping to
associate them").

---

## F3 — HIGH: the Airflow SQL path drops *all* lineage, silently, when objects are not schema-qualified

**Symptom.** The `INSERT … SELECT` tasks produced COMPLETE events with
`input_count=0, output_count=0` and no `columnLineage` facet — only a job facet
carrying the SQL text. Nothing in the Airflow task log hinted at a problem.

**Root cause (reproduced in-process).** The OpenLineage SQL parser resolves
unqualified names against `hook.get_openlineage_default_schema()` (PostgreSQL
`CURRENT_SCHEMA`, i.e. `public`). The generated metadata query is therefore

```sql
... FROM information_schema.columns
WHERE table_schema = 'public' AND table_name IN ('v_order_base', …)   -- returns 0 rows
```

so every input and output is discarded. Once the statements referenced
`e2e_dwd.dwd_order_fact` / `e2e_dwd.v_order_base` explicitly, the same task
emitted 4 inputs, 2 outputs, `has_lineage=t`, with `columnLineage` facets.

**Impact.** A pipeline can look healthy in Airflow while contributing zero
lineage, and there is no signal anywhere in the product. The only surviving
trace is a job facet (`sql.query`) on the run.

**Suggested fix.** On the product side, surface events that carry a `sql` facet
but no inputs/outputs as an "ingestion produced no lineage" diagnostic on the
run detail page (the pieces already exist: `extractionError` is stored for
F5-style failures, but an empty-but-present SQL facet is ignored). Document the
schema-qualification requirement for SQL operators.

---

## F4 — HIGH: the ingested SQL column lineage is a cross-product and invents columns

**Symptom.** For `e2e_dwd.dwd_order_fact`, 21 of the 48 distinct ingested target
columns do not exist on the table:

```
 _0 _1 … _12        (positional placeholders from the parser)
 avg_order_value  city  collection_rate  full_name  loyalty_tier
 net_collected  tenure_days  value_rank       <- real columns of *other* tables
```

and CTE aliases were resolved as sources:

```
test-pg-1;e2e;public;line_agg -> test-pg-1;e2e;e2e_dwd;dwd_order_fact
test-pg-1;e2e;public;agg      -> test-pg-1;e2e;e2e_ads;ads_customer_segment
```

Every parsed input is attached to every parsed output of the same run:
`v_order_line → dwd_customer_360`, `mv_daily_sales → ads_customer_segment`, and
so on, none of which the SQL actually does.

**Root cause.** The upstream Airflow OpenLineage SQL extractor parses the whole
`sql` attribute as one unit and produces one flat input/output set per run; the
CTE names survive as table references. Metaxisdata then stores the facet
verbatim (`backend/plugin/openlineage/processor.go:107`
`processOutputDataset` → `buildColumnLineage` at `processor.go:279`) without
checking that the target column exists on the target relation.

**Impact.** Column-level lineage from the Airflow path mixes true edges with
false ones, which is worse than table-level-only for a governance product: a
"where does this column come from" answer through an `INSERT … SELECT` load
cannot be trusted today.

**Suggested fix.**
1. Validate ingested `(target_guid, target_column)` against the registry and
   store unknown columns as a diagnostic rather than an edge.
2. Drop edges whose source is a name that resolves to no relation (the CTE
   aliases) — currently the resolver happily fabricates
   `test-pg-1;e2e;public;line_agg`.
3. Consider emitting one OpenLineage event per statement (one operator per
   statement, or `sql=` as a list) so the extractor cannot cross-contaminate
   outputs; that is a fixture-side mitigation, not a product fix.

---

## F5 — LOW: `REFRESH MATERIALIZED VIEW` is unparseable

The extractor records an `extractionError` run facet for that statement
(`Expected: an SQL statement, found: REFRESH`). Harmless here because the
materialized view's lineage comes from the definition analyzer, but it does add
a failing statement to every run that refreshes an MV.

---

## What the run proved works

| Area | Evidence |
| --- | --- |
| MySQL → PG (DataX/OL) | 8 table edges; column edges inferred from the DataX schema facet, column-for-column |
| PG analyzer | 6 views + 1 materialized view + 2 serving views analysed, **0 errors** — including `DISTINCT ON`, `GROUPING SETS`, `LATERAL … LIMIT`, `generate_series`, `FILTER` on aggregates *and* on window functions |
| PG internal chain | `e2e_ods.* → v_order_base/v_order_line → v_customer_360 / mv_daily_sales → dwd_* / ads_*` walks correctly (44 table edges) |
| StarRocks analyzer | 2 views + 1 async materialized view resolved against the *real* landed tables (4 edges) |
| MySQL analyzer | 2 source views resolved |
| OpenLineage ingestion | API key auth, rate-limit path, run/job/dataset persistence, 63 runs from 4 DAGs, `has_lineage` correct |
| Data flow | 8 tables MySQL → PG; 2 000/300/1 921/1 921/38 rows in the PG layers; identical counts in StarRocks; SR views and async MV return 75 / 1 723 rows |
| Relation typing | analyzer edges carry `AGGREGATE` (4) vs `DIRECT` (1) correctly, e.g. `v_order_line.line_amount → mv_daily_sales.net_line_amount` |

## Reproducing the checks

```bash
PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f verify_chain.sql
```

Section 4 is the quickest regression gate: after fixing F1 it must return only
the CTE phantoms (F4), and after fixing F4 it must return no rows.
