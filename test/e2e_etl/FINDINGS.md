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
Row counts and every finding below reproduced byte-for-byte — same pre-fix hop
edge counts (5 / 8 / 44 / 5 / 4), same 5 phantom StarRocks GUIDs, same 2 CTE
phantoms, 0 analyzer errors.

## How to read the numbers

Everything above and in the findings below was measured on that first run, before
any fix landed. The fixes changed what the chain stores, so several of those
figures are deliberately superseded; **"Post-fix verification"** at the end holds
the current ones and states the basis of each count. The two sections are a
before and an after, not a contradiction.

The basis matters because a count of stored rows also depends on how many chains
have run against the database: ingestion files each run's edges under that run's
own meta row, so the same logical edge appears once per run that produced it (this
database has accumulated several chains). Every count here that must be stable is
therefore a count of **distinct** relation pairs or distinct column edges, and
says so.

## Fix status

F1, F2, F3, F4, F6, F7 and F8 are fixed in this repository; F5 is upstream, and
this repository now surfaces it instead of hiding it (see F5). Each fix carries
hermetic unit coverage, and every one was re-verified on this chain after the
last backend change (see "Post-fix verification" below).

| # | State | Where |
| --- | --- | --- |
| F1 | fixed | `backend/plugin/openlineage/resolver.go` `isMySQLLike` |
| F2 | fixed | `resolver.go` `normalizeHost`/`normalizePort`, plus a warning on the external fallback |
| F3 | fixed (signal added) | `processor.go` `warnMissingSQLLineage`, run-detail alert in `frontend/src/pages/openlineage/OpenLineageRunDetailPage.vue` |
| F4 | fixed (table columns) | `backend/plugin/openlineage/lineage_validation.go` |
| F5 | upstream; signal added, parser upgraded | Airflow's SQL extractor cannot parse `REFRESH MATERIALIZED VIEW` or `CREATE MATERIALIZED VIEW … WITH DATA` |
| F6 | fixed | `backend/store/openlineage_task.go` (found while verifying F1–F4) |
| F7 | fixed | `backend/plugin/openlineage/sql_facet_lineage.go` — SQL-facet events are analyzed, not believed |
| F8 | fixed | `queryLocalWildcardSourceRef` in PostgreSQL **and** the MySQL family (StarRocks already had it) |
| A | fixed | `backend/api/v1/lineage_service.go` — an unresolved node renders instead of answering `CodeNotFound` |
| B | fixed | `RevalidateUnresolvedLineage` + `FindColumnLineageWithUnknownEndpoint`, and `RevalidateContradictedColumnClaims` + `FindColumnLineageWithContradictedColumnClaim`; the maintenance pass runs both, and a schema sync signals `backend/runner/lineagevalidation` |

`verify_chain.sql` sections 4 and 6 must be **empty** — no lineage endpoint may be
unregistered and no edge may be sourced from a CTE — and section 5 must list the
table's real columns and flag nothing as bogus.

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

## F5 — LOW: materialized-view statements are partly unparseable upstream

The producer's SQL parser is not the database. The Airflow extractor feeds the
same statement it executed to `openlineage-sql` (a compiled Rust extension) to
derive the run's datasets, and that parser fails on three PostgreSQL
materialized-view shapes, recording an `extractionError` run facet for each
instead of lineage:

| Statement | `openlineage-sql` 1.37.0 | 1.53.0 |
| --- | --- | --- |
| `DROP MATERIALIZED VIEW … CASCADE` | error | parses |
| `CREATE MATERIALIZED VIEW … WITH DATA` | error | error (`Expected: end of statement, found: WITH`) |
| `REFRESH MATERIALIZED VIEW …` | error | error (`Expected: an SQL statement, found: REFRESH`) |

