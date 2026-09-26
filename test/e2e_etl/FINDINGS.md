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

## Fix status

F1, F2, F3, F4, F6, F7 and F8 are fixed in this repository; F5 is upstream and
untouched.
Each fix carries hermetic unit coverage, and all four were re-verified on this
chain after rebuilding and restarting the server (see "Post-fix verification"
below).

| # | State | Where |
| --- | --- | --- |
| F1 | fixed | `backend/plugin/openlineage/resolver.go` `isMySQLLike` |
| F2 | fixed | `resolver.go` `normalizeHost`/`normalizePort`, plus a warning on the external fallback |
| F3 | fixed (signal added) | `processor.go` `warnMissingSQLLineage`, run-detail alert in `frontend/src/pages/openlineage/OpenLineageRunDetailPage.vue` |
| F4 | fixed (table columns) | `backend/plugin/openlineage/lineage_validation.go` |
| F5 | upstream | Airflow's SQL extractor cannot parse `REFRESH MATERIALIZED VIEW` |
| F6 | fixed | `backend/store/openlineage_task.go` (found while verifying F1–F4) |
| F7 | fixed | `backend/plugin/openlineage/sql_facet_lineage.go` — SQL-facet events are analyzed, not believed |
| F8 | fixed | `queryLocalWildcardSourceRef` in PostgreSQL **and** the MySQL family (StarRocks already had it) |

`verify_chain.sql` section 4 must list only the two CTE aliases, and section 5
must return the table's real columns and nothing else.

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

**Fixed.** `isMySQLLike` now lists `STARROCKS` and `DORIS` alongside the MySQL
family, so a two-part producer name is `database.table` for every MySQL-wire
engine. Covered by `TestBuildGUID` (StarRocks two-part, Doris two-part, and the
explicit `db..table` form) and `TestIsMySQLLike`.

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

**Fixed.** `matchHostPort` runs both hosts through `normalizeHost`, which folds
`localhost`, `127.0.0.1`, `::1` and the bracketed/expanded IPv6 loopback spelling
onto one token, and through `normalizePort`, which compares ports numerically.
The external fallback in `ResolveDataset` now logs one warning per namespace
(`did not match any registered instance ... check the instance's data source host
and port, or add a namespace mapping`), so the next mismatch is visible instead of
silent. Covered by `TestMatchHostPort` (alias spellings, bracketed IPv6, padded
port, aliases still respecting the port, and a real host not being swallowed).

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

**Fixed (signal only).** `warnMissingSQLLineage` logs a structured warning
(`carries a SQL facet but no datasets, so no lineage could be extracted`) with
the job, run and a hint about unqualified names, and the run detail page renders
an alert whenever an event carries a SQL facet with zero datasets
(`hasOpenLineageUnparsedSQL` in `frontend/src/lib/openlineage.ts`, i18n keys in
both locales, 4 new cases in `openlineage.test.ts`). The extractor itself still
belongs to the Airflow provider, and qualifying the tables remains the producer's
job; the point is that the drop is no longer silent.

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

**Fixed (the part the registry can prove).**
`backend/plugin/openlineage/lineage_validation.go` checks every ingested edge
before it is stored, using a digest-only batch lookup
(`ListMetaRegistryResourceDigest`) so validation never loads metadata:

- a column claim is dropped when its relation is a known **TABLE** and the column
  it names is not one of that table's columns — that is where the positional
  `_0…_12` placeholders and the cross-product columns came from;
- the edge itself survives as a **table-level** edge (both columns blanked,
  transformations cleared). The first version dropped the whole edge and that lost
  a *true* dependency: the extractor reports `dwd_order_fact` reading
  `v_customer_payments` only through the positional claim `_3`, so the pair
  disappeared. A run that read a relation and wrote another one really did
  depend on it, even when the column mapping cannot be trusted;
- degraded duplicates collapse to one row per relation pair;
- a relation the registry does not know yet is left intact and reported:
  ingestion runs before the next schema sync, so dropping it would lose the
  lineage of a table created minutes ago, and the event is only processed once;
- external datasets are never judged by the registry;
- views and materialized views keep their columns inside their own metadata
  rather than as registry rows, so a claim against one is left alone;
- every blanked claim and every unresolved relation is logged.

Measured on the live metadata of this chain: 1 296 ingested edges in, 267
unverifiable column claims blanked, 488 references to unresolved relations (the
five phantom StarRocks GUIDs and the two CTE aliases) reported, and the
`e2e` column-level edges left with **no** column that the target table does not
have. Hermetic coverage: `lineage_validation_test.go` (9 cases).

---

## F5 — LOW: `REFRESH MATERIALIZED VIEW` is unparseable

