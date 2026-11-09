#!/usr/bin/env bash
# Run the data-quality end-to-end fixture and assert its exact expectations.
#
#   METAXISDATA_EMAIL=admin@example.com METAXISDATA_PASSWORD=... ./run_quality.sh
#   ./run_quality.sh --no-install   # reuse the fixture state, do not reset it
#
# The driver creates the rule suite, runs it, and checks the verdict matrix, the
# issue/notification lifecycle, the outbound webhook, the scheduled tick, the
# report, the read models and the PostgreSQL semantics. It exits non-zero on the
# first failing expectation; every check it made is printed.
#
# After a successful run, verify_quality.sql is the human-readable evidence and
# its section 10 gates must all be 0.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

: "${METAXISDATA_EMAIL:?set METAXISDATA_EMAIL to a workspace administrator}"
: "${METAXISDATA_PASSWORD:?set METAXISDATA_PASSWORD for that administrator}"

export METAXISDATA_URL="${METAXISDATA_URL:-http://localhost:8083}"
export MYSQL_CONTAINER="${MYSQL_CONTAINER:-mysql8-db-1}"
export MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-rootadmin}"
export PG_CONTAINER="${PG_CONTAINER:-pg16-db-1}"

if [[ "${1:-}" != "--no-install" ]]; then
  ./install.sh
  echo
fi

echo "== run the fixture"
python3 tools/dq_driver.py run

echo
echo "assertions passed. read the evidence with:"
echo "  PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f verify_quality.sql"
