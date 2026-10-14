"""e2e step 5/6 — PostgreSQL e2e_dwd/e2e_ads -> MSSQL, and the MSSQL layer over it.

Two producers meet on this hop. The DataX copy emits one OpenLineage event per
table whose output dataset is the MSSQL table (namespace ``mssql://127.0.0.1:1433``,
name ``e2e.e2e_ods.<table>``), so the table- and column-level lineage of the copy
comes from the event, exactly as it does for the MySQL and StarRocks hops.

Everything below the landing tables is T-SQL this DAG runs with sqlcmd, and sqlcmd
reports nothing to anyone: no event, no SQL facet. What that layer contributes to
the lineage is therefore the objects themselves - the MSSQL definition analyzer
reads their definitions after the schema sync stores them, which is why the views
here are the interesting part of this hop.
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

E2E_ROOT = Path(os.environ.get("E2E_ROOT", "/home/ran/airflow/e2e_etl"))
DATAX_HOME = os.environ.get("DATAX_HOME", "/home/ran/source/datax")
OL_URL = os.environ.get("OPENLINEAGE_URL", "http://localhost:8083")
SQLCMD = E2E_ROOT / "tools" / "mssql_sqlcmd.sh"
MSSQL_DIR = E2E_ROOT / "mssql"

# The credential the tasks run sqlcmd with. It is templated from the connection
# because a task cannot read the metadata database itself: Airflow points a task's
# AIRFLOW__DATABASE__SQL_ALCHEMY_CONN at a non-url on purpose, so a hook or the CLI
# inside the task fails, and the task runner is what has to render the connection in.
#
# PATH travels with it because a BashOperator's `env` replaces the task's environment
# rather than extending it (the standard provider passes `env if env else os.environ`).
# Without PATH, bash still finds a command through its built-in default, but a program
# that resolves its own executable through PATH fails: psql answers
# "could not find own program executable" when the verification task tries to run it.
MSSQL_ENV = {
    "PATH": os.environ.get("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"),
    "HOME": os.environ.get("HOME", "/home/ran"),
    "SQLCMDPASSWORD": "{{ conn.mssql_e2e.password }}",
}

# The DataX OpenLineage provider ships no plugin for sqlserverreader/sqlserverwriter,
# and a hop it cannot name is a hop that emits no datasets at all. The fixture
# supplies the plugin; it is deployed beside the fixtures rather than installed.
sys.path.insert(0, str(E2E_ROOT / "tools"))
from datax_lineage_mssql import MssqlLineagePlugin  # noqa: E402

JOB_DIR = E2E_ROOT / "datax" / "pg_to_mssql"
TABLES = sorted(p.stem for p in JOB_DIR.glob("*.json"))

log = logging.getLogger(__name__)
log.info("e2e_05: found %d DataX jobs in %s", len(TABLES), JOB_DIR)


def apply_sql(name: str, task_id: str) -> BashOperator:
    # Every fixture script selects its own database, so the connection stays on
    # master and the script decides where it lands.
    return BashOperator(
        task_id=task_id,
        env=MSSQL_ENV,
        bash_command=f"set -euo pipefail; {SQLCMD} {MSSQL_DIR}/{name}",
    )


with DAG(
    dag_id="e2e_05_pg_to_mssql",
    description="PostgreSQL e2e_dwd/e2e_ads -> MSSQL e2e_ods, then the MSSQL layer over it",
    start_date=datetime(2026, 1, 1),
    schedule=None,
    catchup=False,
    is_paused_upon_creation=False,
    max_active_runs=1,
    tags=["e2e", "lineage", "mssql", "datax"],
) as dag:
    ods_ddl = apply_sql("10_ods_ddl.sql", "mssql_ods_ddl")

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

    dwd_ddl = apply_sql("20_dwd_ddl.sql", "mssql_dwd_ddl")
    dwd_load = apply_sql("30_dwd_load.sql", "mssql_dwd_load")
    dwd_views = apply_sql("40_dwd_views.sql", "mssql_dwd_views")
    ads_views = apply_sql("50_ads_views.sql", "mssql_ads_views")
    # The boundary fixtures go last: they read the views the two steps above created.
    gap_fixtures = apply_sql("60_gap_fixtures.sql", "mssql_gap_fixtures")

    verify = BashOperator(
        task_id="verify_counts",
        env=MSSQL_ENV,
        bash_command=f"""
set -euo pipefail
echo "--- PostgreSQL"
PGPASSWORD=dev psql -h localhost -U dev -d e2e -t -A -c "
  select 'e2e_dwd.dwd_customer_360', count(*) from e2e_dwd.dwd_customer_360
  union all select 'e2e_ads.ads_daily_sales', count(*) from e2e_ads.ads_daily_sales"
echo "--- MSSQL"
MSSQL_DB=e2e {SQLCMD} -q "
  select 'e2e_ods.dwd_customer_360', count(*) from e2e_ods.dwd_customer_360
  union all select 'e2e_ods.ads_daily_sales', count(*) from e2e_ods.ads_daily_sales
  union all select 'e2e_dwd.dim_customer', count(*) from e2e_dwd.dim_customer
  union all select 'e2e_dwd.fact_daily_sales', count(*) from e2e_dwd.fact_daily_sales
  union all select 'e2e_ads.v_kpi_daily', count(*) from e2e_ads.v_kpi_daily
  union all select 'e2e_ads.v_pivot_channel', count(*) from e2e_ads.v_pivot_channel
  union all select 'e2e_dwd.v_customer_value', count(*) from e2e_dwd.v_customer_value"
""",
    )

    ods_ddl >> sync_tasks >> dwd_ddl >> dwd_load
    dwd_load >> dwd_views >> ads_views >> gap_fixtures
    gap_fixtures >> verify
