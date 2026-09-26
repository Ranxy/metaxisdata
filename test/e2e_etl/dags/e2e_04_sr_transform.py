"""e2e step 4/4 — StarRocks e2e_ads objects over the landed tables.

StarRocks views and the async materialized view are resolved by the StarRocks
lineage analyzer from the definitions stored at sync time.
"""

from __future__ import annotations

import os
from datetime import datetime
from pathlib import Path

from airflow import DAG
from airflow.operators.bash import BashOperator

E2E_ROOT = Path(os.environ.get("E2E_ROOT", "/home/ran/airflow/e2e_etl"))
SR_MYSQL = "docker exec -i starrocks mysql -uroot -h127.0.0.1 -P9030"

with DAG(
    dag_id="e2e_04_sr_transform",
    description="StarRocks e2e_ads views and materialized view over e2e_ods",
    start_date=datetime(2026, 1, 1),
    schedule=None,
    catchup=False,
    is_paused_upon_creation=False,
    max_active_runs=1,
    tags=["e2e", "lineage", "starrocks"],
) as dag:
    create_sr_objects = BashOperator(
        task_id="create_sr_ads_objects",
        bash_command=f"set -euo pipefail; {SR_MYSQL} < {E2E_ROOT}/starrocks/20_ads_ddl.sql",
    )

    refresh_mv = BashOperator(
        task_id="refresh_sr_materialized_view",
        bash_command=(
            "set -euo pipefail; "
            f"{SR_MYSQL} -e \"refresh materialized view e2e_ads.mv_sr_region_daily with sync mode;\""
        ),
    )

    verify = BashOperator(
        task_id="verify_counts",
        bash_command=f"""
set -euo pipefail
{SR_MYSQL} -N -B -e "
  select 'e2e_ads.v_sr_channel_region', count(*) from e2e_ads.v_sr_channel_region
  union all select 'e2e_ads.v_sr_sales_band', count(*) from e2e_ads.v_sr_sales_band
  union all select 'e2e_ads.mv_sr_region_daily', count(*) from e2e_ads.mv_sr_region_daily"
""",
    )

    create_sr_objects >> refresh_mv >> verify
