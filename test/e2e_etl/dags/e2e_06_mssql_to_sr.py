"""e2e step 6/6 — MSSQL e2e_ads.v_kpi_daily -> StarRocks e2e_ods.mssql_kpi_daily.

The source of this hop is a view, not a table: the DataX event's input dataset is
the GUID the MSSQL definition analyzer already owns (ms-sql-1;e2e;e2e_ads;v_kpi_daily),
so an edge produced by an analyzer and an edge produced by an ingested event meet at
one node. Getting that name wrong is the failure this hop exists to catch - a
producer that writes its own spelling of the GUID draws a second, disconnected
node and the chain looks broken while every row count agrees.
"""

from __future__ import annotations

import os
import sys
from datetime import datetime
from pathlib import Path

from airflow import DAG
from airflow.operators.bash import BashOperator
from airflow_provider_datax_openlineage.operators.datax import DataXOperator

E2E_ROOT = Path(os.environ.get("E2E_ROOT", "/home/ran/airflow/e2e_etl"))
DATAX_HOME = os.environ.get("DATAX_HOME", "/home/ran/source/datax")
OL_URL = os.environ.get("OPENLINEAGE_URL", "http://localhost:8083")
SR_MYSQL = "docker exec -i starrocks mysql -uroot -h127.0.0.1 -P9030"
SQLCMD = E2E_ROOT / "tools" / "mssql_sqlcmd.sh"

# Templated from the connection: a task cannot read the metadata database itself
# (Airflow points a task's AIRFLOW__DATABASE__SQL_ALCHEMY_CONN at a non-url), so the
# task runner renders the credential into the task's environment instead. PATH comes
# along because a BashOperator's `env` replaces the environment rather than extending
# it, and a program that resolves its own executable through PATH needs it.
MSSQL_ENV = {
    "PATH": os.environ.get("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"),
    "HOME": os.environ.get("HOME", "/home/ran"),
    "SQLCMDPASSWORD": "{{ conn.mssql_e2e.password }}",
}

sys.path.insert(0, str(E2E_ROOT / "tools"))
from datax_lineage_mssql import MssqlLineagePlugin  # noqa: E402

JOB_DIR = E2E_ROOT / "datax" / "mssql_to_sr"
TABLES = sorted(p.stem for p in JOB_DIR.glob("*.json"))

with DAG(
    dag_id="e2e_06_mssql_to_sr",
    description="MSSQL e2e_ads views -> StarRocks e2e_ods/e2e_ads (DataX + OpenLineage)",
    start_date=datetime(2026, 1, 1),
    schedule=None,
    catchup=False,
    is_paused_upon_creation=False,
    max_active_runs=1,
    tags=["e2e", "lineage", "mssql", "starrocks", "datax"],
) as dag:
    sr_ddl = BashOperator(
        task_id="create_sr_mssql_objects",
        bash_command=f"set -euo pipefail; {SR_MYSQL} < {E2E_ROOT}/starrocks/30_mssql_serving_ddl.sql",
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
                extra_lineage_plugins=[MssqlLineagePlugin()],
            )
        )

    verify = BashOperator(
        task_id="verify_counts",
        env=MSSQL_ENV,
        bash_command=f"""
set -euo pipefail
echo "--- MSSQL"
MSSQL_DB=e2e {SQLCMD} -q "
  select 'e2e_ads.v_kpi_daily', count(*), sum(net_line_amount) from e2e_ads.v_kpi_daily"
echo "--- StarRocks"
{SR_MYSQL} -N -B -e "
  select 'e2e_ods.mssql_kpi_daily', count(*), sum(net_line_amount) from e2e_ods.mssql_kpi_daily
  union all select 'e2e_ads.v_sr_mssql_kpi', count(*), sum(net_line_amount) from e2e_ads.v_sr_mssql_kpi"
""",
    )

    sr_ddl >> sync_tasks >> verify
