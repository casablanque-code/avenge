# A.V.E.N.G.E

**Anomaly Vibration Edge Network Go Engine**

A high-performance, industrial-grade Edge Computing engine designed for real-time vibration analytics, inline signal compression, and predictive anomaly detection in Industry 4.0 environments.

Developed in Go, the engine processes high-frequency IIoT sensor data streams with sub-millisecond latencies, utilizing customized signal processing pipelines and zero-heap-allocation algorithms in the hot path.

---

## Architectural Overview

AVENGE shifts heavy analytical computations directly to the edge (e.g., industrial gateways, single-board computers), drastically mitigating network bandwidth inflation and cutting cloud storage overhead. 

Instead of dumping raw, redundant high-frequency ADC telemetry over the network, AVENGE performs inline feature extraction, applies lossy-but-meaningful time-series compression, evaluates anomaly states locally, and pushes structural updates downstream.

+───────────────────────────────+      JSONL Stream      +───────────────────────────────+
│    Vibration Sensor Axis      ├───────────────────────>│         AVENGE Engine         │
│   (e.g., MPU-6050 Emulator)   │  (Named Pipe / Stdin)  │    (FFT -> SDT -> Z-Score)    │
+───────────────────────────────+                        +───────────────┬───────────────+
                                                                         │
                                                                         │ Custom MQTT Client
                                                                         │ (QoS 1 Protocols)
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
│    (Real-time Dashboards)     │
+───────────────────────────────+

---

## Core Technical Features

* High-Performance Streaming Pipeline: Asynchronous processing architecture optimized for low-power edge nodes.
* Zero Allocations in Hot Path: All critical processing structures (FFT twiddle factors, ring buffers, and matrix spaces) are pre-allocated during the application warmup phase. Garbage Collector overhead is completely bypassed during operational runtime.
* Autonomous Edge Operations: Fault detection logic runs completely out-of-band from centralized servers. Anomaly evaluation takes < 1ms, enabling instant closed-loop safety responses at the machine level.
* Advanced Bandwidth Optimization: Inline data compression algorithms allow telemetry delivery even over highly constrained physical layers (such as LoRaWAN, cellular links, or satellite networks).

---

## Algorithmic Framework

1. Cooley-Tukey Radix-2 FFT Frame Processing
Time-domain vibration arrays are converted into frequency spectrum coefficients using a high-speed Fast Fourier Transform (configured at N=512 samples). 
* DC Offset Mitigation: The static gravitational acceleration component (1g) is dynamically stripped from the window prior to execution, ensuring subtle, high-frequency shaft degradation harmonics are not masked by static bias.

2. Swinging Door Trending (Bristol SDT Compression)
To prevent database bloating from immutable harmonic repetitions, AVENGE implements an inline Bristol SDT algorithm (configured with a deadband threshold of epsilon = 0.05g). The engine records only structural "turning points" where the signal vector significantly deviates from the established slope, achieving 10x to 50x data compression during steady-state machine operations without losing peak physical information.

3. Stateful Moving Z-Score Anomaly Detection
The system tracks signal degradation via windowed Root Mean Square (RMS) energy. A normal operational baseline (mean and std dev) is continuously updated using an Exponential Moving Average (EMA) with a smoothing factor of alpha=0.1.
* Anti-Adaptation Interlocking: The moment a critical statistical threshold (Z > 3.5) is violated, the baseline update loop is instantly frozen. This stops the engine from adapting to or normalizing a progressive, creeping machine fault, ensuring the alarm remains active.

---

## Data Schema & Ingestion Layout

MQTT Topic Topology (Protocol 3.1.1)
The engine utilizes an optimized TCP-socket MQTT client to dispatch messages under two strictly partitioned channels:
* sm/telemetry/sensor_id — High-frequency turning points captured by the SDT compression engine.
* sm/anomaly/sensor_id — Rare, discrete edge-triggered state-transition events (onset / clear).

