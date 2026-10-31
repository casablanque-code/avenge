#!/usr/bin/env bash
# gen-passwd.sh — create core/mosquitto/config/passwd from core/.env
#
# Usage:  core/mosquitto/gen-passwd.sh        (run from anywhere)
#         core/mosquitto/gen-passwd.sh -h     (help)
#
# Reads MQTT_EDGE_PASSWORD and MQTT_TELEGRAF_PASSWORD from core/.env and
# writes hashed entries for the users "edge" and "telegraf".
# Requires Docker (uses the eclipse-mosquitto:2.0 image for hashing).
# Restart the broker afterwards: docker compose -f core/compose.yaml restart mosquitto

set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
fi

CORE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$CORE_DIR/.env"
OUT="$CORE_DIR/mosquitto/config/passwd"

[[ -f "$ENV_FILE" ]] || { echo "error: $ENV_FILE not found (copy core/.env.example)" >&2; exit 1; }

# Read a KEY=value line without sourcing the file (values may contain symbols).
get() { grep -E "^$1=" "$ENV_FILE" | tail -n1 | cut -d= -f2-; }

EDGE_PW="$(get MQTT_EDGE_PASSWORD)"
TELEGRAF_PW="$(get MQTT_TELEGRAF_PASSWORD)"

for pair in "MQTT_EDGE_PASSWORD:$EDGE_PW" "MQTT_TELEGRAF_PASSWORD:$TELEGRAF_PW"; do
  name="${pair%%:*}"; val="${pair#*:}"
  [[ -n "$val" ]] || { echo "error: $name is empty in $ENV_FILE" >&2; exit 1; }
  [[ "$val" != "changeme" ]] || echo "warning: $name is still 'changeme'" >&2
  [[ "$val" =~ [[:space:]] ]] && { echo "error: $name must not contain spaces" >&2; exit 1; }
done

hash_line() {
  docker run --rm eclipse-mosquitto:2.0 sh -c \
    'mosquitto_passwd -c -b /tmp/p "$1" "$2" >/dev/null 2>&1 && cat /tmp/p' _ "$1" "$2"
}

# If the stack was started before this script ever ran, Docker created an
# empty directory at the bind-mount path; replace it with the real file.
if [[ -d "$OUT" ]]; then
  rmdir "$OUT" 2>/dev/null || { echo "error: $OUT is a non-empty directory; remove it manually" >&2; exit 1; }
fi

umask 022
{ hash_line edge "$EDGE_PW"; hash_line telegraf "$TELEGRAF_PW"; } > "$OUT.tmp"
mv "$OUT.tmp" "$OUT"

echo "┌──────────────────────────────────────────────┐"
echo "│ passwd written: users edge, telegraf         │"
echo "└──────────────────────────────────────────────┘"
echo "File: $OUT"
echo "Next: docker compose -f core/compose.yaml up -d --force-recreate mosquitto telegraf"
