#!/usr/bin/env bash
# Run the four-hop e2e chain in order, waiting for each DAG to finish.
#
# The hops are not wired together with cross-DAG dependencies: hop 3 reads the
# PostgreSQL tables that hop 2 writes, so firing them concurrently can copy a
# half-loaded table. This script makes the documented order executable.
#
#   ./run_chain.sh              # trigger and wait, fail on the first failure
#   TIMEOUT=600 ./run_chain.sh  # per-DAG wait budget in seconds (default 900)
set -euo pipefail

AIRFLOW_DIR="${AIRFLOW_DIR:-/home/ran/pycode/airflow}"
TIMEOUT="${TIMEOUT:-900}"
STAMP="$(date +%Y%m%dT%H%M%S)"
DAGS=(e2e_01_mysql_to_pg e2e_02_pg_transform e2e_03_pg_to_sr e2e_04_sr_transform)

airflow() { (cd "$AIRFLOW_DIR" && uv run airflow "$@"); }

run_state() { # run_state <dag_id> <run_id>
  # `dags state` prints the plain state on its own line; provider log lines can
  # precede it, so keep the last recognised value.
  airflow dags state "$1" "$2" 2>/dev/null | grep -oE '^(success|failed|running|queued)$' | tail -1
}

for dag in "${DAGS[@]}"; do
  run_id="e2e_${STAMP}_${dag}"
  airflow dags unpause "$dag" >/dev/null 2>&1 || true
  airflow dags trigger "$dag" --run-id "$run_id" >/dev/null
  printf '%-22s started  %s\n' "$dag" "$run_id"

  elapsed=0
  while :; do
    state="$(run_state "$dag" "$run_id" || true)"
    case "$state" in
      success)
        printf '%-22s success  (%ss)\n' "$dag" "$elapsed"
        break
        ;;
      failed)
        printf '%-22s FAILED   (%ss)\n' "$dag" "$elapsed"
        airflow tasks states-for-dag-run "$dag" "$run_id" 2>/dev/null || true
        exit 1
        ;;
    esac
    if [ "$elapsed" -ge "$TIMEOUT" ]; then
      printf '%-22s TIMEOUT after %ss (state=%s)\n' "$dag" "$TIMEOUT" "${state:-unknown}"
      exit 1
    fi
    sleep 10
    elapsed=$((elapsed + 10))
  done
done

echo
echo "chain finished; verifying row counts and lineage:"
echo "  PGPASSWORD=dev psql -h localhost -U dev -d metaxisdata -f $(dirname "$0")/verify_chain.sql"
