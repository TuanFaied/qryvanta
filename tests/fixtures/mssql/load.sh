#!/usr/bin/env bash
# Loads the QryvantaSample fixture database. Used by the `fixtures` service in
# docker-compose.yml; it can also be run by hand anywhere sqlcmd (mssql-tools18) is installed:
#
#   MSSQL_HOST=localhost,14333 MSSQL_SA_PASSWORD=... QRYVANTA_READER_PASSWORD=... ./load.sh
set -euo pipefail

host="${MSSQL_HOST:-sqlserver}"
dir="$(cd "$(dirname "$0")" && pwd)"
sqlcmd="${SQLCMD:-/opt/mssql-tools18/bin/sqlcmd}"

: "${MSSQL_SA_PASSWORD:?MSSQL_SA_PASSWORD must be set}"
: "${QRYVANTA_READER_PASSWORD:?QRYVANTA_READER_PASSWORD must be set}"

for script in "$dir"/[0-9][0-9]_*.sql; do
  echo "Running $(basename "$script")"
  # -C trusts the container's self-signed certificate; -b stops on the first error.
  "$sqlcmd" -S "$host" -U sa -P "$MSSQL_SA_PASSWORD" -C -b \
    -v ReaderPassword="$QRYVANTA_READER_PASSWORD" -i "$script"
done

echo "QryvantaSample is ready."
