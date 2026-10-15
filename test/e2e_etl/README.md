# e2e lineage fixture: MySQL → PostgreSQL → StarRocks / MSSQL / Doris

A reproducible, five-engine ETL chain whose only purpose is to exercise
Metaxisdata's table- and column-level lineage end to end:

```
                        Airflow (localhost:8080)
                               │
  MySQL  e2e_ods                │   DAG e2e_01 (DataX mysqlreader → postgresqlwriter)
  ┌──────────────────┐          │   ┌──────────────────────────────────────────────┐
  │ regions          │──────────┼──▶│ PG  e2e.e2e_ods  (landing, 8 tables)         │
  │ sales_reps       │          │   └──────────────────────────────────────────────┘
  │ customers        │          │                    │  DAG e2e_02 (SQL views + INSERT…SELECT)
  │ products         │          │                    ▼
  │ orders           │          │   ┌──────────────────────────────────────────────┐
  │ order_items      │          │   │ PG  e2e.e2e_dwd  6 views + 1 matview + 2 tbs │
  │ payments         │          │   │ PG  e2e.e2e_ads  1 view + 2 tables           │
  │ refunds          │          │   └──────────────────────────────────────────────┘
  └──────────────────┘          │         │                          │
                                │  e2e_03 (DataX pg → sr)    e2e_05 (DataX pg → sqlserver)
                                │         ▼                          ▼
                                │  ┌───────────────────┐   ┌──────────────────────────────┐
                                │  │ StarRocks e2e_ods │   │ MSSQL e2e.e2e_ods  2 tables  │
                                │  │ StarRocks e2e_ads │   │ MSSQL e2e.e2e_dwd  2 tables  │
                                │  │            e2e_04 │   │ MSSQL e2e.e2e_ads 10 views   │
                                │  └───────────────────┘   └──────────────────────────────┘
                                │                                     │  DAG e2e_06 (DataX
                                │                                     ▼   sqlserver → starrocks)
                                │  ┌────────────────────────────────────────────────────┐
                                │  │ StarRocks e2e_ods.mssql_kpi_daily + 1 view over it │
                                │  └────────────────────────────────────────────────────┘
                                │
                                │  e2e_07 (DataX pg → doris)    e2e_08 (DataX doris → starrocks)
                                │         ▼                              ▼
                                │  ┌──────────────────────────┐  ┌──────────────────────────────┐
                                │  │ Doris e2e_ods 2 tables   │─▶│ StarRocks e2e_ods            │
                                │  │ Doris e2e_ads 6 views +  │  │  .doris_kpi_daily + 1 view   │
                                │  │  1 async matview         │  └──────────────────────────────┘
                                │  └──────────────────────────┘
```

Row counts are deterministic (seed `20260407`): 300 customers, 100 products,
2 000 orders, 4 407 order items, 1 823 payments, 89 refunds, 12 regions,
12 sales reps; the DWD/ADS layers derive 2 000 order-fact rows, 300 customer
rows and 1 921 daily-sales rows. The MSSQL and Doris hops land 300 + 1 921 of
them, and the view each of their last hops copies returns one row per region
(3 in both cases).

## Layout

| Path | What it is |
| --- | --- |
| `mysql/10_schema.sql` | source DDL (8 tables) |
| `mysql/20_seed.sql` | generated deterministic seed (do not edit by hand) |
| `mysql/90_views.sql` | 2 MySQL views (MySQL analyzer coverage) |
| `pg/05_ods_ddl.sql` | landing tables, `e2e_ods` schema |
| `pg/10_dwd_ddl.sql` | 6 analysable views, 1 materialized view, 2 fact tables |
| `pg/20_dwd_load.sql` | `INSERT … SELECT` loads (fully schema-qualified on purpose) |
| `pg/30_ads_ddl.sql`, `pg/40_ads_load.sql` | serving layer, cross-schema loads |
| `pg/50_experimental_pg_features.sql` | deliberately exotic PG definitions (GROUPING SETS, LATERAL, generate_series, `FILTER` on a window, arrays/jsonb) |
| `starrocks/10_ods_ddl.sql` | landing tables (must exist before the DataX Stream Load) |
| `starrocks/20_ads_ddl.sql` | StarRocks views + async materialized view |
| `starrocks/30_mssql_serving_ddl.sql` | the StarRocks table the MSSQL hop loads, plus a view over it |
| `mssql/00_bootstrap.sql` | the `e2e` database and its three schemas |
| `mssql/10_ods_ddl.sql` | landing tables, one column per PostgreSQL column they copy |
| `mssql/20_dwd_ddl.sql`, `mssql/30_dwd_load.sql` | MSSQL-side dimension and fact, loaded with `MERGE` and an `INSERT … SELECT` over a window |
| `mssql/40_dwd_views.sql`, `mssql/50_ads_views.sql` | the analysable MSSQL views: CTE joins, aggregates over windows, a wildcard, CUBE / ROLLUP / GROUPING SETS, PIVOT, CROSS APPLY |
| `mssql/60_gap_fixtures.sql` | the shapes the analyzer is not expected to model, each left visible as a reported gap or a recorded failure |
| `starrocks/40_doris_serving_ddl.sql` | the StarRocks table the Doris hop loads, plus a view over it |
| `doris/10_ods_ddl.sql` | landing tables for the PostgreSQL hop (a Stream Load needs them to exist) |
| `doris/20_ads_ddl.sql` | the read-only user the copy out of Doris needs, a VARIANT/ARRAY source table, six analysable views and one async materialized view |
| `datax/` | generated DataX job configs, one file per table and hop |
| `dags/` | the eight Airflow DAGs |
| `tools/gen_mysql_seed.py`, `tools/gen_datax_jobs.py`, `tools/make_ol_key.py` | generators |
| `tools/mssql_sqlcmd.sh` | runs T-SQL in the local MSSQL container, credential from the environment |
| `tools/datax_lineage_mssql.py` | the DataX lineage plugin for `sqlserverreader` / `sqlserverwriter`, which the provider does not ship |
| `tools/datax_lineage_doris.py` | the DataX lineage plugin for `doriswriter`, which the provider does not ship |
| `tools/trigger_sync.py` | syncs instances, so the objects the chain just created are discovered and analysed |
| `verify_chain.sql` | lineage-chain verification queries (run against the metadata DB) |
| `run_chain.sh` | triggers the eight DAGs in order and waits for each (no cross-DAG dependencies exist, so firing them together can copy a half-loaded table) |
| `FINDINGS.md` | what the full runs proved, including the defects they exposed |

