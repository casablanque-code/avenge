# A.V.E.N.G.E

### Anomaly Vibration Edge Network Go Engine

## 1. Introduction

Industrial systems rely heavily on continuous monitoring of mechanical components such as motors, bearings, and turbines. Vibration analysis is one of the most effective methods for detecting early signs of failure.

Traditional approaches often rely on centralized processing, where raw sensor data is transmitted to cloud systems for analysis. This introduces latency, increases bandwidth usage, and reduces system resilience.

**A.V.E.N.G.E** addresses these limitations by moving signal processing and anomaly detection directly to the edge.

---

## 2. Problem Statement

Modern IIoT environments face several challenges:

* High-frequency vibration data generates significant network load
* Latency between data acquisition and anomaly detection delays response
* Centralized systems are vulnerable to connectivity issues
* Many solutions rely on opaque machine learning models that are difficult to debug

There is a need for a system that is:

* Deterministic
* Low-latency
* Resource-efficient
* Scalable across thousands of sensors

---

## 3. Solution Overview

A.V.E.N.G.E is an edge-first vibration analytics engine implemented in Go, designed to process streaming sensor data in real time.

Instead of transmitting raw signals, the system extracts features, detects anomalies, and sends only meaningful data downstream.

### Core Principles

* **Edge-first processing**
* **Streaming architecture**
* **Deterministic statistical models**
* **Minimal memory footprint**
* **Scalable ingestion via MQTT**

---

## 4. System Architecture

```
Sensor -> Edge Engine -> MQTT -> Telegraf -> ClickHouse -> Grafana
```

### Components

#### Sensors

Produce high-frequency vibration signals (USB or RS-485 serial, or a simulator).

#### Edge Engine (Go)

Runs next to the sensor and performs:

* Signal buffering
* FFT transformation
* Feature extraction (RMS, band energy)
* Compression of the RMS trend (Swinging Door Trending)
* Anomaly detection and combined scoring

Only compressed features and state transitions leave the device, not raw samples.

#### MQTT Layer

Lightweight transport between edge devices and the central stack (MQTT 3.1.1, QoS 1). The broker requires authentication and enforces per-user topic ACLs.

#### Ingestion (Telegraf)

Subscribes to the MQTT topics and writes the messages into ClickHouse over HTTP.

#### Storage (ClickHouse)

Stores time-series data for analysis and visualization. Materialized views parse the raw messages into typed tables and per-minute aggregates.

#### Visualization (Grafana)

Provides real-time dashboards and anomaly tracking, reached through a local reverse proxy (Caddy).

---

## 5. Signal Processing Pipeline

The edge engine processes incoming data in a streaming pipeline:

1. **Buffering**
   Fixed-size windows of signal data are collected.

2. **FFT (Fast Fourier Transform)**
   Converts time-domain signals into frequency domain.

3. **Feature Extraction**

   * RMS (signal energy)
   * Band-specific energy
   * Peak detection (optional)

4. **Normalization**
   Ensures consistent scaling across windows.

5. **Compression (SDT)**
   The window RMS trend is compressed with Swinging Door Trending: only turning points that leave the deadband are kept and published.

---

## 6. Anomaly Detection Model

A.V.E.N.G.E uses lightweight statistical methods instead of heavy ML models.

### 6.1 Baseline Estimation

For each metric (RMS or band energy), a baseline is maintained using an Exponential Moving Average (EMA):

* Mean (μ)
* Standard deviation (σ)

Baseline updates are paused or slowed during anomaly states to prevent contamination.

---

### 6.2 Z-Score Detection

An anomaly score is computed as:

```
z = (x - μ) / σ
```

Where:

* x is the current measurement
* μ and σ are baseline statistics

---

### 6.3 Hysteresis

To prevent oscillation around thresholds:

* Entry threshold: detects anomaly onset
* Exit threshold: confirms return to normal

This stabilizes state transitions and reduces noise. Defaults: the RMS detector enters at z > 3.5 and exits at z <= 2.5; band detectors enter at z > 4.0 and exit at z <= 2.5.

---

### 6.4 Severity Levels

Instead of binary classification, anomalies are categorized:

* Normal
* Warning
* Critical

This enables prioritization and better visualization. With the default thresholds, a z-score above 2.5 is a warning and above 3.5 is critical.

### 6.5 Combined Scoring

The RMS detector and the per-band detector run independently. The final decision takes the more alarming of the two z-scores ("max wins"), so a strong band-specific signal is not hidden by a calm overall RMS. The result is published as `final_score` together with the `dominant_signal` that produced it.

---

## 7. Frequency Band Analysis

Beyond global RMS, A.V.E.N.G.E supports band-based detection:

* Signal spectrum is divided into frequency bands
* Each band maintains its own baseline
* Weighted scoring highlights critical frequency ranges

The default bands for a 1 kHz-class sensor are `shaft` (0-50 Hz), `harmonic` (50-150 Hz), `fault` (150-400 Hz) and `hf_noise` (400 Hz to Nyquist).

This allows detection of:

* Bearing defects
* Imbalance
* Misalignment

---

## 8. Performance Characteristics

* Constant memory usage proportional to FFT window size
* No heap allocations in the hot path after initialization
* Low-latency processing suitable for real-time systems
* Scalable to thousands of sensor streams (with proper orchestration)

---

## 9. Advantages Over Traditional Approaches

| Approach               | Limitation     | A.V.E.N.G.E Advantage      |
| ---------------------- | -------------- | -------------------------- |
| Cloud-based processing | High latency   | Edge processing            |
| Raw data streaming     | High bandwidth | Feature-level transmission |
| ML-heavy systems       | Opaque, heavy  | Deterministic models       |
| Centralized systems    | Fragile        | Distributed resilience     |

---

## 10. Use Cases

* Predictive maintenance of industrial machinery
* Monitoring of rotating equipment
* Early detection of bearing failures
* Real-time anomaly detection in manufacturing lines

---

## 11. Future Work

* Adaptive thresholds per operating mode
* Alerting and integration (webhooks, messaging) and a "sensor went silent" alert
* TLS for MQTT (the current client is plaintext, intended for a trusted LAN)
* Advanced spectral analysis (peak tracking, harmonics)
* Validation on real sensor hardware

---

## 12. Security Model

* MQTT: no anonymous access; `edge` may only publish telemetry and anomaly topics, `telegraf` may only read them.
* ClickHouse: Telegraf may only INSERT into the landing table, Grafana is read-only; the admin account is used for manual work only.
* Network: ClickHouse is bound to localhost, Grafana is reachable only through the local proxy, the MQTT port is bound to a chosen interface.
* Limitation: MQTT is not encrypted. Keep it on a trusted network or add a TLS-terminating layer.

Operational details and credential handling: [OPERATIONS.md](OPERATIONS.md).

---

## 13. Conclusion

A.V.E.N.G.E demonstrates that effective anomaly detection does not require heavy machine learning models or centralized infrastructure.

By combining efficient signal processing with lightweight statistical methods, it provides a scalable and transparent solution for real-time vibration analytics at the edge.

---
