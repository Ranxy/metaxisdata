"""e2e step 7/8 — PostgreSQL e2e_dwd/e2e_ads -> Doris e2e_ods via DataX.

Doris is the fifth engine of this chain, and the only one whose writer is not a
statement: doriswriter is a Stream Load, so it addresses the FE twice - the HTTP
entry in ``loadUrl`` and the MySQL-protocol entry in ``connection.jdbcUrl``. The two
ports are separate on this host (8031 HTTP, 9031 MySQL; the StarRocks container
already owns 9030), and using the MySQL port for the load fails with a redirect that
never reaches a backend.

The provider ships no plugin for doriswriter, so a job through it would emit an
event with no output dataset at all - lineage that is not wrong, just absent. The
fixture supplies tools/datax_lineage_doris.py instead of patching the provider,
which lives in its own repository.
"""

from __future__ import annotations

import logging
import os
import sys
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

sys.path.insert(0, str(E2E_ROOT / "tools"))
from datax_lineage_doris import DorisLineagePlugin  # noqa: E402

JOB_DIR = E2E_ROOT / "datax" / "pg_to_doris"
TABLES = sorted(p.stem for p in JOB_DIR.glob("*.json"))
log.info("e2e_07: found %d DataX jobs in %s", len(TABLES), JOB_DIR)

with DAG(
    dag_id="e2e_07_pg_to_doris",
    description="PostgreSQL e2e_dwd/e2e_ads -> Doris e2e_ods (DataX + OpenLineage)",
    start_date=datetime(2026, 1, 1),
    schedule=None,
    catchup=False,
    is_paused_upon_creation=False,
    max_active_runs=1,
    tags=["e2e", "lineage", "doris", "postgres", "datax"],
) as dag:
    create_doris_tables = BashOperator(
        task_id="create_doris_ods_tables",
        bash_command=f"set -euo pipefail; {DORIS_MYSQL} < {E2E_ROOT}/doris/10_ods_ddl.sql",
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
                extra_lineage_plugins=[DorisLineagePlugin()],
            )
        )

    verify = BashOperator(
        task_id="verify_counts",
        bash_command=f"""
set -euo pipefail
echo "--- PostgreSQL sources"
PGPASSWORD=dev psql -h localhost -U dev -d e2e -t -A -c "
  select 'dwd_customer_360', count(*) from e2e_dwd.dwd_customer_360
  union all select 'ads_daily_sales', count(*) from e2e_ads.ads_daily_sales"
echo "--- Doris e2e_ods"
{DORIS_MYSQL} -N -B -e "
  select 'dwd_customer_360', count(*) from e2e_ods.dwd_customer_360
  union all select 'ads_daily_sales', count(*) from e2e_ods.ads_daily_sales"
""",
    )

    create_doris_tables >> sync_tasks >> verify
