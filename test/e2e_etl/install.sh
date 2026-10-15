#!/usr/bin/env bash
# Deploy the e2e lineage fixture into the local Airflow home and (optionally)
# bootstrap the database instances.
#
#   ./install.sh              # deploy DAGs + DataX jobs + fixtures
#   ./install.sh --bootstrap  # also load the MySQL fixture and create the PG, MSSQL and Doris databases
#
# Airflow Home is /home/ran/airflow; the fixture is deployed to
# $AIRFLOW_HOME/e2e_etl and the DAG files to $AIRFLOW_HOME/dags.
#
# The MSSQL hop needs the local container's sa password. This script reads it from
# MSSQL_SA_PASSWORD, or from the mssql_e2e Airflow connection when that variable is
# not set, and ensures the connection exists - the DAGs render their credential from
# that connection, because a task process cannot read the metadata database itself.
# The password is written into the deployed DataX job copies only: the copies under
# test/e2e_etl/datax carry no credential, because this repository is public.
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AIRFLOW_HOME="${AIRFLOW_HOME:-/home/ran/airflow}"
AIRFLOW_DIR="${AIRFLOW_DIR:-/home/ran/pycode/airflow}"
DEST="$AIRFLOW_HOME/e2e_etl"
DAGS_DIR="$AIRFLOW_HOME/dags"

MYSQL_CONTAINER="${MYSQL_CONTAINER:-mysql8-db-1}"
MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-rootadmin}"
PG_CONTAINER="${PG_CONTAINER:-pg16-db-1}"
SR_CONTAINER="${SR_CONTAINER:-starrocks}"
DORIS_CONTAINER="${DORIS_CONTAINER:-doris-fe}"

airflow() { (cd "$AIRFLOW_DIR" && uv run airflow "$@"); }

# mssql_password prints the local SQL Server password, or nothing.
#
# install.sh runs outside a task, so unlike a DAG task it can read the Airflow
# connection itself; MSSQL_SA_PASSWORD wins when it is set, so a deployment that
# already exports the credential needs nothing else.
mssql_password() {
  if [ -n "${MSSQL_SA_PASSWORD:-}" ]; then
    printf '%s' "$MSSQL_SA_PASSWORD"
    return
  fi
  airflow connections get mssql_e2e -o json 2>/dev/null |
    python3 -c 'import json,sys
try:
    found = json.load(sys.stdin)
    if isinstance(found, list):
        found = found[0] if found else {}
    print(found.get("password") or "", end="")
except Exception:
    pass' 2>/dev/null || true
}

bootstrap() {
  echo "== bootstrap PostgreSQL database e2e"
  docker exec "$PG_CONTAINER" psql -U postgres -tAc \
    "select 1 from pg_database where datname='e2e'" | grep -q 1 || \
    docker exec "$PG_CONTAINER" psql -U postgres -c "CREATE DATABASE e2e OWNER dev;"
  docker exec "$PG_CONTAINER" psql -U postgres -d e2e -c \
    "CREATE SCHEMA IF NOT EXISTS e2e_ods AUTHORIZATION dev;
     CREATE SCHEMA IF NOT EXISTS e2e_dwd AUTHORIZATION dev;
     CREATE SCHEMA IF NOT EXISTS e2e_ads AUTHORIZATION dev;"

  echo "== bootstrap MySQL e2e_ods"
  docker exec -i "$MYSQL_CONTAINER" mysql -uroot -p"$MYSQL_ROOT_PASSWORD" < "$SRC/mysql/10_schema.sql"
  docker exec -i "$MYSQL_CONTAINER" mysql -uroot -p"$MYSQL_ROOT_PASSWORD" < "$SRC/mysql/20_seed.sql"
  docker exec -i "$MYSQL_CONTAINER" mysql -uroot -p"$MYSQL_ROOT_PASSWORD" < "$SRC/mysql/90_views.sql"
  docker exec "$MYSQL_CONTAINER" mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -e \
    "select 'customers', count(*) from e2e_ods.customers union all select 'orders', count(*) from e2e_ods.orders;"

  echo "== bootstrap StarRocks e2e_ods/e2e_ads (empty tables)"
  docker exec -i "$SR_CONTAINER" mysql -uroot -h127.0.0.1 -P9030 < "$SRC/starrocks/10_ods_ddl.sql"
  docker exec -i "$SR_CONTAINER" mysql -uroot -h127.0.0.1 -P9030 < "$SRC/starrocks/20_ads_ddl.sql"

  echo "== bootstrap Doris e2e_ods/e2e_ads (landing tables + the analyzer layer)"
  docker exec -i "$DORIS_CONTAINER" mysql -uroot -h127.0.0.1 -P9030 < "$SRC/doris/10_ods_ddl.sql"
  docker exec -i "$DORIS_CONTAINER" mysql -uroot -h127.0.0.1 -P9030 < "$SRC/doris/20_ads_ddl.sql"

  echo "== bootstrap MSSQL database e2e (empty schemas)"
  if [ -z "$MSSQL_PASSWORD" ]; then
    echo "   skipped: no SQL Server password (set MSSQL_SA_PASSWORD, or create the mssql_e2e connection)"
  else
    MSSQL_SA_PASSWORD="$MSSQL_PASSWORD" "$SRC/tools/mssql_sqlcmd.sh" "$SRC/mssql/00_bootstrap.sql" >/dev/null
    echo "   the e2e database and its three schemas are there; 10_ods_ddl creates the tables"
  fi
}