The extractor records an `extractionError` run facet for that statement
(`Expected: an SQL statement, found: REFRESH`). Harmless here because the
materialized view's lineage comes from the definition analyzer, but it does add
a failing statement to every run that refreshes an MV.

---

## F6 — the task list crashed on a task whose latest run was gone

**Symptom.** `OpenLineageService/ListOpenLineageTasks` answered `internal: failed
to list openlineage tasks: failed to scan openlineage task: sql: Scan error on
column index 21, name "event_type": converting NULL to string is unsupported`, so
one stale row took the whole Tasks page down.

**Root cause.** `backend/store/openlineage_task.go` reads a task's latest-run
fields through `LEFT JOIN openlineage_run AS latest_run`. When that run is missing
the join yields NULL for `latest_run.event_type` and `latest_run.raw_payload`, and
`event_type` was scanned into a plain `string`. The other two LEFT JOINs in the
store already scan through `sql.NullString` or `COALESCE`; this one did not.

**How the row got there.** The writers keep `latest_run_guid` valid, and the
retention path repairs it: `DeleteOpenLineageRunsBefore` calls
`reconcileOpenLineageTask`, which rebuilds the aggregate from the surviving runs or
deletes the task when none are left. The two rows in this database came from this
investigation's own cleanup, which deleted probe runs with SQL and left their task
summaries behind. Any out-of-band partial delete — manual SQL, a restored
snapshot, imported rows — reproduces it, and the read path should not depend on
that being impossible.

**Fixed.** The joined columns are scanned as nullable. A task whose latest run is
gone is returned without latest-run detail (guid, run id, event type and payload
cleared; the stored counters and timestamps kept) and logged as
`openlineage task points at a run that no longer exists`. The stale rows in this
database were removed the way the retention reconcile removes a task with no runs
left.

Guard: `TestOpenLineageTaskListSurvivesAPrunedLatestRunRealServerIntegration`
drives the real server, deletes a run out of band and requires the listing to keep
working. It fails with the scan error above without the fix, and passes with it.

## F7 — SQL-facet lineage now comes from our own analyzer

**Why.** Everything F4 could not decide came from the same root: the producer's
`columnLineage` facet is a *run-level guess*. Airflow's SQL extractor merges every
statement of a task into one input/output set, attaches that one facet to every
output, and attributes an output column by finding a same-named column anywhere in
the inputs. With two inputs exposing `paid_amount` it stored
`v_customer_360.paid_amount -> dwd_order_fact.paid_amount` while the statement
reads `v_customer_payments.paid_amount` — plausible, wrong, and invisible to any
existence check. The same facet also carried `_0…_12` positional placeholders and,
separately, CTE aliases resolved as tables.

**What changed.** `backend/plugin/openlineage/sql_facet_lineage.go`. When an event
carries `job.facets.sql.query`, ingestion no longer reads the facet's column
claims. It resolves the anchor dataset (first output, else first input) to an
instance, takes that instance's engine and the anchor's database/schema as the
analysis context, and runs the SQL through the process-wide statement analyzer
(`lineage.Analyzer`, the same one that backs view definitions and manual SQL).
The analyzer splits the script per statement, resolves CTEs, and answers with the
columns each statement really reads. Those relations are mapped onto the run's own
GUIDs and stored. A statement result (`__result__`) and a loading statement's file
(`__file__`) name no stored object and are skipped.

When the SQL cannot be analyzed at all — no analyzer for the engine, an
unparseable script, or an anchor that belongs to no instance — ingestion stores
only the run-level dependencies (inputs × outputs, no column claims) and warns.
The facet's column claims are never used for an event that carries SQL. Events
without SQL (DataX and friends) keep the old path unchanged.

**Verification on the fixture** (rebuild, restart, `install.sh --bootstrap`,
`run_chain.sh`, 4 DAGs green):

| Check | Before F7 | After F7 |
| --- | --- | --- |
| `dwd_order_fact.paid_amount` / `.refund_amount` source | `v_customer_360` ❌ | **`v_customer_payments`** ✅ |
| `dwd_customer_360.paid_amount` / `.refund_amount` source | `v_customer_360` ✅ | `v_customer_360` ✅ |
| Positional `_N` columns in ingested edges | 21+ | **0** |
| CTE aliases stored as relations | `public.agg`, `public.line_agg` | **0** (F8 fixed) |
| Chain depth from `mysql e2e_ods.orders` | 5 | 5 |
| Bogus target columns on the SQL-loaded tables | 0 | 0 |

