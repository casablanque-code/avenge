# Operations

How to run, configure and maintain the AVENGE stack. Concept and algorithms: [DOC.md](DOC.md).

## Ports

| Service    | Address                                       | Notes |
| ---------- | --------------------------------------------- | ----- |
| Grafana    | `http://grafana.localhost`                    | Via Caddy on `127.0.0.1:80`, same machine only. Grafana publishes no port. |
| Mosquitto  | `$MQTT_BIND:1883`                             | Auth + ACL required. Set `MQTT_BIND` to the LAN IP so edge devices can connect. |
| ClickHouse | `127.0.0.1:8123` (HTTP), `127.0.0.1:9000`     | Local only. |

## First run

Core (server or laptop):

```
cp core/.env.example core/.env         # set every password
core/mosquitto/gen-passwd.sh           # BEFORE the first `up` (creates the MQTT password file)
docker compose -f core/compose.yaml up -d
core/clickhouse/init.sh                # needs a healthy sm_clickhouse
docker compose -f core/compose.yaml restart telegraf
```

Edge (Raspberry Pi):

```
cp edge/.env.example edge/.env         # SERIAL_INTERFACE, CENTRAL_HOST, MQTT_PASSWORD, DIALOUT_GID
docker compose -f edge/compose.yaml up -d
```

## Secrets: who owns what

Compose reads `.env` only when containers are created. `docker compose restart` does **not** pick up changes; use
`docker compose -f core/compose.yaml up -d --force-recreate <services>`.

| Variable                       | File        | Used by                                | To change                                                                 |
| ------------------------------ | ----------- | -------------------------------------- | ------------------------------------------------------------------------- |
| `CLICKHOUSE_PASSWORD`          | `core/.env` | Admin: `init.sh`, manual queries       | Recreate `clickhouse`.                                                    |
| `CLICKHOUSE_TELEGRAF_PASSWORD` | `core/.env` | Telegraf to ClickHouse (INSERT only)   | Recreate `clickhouse telegraf`.                                           |
| `CLICKHOUSE_GRAFANA_PASSWORD`  | `core/.env` | Grafana to ClickHouse (read-only)      | Recreate `clickhouse grafana`.                                            |
| `MQTT_TELEGRAF_PASSWORD`       | `core/.env` | Telegraf to Mosquitto (read-only)      | Run `gen-passwd.sh`, recreate `mosquitto telegraf`.                       |
| `MQTT_EDGE_PASSWORD`           | `core/.env` | Edge devices to Mosquitto (write-only) | Run `gen-passwd.sh`, recreate `mosquitto`, then update every edge `.env`. |
| `MQTT_PASSWORD`                | `edge/.env` | Must equal `MQTT_EDGE_PASSWORD`        | Recreate `avenge-edge` on the device.                                     |
| `GRAFANA_ADMIN_PASSWORD`       | `core/.env` | Grafana web login                      | Read only on Grafana's first start. For a live instance run `docker exec sm_grafana grafana cli admin reset-admin-password '<new>'` and update `.env`. |

Rules: use letters and digits only (Telegraf puts its ClickHouse password into a URL). Never commit `.env` or `core/mosquitto/config/passwd`.

## Troubleshooting

| Symptom                                           | Cause                                         | Fix |
| ------------------------------------------------- | --------------------------------------------- | --- |
| Telegraf log: `not authorised`                    | `passwd` is out of date                       | `gen-passwd.sh`, recreate `mosquitto telegraf`. |
| Edge log: `CONNACK: ... not authorized`           | `MQTT_PASSWORD` differs from `MQTT_EDGE_PASSWORD` | Align the two, recreate `avenge-edge`. |
| Telegraf log: `Not enough privileges`             | Views not yet switched to DEFINER security    | Run `core/clickhouse/init.sh`, restart `telegraf`. |
| Grafana datasource error                          | Stale Grafana password                        | Recreate `clickhouse grafana`. |
| Mosquitto fails, `passwd` is a directory          | `up` ran before `gen-passwd.sh`               | `rmdir core/mosquitto/config/passwd`, run `gen-passwd.sh`, `up -d`. |
| Edge exits with permission denied on `/dev/tty*`  | Wrong `DIALOUT_GID` or `SERIAL_PORT`          | `getent group dialout` on the host, fix `edge/.env`. |

## Health checks

```
docker compose -f core/compose.yaml ps                   # all Up, grafana/clickhouse/mosquitto healthy
ss -ltnp | grep -E ':(80|1883|8123|9000)\b'              # only 127.0.0.1, 1883 on $MQTT_BIND
curl -sI http://grafana.localhost | head -1              # HTTP/1.1 302 Found
```