Database Infrastructure (ClickHouse DDL)
* iot.telemetry: Time-series table mapping the compressed vibration data. Configured with a MergeTree engine and an automated data eviction layer: TTL ts + INTERVAL 30 DAY.
* iot.anomaly_events: Relational table capturing historical alerts and state-changes. Used for Grafana annotations and long-term audits: TTL ts + INTERVAL 90 DAY.
* iot.telemetry_1m & iot.mv_telemetry_1m: A real-time Materialized View powered by a SummingMergeTree. It rolls up telemetry raw points into 1-minute statistical aggregates on the fly, guaranteeing instant rendering of multi-month dashboards.

---

## Performance Benchmarks (E2E Test)

Actual throughput log from an end-to-end stress test executed at a native 1 kHz hardware sampling rate:

  edge-filter starting (streaming mode, constant memory)
    window=512 samples  rate=1000 Hz  SDT epsilon=0.050 g

  win    1  t=  0.511s  [ WARM ]  fft:   50.8Hz(1.4e-01)   48.8Hz(6.4e-02)  [113.899ms]
  win    2  t=  1.023s  [ WARM ]  fft:   50.8Hz(1.4e-01)   48.8Hz(6.3e-02)  [617us]
  ...
  win   12  t=  6.143s  [  OK  ]  fft:   50.8Hz(1.4e-01)   48.8Hz(6.4e-02)  [558us]
  win   13  t=  6.655s  [ ANOM ]  fft:   50.8Hz(1.5e-01)   48.8Hz(6.3e-02)   ANOMALY z=+3.6 rms=1.2914  [602us]
  win   14  t=  7.167s  [ ANOM ]  fft:   50.8Hz(1.5e-01)  312.5Hz(7.9e-02)  [728us]
  win   15  t=  7.679s  [ ANOM ]  fft:   50.8Hz(1.4e-01)  312.5Hz(1.3e-01)  [646us]

* Execution Velocity: After allocator warmup (Window 1), computing the complete analytical window (512 samples) settles between 550-700 microseconds. The pipeline runs roughly 700x faster than real-time, leaving plenty of CPU overhead on low-power edge nodes.
* Diagnostic Precision: At Window 13, the stateful detector catches an analytical Z-score anomaly (z=+3.6). At Windows 14 and 15, the FFT subsystem isolates a major frequency surge exactly at 312.5Hz, mapping directly to the Ball Pass Frequency Outer Race (BPFO) defect signature of a failing bearing.

---

## Deployment & Execution

1. Provision Ingestion Stack
Spin up the Dockerized infrastructure (Eclipse Mosquitto MQTT, Telegraf Agent, ClickHouse Server, Grafana) and initialize storage tables:
  docker compose down -v
  docker compose up -d

2. Compile Edge Binary
Navigate to the edge subsystem and compile the optimized Go executable:
  cd edge
  go build -o edge-filter cmd/main_2.go

3. Run Pipeline Integration Test
Launch the physics-based bearing signal generator and pipe the raw high-frequency telemetry directly into the AVENGE engine:
  python3 firmware-sim/signal_generator.py stream --anomaly-after 6.0 | ./edge-filter --sensor-id bearing_01

4. Visualize via Grafana
* Navigate to http://localhost:3000 (Default Credentials: admin / admin).
* Go to Dashboards -> Import.
* Load the pre-configured vibration.json profile from the repository root.
* Monitor live charts for RMS trends, structural SDT points, active Z-score alerts, and real-time bandwidth compression ratios.

---

## Technology Stack

* Edge Application: Go 1.21+ (Pure native sockets, zero external framework dependencies)
* Ingestion Middleware: InfluxData Telegraf
* Broker Architecture: MQTT 3.1.1 (Eclipse Mosquitto)
* OLAP Database Cluster: Yandex ClickHouse
* Analytics Frontend: Grafana Labs

---

## Strategic Roadmap

* [x] Adaptive thresholds (Rolling EMA mean/std deviation)
* [x] Strict MQTT QoS 1 Ingestion Handshaking & Buffering
* [ ] Frequency band-specific anomaly filtering (Sub-band partitioning)
* [ ] Multi-sensor input orchestration & Cross-axis correlation
* [ ] Outbound alert notifications (Slack / Discord Webhooks, PagerDuty integration)
* [ ] Edge REST API layer for configuration updates