# mssql_connection ensures the Airflow connection the sqlcmd wrapper falls back to
# exists, so a DAG task does not have to depend on the environment the scheduler was
# started from. An existing connection is left alone: it may point elsewhere on purpose.
mssql_connection() {
  [ -n "$MSSQL_PASSWORD" ] || return 0
  if airflow connections get mssql_e2e >/dev/null 2>&1; then
    echo "   the mssql_e2e connection already exists; left as it is"
    return 0
  fi
  airflow connections add mssql_e2e \
    --conn-type generic --conn-host 127.0.0.1 --conn-port 1433 --conn-login sa \
    --conn-password "$MSSQL_PASSWORD" --conn-description "local MSSQL container (e2e_etl)" >/dev/null
  echo "   added the mssql_e2e connection"
}

# mssql_job_passwords fills the SQL Server password into the deployed DataX jobs.
# DataX reads its configuration from a file and cannot read a variable, and the
# committed copies must stay credential-free, so the deploy makes that substitution.
mssql_job_passwords() {
  [ -n "$MSSQL_PASSWORD" ] || return 0
  python3 - "$DEST/datax" "$MSSQL_PASSWORD" <<'PY'
import json
import pathlib
import sys

root, password = pathlib.Path(sys.argv[1]), sys.argv[2]
filled = []
for job in sorted(root.glob("*/[!_]*.json")):
    config = json.loads(job.read_text())
    touched = False
    for content in config.get("job", {}).get("content", []):
        for side in ("reader", "writer"):
            plugin = content.get(side, {})
            if plugin.get("name") in ("sqlserverreader", "sqlserverwriter"):
                plugin.setdefault("parameter", {})["password"] = password
                touched = True
    if touched:
        job.write_text(json.dumps(config, indent=2) + "\n")
        filled.append(f"{job.parent.name}/{job.name}")
print(f"   filled the SQL Server password into {len(filled)} deployed jobs: {', '.join(filled)}")
PY
}

MSSQL_PASSWORD="$(mssql_password)"

echo "== regenerate DataX job configs"
python3 "$SRC/tools/gen_datax_jobs.py" >/dev/null

echo "== deploy fixtures to $DEST"
mkdir -p "$DEST"
for d in mysql pg starrocks mssql doris datax tools; do
  mkdir -p "$DEST/$d"
  cp -r "$SRC/$d/." "$DEST/$d/"
done

echo "== deploy DAGs to $DAGS_DIR"
mkdir -p "$DAGS_DIR"
cp "$SRC"/dags/e2e_*.py "$DAGS_DIR/"
cp "$SRC/verify_chain.sql" "$DEST/"

echo "== MSSQL credentials"
mssql_connection
mssql_job_passwords

if [[ "${1:-}" == "--bootstrap" ]]; then
  bootstrap
fi

cat <<EOF

Deployed.
  fixtures : $DEST
  DAGs     : $DAGS_DIR/e2e_0*.py

Run order (all DAGs are manual, schedule=None):
  1) e2e_01_mysql_to_pg   MySQL e2e_ods -> PG e2e.e2e_ods
  2) e2e_02_pg_transform  PG e2e_ods -> e2e_dwd -> e2e_ads
  3) e2e_03_pg_to_sr      PG e2e_dwd/e2e_ads -> StarRocks e2e_ods
  4) e2e_04_sr_transform  StarRocks e2e_ads views + materialized view
  5) e2e_05_pg_to_mssql   PG e2e_dwd/e2e_ads -> MSSQL e2e_ods, then the MSSQL layer
  6) e2e_06_mssql_to_sr   MSSQL e2e_ads.v_kpi_daily -> StarRocks
  7) e2e_07_pg_to_doris   PG e2e_dwd/e2e_ads -> Doris e2e_ods
  8) e2e_08_doris_transform  the Doris e2e_ads layer, then Doris e2e_ads.v_doris_kpi_daily -> StarRocks

Trigger with:
  cd $AIRFLOW_DIR && uv run airflow dags trigger e2e_01_mysql_to_pg
EOF