Guard: `TestOpenLineageSqlFacetUsesTheAnalysedSourceRealServerIntegration` drives
the real server with an `INSERT … SELECT` over two views that both expose
`amount`, states the *wrong* source in the facet, and requires the stored edge to
name the view the SQL reads. It fails without the analyzer path and passes with it.

## F8 — the PostgreSQL analyzer resolves one CTE reference as a table

**Symptom.** After F7 the only unregistered lineage endpoint left in the fixture
is `test-pg-1;e2e;e2e_ads;base`, from one edge:

```
test-pg-1;e2e;e2e_ads;base  *  ->  test-pg-1;e2e;e2e_ads;ads_customer_segment  customer_count   (AGGREGATE)
```

`base` is a CTE in `test/e2e_etl/pg/40_ads_load.sql` (`WITH base AS (…), agg AS (…)`),
referenced from the nested `agg` CTE. The analyzer resolved it as a table in the
default schema (the anchor's `e2e_ads`) with a wildcard column, so the edge points
at a relation that does not exist. The CTE in `20_dwd_load.sql` does not do this,
so the gap is specific to the nested/second CTE shape.

**Why it surfaced now.** Before F7 this package stored the provider's relations,
which had their own CTE artifacts; the analyzer's output was not what the
OpenLineage path stored. F7 makes the analyzer authoritative for SQL events, so
its scope handling is now the visible one.

**Impact.** One phantom node and one table-level-style edge (source column `*`) per
event of that shape; F6's rule keeps unknown relations (they may be a table that
has not synced yet), so it is kept and reported rather than dropped.

**Root cause (the earlier "CTE out of scope" reading was wrong).** The CTE was
found: its named columns resolved through it correctly, and the catalog was never
asked about `base`. What failed is the *table-wide* expression branch. `COUNT(*)`
has no source column of its own, so `processExpressionTarget` attributes it to
every relation in scope with `scope.WildcardSourceRef`, which marks the reference
**resolved**. A resolved reference is returned as-is by the resolver, so the
relation is never attached to it and the existing "a CTE or derived table
contributes its own columns' lineage" branch in `generateEdgeFromSource` cannot
recognise it — the edge is written with the CTE's name as its source, and nothing
marks it temporary, so it leaves the analyzer as a relation.

StarRocks had already solved exactly this: its `queryLocalWildcardSourceRef` keeps
the reference unresolved for a CTE or derived table, so the scope resolver finds it
by its scope key and the temp-table trace flattens it. The PostgreSQL analyzer was
still using the MySQL family's `scope.WildcardSourceRef` at all three sites where a
`*` or a table-wide call reads a relation's rows.

**Fixed.** Ported that rule
(`backend/plugin/lineage/postgresql/analyzer.go` `queryLocalWildcardSourceRef`, used
by `processStarTarget`, `processTableStar` and `processExpressionTarget`). A base
table keeps the resolved reference — looking it up again by name would fail for an
aliased relation — while a CTE or derived table stays resolvable, so `COUNT(*)` over
a CTE flattens to the columns the CTE read.

**The MySQL family had it too.** The same three sites in the shared MySQL-family
body (`backend/plugin/lineage/mysql/analyzer.go`, which TiDB and MariaDB are
generated from) used `scope.WildcardSourceRef`. Probed with the same statement
shape, MySQL emitted `base.* -> summary.total` (temp=false) before the port. The
rule now lives in all three dialects — PostgreSQL, the MySQL family, and StarRocks,
which had it from the start — and TiDB/MariaDB were regenerated from the shared
body. So the defect was never engine-specific: it was the MySQL family's original
rule, and the two dialects that fixed it (StarRocks here, PostgreSQL now) are the
ones that carry the correction.

**Verification.** `TestWildcardAggregateOverACTEDoesNotNameTheCTE` exists in both the
PostgreSQL and MySQL packages, using the fixture's statement shape with a stub
catalog; each fails without its fix ("Should not be: base") and passes with it. On
the rebuilt chain `verify_chain.sql` sections 4 and 6 are **empty**, the two
provenance claims and the five-hop chain are unchanged, and the analyzer corpora of
all five dialects pass. The fixture now covers that side on the chain too:
`mysql/90_views.sql` defines `v_region_order_stats`, a nested CTE whose inner
relation is aggregated with `COUNT(*)`, and `verify_chain.sql` section 8 lists its
sources — `orders` and `customers`, never `base` or `agg`.

## Post-fix verification

Rebuilt, restarted the server on :8083, cleared the OpenLineage artifacts from
the earlier runs, then `install.sh --bootstrap` + `run_chain.sh` again: 4 DAGs,
26 tasks, all green.

