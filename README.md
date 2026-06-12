# A.V.E.N.G.E

**Anomaly Vibration Edge Network Go Engine**

![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)
![License](https://img.shields.io/badge/license-MIT-green?style=flat)
![Build](https://img.shields.io/badge/build-passing-brightgreen?style=flat)
![Tests](https://img.shields.io/badge/tests-13%20passing-brightgreen?style=flat)

A high-performance Edge Computing engine for real-time vibration analytics, inline signal compression, and predictive anomaly detection in Industry 4.0 environments.

Built in Go with zero external dependencies. Processes high-frequency IIoT sensor streams with sub-millisecond window latency using pre-allocated signal processing pipelines and a zero-heap-allocation hot path.

---

## Prerequisites

| Requirement | Version |
|---|---|
| Go | 1.22+ |
| Docker + Docker Compose | 24.0+ |
| Python | 3.10+ |

---

## Architectural Overview

AVENGE shifts heavy analytical computation directly to the edge — industrial gateways, single-board computers, or embedded Linux nodes. Instead of flooding the network with raw high-frequency ADC samples, the engine performs inline feature extraction, applies time-series compression, evaluates anomaly state locally, and pushes only structural updates downstream.

```
+───────────────────────────────+      JSONL Stream      +───────────────────────────────+
│    Vibration Sensor Axis      ├───────────────────────>│         AVENGE Engine         │
│   (e.g., MPU-6050 / ESP32)    │  (Named Pipe / Stdin)  │    (FFT -> SDT -> Z-Score)    │
+───────────────────────────────+                        +───────────────┬───────────────+
                                                                         │
                                                                         │ MQTT 3.1.1 (QoS 1)
                                                                         │ sm/telemetry/<id>
                                                                         │ sm/anomaly/<id>
                                                                         ▼
+───────────────────────────────+     Native TCP Bulk    +───────────────────────────────+
│    Analytics Database         │<───────────────────────┤        Telegraf Agent         │
│     (ClickHouse OLAP)         │      Ingestion         │      (MQTT Consumer In)       │
+───────────────┬───────────────+                        +───────────────────────────────+
                │
                │ SQL Queries
                ▼
+───────────────────────────────+
│       Grafana Analytics       │
│    (Auto-provisioned dash)    │
+───────────────────────────────+
```

---

## Core Technical Features

- **Zero Allocations in Hot Path** — FFT buffers, ring buffer, and SDT accumulator are pre-allocated at startup. The GC is not involved during steady-state processing.
- **Autonomous Edge Operations** — anomaly detection runs entirely offline from cloud. Detection latency is under 1 ms per 512-sample window, enabling closed-loop safety responses at the machine level.
- **Self-contained MQTT Client** — MQTT 3.1.1 implemented from spec over raw `net.Conn`. No external dependencies, no module proxy required. Suitable for air-gapped deployments.
- **Bandwidth-Aware Design** — SDT compression reduces telemetry to structural turning points only. Compatible with constrained uplinks: LoRaWAN, cellular, satellite.

---

## Algorithmic Framework

### 1. Cooley-Tukey Radix-2 FFT

Time-domain vibration windows are converted to frequency-domain coefficients using a DIT radix-2 FFT (N=512 samples, ~0.5 s at 1 kHz).

- **DC Offset Mitigation** — the static gravitational component (1 g) is subtracted in-place before each transform, preventing it from masking high-frequency fault harmonics.
- **FFTProcessor** — reuses `[]complex128` and power spectrum buffers across calls. Zero heap allocations per window after warmup.

### 2. Swinging Door Trending (Bristol SDT)

Inline time-series compression based on the Bristol SDT algorithm (1990). The engine stores only "turning points" — samples where the signal vector deviates beyond a linear deadband (ε = 0.05 g). Intermediate points that fall within the corridor are discarded.

Compression results on synthetic signal:
- Steady-state normal vibration: **20–50×**
- Anomaly zone: **~2×** — degraded ratio is itself a secondary fault indicator

### 3. Stateful Z-Score Anomaly Detection

Window RMS energy is compared against a rolling baseline (mean ± std dev) updated via Exponential Moving Average (α = 0.1).

- **Warmup phase** — first N windows establish the normal RMS distribution before alerting is enabled.
- **Anti-adaptation interlock** — once Z > 3.5 threshold is crossed, the baseline update loop freezes. The engine stops normalizing a creeping fault and holds the alarm state until the signal returns to baseline.
- State transitions emit typed MQTT events: `onset` (NORMAL → ANOMALY) and `clear` (ANOMALY → NORMAL).

---

## Repository Structure

```
avenge/
├── firmware-sim/               # Hardware-free signal generator (Python)
│   ├── signal_generator.py     # Synthetic MPU-6050 output: normal + bearing fault modes
│   └── inspect.py              # ASCII sparkline visualizer
├── edge/                       # AVENGE engine (Go)
│   ├── cmd/edge-filter/
│   │   └── main.go             # Streaming pipeline entrypoint
│   └── internal/
│       ├── sensor/             # JSONL reader (hardware-agnostic interface)
│       ├── fft/                # Cooley-Tukey FFT + FFTProcessor
│       ├── filter/             # Swinging Door Trending compression
│       ├── anomaly/            # RMS + Z-score stateful detector
│       └── mqtt/               # MQTT 3.1.1 client + typed publisher
└── core/                       # Central analytics stack
    ├── docker-compose.yml      # Mosquitto + Telegraf + ClickHouse + Grafana
    ├── mosquitto/config/
    ├── telegraf/
    ├── clickhouse/             # DDL: telemetry, anomaly_events, telemetry_1m MV
    └── grafana/provisioning/   # Auto-provisioned datasource + dashboard
```

---

## Data Schema & Ingestion

### MQTT Topic Layout

| Topic | Payload | Frequency |
|---|---|---|
| `sm/telemetry/<sensor_id>` | SDT turning points + window RMS | Every ~10 s (20 windows) |
| `sm/anomaly/<sensor_id>` | State transition event (onset / clear) | On state change only |

### ClickHouse Tables

| Table | Engine | TTL | Purpose |
|---|---|---|---|
| `iot.telemetry` | MergeTree | 30 days | Compressed vibration points |
| `iot.anomaly_events` | MergeTree | 90 days | Alert history |
| `iot.telemetry_1m` | SummingMergeTree | — | 1-minute RMS aggregates (Materialized View) |

---

## Performance Benchmarks

End-to-end test at 1 kHz sampling rate, 512-sample windows:

```
edge-filter starting (streaming mode, constant memory)
  window=512 samples  rate=1000 Hz  SDT ε=0.050 g

win    1  t=  0.511s  [ WARM ]  fft:   50.8Hz(1.4e-01)   48.8Hz(6.4e-02)  [113.899ms]
win    2  t=  1.023s  [ WARM ]  fft:   50.8Hz(1.4e-01)   48.8Hz(6.3e-02)  [617us]
...
win   12  t=  6.143s  [  OK  ]  fft:   50.8Hz(1.4e-01)   48.8Hz(6.4e-02)  [558us]
win   13  t=  6.655s  [ ANOM ]  fft:   50.8Hz(1.5e-01)   48.8Hz(6.3e-02)  ⚡ ANOMALY z=+3.6σ rms=1.2914  [602us]
win   14  t=  7.167s  [ ANOM ]  fft:   50.8Hz(1.5e-01)  312.5Hz(7.9e-02)  [728us]
win   15  t=  7.679s  [ ANOM ]  fft:   50.8Hz(1.4e-01)  312.5Hz(1.3e-01)  [646us]
```

- **Throughput** — after allocator warmup (window 1), each analytical window processes in **550–700 µs** (~700× faster than real-time). Leaves substantial CPU headroom on Raspberry Pi-class hardware.
- **Detection precision** — window 13: Z-score detector fires at z=+3.6σ. Windows 14–15: FFT isolates a surge at exactly **312.5 Hz**, the theoretical Ball Pass Frequency Outer Race (BPFO) signature for a failing bearing at 50 Hz shaft speed.

---

## Deployment

### 1. Download the ClickHouse Grafana plugin (one-time)

Grafana's `GF_INSTALL_PLUGINS` requires reaching `grafana.com`, which may be
blocked on isolated networks. Download the plugin manually instead:

```bash
mkdir -p core/grafana/plugins
curl -L -o /tmp/clickhouse-plugin.zip \
  https://github.com/grafana/clickhouse-datasource/releases/download/v4.0.4/grafana-clickhouse-datasource-4.0.4.linux_amd64.zip
unzip -o /tmp/clickhouse-plugin.zip -d core/grafana/plugins/
```

### 2. Start the analytics stack

```bash
cd core/
docker compose up -d
sleep 20 && docker compose ps   # wait for clickhouse + mosquitto + grafana healthy
```

### 3. Apply the ClickHouse schema (first run only)

ClickHouse 24.3-alpine has a known bug where multi-statement
`docker-entrypoint-initdb.d` scripts cause a double-process startup
(EADDRINUSE). The schema is applied manually instead:

```bash
docker exec sm_clickhouse clickhouse-client \
  --user default --password clickhouse \
  --query "CREATE DATABASE IF NOT EXISTS iot"

docker exec -i sm_clickhouse clickhouse-client \
  --user default --password clickhouse --multiquery \
  < clickhouse/init.sql

# Verify
docker exec sm_clickhouse clickhouse-client \
  --user default --password clickhouse \
  --query "SHOW TABLES FROM iot"
```

Expected: `anomaly_events`, `mv_anomaly_parse`, `mv_telemetry_1m`,
`mv_telemetry_parse`, `telemetry`, `telemetry_1m`, `telemetry_raw`.

Restart Telegraf so it reconnects cleanly now that the schema exists:

```bash
docker compose restart telegraf
```

### 4. Build the edge binary

```bash
cd ../edge/
go build -o edge-filter ./cmd/edge-filter/
```

### 5. Run the pipeline

Normal mode (steady-state telemetry):

```bash
python3 firmware-sim/signal_generator.py stream \
  | ./edge/edge-filter --broker localhost:1883 --sensor-id bearing_01
```

Fault injection (anomaly triggers after 15 seconds):

```bash
python3 firmware-sim/signal_generator.py stream --anomaly-after 15 \
  | ./edge/edge-filter --broker localhost:1883 --sensor-id bearing_01
```

Offline mode (no broker required):

```bash
python3 firmware-sim/signal_generator.py batch --samples 8000 --anomaly \
  | ./edge/edge-filter --no-mqtt --sensor-id bearing_01
```

### 6. Open Grafana

Navigate to `http://localhost:3000` (credentials: `admin` / `admin`).

The **Vibration Monitor** dashboard is auto-provisioned on first startup — no manual import required. It includes:
- RMS vibration timeline (1-minute aggregates)
- Raw SDT turning points scatter
- Anomaly events table with color-coded state transitions
- SDT compression ratio gauge
- Anomaly rate stat panel

---

## Technology Stack

| Component | Technology |
|---|---|
| Edge engine | Go 1.22, pure stdlib |
| Signal generator | Python 3.10+ |
| MQTT broker | Eclipse Mosquitto 2.0 |
| Ingestion agent | InfluxData Telegraf 1.30 |
| Time-series database | ClickHouse 24.3 |
| Dashboards | Grafana 10.4 |
| Containerization | Docker Compose |

---

## Roadmap

- [x] Cooley-Tukey FFT with zero-alloc processor
- [x] Bristol SDT time-series compression
- [x] Stateful Z-score anomaly detection with anti-adaptation interlock
- [x] MQTT 3.1.1 client (stdlib only, QoS 0/1)
- [x] Auto-provisioned Grafana dashboard
- [ ] Frequency band-specific anomaly filtering (sub-band partitioning)
- [ ] Multi-sensor orchestration and cross-axis correlation
- [ ] Outbound alert webhooks (Slack, PagerDuty)
- [ ] Edge REST API for runtime configuration
- [ ] Step 4: real MPU-6050 hardware integration (ESP32 + Raspberry Pi)
