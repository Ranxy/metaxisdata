# e2e lineage fixture: MySQL → PostgreSQL → StarRocks

A reproducible, three-engine ETL chain whose only purpose is to exercise
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
  └──────────────────┘          │                    │  DAG e2e_03 (DataX postgresqlreader → starrockswriter)
                                │                    ▼
                                │   ┌──────────────────────────────────────────────┐
                                │   │ StarRocks e2e_ods 5 tables                   │
                                │   │ StarRocks e2e_ads 2 views + 1 async matview   │  DAG e2e_04
                                │   └──────────────────────────────────────────────┘
```

Row counts are deterministic (seed `20260407`): 300 customers, 100 products,
2 000 orders, 4 407 order items, 1 823 payments, 89 refunds, 12 regions,
12 sales reps; the DWD/ADS layers derive 2 000 order-fact rows, 300 customer
rows and 1 921 daily-sales rows.

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
| `datax/` | generated DataX job configs, one file per table and hop |
| `dags/` | the four Airflow DAGs |
| `tools/gen_mysql_seed.py`, `tools/gen_datax_jobs.py`, `tools/make_ol_key.py` | generators |
| `verify_chain.sql` | lineage-chain verification queries (run against the metadata DB) |
| `run_chain.sh` | triggers the four DAGs in order and waits for each (no cross-DAG dependencies exist, so firing them together can copy a half-loaded table) |
| `FINDINGS.md` | what the first full run proved, including the defects it exposed |

## Prerequisites

- Docker instances registered in Metaxisdata as `mysql-dev-1` (127.0.0.1:4417),
  `test-pg-1`, `starrocks-dev-1` (127.0.0.1:9030).
- Airflow home `/home/ran/airflow` with the DataX OpenLineage provider
  (`airflow_provider_datax_openlineage`) and DataX at `/home/ran/source/datax`.
- A `pg_e2e` Airflow connection pointing at PostgreSQL database `e2e`, login
  `dev` / `dev`. Its host must be the instance's registered data source host;
  the loopback spellings (`localhost`, `127.0.0.1`, `::1`) are interchangeable
  since F2 was fixed, but any other spelling still needs a namespace mapping.

## Run

```bash
./install.sh --bootstrap     # MySQL fixture + PG database/schemas + DAGs + DataX jobs
./run_chain.sh               # e2e_01 → e2e_02 → e2e_03 → e2e_04, waiting on each
```

`run_chain.sh` takes about a minute, fails loudly on the first non-success task
and prints the per-DAG duration. Equivalent by hand:

```bash
cd /home/ran/pycode/airflow
uv run airflow dags unpause e2e_01_mysql_to_pg   # only if dags_are_paused_at_creation=True
uv run airflow dags trigger e2e_01_mysql_to_pg   # wait for success, then e2e_02, e2e_03, e2e_04
```

Each DAG ends in a `verify_counts` task that prints both sides of its hop, so
the Airflow task log is itself the reconciliation evidence. Re-running is safe:
the hop-1 and hop-3 DAGs delete/truncate their targets first, and the PG scripts
are idempotent.

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

## Deliberate complexity

The transforms are not decoration — each construct is there to be either
resolved correctly or fail loudly: multi-CTE `INSERT … SELECT`, joins across
three schemas, `row_number`/`rank`/`sum() over` windows, `CASE` bucketing,
`FILTER (WHERE …)` aggregates, `DISTINCT ON`, `GROUPING SETS`, `LATERAL`,
`generate_series`, ratio math with `NULLIF`, scalar subqueries with `LIMIT`,
`UNION`-free but cross-schema reads, a materialized view read by DataX, and
StarRocks async materialized views over landed tables.