| Check | Before | After |
| --- | --- | --- |
| F1: PG → StarRocks edges on registered GUIDs | 0 of 5 (all phantom) | **5 of 5** |
| F1: upstream walk from `starrocks-dev-1;e2e_ods;;mv_daily_sales.net_line_amount` | 0 rows | 4 hops back to `mysql-dev-1;e2e_ods;;order_items.line_amount` |
| F1: deepest chain from `mysql e2e_ods.orders` | 3 hops, ending on phantom GUIDs | **5 hops**, ending on the real `starrocks-dev-1;e2e_ads;;v_sr_channel_region` / `…;v_sr_sales_band` / `…;mv_sr_region_daily` |
| F1: unregistered lineage endpoints | 5 phantom StarRocks GUIDs + 2 CTE aliases | **2 CTE aliases only** |
| F2: DAG-2 run with connection host `127.0.0.1` (instance `localhost`) | 152 external endpoints | **0 external, 55 internal edges** |
| F4: column edges naming a column the target table lacks | 267 edges / 31 bogus columns on `dwd_customer_360` alone | **0** |
| F4: `dwd_order_fact` real columns claimed | 27 of 48 | 27 of 27 (21 bogus claims gone) |
| F4: PG-internal table pairs | 44 (including false cross-product pairs) | **44** — same coverage, but the pairs that only existed through bogus columns now carry no column mapping |
| Analyzer failures on `e2e` objects | 0 | 0 |

What is left, by design: the two CTE aliases
(`test-pg-1;e2e;public;agg`, `…;line_agg`) still have four table-level edges.
They are counted and logged on every ingestion, and the server warns
(`ingested lineage referenced relations the metadata registry does not have`).

## What the run proved works

| Area | Evidence |
| --- | --- |
| MySQL → PG (DataX/OL) | 8 table edges; column edges inferred from the DataX schema facet, column-for-column |
| PG analyzer | 6 views + 1 materialized view + 2 serving views analysed, **0 errors** — including `DISTINCT ON`, `GROUPING SETS`, `LATERAL … LIMIT`, `generate_series`, `FILTER` on aggregates *and* on window functions |
| PG internal chain | `e2e_ods.* → v_order_base/v_order_line → v_customer_360 / mv_daily_sales → dwd_* / ads_*` walks correctly (44 table edges) |
| StarRocks analyzer | 2 views + 1 async materialized view resolved against the *real* landed tables (4 edges) |
| MySQL analyzer | 3 source views resolved, one of them a nested CTE with `COUNT(*)` (F8's shape) |
| OpenLineage ingestion | API key auth, rate-limit path, run/job/dataset persistence, 63 runs from 4 DAGs, `has_lineage` correct |
| Data flow | 8 tables MySQL → PG; 2 000/300/1 921/1 921/38 rows in the PG layers; identical counts in StarRocks; SR views and async MV return 75 / 1 723 rows |
| Relation typing | analyzer edges carry `AGGREGATE` (4) vs `DIRECT` (1) correctly, e.g. `v_order_line.line_amount → mv_daily_sales.net_line_amount` |

## Remaining limitations

1. **View and materialized-view column claims are not validated.** Only a TABLE
   stores its columns as `meta_registry_resource` rows (12 TABLE objects hold
   all 157 COLUMN rows of the `e2e` database); a view keeps its columns inside
   its own `viewMetadata`. Validating those would mean loading and parsing that
   metadata per event, and the analyser already produces precise view lineage, so
   ingestion leaves them alone.
2. **A degraded edge loses its column detail.** When one side of a mapping is
   unverifiable the whole mapping is, so the pair is stored without columns. The
   dependency stays visible; "which column fed which" does not.
3. **A relation the registry does not know is still stored.** They cannot be told
   apart from a table created after the last schema sync, and dropping the latter
   would lose real lineage permanently, so they are kept, counted and logged;
   removing them needs deferred re-validation (check unresolved relations after the
   next sync). The fixture no longer produces one: with F7 and F8 fixed, sections 4
   and 6 of `verify_chain.sql` are empty.
4. **One event carries the whole SQL script.** The extractor still merges every
   statement of a task into one input/output set and one facet. F7 makes that
   irrelevant for column lineage (the analyzer splits the script itself), but the
   run record still reports one input/output set, and a producer that states its
   lineage *without* SQL keeps the old facet semantics.
5. **`REFRESH MATERIALIZED VIEW` stays unparseable** (F5) — upstream.

## Reproducing the checks

```bash
PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f verify_chain.sql
```

Section 4 is the quickest regression gate: it must list only the two CTE
aliases, and section 5 must list the table's real columns and flag nothing as
bogus. Both exercise the ingestion path, so the server has to be rebuilt and
restarted after a change there.
