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
cp core/.env.example core/.env         # set every password (letters and digits only)
docker compose -f core/compose.yaml up -d
```

That is all. Startup is ordered and checked automatically:

1. `preflight` rejects missing, placeholder (`changeme`) and non-alphanumeric passwords with a readable message.
2. `mosquitto` builds its password file from `.env` at every start.
3. `clickhouse-init` applies the schema as soon as ClickHouse is healthy (idempotent, safe on every `up`).
4. `telegraf` and `grafana` start once the schema exists.

Edge (Raspberry Pi):

```
cp edge/.env.example edge/.env         # SERIAL_INTERFACE, CENTRAL_HOST, MQTT_PASSWORD, DIALOUT_GID
docker compose -f edge/compose.yaml up -d
```

## Accounts

Usernames are fixed by the configuration; only passwords live in `.env`.

| Service            | Username                          | Purpose                          | Permissions                                   | Password variable (`core/.env`)  |
| ------------------ | --------------------------------- | -------------------------------- | --------------------------------------------- | -------------------------------- |
| Grafana (web)      | `admin`                           | Dashboard login                  | Admin                                         | `GRAFANA_ADMIN_PASSWORD`         |
| ClickHouse (admin) | `default` (or `CLICKHOUSE_USER`)  | Manual queries, schema           | Everything                                    | `CLICKHOUSE_PASSWORD`            |
| ClickHouse         | `telegraf`                        | Telegraf writes telemetry        | INSERT on `iot.telemetry_raw` only            | `CLICKHOUSE_TELEGRAF_PASSWORD`   |
| ClickHouse         | `grafana`                         | Grafana reads                    | SELECT on `iot.*`, read-only                  | `CLICKHOUSE_GRAFANA_PASSWORD`    |
| MQTT               | `edge`                            | Edge devices publish             | Write `sm/telemetry/#`, `sm/anomaly/#`        | `MQTT_EDGE_PASSWORD`             |
| MQTT               | `telegraf`                        | Telegraf subscribes              | Read `sm/telemetry/#`, `sm/anomaly/#`, `$SYS` | `MQTT_TELEGRAF_PASSWORD`         |

The edge device stores the `edge` password as `MQTT_PASSWORD` (and the user as `MQTT_USER`) in `edge/.env`; the two values must match the core side.
Source of truth: `core/mosquitto/config/acl` and `core/clickhouse/users.d/avenge-users.xml`.

## Changing a password

Edit `core/.env`, then run:

```
docker compose -f core/compose.yaml up -d
```

Compose notices the changed environment and recreates only the affected services; the broker rebuilds its password file and ClickHouse reloads its accounts on start. Then:

- `MQTT_EDGE_PASSWORD`: put the same value into `MQTT_PASSWORD` in every `edge/.env` and run `docker compose -f edge/compose.yaml up -d` on each device.
- `GRAFANA_ADMIN_PASSWORD`: Grafana reads it only on its first start. For a live instance run `docker exec sm_grafana grafana cli admin reset-admin-password '<new>'` and update `.env`.

Plain `docker compose restart` does **not** pick up `.env` changes; always use `up -d`.

Rules: letters and digits only. Never commit `.env`.

## Troubleshooting

| Symptom                                                 | Cause                                           | Fix |
| ------------------------------------------------------- | ----------------------------------------------- | --- |
| `required variable ... is missing a value`              | `core/.env` missing or incomplete               | `cp core/.env.example core/.env`, fill in every password. |
| `preflight` prints `ERROR: ... placeholder` or `... only letters and digits` | Placeholder or unsupported character in a password | Fix the named variable in `core/.env`, run `up -d`. |
| Telegraf log: `not authorised`                          | `.env` edited but containers not recreated      | `docker compose -f core/compose.yaml up -d`. |
| Edge log: `CONNACK: ... not authorized`                 | `MQTT_PASSWORD` differs from `MQTT_EDGE_PASSWORD` | Align the two, `up -d` on the device. |
| Telegraf log: `Not enough privileges`                   | Schema from an older version                    | `docker compose -f core/compose.yaml run --rm clickhouse-init`, then `up -d`. |
| Grafana datasource error                                | Stale Grafana password                          | `docker compose -f core/compose.yaml up -d`. |
| Edge exits with permission denied on `/dev/tty*`        | Wrong `DIALOUT_GID` or `SERIAL_PORT`            | `getent group dialout` on the host, fix `edge/.env`. |

## Health checks

```
docker compose -f core/compose.yaml ps                   # long-running services Up; grafana/clickhouse/mosquitto healthy
ss -ltnp | grep -E ':(80|1883|8123|9000)\b'              # only 127.0.0.1, 1883 on $MQTT_BIND
curl -sI http://grafana.localhost | head -1              # HTTP/1.1 302 Found
```

`preflight` and `clickhouse-init` show as `Exited (0)`; that is expected.
