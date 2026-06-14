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
Sensors → MQTT → Edge Engine → ClickHouse → Grafana
```

### Components

#### Sensors

Produce high-frequency vibration signals.

#### MQTT Layer

Acts as a lightweight, scalable transport for sensor data.

#### Edge Engine (Go)

Performs:

* Signal buffering
* FFT transformation
* Feature extraction (RMS, band energy)
* Anomaly detection

#### Storage (ClickHouse)

Stores time-series data for analysis and visualization.

#### Visualization (Grafana)

Provides real-time dashboards and anomaly tracking.

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

This stabilizes state transitions and reduces noise.

---

### 6.4 Severity Levels

Instead of binary classification, anomalies are categorized:

* Normal
* Warning
* Critical

This enables prioritization and better visualization.

---

## 7. Frequency Band Analysis

Beyond global RMS, A.V.E.N.G.E supports band-based detection:

* Signal spectrum is divided into frequency bands
* Each band maintains its own baseline
* Weighted scoring highlights critical frequency ranges

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

* Multi-sensor orchestration
* Adaptive thresholds per operating mode
* MQTT reliability (buffering, QoS)
* Alerting and integration (webhooks, messaging)
* Advanced spectral analysis (peak tracking, harmonics)

---

## 12. Conclusion

A.V.E.N.G.E demonstrates that effective anomaly detection does not require heavy machine learning models or centralized infrastructure.

By combining efficient signal processing with lightweight statistical methods, it provides a scalable and transparent solution for real-time vibration analytics at the edge.

---
