#!/usr/bin/env bash
# Deploy the e2e lineage fixture into the local Airflow home and (optionally)
# bootstrap the three database instances.
#
#   ./install.sh              # deploy DAGs + DataX jobs + fixtures
#   ./install.sh --bootstrap  # also load the MySQL fixture and create the PG database
#
# Airflow Home is /home/ran/airflow; the fixture is deployed to
# $AIRFLOW_HOME/e2e_etl and the DAG files to $AIRFLOW_HOME/dags.
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AIRFLOW_HOME="${AIRFLOW_HOME:-/home/ran/airflow}"
DEST="$AIRFLOW_HOME/e2e_etl"
DAGS_DIR="$AIRFLOW_HOME/dags"

MYSQL_CONTAINER="${MYSQL_CONTAINER:-mysql8-db-1}"
MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-rootadmin}"
PG_CONTAINER="${PG_CONTAINER:-pg16-db-1}"
SR_CONTAINER="${SR_CONTAINER:-starrocks}"

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
}

echo "== regenerate DataX job configs"
python3 "$SRC/tools/gen_datax_jobs.py" >/dev/null

echo "== deploy fixtures to $DEST"
mkdir -p "$DEST"
for d in mysql pg starrocks datax tools; do
  mkdir -p "$DEST/$d"
  cp -r "$SRC/$d/." "$DEST/$d/"
done

echo "== deploy DAGs to $DAGS_DIR"
mkdir -p "$DAGS_DIR"
cp "$SRC"/dags/e2e_*.py "$DAGS_DIR/"
cp "$SRC/verify_chain.sql" "$DEST/"

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

Trigger with:
  cd /home/ran/pycode/airflow && uv run airflow dags trigger e2e_01_mysql_to_pg
EOF
