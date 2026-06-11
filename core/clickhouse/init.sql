-- ClickHouse DDL for Smart Manufacturing PoC
-- Engine: MergeTree with TTL (auto-expire raw data after 30 days)
-- Run automatically via docker-entrypoint-initdb.d

CREATE DATABASE IF NOT EXISTS iot;

-- ── Telemetry table ────────────────────────────────────────────────────
-- Stores SDT-compressed vibration aggregates sent every ~10 seconds.
-- Each row = one SDT "turning point" (representative of many raw samples).
CREATE TABLE IF NOT EXISTS iot.telemetry
(
    -- When the original sample was recorded (edge timestamp)
    ts          DateTime64(3, 'UTC'),
    -- Sensor identifier — for when you add more machines
    sensor_id   LowCardinality(String),
    -- Vibration value (g). SDT-compressed, not every raw sample.
    value       Float32,
    -- RMS of the window this point belongs to
    window_rms  Float32,
    -- Whether the anomaly detector was active at this window
    is_anomaly  UInt8,
    -- SDT compression ratio of the batch this point came from
    sdt_ratio   Float32
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (sensor_id, ts)
TTL toDateTime(ts) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- ── Anomaly events table ───────────────────────────────────────────────
-- One row per anomaly state transition (NORMAL→ANOMALY or ANOMALY→NORMAL).
-- Small table — queried for alert history and Grafana annotations.
CREATE TABLE IF NOT EXISTS iot.anomaly_events
(
    ts              DateTime64(3, 'UTC'),
    sensor_id       LowCardinality(String),
    -- 'onset' or 'clear'
    event_type      LowCardinality(String),
    rms             Float32,
    z_score         Float32,
    baseline_mean   Float32,
    baseline_stddev Float32
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (sensor_id, ts)
TTL toDateTime(ts) + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;

-- ── Materialized view: per-minute RMS aggregates ───────────────────────
-- Pre-aggregates telemetry for fast Grafana queries over long time ranges.
CREATE TABLE IF NOT EXISTS iot.telemetry_1m
(
    ts_bucket   DateTime('UTC'),
    sensor_id   LowCardinality(String),
    rms_avg     Float32,
    rms_max     Float32,
    point_count UInt32,
    anomaly_any UInt8
)
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(ts_bucket)
ORDER BY (sensor_id, ts_bucket);

CREATE MATERIALIZED VIEW IF NOT EXISTS iot.mv_telemetry_1m
TO iot.telemetry_1m
AS SELECT
    toStartOfMinute(ts)         AS ts_bucket,
    sensor_id,
    avg(window_rms)             AS rms_avg,
    max(window_rms)             AS rms_max,
    count()                     AS point_count,
    max(is_anomaly)             AS anomaly_any
FROM iot.telemetry
GROUP BY ts_bucket, sensor_id;
