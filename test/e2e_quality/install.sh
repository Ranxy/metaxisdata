#!/usr/bin/env bash
# Install/reset the data-quality fixture against a running Metaxisdata server.
#
#   METAXISDATA_EMAIL=admin@example.com METAXISDATA_PASSWORD=... ./install.sh
#
# What it does, idempotently:
#   1. recreates the MySQL database e2e_dq and its deterministic defect seed;
#   2. recreates the PostgreSQL databases e2e_dq / e2e_dq_ref and their seeds;
#   3. makes sure both registered instances carry a READ_ONLY data source: the
#      MySQL one is created (or updated) for the SELECT-only user the fixture
#      owns, and the PostgreSQL one is created only when missing - a pre-existing
#      one must be able to read e2e_dq / e2e_dq_ref (the default dev login can);
#   4. creates the fixture-owned instance dq-noread (used only for the
#      "no READ_ONLY source" scenario);
#   5. full-syncs all three instances and waits until every fixture table has a
#      registry GUID.
#
# Prerequisites are the same local environment test/e2e_etl uses: a running
# Metaxisdata server on :8083, the mysql8-db-1 / pg16-db-1 containers, and the
# mysql-dev-1 / test-pg-1 instances already registered. Airflow and DataX are
# not needed here.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

: "${METAXISDATA_EMAIL:?set METAXISDATA_EMAIL to a workspace administrator}"
: "${METAXISDATA_PASSWORD:?set METAXISDATA_PASSWORD for that administrator}"

export METAXISDATA_URL="${METAXISDATA_URL:-http://localhost:8083}"
export MYSQL_CONTAINER="${MYSQL_CONTAINER:-mysql8-db-1}"
export MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-rootadmin}"
export PG_CONTAINER="${PG_CONTAINER:-pg16-db-1}"

echo "== server ${METAXISDATA_URL}"
health="$(curl -s -m 5 -o /dev/null -w '%{http_code}' "${METAXISDATA_URL}/healthz" || true)"
if [[ "$health" != "200" ]]; then
  echo "the server is not answering ${METAXISDATA_URL}/healthz (got ${health:-no response})" >&2
  echo "start it first, for example:" >&2
  echo "  PG_URL='postgres://dev:dev@localhost:5432/metaxisdata?sslmode=disable' go run ./backend/bin/server/main.go --debug" >&2
  exit 1
fi

echo "== mysql container ${MYSQL_CONTAINER}"
if ! docker exec "$MYSQL_CONTAINER" mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -N -e "select 1" >/dev/null 2>&1; then
  echo "cannot reach ${MYSQL_CONTAINER} with MYSQL_ROOT_PASSWORD (see test/e2e_etl/README.md)" >&2
  exit 1
fi

echo "== postgres container ${PG_CONTAINER}"
if ! docker exec "$PG_CONTAINER" psql -U postgres -tAc "select 1" >/dev/null 2>&1; then
  echo "cannot reach ${PG_CONTAINER}; set PG_CONTAINER if the container has another name" >&2
  exit 1
fi

python3 tools/dq_driver.py bootstrap

cat <<'EOF'

installed.
  run the fixture : ./run_quality.sh
  read the evidence: PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f verify_quality.sql
EOF