## Prerequisites

- Docker instances registered in Metaxisdata as `mysql-dev-1` (127.0.0.1:4417),
  `test-pg-1`, `starrocks-dev-1` (127.0.0.1:9030), `ms-sql-1` (127.0.0.1:1433) and
  `doris-dev-1` (127.0.0.1:9031, engine `DORIS`, user `root`, no password).
- Airflow home `/home/ran/airflow` with the DataX OpenLineage provider
  (`airflow_provider_datax_openlineage`) and DataX at `/home/ran/source/datax`.
- A `pg_e2e` Airflow connection pointing at PostgreSQL database `e2e`, login
  `dev` / `dev`. Its host must be the instance's registered data source host;
  the loopback spellings (`localhost`, `127.0.0.1`, `::1`) are interchangeable
  since F2 was fixed, but any other spelling still needs a namespace mapping.
- The MSSQL container `mssql` (`mcr.microsoft.com/mssql/server:2019-latest`, with
  `sqlcmd` at `/opt/mssql-tools18/bin/sqlcmd`) and the `sa` password. The DAGs read
  it from the `mssql_e2e` Airflow connection, and `install.sh --bootstrap` creates
  that connection from `MSSQL_SA_PASSWORD` when it is set. No password is committed:
  the DataX job configs under `datax/` are deployed without one and `install.sh`
  fills it into the deployed copies.
- The Doris containers `doris-fe` and `doris-be`
  (`apache/doris:fe-4.1.4` / `be-4.1.4`, one of each). The FE's MySQL-protocol port
  is published as 9031 and its HTTP port as 8031; Stream Load is the HTTP one. The
  copy out of Doris reads as the `e2e_reader` user `doris/20_ads_ddl.sql` creates,
  because DataX's rdbms readers refuse an empty password and root has none here.

## Run

```bash
MSSQL_SA_PASSWORD=… ./install.sh --bootstrap   # MySQL fixture + PG/MSSQL/Doris databases + DAGs + DataX jobs
./run_chain.sh                                 # e2e_01 … e2e_08, waiting on each
python3 tools/trigger_sync.py --email … --password … ms-sql-1 doris-dev-1 starrocks-dev-1
```

`run_chain.sh` takes a few minutes, fails loudly on the first non-success task and
prints the per-DAG duration. Equivalent by hand:

```bash
cd /home/ran/pycode/airflow
uv run airflow dags unpause e2e_01_mysql_to_pg   # only if dags_are_paused_at_creation=True
uv run airflow dags trigger e2e_01_mysql_to_pg   # wait for success, then e2e_02 … e2e_08
```

Each DAG ends in a `verify_counts` task that prints both sides of its hop, so the
Airflow task log is itself the reconciliation evidence. Re-running is safe: the
DataX hops delete/truncate their targets first, and every SQL script is idempotent.

The last step is not optional: the platform only analyses what a schema sync has
registered, and an instance with no sync interval is never synced on its own — the
MSSQL instance has none, so the `e2e` database and its views stay invisible until
something syncs `ms-sql-1`. `tools/trigger_sync.py` is that something. The two hops
that end on StarRocks (`e2e_06`, `e2e_08`) also create tables and views there, so
`starrocks-dev-1` is named too: it carries a sync interval, but waiting out the
interval is not the same as a gate. The analysis runs behind the sync; give it a few
seconds before running `verify_chain.sql`.

## Verify lineage

```bash
PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f verify_chain.sql
```

It answers, in order: edges per hop; the downstream closure of
`mysql e2e_ods.orders`; the upstream column provenance of a StarRocks column
(the same query twice — once for the GUID the metadata registry knows, once for
the GUID the DataX event actually wrote); lineage endpoints that are not
registered objects; ingested target columns that do not exist on the target
table; phantom CTE sources; analyzer failures.

