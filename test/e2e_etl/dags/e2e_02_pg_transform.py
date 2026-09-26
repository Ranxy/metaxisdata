"""e2e step 2/4 — PostgreSQL transforms: e2e_ods -> e2e_dwd -> e2e_ads.

Views and the materialized view are analysed from their stored definitions by
the PostgreSQL lineage analyzer; the fact/serving tables are loaded with
INSERT ... SELECT executed here, which the OpenLineage SQL extractor turns into
column-level lineage events.
"""

from __future__ import annotations

import os
from datetime import datetime
from pathlib import Path

from airflow import DAG
from airflow.operators.bash import BashOperator
from airflow.providers.common.sql.operators.sql import SQLExecuteQueryOperator

E2E_ROOT = Path(os.environ.get("E2E_ROOT", "/home/ran/airflow/e2e_etl"))
PG_CONN = "pg_e2e"
SQL_DIR = E2E_ROOT / "pg"


def sql(name: str) -> str:
    return (SQL_DIR / name).read_text()


with DAG(
    dag_id="e2e_02_pg_transform",
    description="PostgreSQL e2e_ods -> e2e_dwd -> e2e_ads (views, matview, INSERT SELECT)",
    start_date=datetime(2026, 1, 1),
    schedule=None,
    catchup=False,
    is_paused_upon_creation=False,
    max_active_runs=1,
    tags=["e2e", "lineage", "postgres"],
) as dag:
    dwd_ddl = SQLExecuteQueryOperator(task_id="dwd_ddl", conn_id=PG_CONN, sql=sql("10_dwd_ddl.sql"))
    dwd_load = SQLExecuteQueryOperator(task_id="dwd_load", conn_id=PG_CONN, sql=sql("20_dwd_load.sql"))
    edge_views = SQLExecuteQueryOperator(
        task_id="pg_edge_case_views", conn_id=PG_CONN, sql=sql("50_experimental_pg_features.sql")
    )
    ads_ddl = SQLExecuteQueryOperator(task_id="ads_ddl", conn_id=PG_CONN, sql=sql("30_ads_ddl.sql"))
    ads_load = SQLExecuteQueryOperator(task_id="ads_load", conn_id=PG_CONN, sql=sql("40_ads_load.sql"))

    verify = BashOperator(
        task_id="verify_counts",
        bash_command="""
set -euo pipefail
PGPASSWORD=dev psql -h localhost -U dev -d e2e -t -A -c "
  select 'e2e_dwd.dwd_order_fact', count(*) from e2e_dwd.dwd_order_fact
  union all select 'e2e_dwd.dwd_customer_360', count(*) from e2e_dwd.dwd_customer_360
  union all select 'e2e_dwd.mv_daily_sales', count(*) from e2e_dwd.mv_daily_sales
  union all select 'e2e_dwd.v_customer_360', count(*) from e2e_dwd.v_customer_360
  union all select 'e2e_dwd.v_product_performance', count(*) from e2e_dwd.v_product_performance
  union all select 'e2e_ads.ads_daily_sales', count(*) from e2e_ads.ads_daily_sales
  union all select 'e2e_ads.ads_customer_segment', count(*) from e2e_ads.ads_customer_segment
  union all select 'e2e_ads.v_ads_region_summary', count(*) from e2e_ads.v_ads_region_summary"
""",
    )

    dwd_ddl >> [dwd_load, edge_views]
    dwd_load >> ads_ddl >> ads_load
    [ads_load, edge_views] >> verify
