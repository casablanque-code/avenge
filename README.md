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
┌────────────────────┐    Native TCP bulk   ┌─────────────────────────┐
│  ClickHouse (OLAP)   │<─────────────────────┤    Telegraf Agent       │
└──────────┬───────────┘     ingestion       │   (MQTT consumer)        │
           │ SQL                              └─────────────────────────┘
           ▼
┌────────────────────┐
│      Grafana         │
│ (auto-provisioned)   │
└────────────────────┘
```

The stack is split into two independently deployable units:

- **`edge/`** — runs on the device next to the sensor (Raspberry Pi, industrial gateway, or a laptop for local dev). Reads raw samples, runs the full DSP pipeline, publishes only structural updates over MQTT.
- **`core/`** — runs on a server or laptop on the same network. Mosquitto broker, Telegraf ingestion, ClickHouse storage, Grafana dashboards. One `docker compose up` for the whole stack.

Both sides are configured entirely through `.env` files — the edge binary itself is never rebuilt to switch sensor interface, sensor ID, or broker address.

---

## Repository Structure

```
avenge/
├── firmware-sim/                # Hardware-free signal generator (Python)
│   ├── signal_generator.py      # Synthetic sensor output: normal + bearing fault modes
│   └── inspect.py               # ASCII sparkline visualizer
├── edge/                        # AVENGE engine (Go)
│   ├── cmd/edge-filter/main.go  # Pipeline entrypoint, graceful shutdown
│   ├── Dockerfile               # Multi-stage build → distroless, ARM64-ready
│   ├── docker-compose.yml       # Deploy target: Raspberry Pi / edge device
│   ├── .env.example
│   └── internal/
│       ├── sensor/              # SampleSource: stdin/JSONL reader + SerialReader (USB/RS-485)
|	|-- envfile/		 # envfile loader
│       ├── fft/                 # Cooley-Tukey FFT + zero-alloc processor
│       ├── filter/               # Swinging Door Trending compression
│       ├── anomaly/             # RMS + Z-score stateful detector, band-specific filtering
│       └── mqtt/                # MQTT 3.1.1 client, resilient reconnect wrapper, publisher
└── core/                         # Central analytics stack
    ├── docker-compose.yml       # Mosquitto + Telegraf + ClickHouse + Grafana
    ├── .env.example
    ├── mosquitto/config/
    ├── telegraf/
    ├── clickhouse/               # DDL: telemetry, anomaly_events, telemetry_1m MV
    └── grafana/provisioning/    # Auto-provisioned datasource + dashboard
```

---

## Configuration

Both `edge/` and `core/` read from a local `.env` (gitignored). Copy the example and adjust:

```bash
cp core/.env.example core/.env
cp edge/.env.example edge/.env
```

**`edge/.env`** — selects the sensor source and where to send data:

```env
SERIAL_INTERFACE=usb        # usb | rs485 | (unset → stdin, for local dev with firmware-sim)
USB_PORT=/dev/ttyACM0
RS485_PORT=/dev/ttyUSB0
RS485_BAUD=230400
SENSOR_ID=bearing_01
CENTRAL_HOST=192.168.1.50   # IP of the machine running core/
```

**`core/.env`** — broker, database credentials, exposed ports:

```env
CLICKHOUSE_USER=default
CLICKHOUSE_PASSWORD=changeme
GRAFANA_ADMIN_PASSWORD=changeme
```

Switching from local dev (everything on one laptop) to a real deployment (edge device + separate server) is a `.env` edit, not a code or rebuild change.

---

## Running Locally

### 1. Core stack

```bash
cd core
docker compose up -d
docker compose ps   # wait for clickhouse + mosquitto + grafana healthy
```

### 2. ClickHouse schema (first run only)

ClickHouse 24.3-alpine has a known issue where multi-statement `docker-entrypoint-initdb.d` scripts cause a double-process startup. Schema is applied manually:

```bash
docker exec sm_clickhouse clickhouse-client \
  --user default --password "$CLICKHOUSE_PASSWORD" \
  --query "CREATE DATABASE IF NOT EXISTS iot"

docker exec -i sm_clickhouse clickhouse-client \
  --user default --password "$CLICKHOUSE_PASSWORD" --multiquery \
  < clickhouse/init.sql

docker compose restart telegraf
```

### 3. Build and run the edge binary

```bash
cd ../edge
go build -o edge-filter ./cmd/edge-filter/
cd ..

# Normal mode — synthetic steady-state signal
python3 firmware-sim/signal_generator.py stream \
  | ./edge/edge-filter --broker localhost:1883 --sensor-id bearing_01

# Fault injection — anomaly after 15s
python3 firmware-sim/signal_generator.py stream --anomaly-after 15 \
  | ./edge/edge-filter --broker localhost:1883 --sensor-id bearing_01

# Offline, no broker
python3 firmware-sim/signal_generator.py batch --samples 8000 --anomaly \
  | ./edge/edge-filter --no-mqtt --sensor-id bearing_01
```

### 4. Grafana

`http://localhost:3000` — credentials from `GRAFANA_ADMIN_PASSWORD` in `core/.env`. The **Vibration Monitor** dashboard is auto-provisioned: RMS timeline, SDT scatter, anomaly events table, compression ratio gauge.

---

## Running on Real Hardware

Edge device (Raspberry Pi or similar):

```bash
cp edge/.env.example edge/.env
# set SERIAL_INTERFACE=usb (or rs485), CENTRAL_HOST=<core stack IP>
docker compose -f edge/docker-compose.yml up -d
```

The container image is built separately and pushed to a registry — see `edge/Dockerfile` for the multi-stage ARM64 build. The compose file mounts both `/dev/ttyACM0` and `/dev/ttyUSB0`; Docker ignores whichever doesn't exist on the host, so the same file works for USB and RS-485 setups without editing.

Core stack runs unchanged on a server or laptop on the same network — only `CENTRAL_HOST` in `edge/.env` needs to point at it.

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
- [x] Dockerfile for edge binary (ARM64, distroless)
- [ ] Real sensor hardware validation (Raspberry Pi + ADXL345/MPU6050 bench test)
- [ ] Outbound alert webhooks (Slack, PagerDuty)
- [ ] Edge REST API for runtime configuration
- [ ] CI: automated build + test on push, image publish on tag