Sections 14-22 are the MSSQL hop's: how many edges each pair carries, that the
positional copy maps same-name columns, that the edge into StarRocks starts at the
MSSQL *view* the analyzer produced, that CUBE/ROLLUP/GROUPING SETS keep base-table
grouping keys, and that the three shapes the analyzer cannot model are reported
instead of guessed. Sections 4, 6, 15, 21 and the first number of 22 must be empty.

Sections 23-32 are the Doris hop's: the same per-pair count and same-name mapping,
that the edge into StarRocks starts at the Doris view, that the analyzer resolved
every object with no error, that a grouping-set key names the columns rather than the
grouped expression, that a VARIANT sub-path and an ARRAY LATERAL VIEW stay attributed
to the column they read, that a short declared column list on a CTE is exposed
positionally, and that no Doris edge is addressed with a schema segment. Sections 24,
28, 31 and 32 must be empty.

## The MSSQL hop

Everything else in the chain addresses a relation with `<instance>;<database>;<schema>;<name>`
like any other engine; MSSQL is the only one here where a producer that writes
`database=schema.table` in the wrong order lands on an object that does not exist.
Two producers have to agree on it, and both are checked: the DataX hop (section 15)
and the last hop, whose source is a view the analyzer itself produced (section 17).

Three of its objects are deliberately beyond the analyzer, because a boundary nobody
writes down is a boundary that quietly halves a graph:

| Object | What the run records |
| --- | --- |
| `e2e_ads.v_parenthesised` | the parser rejects the body: a failure on the version row, and no edges at all (section 22) |
| `e2e_ads.v_pivot_over_derived` | a PIVOT over a derived table cannot enumerate its grouping column: three edges kept, `region` reported unresolved (sections 19-20) |
| `e2e_ads.v_tvf_source` | a table-valued function is not a relation the registry can address: the joined view's edges kept, the function's columns reported unresolved (sections 19-20) |
| `e2e_ads.usp_refresh_kpi`, `e2e_dwd.fn_region_net` | a routine body is not an object the analyzer reads, so they have no analysis row at all (section 22) |

## The Doris hop

Doris is addressed by three parts (`<instance>;<database>;;<name>`): it has no schema,
so a producer that names one lands on an object no sync registered. That is what
happened on the first run of this hop; section 31 is now the gate for the shape (it
must stay empty) and section 25 reports the source it has to be on — the DataX reader's
JDBC URL named `e2e_ods` while its table was written as `e2e_ads.v_doris_kpi_daily`, the
provider's MySQL plugin prefixed the URL's database to the qualified table, and the
resulting three-part dataset name resolved to `doris-dev-1;e2e_ods;e2e_ads;…` (F10).
The fixture now names the database once, in the reader URL, and the hop lands on the
GUID the analyzer owns.

Three engine facts shape the fixture:

| Fact | What it means here |
| --- | --- |
| Doris stores a view body with every column qualified by catalog and database (`internal`.`e2e_ods`.`t`.`c`) | the analyzer has to read that back as a relation in the same database; the definitions in `doris/20_ads_ddl.sql` are the ones the engine keeps, not the ones written |
| `information_schema.tables` reports an async materialized view as `BASE TABLE`, and its body lives only in `mv_infos()` | the schema sync branches on the engine for it, and a fresh MV is empty until it refreshes — the DAG refreshes it and waits before the copy |
| A DataX rdbms reader refuses an empty password, and Stream Load is an HTTP API that redirects to a BE | the fixture creates the read-only `e2e_reader` user, and the writer carries both the FE HTTP port (8031) and the MySQL port (9031) |

The analysable layer is engine-valid SQL only; the shapes it adds are the ones that
are Doris's own: a VARIANT subcolumn path (`props['user']['name']`), an ARRAY LATERAL
VIEW over a derived table (which omni's Doris grammar rejects, so the analyzer
recovers it), a CTE with a short declared column list, `GROUP BY ROLLUP(...)`, a
window rank over an aggregate, an async materialized view, and a view over that
matview and another view as the object the last hop copies.

## Deliberate complexity

The transforms are not decoration — each construct is there to be either
resolved correctly or fail loudly: multi-CTE `INSERT … SELECT`, joins across
three schemas, `row_number`/`rank`/`sum() over` windows, `CASE` bucketing,
`FILTER (WHERE …)` aggregates, `DISTINCT ON`, `GROUPING SETS`, `LATERAL`,
`generate_series`, ratio math with `NULLIF`, scalar subqueries with `LIMIT`,
`UNION`-free but cross-schema reads, a materialized view read by DataX, and
StarRocks async materialized views over landed tables. The MSSQL side adds a
`MERGE` upsert, `CUBE`/`ROLLUP`/`GROUPING SETS`, `PIVOT`, `CROSS APPLY`, a table
hint behind an alias, and a wildcard view over the dimension table. The Doris side
adds `VARIANT` subcolumn paths, an ARRAY `LATERAL VIEW`, `ROLLUP` over the landed
tables, and an async materialized view read through a view by the last hop.
