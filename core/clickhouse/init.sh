#!/usr/bin/env bash
# init.sh — apply the AVENGE schema to the running ClickHouse container
#
# Usage:  core/clickhouse/init.sh        (run from anywhere)
#         core/clickhouse/init.sh -h     (help)
#
# Reads CLICKHOUSE_USER / CLICKHOUSE_PASSWORD (admin) from core/.env,
# creates the iot database and applies core/clickhouse/init.sql.
# Safe to re-run. The container sm_clickhouse must be healthy.

set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
fi

CORE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$CORE_DIR/.env"
[[ -f "$ENV_FILE" ]] || { echo "error: $ENV_FILE not found (copy core/.env.example)" >&2; exit 1; }

# Read a KEY=value line without sourcing the file (values may contain symbols).
get() { grep -E "^$1=" "$ENV_FILE" | tail -n1 | cut -d= -f2-; }

CH_USER="$(get CLICKHOUSE_USER)"; CH_USER="${CH_USER:-default}"
CH_PASS="$(get CLICKHOUSE_PASSWORD)"
[[ -n "$CH_PASS" ]] || { echo "error: CLICKHOUSE_PASSWORD is empty in $ENV_FILE" >&2; exit 1; }

state="$(docker inspect -f '{{.State.Health.Status}}' sm_clickhouse 2>/dev/null || true)"
[[ "$state" == "healthy" ]] || { echo "error: sm_clickhouse is not healthy (state: ${state:-not running})" >&2; exit 1; }

ch() { docker exec -i sm_clickhouse clickhouse-client --user "$CH_USER" --password "$CH_PASS" "$@"; }

ch --query "CREATE DATABASE IF NOT EXISTS iot"
ch --multiquery < "$CORE_DIR/clickhouse/init.sql"

echo "┌──────────────────────────────────────────────┐"
echo "│ schema applied (database: iot)               │"
echo "└──────────────────────────────────────────────┘"
ch --query "SELECT name AS object, engine FROM system.tables WHERE database = 'iot' ORDER BY name FORMAT PrettyCompactMonoBlock"
echo "Next: docker compose -f core/compose.yaml restart telegraf"
