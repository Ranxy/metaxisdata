#!/usr/bin/env bash
# Run T-SQL against the local MSSQL container as sa, without putting the password
# anywhere it can be read or committed.
#
#   ./mssql_sqlcmd.sh mssql/10_ods_ddl.sql     # apply a script (the script selects its database)
#   ./mssql_sqlcmd.sh -q "select count(*) from e2e_dwd.dim_customer"
#   ./mssql_sqlcmd.sh --print-password         # what install.sh substitutes into the deployed DataX jobs
#
# The password comes from MSSQL_SA_PASSWORD, falling back to the Airflow
# connection mssql_e2e, so neither a DAG task nor a fixture file has to carry it.
# It is forwarded into the container through the environment — sqlcmd reads
# SQLCMDPASSWORD, and `docker exec -e VAR` (no value) keeps it off the command line.
#
#   MSSQL_SA_PASSWORD=… ./mssql_sqlcmd.sh -q "select 1"
set -euo pipefail

CONTAINER="${MSSQL_CONTAINER:-mssql}"
SQLCMD_BIN="${SQLCMD_BIN:-/opt/mssql-tools18/bin/sqlcmd}"
LOGIN="${MSSQL_LOGIN:-sa}"
DATABASE="${MSSQL_DB:-e2e}"

usage() {
  echo "usage: $0 <file.sql> | $0 -q '<query>' | $0 --print-password" >&2
  exit 2
}

password() {
  if [ -n "${MSSQL_SA_PASSWORD:-}" ]; then
    printf '%s' "$MSSQL_SA_PASSWORD"
    return
  fi
  # Inside an Airflow task the CLI is the running deployment's own; from a shell
  # it is whatever `airflow` on PATH points at.
  local cli="" candidate
  for candidate in \
    "${AIRFLOW_BIN:-}" \
    "$(command -v airflow 2>/dev/null || true)" \
    "${AIRFLOW_DIR:-/home/ran/pycode/airflow}/.venv/bin/airflow"; do
    if [ -n "$candidate" ] && [ -x "$candidate" ]; then
      cli="$candidate"
      break
    fi
  done
  if [ -z "$cli" ]; then
    return
  fi
  "$cli" connections get mssql_e2e -o json 2>/dev/null |
    python3 -c 'import json,sys
try:
    found = json.load(sys.stdin)
    if isinstance(found, list):
        found = found[0] if found else {}
    print(found.get("password") or "", end="")
except Exception:
    pass' 2>/dev/null || true
}

SQLCMDPASSWORD="$(password)"
if [ -z "$SQLCMDPASSWORD" ]; then
  echo "$0: no SQL Server password: set MSSQL_SA_PASSWORD, or create the mssql_e2e Airflow connection" >&2
  exit 1
fi
export SQLCMDPASSWORD

case "${1:-}" in
  --print-password)
    printf '%s\n' "$SQLCMDPASSWORD"
    ;;
  -q | --query)
    [ $# -ge 2 ] || usage
    exec docker exec -e SQLCMDPASSWORD "$CONTAINER" "$SQLCMD_BIN" \
      -S localhost -U "$LOGIN" -C -b -d "$DATABASE" -h -1 -W -Q "$2"
    ;;
  "" | -*)
    usage
    ;;
  *)
    FILE="$1"
    [ -f "$FILE" ] || {
      echo "$0: no such file: $FILE" >&2
      exit 1
    }
    # -b makes a failing batch set the exit code, which is what a DAG task reads.
    exec docker exec -i -e SQLCMDPASSWORD "$CONTAINER" "$SQLCMD_BIN" \
      -S localhost -U "$LOGIN" -C -b -d master <"$FILE"
    ;;
esac
