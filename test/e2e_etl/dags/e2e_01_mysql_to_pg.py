"""e2e step 1/4 — MySQL e2e_ods -> PostgreSQL e2e.e2e_ods via DataX.

Each task is one DataX table copy; the provider emits an OpenLineage event per
task whose input is the MySQL table and whose output is the PostgreSQL table,
so table- and column-level lineage for this hop come from OpenLineage.
"""

from __future__ import annotations

import logging
import os
from datetime import datetime
from pathlib import Path

from airflow import DAG
from airflow.operators.bash import BashOperator
from airflow.providers.common.sql.operators.sql import SQLExecuteQueryOperator
from airflow_provider_datax_openlineage.operators.datax import DataXOperator

log = logging.getLogger(__name__)

E2E_ROOT = Path(os.environ.get("E2E_ROOT", "/home/ran/airflow/e2e_etl"))
DATAX_HOME = os.environ.get("DATAX_HOME", "/home/ran/source/datax")
OL_URL = os.environ.get("OPENLINEAGE_URL", "http://localhost:8083")
PG_CONN = "pg_e2e"

JOB_DIR = E2E_ROOT / "datax" / "mysql_to_pg"
TABLES = sorted(p.stem for p in JOB_DIR.glob("*.json"))
log.info("e2e_01: found %d DataX jobs in %s", len(TABLES), JOB_DIR)

with DAG(
    dag_id="e2e_01_mysql_to_pg",
    description="MySQL e2e_ods -> PostgreSQL e2e.e2e_ods (DataX + OpenLineage)",
    start_date=datetime(2026, 1, 1),
    schedule=None,
    catchup=False,
    is_paused_upon_creation=False,
    max_active_runs=1,
    tags=["e2e", "lineage", "datax"],
) as dag:
    create_ods_tables = SQLExecuteQueryOperator(
        task_id="create_pg_ods_tables",
        conn_id=PG_CONN,
        sql=(E2E_ROOT / "pg" / "05_ods_ddl.sql").read_text(),
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
        bash_command="""
set -euo pipefail
echo "--- MySQL e2e_ods"
docker exec mysql8-db-1 mysql -uroot -prootadmin -N -B -e "
  select 'customers', count(*) from e2e_ods.customers
  union all select 'orders', count(*) from e2e_ods.orders
  union all select 'order_items', count(*) from e2e_ods.order_items
  union all select 'payments', count(*) from e2e_ods.payments
  union all select 'refunds', count(*) from e2e_ods.refunds
  union all select 'products', count(*) from e2e_ods.products
  union all select 'regions', count(*) from e2e_ods.regions
  union all select 'sales_reps', count(*) from e2e_ods.sales_reps"
echo "--- PostgreSQL e2e.e2e_ods"
PGPASSWORD=dev psql -h localhost -U dev -d e2e -t -A -c "
  select 'customers', count(*) from e2e_ods.customers
  union all select 'orders', count(*) from e2e_ods.orders
  union all select 'order_items', count(*) from e2e_ods.order_items
  union all select 'payments', count(*) from e2e_ods.payments
  union all select 'refunds', count(*) from e2e_ods.refunds
  union all select 'products', count(*) from e2e_ods.products
  union all select 'regions', count(*) from e2e_ods.regions
  union all select 'sales_reps', count(*) from e2e_ods.sales_reps"
""",
    )

    create_ods_tables >> sync_tasks >> verify
