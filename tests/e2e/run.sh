#!/usr/bin/env bash
# End-to-end test of the AVENGE core stack.
#
# Starts the real stack with `docker compose up -d` using throwaway random
# passwords, replays a seeded simulator signal through the edge engine, and
# checks the data path, the security posture and the password-rotation flow.
#
# Usage:  tests/e2e/run.sh
# Needs:  docker (with compose v2), python3, openssl, curl, and a built edge
#         binary (default: edge/edge-filter; override with EDGE_BIN=...).
# Notes:  uses ports 80, 1883, 8123, 9000 on localhost. Refuses to run if
#         core/.env already exists unless E2E_FORCE=1 (the file is backed up
#         and restored). E2E_KEEP=1 leaves the stack running afterwards.
#         RUN_SECONDS (default 36) sets the replay length.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE_FILE="$ROOT/core/compose.yaml"
ENV_FILE="$ROOT/core/.env"
ENV_BACKUP="$ROOT/core/.env.e2e-backup"
EDGE_BIN="${EDGE_BIN:-$ROOT/edge/edge-filter}"
RUN_SECONDS="${RUN_SECONDS:-36}"
SENSOR="e2e"

compose() { docker compose -f "$COMPOSE_FILE" "$@"; }

# ── tiny test framework ─────────────────────────────────────────────────
FAILED=0
workdir=""
env_created=0
started=0
step()  { printf '\n== %s\n' "$1"; }
ok()    { printf '  \033[32m✓\033[0m %s\n' "$1"; }
bad()   { printf '  \033[31m✗\033[0m %s\n' "$1"; FAILED=$((FAILED + 1)); }
check() { local desc=$1; shift; if "$@" >/dev/null 2>&1; then ok "$desc"; else bad "$desc"; fi; }
check_fails() { local desc=$1; shift; if "$@" >/dev/null 2>&1; then bad "$desc"; else ok "$desc"; fi; }
wait_for() { # <timeout-seconds> <command...>
  local timeout=$1 i=0; shift
  until "$@" >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -gt "$timeout" ]; then return 1; fi
    sleep 1
  done
}

cleanup() {
  local rc=$?
  if [ "$started" -eq 1 ] && { [ "$rc" -ne 0 ] || [ "$FAILED" -ne 0 ]; }; then
    printf '\n== container logs (last 120 lines each)\n'
    compose logs --no-color --tail=120 2>&1 || true
    if [ -n "$workdir" ] && [ -f "$workdir/edge.log" ]; then
      printf '\n== edge-filter log (tail)\n'
      tail -n 40 "$workdir/edge.log" || true
    fi
  fi
  if [ "$started" -eq 1 ] && [ "${E2E_KEEP:-0}" != 1 ]; then
    compose down -v --remove-orphans >/dev/null 2>&1 || true
  fi
  if [ "$env_created" -eq 1 ]; then
    rm -f "$ENV_FILE"
    if [ -f "$ENV_BACKUP" ]; then mv "$ENV_BACKUP" "$ENV_FILE"; fi
  fi
  [ -n "$workdir" ] && rm -rf "$workdir"
  return "$rc"
}
trap cleanup EXIT

