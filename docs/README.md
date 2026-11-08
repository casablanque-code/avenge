# Documentation map

| Document | What it is |
|---|---|
| [RESEARCH.md](RESEARCH.md) | Research notes: hypotheses, evidence status, the evidence boundary, limits |
| [LAB-NOTES.md](LAB-NOTES.md) | Field notes: bugs, wrong turns and what they taught us |
| [experiments/](experiments/README.md) | One file per experiment: setup, data, result, what it does not show |
| [adr/](adr/README.md) | Architecture decision records |
| [DOC.md](DOC.md) | Concept, algorithms, security model |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Diagrams, deployment, data model, sequences |
| [OPERATIONS.md](OPERATIONS.md) | Ports, setup wizard, first run, accounts, secrets, microphone, troubleshooting |
| [TESTING.md](TESTING.md) | What CI checks and how to run it locally |
| [RELEASING.md](RELEASING.md) | How a release is made, checked and verified |

## Repository layout

```
avenge/
├── firmware/ADXL345/            # Sensor firmware (ESP32 + ADXL345, Arduino sketch)
├── firmware-sim/                # Hardware-free signal generator (Python)
├── edge/                        # The engine (Go)
│   ├── cmd/edge-filter/         # Entrypoint, flags, --help, --explain, graceful shutdown
│   ├── cmd/avenge/              # Setup wizard and doctor: core setup, core pair, edge setup, doctor
│   ├── internal/                # sensor, profile, capture, fft, filter (SDT), anomaly, scoring, mqtt, envfile,
│   │                            # envedit, prompt, pairing, probe, netinfo (used by avenge)
│   ├── Dockerfile               # Multi-stage build -> distroless, ARM64-ready
│   ├── compose.yaml             # Deploy target: Raspberry Pi / edge device
│   └── .env.example
├── core/                        # Central stack
│   ├── compose.yaml             # Mosquitto + Telegraf + ClickHouse + Grafana + Caddy
│   ├── mosquitto/ telegraf/ clickhouse/ grafana/provisioning/
│   └── .env.example
├── tests/
│   ├── e2e/run.sh               # Full-stack end-to-end test (Docker)
│   └── lint/                    # repo-checks.sh, changelog-section.sh, mermaid/
├── tools/                       # build-binaries.sh (release archives), release.sh (prepare, check)
├── .github/                     # CI, release workflow, Dependabot
└── docs/
```

## What goes over the wire

| MQTT topic | Payload | When |
|---|---|---|
| `sm/telemetry/<sensor_id>` | Window RMS, SDT compression ratio, combined score, severity, dominant signal. No samples. | Every 20 windows (vibration) or every 10 s (audio) |
| `sm/anomaly/<sensor_id>` | `onset` or `clear`, with the signal and the band that caused it | On change of state only |

| ClickHouse table | Engine | TTL | Purpose |
|---|---|---|---|
| `iot.telemetry` | MergeTree | `RETENTION_DAYS` (30) | Telemetry points |
| `iot.anomaly_events` | MergeTree | `RETENTION_DAYS` (30) | Alert history |
| `iot.telemetry_1m` | SummingMergeTree (MV) | `RETENTION_DAYS` (30) | 1-minute RMS aggregates |

Full data model: [ARCHITECTURE.md](ARCHITECTURE.md).

## Stack

Go (version in `edge/go.mod`, standard library plus raw syscalls for serial I/O), Python 3.10+ for the signal generator, Eclipse Mosquitto, InfluxData Telegraf, ClickHouse, Grafana, Caddy, Docker Compose with a distroless runtime image. Versions are pinned in `core/compose.yaml` and `edge/Dockerfile`.
