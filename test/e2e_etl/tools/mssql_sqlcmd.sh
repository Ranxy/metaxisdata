#!/usr/bin/env bash
# Run T-SQL against the local MSSQL container as sa.
#
#   ./mssql_sqlcmd.sh mssql/10_ods_ddl.sql     # apply a script (the script selects its database)
#   MSSQL_SA_PASSWORD=… ./mssql_sqlcmd.sh -q "select count(*) from e2e_dwd.dim_customer"
#
# The password comes from the environment, never from this file and never from a
# command line: sqlcmd reads SQLCMDPASSWORD, and `docker exec -e VAR` (with no
# value) forwards it into the container from the caller's environment, so it stays
# out of `ps` on both sides.
#
#   SQLCMDPASSWORD      what the DAGs set, rendered from the mssql_e2e connection
#   MSSQL_SA_PASSWORD   the same credential under the name install.sh uses
#
# A DAG task cannot look the password up itself: Airflow points a task's
# AIRFLOW__DATABASE__SQL_ALCHEMY_CONN at a non-url on purpose, so a hook or the CLI
# inside the task cannot reach the metadata database. The task runner renders the
# connection into the task's environment before the task starts, and this script
# reads it there.
set -euo pipefail

CONTAINER="${MSSQL_CONTAINER:-mssql}"
SQLCMD_BIN="${SQLCMD_BIN:-/opt/mssql-tools18/bin/sqlcmd}"
LOGIN="${MSSQL_LOGIN:-sa}"
DATABASE="${MSSQL_DB:-e2e}"

usage() {
  echo "usage: $0 <file.sql> | $0 -q '<query>'" >&2
  exit 2
}

SQLCMDPASSWORD="${SQLCMDPASSWORD:-${MSSQL_SA_PASSWORD:-}}"
if [ -z "$SQLCMDPASSWORD" ]; then
  echo "$0: no SQL Server password: set SQLCMDPASSWORD or MSSQL_SA_PASSWORD" >&2
  exit 1
fi
export SQLCMDPASSWORD

case "${1:-}" in
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