# ── helpers ─────────────────────────────────────────────────────────────
rand() { openssl rand -hex 10; }
ch() { # <user> <password> <query>
  docker exec sm_clickhouse clickhouse-client --user "$1" --password "$2" --query "$3"
}
mq_pub() { # <user> <password> <topic> <message>
  docker exec sm_mosquitto mosquitto_pub -h localhost -u "$1" -P "$2" -t "$3" -m "$4"
}
is_healthy() { [ "$(docker inspect -f '{{.State.Health.Status}}' "$1")" = healthy ]; }
exited_ok()  { [ "$(docker inspect -f '{{.State.Status}} {{.State.ExitCode}}' "$1")" = "exited 0" ]; }
published_local_only() { # <container> <port>
  local out
  out=$(docker port "$1" "$2/tcp") || return 1
  [ -n "$out" ] && ! grep -qv '^127\.0\.0\.1:' <<<"$out"
}
rows_at_least() { # <query> <minimum>
  local n
  n=$(ch default "${PW[CLICKHOUSE_PASSWORD]}" "$1") || return 1
  [ "$n" -ge "$2" ]
}
gf() { curl -s --max-time 10 --resolve grafana.localhost:80:127.0.0.1 "$@"; }
grafana_answers() {
  local code
  code=$(gf -o /dev/null -w '%{http_code}' http://grafana.localhost/)
  [ "$code" = 200 ] || [ "$code" = 302 ]
}
datasource_healthy() {
  gf -u "admin:${PW[GRAFANA_ADMIN_PASSWORD]}" \
    http://grafana.localhost/api/datasources/uid/clickhouse_iot/health | grep -Eq '"status": ?"OK"'
}
dashboard_provisioned() {
  gf -u "admin:${PW[GRAFANA_ADMIN_PASSWORD]}" \
    'http://grafana.localhost/api/search?query=Vibration' | grep -q 'Vibration'
}
replay() ( # runs in a subshell so the cd does not leak
  cd "$workdir"
  timeout "$RUN_SECONDS" python3 "$ROOT/firmware-sim/signal_generator.py" \
    --seed 42 stream --rate 1600 --anomaly-after 10 2>/dev/null \
    | env SENSOR_SAMPLE_RATE=1600 CENTRAL_HOST=127.0.0.1 MQTT_PORT=1883 \
        MQTT_USER=edge MQTT_PASSWORD="${PW[MQTT_EDGE_PASSWORD]}" \
        "$EDGE_BIN" --sensor-id "$SENSOR" >"$workdir/edge.log" 2>&1
  rc=("${PIPESTATUS[@]}")
  # rc[0] is `timeout` (124 = stopped on schedule), rc[1] is the edge engine.
  [ "${rc[1]}" -eq 0 ]
)

# ── 0. prerequisites ────────────────────────────────────────────────────
step "Prerequisites"
for tool in docker python3 openssl curl; do
  command -v "$tool" >/dev/null || { echo "missing tool: $tool"; exit 2; }
done
[ -x "$EDGE_BIN" ] || { echo "edge binary not found: $EDGE_BIN (build it: cd edge && go build -o edge-filter ./cmd/edge-filter)"; exit 2; }
if [ -f "$ENV_FILE" ]; then
  [ "${E2E_FORCE:-0}" = 1 ] || { echo "$ENV_FILE exists; set E2E_FORCE=1 to back it up and continue"; exit 2; }
  mv "$ENV_FILE" "$ENV_BACKUP"
fi
env_created=1
ok "tools and edge binary present"

# ── 1. throwaway credentials, derived from .env.example ────────────────
declare -A PW
while IFS= read -r line; do
  if [[ "$line" =~ ^([A-Z_]+PASSWORD)=.*$ ]]; then
    var="${BASH_REMATCH[1]}"
    PW[$var]="$(rand)"
    printf '%s=%s\n' "$var" "${PW[$var]}"
  else
    printf '%s\n' "$line"
  fi
done <"$ROOT/core/.env.example" >"$ENV_FILE"
workdir="$(mktemp -d)" # keeps stray .env files out of the edge binary's lookup path
started=1

# ── 2. preflight rejects bad credentials ────────────────────────────────
step "Preflight"
check_fails "placeholder password is rejected" \
  env CLICKHOUSE_PASSWORD=changeme docker compose -f "$COMPOSE_FILE" run --rm --no-deps preflight
check_fails "non-alphanumeric password is rejected" \
  env MQTT_EDGE_PASSWORD='a:b@c' docker compose -f "$COMPOSE_FILE" run --rm --no-deps preflight

# ── 3. one command starts everything ────────────────────────────────────
step "docker compose up -d"
compose up -d
wait_for 120 is_healthy sm_clickhouse
wait_for 120 is_healthy sm_mosquitto
wait_for 120 is_healthy sm_grafana
check "preflight finished successfully" exited_ok sm_preflight
check "schema applied by clickhouse-init" exited_ok sm_clickhouse_init
check "clickhouse healthy" is_healthy sm_clickhouse
check "mosquitto healthy" is_healthy sm_mosquitto
check "grafana healthy" is_healthy sm_grafana
check "grafana answers through Caddy" grafana_answers

# ── 4. security posture ─────────────────────────────────────────────────
step "Network exposure"
check "clickhouse 8123 bound to localhost" published_local_only sm_clickhouse 8123
check "clickhouse 9000 bound to localhost" published_local_only sm_clickhouse 9000
check "mosquitto 1883 bound to MQTT_BIND (localhost)" published_local_only sm_mosquitto 1883
check "caddy 80 bound to localhost" published_local_only sm_caddy 80
check_fails "mosquitto websocket 9001 is not published" docker port sm_mosquitto 9001/tcp
check "grafana publishes no port" test -z "$(docker port sm_grafana)"

step "MQTT authentication and ACL"
check_fails "anonymous publish is refused" \
  docker exec sm_mosquitto mosquitto_pub -h localhost -t sm/telemetry/anon -m x
check_fails "wrong password is refused" mq_pub edge wrongpassword sm/telemetry/anon x
acl_log="$workdir/acl.log"
docker exec sm_mosquitto mosquitto_sub -h localhost -u telegraf \
  -P "${PW[MQTT_TELEGRAF_PASSWORD]}" -t 'sm/#' -v -W 8 >"$acl_log" 2>/dev/null &
sub_pid=$!
sleep 2
mq_pub edge "${PW[MQTT_EDGE_PASSWORD]}" sm/telemetry/acl-allowed x || true
mq_pub edge "${PW[MQTT_EDGE_PASSWORD]}" sm/forbidden/acl-denied x || true
wait "$sub_pid" || true
check "edge can publish to sm/telemetry/#" grep -q 'sm/telemetry/acl-allowed' "$acl_log"
check_fails "edge cannot publish outside its ACL" grep -q 'sm/forbidden' "$acl_log"

step "ClickHouse least privilege"
check_fails "telegraf cannot read" \
  ch telegraf "${PW[CLICKHOUSE_TELEGRAF_PASSWORD]}" "SELECT 1 FROM iot.telemetry LIMIT 1"
check "grafana can read" \
  ch grafana "${PW[CLICKHOUSE_GRAFANA_PASSWORD]}" "SELECT count() FROM iot.telemetry"
check_fails "grafana cannot insert" \
  ch grafana "${PW[CLICKHOUSE_GRAFANA_PASSWORD]}" "INSERT INTO iot.telemetry_raw VALUES ('x')"
check_fails "grafana cannot drop" \
  ch grafana "${PW[CLICKHOUSE_GRAFANA_PASSWORD]}" "DROP TABLE iot.telemetry"

# ── 5. data path: simulator -> edge -> MQTT -> Telegraf -> ClickHouse ───
step "Replay simulator through the edge engine (${RUN_SECONDS}s, real time)"
check "edge engine ran and exited cleanly" replay
wait_for 60 rows_at_least "SELECT count() FROM iot.telemetry WHERE sensor_id = '$SENSOR'" 3 || true
check "telemetry rows arrived (>= 3)" \
  rows_at_least "SELECT count() FROM iot.telemetry WHERE sensor_id = '$SENSOR'" 3
check "per-minute aggregate populated" \
  rows_at_least "SELECT count() FROM iot.telemetry_1m WHERE sensor_id = '$SENSOR'" 1
check "critical anomaly onset recorded" \
  rows_at_least "SELECT count() FROM iot.anomaly_events WHERE sensor_id = '$SENSOR' AND event_type = 'onset' AND severity = 'critical'" 1

# ── 6. Grafana ──────────────────────────────────────────────────────────
step "Grafana"
check "ClickHouse datasource is healthy" datasource_healthy
check "Vibration dashboard is provisioned" dashboard_provisioned

# ── 7. password rotation: edit .env, docker compose up -d ───────────────
step "Rotate the edge MQTT password (edit .env, run up -d)"
old_pw="${PW[MQTT_EDGE_PASSWORD]}"
new_pw="$(rand)"
sed -i "s/^MQTT_EDGE_PASSWORD=.*/MQTT_EDGE_PASSWORD=$new_pw/" "$ENV_FILE"
compose up -d
wait_for 120 is_healthy sm_mosquitto
check_fails "old edge password is rejected" mq_pub edge "$old_pw" sm/telemetry/rotation x
check "new edge password is accepted" mq_pub edge "$new_pw" sm/telemetry/rotation x

# ── summary ─────────────────────────────────────────────────────────────
echo
if [ "$FAILED" -ne 0 ]; then
  printf '\033[31me2e FAILED: %d check(s)\033[0m\n' "$FAILED"
  exit 1
fi
printf '\033[32me2e passed\033[0m\n'
