# A.V.E.N.G.E

**Anomaly Vibration Edge Network Go Engine**

A Go-based edge computing engine for real-time vibration analytics, inline signal compression, and predictive anomaly detection. Zero external runtime dependencies, zero-heap-allocation hot path.

> Concept, motivation, and the broader case for edge-first vibration analytics live in [`docs/DOC.md`](docs/DOC.md). This README covers what's running and how to run it.

---

## Prerequisites

| Requirement | Version |
|---|---|
| Go | 1.22+ |
| Docker + Docker Compose | 24.0+ |
| Python | 3.10+ |

---

## Architecture

```
┌────────────────────┐                     ┌─────────────────────────┐
│   Sensor source     │   serial / JSONL    │      Edge Engine (Go)    │
│  USB / RS-485 / sim  ├────────────────────>│  FFT → SDT → Z-score    │
└────────────────────┘                     │  Multi-sensor Manager   │
                                            └────────────┬─────────────┘
                                                          │ MQTT 3.1.1 (QoS 1)
                                                          │ sm/telemetry/<id>
                                                          │ sm/anomaly/<id>
                                                          ▼
┌────────────────────┐    HTTP INSERT       ┌─────────────────────────┐
│  ClickHouse (OLAP)   │<─────────────────────┤    Telegraf Agent       │
└──────────┬───────────┘     batched writes  │   (MQTT consumer)        │
           │ SQL                              └─────────────────────────┘
           ▼
┌────────────────────┐
│      Grafana         │
│ (auto-provisioned)   │
└────────────────────┘
```

The stack is split into two independently deployable units:

- **`edge/`** — runs on the device next to the sensor (Raspberry Pi, industrial gateway, or a laptop for local dev). Reads raw samples, runs the full DSP pipeline, publishes only structural updates over MQTT.
- **`core/`** — runs on a server or laptop on the same network. Mosquitto broker, Telegraf ingestion, ClickHouse storage, Grafana dashboards. One compose file for the whole stack.

Both sides are configured entirely through `.env` files — the edge binary itself is never rebuilt to switch sensor interface, sensor ID, or broker address.

---

## Repository Structure

```
avenge/
├── firmware-sim/                # Hardware-free signal generator (Python)
├── edge/                        # AVENGE engine (Go)
│   ├── cmd/edge-filter/         # Pipeline entrypoint, graceful shutdown
│   ├── internal/                # sensor, fft, filter (SDT), anomaly, scoring, mqtt, envfile
│   ├── Dockerfile               # Multi-stage build -> distroless, ARM64-ready
│   ├── compose.yaml             # Deploy target: Raspberry Pi / edge device
│   └── .env.example
├── core/                        # Central analytics stack
│   ├── compose.yaml             # Mosquitto + Telegraf + ClickHouse + Grafana + Caddy
│   ├── Caddyfile                # grafana.localhost -> grafana:3000
│   ├── .env.example
│   ├── mosquitto/               # config, ACL, gen-passwd.sh
│   ├── telegraf/
│   ├── clickhouse/              # init.sql, init.sh, users.d/ (service accounts)
│   └── grafana/provisioning/    # Auto-provisioned datasource + dashboard
└── docs/
    ├── DOC.md                   # Concept, algorithms, security model
    └── OPERATIONS.md            # Ports, first run, secrets, troubleshooting
```

---

## Configuration

Both `edge/` and `core/` read a local, gitignored `.env`:

```
cp core/.env.example core/.env
cp edge/.env.example edge/.env
```