The database executes all three, so the task is green and nothing looks wrong in
Airflow: only the lineage inference failed. `DROP MATERIALIZED VIEW` support
landed in sqlparser-rs ([PR #1743](https://github.com/apache/datafusion-sqlparser-rs/pull/1743)),
which is why 1.53.0 has it and 1.37.0 does not; `REFRESH` and `WITH DATA` have no
upstream support.

**Impact.** The failing statement contributes no dataset to the run, so the run
reads as a job with less lineage than its SQL has. The lineage itself survives —
a view or materialized view is analysed from its stored definition, and since F7
the server analyses the SQL facet itself (our PostgreSQL analyzer handles all
three shapes) — so what is missing is the run's own input/output set, and,
before this change, any trace of why.

**Fixed (signal).** `extractOpenLineageExtractionErrors` in
`frontend/src/lib/openlineage.ts` reads `run.facets.extractionError.errors`, and
`OpenLineageRunDetailPage.vue` renders each failed statement beside its reason.
A run that carries parse errors shows that specific alert instead of only the
generic `hasOpenLineageUnparsedSQL` one, which could not say why the lineage was
missing. Covered by `openlineage.test.ts` (including the facet the extractor
actually emits, captured from `openlineage_run.raw_payload`).

**Upgraded.** The Airflow environment's `openlineage-{python,integration-common,sql}`
moved from 1.37.0 to 1.53.0. They are released in lockstep and
`integration-common` 1.37.0 pins the other two exactly, so they cannot be
upgraded one at a time; the provider requires only `>=1.36.0`, and it imports
unchanged against 1.53.0. Re-verified on this chain: `e2e_02_pg_transform.dwd_ddl`
now records **1** extraction error instead of 2, the remaining one being
`CREATE MATERIALIZED VIEW … WITH DATA`; `dwd_load` still records the `REFRESH`
one.

**Left as-is.** `CREATE MATERIALIZED VIEW … WITH DATA` and `REFRESH MATERIALIZED
VIEW` stay unparseable upstream. `WITH DATA` is PostgreSQL's default, so dropping
it from the fixture would be behaviour-preserving if that error is not wanted.

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

Measured on a chain run after every fix above had landed (server from `0bfef8a`,
`install.sh --bootstrap` + `run_chain.sh`): 4 DAGs, 26 task instances, all green.
Distinct counts, per "How to read the numbers".

| Check | Before | After |
| --- | --- | --- |
| F1: PG → StarRocks edges on registered GUIDs | 0 of 5 (all phantom) | **5 of 5** |
| F1: upstream walk from `starrocks-dev-1;e2e_ods;;mv_daily_sales.net_line_amount` | 0 rows | 4 hops back to `mysql-dev-1;e2e_ods;;order_items.line_amount` |
| F1: deepest chain from `mysql e2e_ods.orders` | 3 hops, ending on phantom GUIDs | **5 hops**, ending on the real `starrocks-dev-1;e2e_ads;;v_sr_channel_region` / `…;v_sr_sales_band` / `…;mv_sr_region_daily` |
| F1: unregistered lineage endpoints (§4) | 5 phantom StarRocks GUIDs + 2 CTE aliases | **0** |
| F1: edges sourced from a CTE-shaped relation (§6) | 2 CTE aliases, 4 table-level edges | **0** |
| F2: endpoints that fall back to an external dataset | 152 edges / 12 external datasets | **0** |
| F4: column edges naming a column the target table lacks | 267 edges / 31 bogus columns on `dwd_customer_360` alone | **0** |
| F4: `dwd_order_fact` target columns claimed | 48 claims, 21 of them bogus | **33 claims, all real** (the table has 34 columns) |
| F4: positional `_N` placeholder columns | 21+ | **0** |
| F4: PG-internal relation pairs | 44 (including false cross-product pairs) | **34** |
| F8: MySQL-internal relation pairs | 5 | **7** |
| Relation pairs per hop (§1) | mysql internal 5 · mysql → PG 8 · PG internal 44 · PG → StarRocks 5 · StarRocks internal 4 | mysql internal 7 · mysql → PG 8 · PG internal 34 · PG → StarRocks 5 · StarRocks internal 4 |
| Analyzer failures on `e2e` objects (§7) | 0 | 0 |
| Analysed objects | 18 | **19** — PostgreSQL 12 views + 1 materialized view, MySQL 3 views, StarRocks 2 views + 1 materialized view |
| Distinct column edges for the `e2e` objects | 849 — basis not recorded | **540** |
| OpenLineage runs per chain | 63 — basis not recorded | **30** — 26 task runs + one DAG-level run per DAG |

The pair counts moved in both directions, and only one movement is the fixture's
own doing:

- **PG-internal 44 → 34.** The 10 pairs that went away are the false cross-product
  pairs F4 lists (`v_order_line → dwd_customer_360`, `mv_daily_sales →
  ads_customer_segment`, …). F7 replaces the producer's run-level guess with the
  analyzer's per-statement result, which never had them. What survives is the real
  chain: `e2e_ods.*` → `v_order_base`/`v_order_line` → `v_customer_360` /
  `mv_daily_sales` / `v_experimental_*` → `dwd_*` / `ads_*`.
- **MySQL-internal 5 → 7** is the fixture's growth, not the analyzer's:
  `mysql/90_views.sql` gained `v_region_order_stats` for F8's shape.

The pre-fix 849 and 63 could not be reconstructed from what is stored: the basis
of neither was recorded, and this database has accumulated several chains (it
holds 1 183 raw / 540 distinct `e2e` edges and 115 run rows, of which 30 belong to
the chain measured here). That is why the table records a basis for every number
it does claim.

Nothing is left unresolved: §4 and §6 are empty, no endpoint falls back to an
external dataset, and §5 lists only columns the table has. The two F7 provenance
claims hold (`dwd_order_fact.paid_amount` and `.refund_amount` come from
`v_customer_payments`, and `v_customer_360`'s own from itself), relation typing is
correct (`v_order_line.line_amount → mv_daily_sales.net_line_amount` is
`AGGREGATE`, the DataX hop is `DIRECT`), and the data flow is unchanged: 8 tables
MySQL → PG, 2 000 / 300 / 1 921 / 1 921 / 38 rows in the PG layers, the same
counts in StarRocks, and the StarRocks views and async materialized view return
75 / 24 454 / 1 723 rows.

## What the run proved works

| Area | Evidence |
| --- | --- |
| MySQL → PG (DataX/OL) | 8 table edges; column edges inferred from the DataX schema facet, column-for-column |
| PG analyzer | 12 views + 1 materialized view analysed, **0 errors** — including `DISTINCT ON`, `GROUPING SETS`, `LATERAL … LIMIT`, `generate_series`, `FILTER` on aggregates *and* on window functions |
| PG internal chain | `e2e_ods.* → v_order_base/v_order_line → v_customer_360 / mv_daily_sales → dwd_* / ads_*` walks correctly (34 relation pairs) |
| StarRocks analyzer | 2 views + 1 async materialized view resolved against the *real* landed tables (4 pairs) |
| MySQL analyzer | 3 source views resolved, one of them a nested CTE with `COUNT(*)` (F8's shape) |
| OpenLineage ingestion | API key auth, rate-limit path, run/job/dataset persistence, 30 runs per chain from 4 DAGs (26 task runs + 4 DAG-level), `has_lineage` correct |
| Data flow | 8 tables MySQL → PG; 2 000/300/1 921/1 921/38 rows in the PG layers; identical counts in StarRocks; SR views and async MV return 75 / 24 454 / 1 723 rows |
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
3. **A relation the registry does not know is still stored - and now re-checked.**
   Keeping it is deliberate: it cannot be told apart from a table created after the
   last schema sync, and dropping it would lose real lineage permanently. What
   changed is that it no longer stays unchecked or invisible:
   - **(A)** the lineage graph reports such a node as an unresolved relation of the
     type the edge claims (`lineageNodeWithMissingMeta`) instead of answering
     `CodeNotFound`, so one pending endpoint cannot fail the whole request;
   - **(B)** two passes re-run the ingestion validation over ingested edges, and
     they cover the two halves of the problem:
     - `FindColumnLineageWithUnknownEndpoint` covers an edge whose endpoint is
       *still* unknown: the object types are filled from the side that is known, a
       claim against that side is blanked, and what is still unknown is counted and
       logged rather than kept silent.
     - `FindColumnLineageWithContradictedColumnClaim` covers the edge whose endpoint
       *became* known. It names a column on a relation that is now a TABLE, and the
       table has no such column. This is the only pass that can still fix such a
       claim: the sync that registered the relation also took the edge out of the
       sweep above, and until this pass existed the claim stayed wrong forever -
       waiting for the maintenance interval did not help. It is what
       `verify_chain.sql` section 11 asserts is empty.
     Both replace a run's edge set as a set, so every edge of an affected run is
     validated together.
   - **(C)** a schema sync that added or changed metadata signals the
     `lineagevalidation` runner, which coalesces the signals of one sync round and
     runs the contradicted-claim pass. A claim is therefore fixed moments after the
     relation appears, not at the next maintenance interval.
   Verified live against this database: a synthetic pending edge carrying one valid
   and one invalid target column came back re-validated - the valid claim kept, the
   invalid one degraded to a table-level edge - with its unknown relation counted
   (`revalidated=2 objectsStillUnknown=1` out of the 22 unknown-endpoint edges present
   at that time; one remains today, an `information_schema` reference, which the
   unknown-endpoint pass deliberately leaves to the analyzer's own re-analysis).
   (C) is guarded by
   `TestSchemaSyncRevalidatesIngestedLineageRealServerIntegration`, which drives the
   real server through the sequence that exposed the gap: an edge against a table
   that does not exist yet, then the table, then the sync. The ghost claim must be
   gone inside the 45 s the test allows, where the maintenance interval is 6 h.
   The maintenance pass runs both sweeps on its interval (6 h) and revalidates
   ingested edges only. The fixture still produces no pending endpoint: with F7 and
   F8 fixed, sections 4 and 6 of `verify_chain.sql` are empty.
4. **One event carries the whole SQL script.** The extractor still merges every
   statement of a task into one input/output set and one facet. F7 makes that
   irrelevant for column lineage (the analyzer splits the script itself), but the
   run record still reports one input/output set, and a producer that states its
   lineage *without* SQL keeps the old facet semantics.
5. **Two materialized-view shapes stay unparseable** (F5) — `REFRESH
   MATERIALIZED VIEW` and `CREATE MATERIALIZED VIEW … WITH DATA`, upstream. The
   run detail page now names them instead of leaving the loss silent.

## Reproducing the checks

```bash
PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f verify_chain.sql
```

Sections 4 and 6 are the quickest regression gate: both must be empty, and section
5 must list the table's real columns and flag nothing as bogus. All three exercise
the ingestion path, so the server has to be rebuilt and restarted after a change
there. Sections 9-13 produce the counts "Post-fix verification" reports: distinct
`e2e` edges, analysed objects, bogus column claims anywhere, positional columns
and external endpoints, and the run totals.
