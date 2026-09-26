"""e2e step 3/4 — PostgreSQL e2e_dwd/e2e_ads -> StarRocks e2e_ods via DataX.

The StarRocks writer plugin in the DataX OpenLineage provider emits the
starrocks:// dataset facet, so this hop should appear in the lineage graph as a
cross-instance edge (PostgreSQL -> StarRocks).
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
SR_MYSQL = "docker exec -i starrocks mysql -uroot -h127.0.0.1 -P9030"

JOB_DIR = E2E_ROOT / "datax" / "pg_to_sr"
TABLES = sorted(p.stem for p in JOB_DIR.glob("*.json"))
log.info("e2e_03: found %d DataX jobs in %s", len(TABLES), JOB_DIR)

with DAG(
    dag_id="e2e_03_pg_to_sr",
    description="PostgreSQL e2e -> StarRocks e2e_ods (DataX + OpenLineage)",
    start_date=datetime(2026, 1, 1),
    schedule=None,
    catchup=False,
    is_paused_upon_creation=False,
    max_active_runs=1,
    tags=["e2e", "lineage", "datax", "starrocks"],
) as dag:
    create_sr_tables = BashOperator(
        task_id="create_sr_ods_tables",
        bash_command=f"set -euo pipefail; {SR_MYSQL} < {E2E_ROOT}/starrocks/10_ods_ddl.sql",
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
echo "--- PostgreSQL sources"
PGPASSWORD=dev psql -h localhost -U dev -d e2e -t -A -c "
  select 'dwd_order_fact', count(*) from e2e_dwd.dwd_order_fact
  union all select 'dwd_customer_360', count(*) from e2e_dwd.dwd_customer_360
  union all select 'mv_daily_sales', count(*) from e2e_dwd.mv_daily_sales
  union all select 'ads_daily_sales', count(*) from e2e_ads.ads_daily_sales
  union all select 'ads_customer_segment', count(*) from e2e_ads.ads_customer_segment"
echo "--- StarRocks e2e_ods"
{SR_MYSQL} -N -B -e "
  select 'dwd_order_fact', count(*) from e2e_ods.dwd_order_fact
  union all select 'dwd_customer_360', count(*) from e2e_ods.dwd_customer_360
  union all select 'mv_daily_sales', count(*) from e2e_ods.mv_daily_sales
  union all select 'ads_daily_sales', count(*) from e2e_ods.ads_daily_sales
  union all select 'ads_customer_segment', count(*) from e2e_ods.ads_customer_segment"
""",
    )

    create_sr_tables >> sync_tasks >> verify