Passwords, which file owns which, and how to change them: [`docs/OPERATIONS.md`](docs/OPERATIONS.md#secrets-who-owns-what).

`edge/.env` selects the sensor source (`SERIAL_INTERFACE`), the core address (`CENTRAL_HOST`) and the MQTT credentials. Run directly (no Docker), the binary loads `edge/.env` itself. Priority, highest to lowest: CLI flag (`--broker`), shell-exported env var, `edge/.env`, built-in default.

---

## Running Locally

Everything on one machine, with the simulator as the sensor.

```
# 1. Core stack (set passwords in core/.env first)
core/mosquitto/gen-passwd.sh
docker compose -f core/compose.yaml up -d
core/clickhouse/init.sh
docker compose -f core/compose.yaml restart telegraf

# 2. Edge binary: set MQTT_USER=edge and MQTT_PASSWORD (= MQTT_EDGE_PASSWORD) in edge/.env
cd edge && go build -o edge-filter ./cmd/edge-filter/ && cd ..

# Normal mode - synthetic steady-state signal
python3 firmware-sim/signal_generator.py stream \
  | ./edge/edge-filter --broker localhost:1883 --sensor-id bearing_01

# Fault injection - anomaly after 15 s
python3 firmware-sim/signal_generator.py stream --anomaly-after 15 \
  | ./edge/edge-filter --broker localhost:1883 --sensor-id bearing_01

# Offline, no broker
python3 firmware-sim/signal_generator.py batch --samples 8000 --anomaly \
  | ./edge/edge-filter --no-mqtt --sensor-id bearing_01
```

Grafana: `http://grafana.localhost` (login `admin`, password `GRAFANA_ADMIN_PASSWORD` from `core/.env`). The **Vibration Monitor** dashboard is auto-provisioned: RMS timeline, SDT scatter, anomaly events table, compression ratio gauge.

---

## Running on Real Hardware

Core: set `MQTT_BIND` in `core/.env` to the machine's LAN IP, then run the steps above.

Edge device (Raspberry Pi or similar):

```
cp edge/.env.example edge/.env
# set SERIAL_INTERFACE, SERIAL_PORT, DIALOUT_GID (getent group dialout),
# CENTRAL_HOST=<core IP>, MQTT_PASSWORD=<MQTT_EDGE_PASSWORD>
docker compose -f edge/compose.yaml up -d
docker compose -f edge/compose.yaml logs -f avenge-edge
```

The image is published to `ghcr.io/casablanque-code/avenge-edge` by the Release workflow on a `v*.*.*` tag (the package must be public, or run `docker login ghcr.io` on the device). Pin the version with `AVENGE_EDGE_TAG`. Exactly one serial device is passed into the container (`SERIAL_PORT`).

---

## Data Schema

| MQTT Topic | Payload | Frequency |
|---|---|---|
| `sm/telemetry/<sensor_id>` | SDT turning points + window RMS | Every ~10s (20 windows) |
| `sm/anomaly/<sensor_id>` | State transition (`onset` / `clear`) | On state change only |

| ClickHouse Table | Engine | TTL | Purpose |
|---|---|---|---|
| `iot.telemetry` | MergeTree | 30 days | Compressed vibration points |
| `iot.anomaly_events` | MergeTree | 90 days | Alert history |
| `iot.telemetry_1m` | SummingMergeTree (MV) | — | 1-minute RMS aggregates |

---

## Technology Stack

| Component | Technology |
|---|---|
| Edge engine | Go 1.22, stdlib + raw syscalls for serial I/O |
| Signal generator | Python 3.10+ |
| MQTT broker | Eclipse Mosquitto 2.0 |
| Ingestion agent | InfluxData Telegraf 1.30 |
| Time-series database | ClickHouse 24.3 |
| Dashboards | Grafana 10.4 |
| Containerization | Docker Compose, distroless runtime image |

---

## Status

- [x] Cooley-Tukey FFT, zero-alloc processor
- [x] Bristol SDT compression
- [x] Stateful Z-score detector with anti-adaptation interlock
- [x] MQTT 3.1.1 client (stdlib only) + resilient auto-reconnect wrapper
- [x] Frequency band-specific anomaly filtering
- [x] Multi-sensor orchestration (`sensor.Manager`)
- [x] Graceful shutdown (context cancellation, drained MQTT buffer)
- [x] Hardware serial input (USB / RS-485), interface selected via `.env`
- [x] Externalized configuration — no rebuild needed to change deployment target
- [x] `.env` auto-loaded by the binary directly, not just by Docker Compose
- [x] Dockerfile for edge binary (ARM64, distroless)
- [ ] Real sensor hardware validation (Raspberry Pi + ADXL345/MPU6050 bench test)
- [ ] Outbound alert webhooks (Slack, PagerDuty)
- [ ] Edge REST API for runtime configuration
- [x] CI: automated build + test on push, image publish on tag
- [x] MQTT authentication + per-user ACL, least-privilege ClickHouse accounts, Caddy proxy
- [ ] MQTT over TLS
- [ ] "Sensor went silent" alert
