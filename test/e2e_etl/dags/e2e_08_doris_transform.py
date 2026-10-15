"""e2e step 8/8 — the Doris analyzer layer, and Doris e2e_ads.v_doris_kpi_daily -> StarRocks.

This is where the two producers of the chain meet for a fifth engine: the Doris
definition analyzer owns `doris-dev-1;e2e_ads;;v_doris_kpi_daily`, and the last DataX
event names the same object. The reader is a plain mysqlreader on the Doris FE's
MySQL-protocol port - Doris speaks the MySQL protocol, and the resolver matches a
dataset by host and port rather than by URL scheme, so the Doris instance answers
for it. A producer that spelled a second GUID would draw a disconnected node while
every row count agreed, which is what this hop exists to catch.

The layer itself is engine-valid SQL the analyzer has to resolve: a VARIANT path
(the one shape that cannot be landed through DataX here, so it is inserted
deterministically), an ARRAY LATERAL VIEW over a derived table, a CTE with a short
declared column list, GROUP BY ROLLUP, a window rank over an aggregate, and an async
materialized view whose refresh has to be waited for before the copy runs.
"""

from __future__ import annotations

import logging
import os
from datetime import datetime
from pathlib import Path

from airflow import DAG
from airflow.operators.bash import BashOperator
from airflow_provider_datax_openlineage.operators.datax import DataXOperator

log = logging.getLogger(__name__)

E2E_ROOT = Path(os.environ.get("E2E_ROOT", "/home/ran/airflow/e2e_etl"))
DATAX_HOME = os.environ.get("DATAX_HOME", "/home/ran/source/datax")
OL_URL = os.environ.get("OPENLINEAGE_URL", "http://localhost:8083")
DORIS_MYSQL = "docker exec -i doris-fe mysql -uroot -h127.0.0.1 -P9030"
SR_MYSQL = "docker exec -i starrocks mysql -uroot -h127.0.0.1 -P9030"
MV = "e2e_ads.mv_region_revenue"

JOB_DIR = E2E_ROOT / "datax" / "doris_to_sr"
TABLES = sorted(p.stem for p in JOB_DIR.glob("*.json"))
log.info("e2e_08: found %d DataX jobs in %s", len(TABLES), JOB_DIR)

with DAG(
    dag_id="e2e_08_doris_transform",
    description="Doris e2e_ads views/matview -> StarRocks e2e_ods/e2e_ads (DataX + OpenLineage)",
    start_date=datetime(2026, 1, 1),
    schedule=None,
    catchup=False,
    is_paused_upon_creation=False,
    max_active_runs=1,
    tags=["e2e", "lineage", "doris", "starrocks", "datax"],
) as dag:
    create_doris_objects = BashOperator(
        task_id="create_doris_ads_objects",
        bash_command=f"set -euo pipefail; {DORIS_MYSQL} < {E2E_ROOT}/doris/20_ads_ddl.sql",
    )

    # Doris refreshes an async materialized view on its own schedule, and a fresh
    # one starts empty; without this the copy that reads it through a view lands no
    # rows, and the run would look broken where it is only unsynchronised.
    refresh_mv = BashOperator(
        task_id="refresh_doris_materialized_view",
        bash_command=f"""
set -euo pipefail
{DORIS_MYSQL} -e "refresh materialized view {MV} complete;"
for _ in $(seq 1 60); do
  state=$({DORIS_MYSQL} -N -B -e "select SyncWithBaseTables from mv_infos('database'='e2e_ads') where Name='mv_region_revenue';" | tr -d '[:space:]')
  if [ "$state" = "1" ]; then break; fi
  sleep 2
done
{DORIS_MYSQL} -N -B -e "select Name, RefreshState, SyncWithBaseTables from mv_infos('database'='e2e_ads');"
[ "$state" = "1" ]
""",
    )

    create_sr_objects = BashOperator(
        task_id="create_sr_doris_objects",
        bash_command=f"set -euo pipefail; {SR_MYSQL} < {E2E_ROOT}/starrocks/40_doris_serving_ddl.sql",
    )

    sync_tasks = []
    for table in TABLES:
        sync_tasks.append(
            DataXOperator(
                task_id=f"sync_{table}",
                job_config=str(JOB_DIR / f"{table}.json"),
                datax_home=DATAX_HOME,
                datax_python=os.environ.get("DATAX_PYTHON", "python"),
                openlineage_url=OL_URL,
            )
        )

    verify = BashOperator(
        task_id="verify_counts",
        bash_command=f"""
set -euo pipefail
echo "--- Doris e2e_ads"
{DORIS_MYSQL} -N -B -e "
  select 'v_doris_kpi_daily', count(*), sum(net_line_amount) from e2e_ads.v_doris_kpi_daily
  union all select 'v_event_props_exploded', count(*), 0 from e2e_ads.v_event_props_exploded
  union all select 'v_customer_segment_rollup', count(*), 0 from e2e_ads.v_customer_segment_rollup"
echo "--- StarRocks e2e_ods/e2e_ads"
{SR_MYSQL} -N -B -e "
  select 'e2e_ods.doris_kpi_daily', count(*), sum(net_line_amount) from e2e_ods.doris_kpi_daily
  union all select 'e2e_ads.v_sr_doris_kpi', count(*), sum(net_line_amount) from e2e_ads.v_sr_doris_kpi"
""",
    )

    create_doris_objects >> refresh_mv >> create_sr_objects >> sync_tasks >> verify
