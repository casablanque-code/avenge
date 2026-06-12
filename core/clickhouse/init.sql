-- ClickHouse schema for AVENGE — Smart Manufacturing PoC
--
-- IMPORTANT: This file is NOT auto-applied via docker-entrypoint-initdb.d.
-- ClickHouse 24.3-alpine has a known issue where multi-statement init
-- scripts cause a double-process startup (EADDRINUSE on 8123/9000/9009).
--
-- Apply manually after `docker compose up -d` and ClickHouse is healthy:
--
--   docker exec sm_clickhouse clickhouse-client \
--     --user default --password clickhouse \
--     --query "CREATE DATABASE IF NOT EXISTS iot"
--
--   docker exec -i sm_clickhouse clickhouse-client \
--     --user default --password clickhouse --multiquery \
--     < core/clickhouse/init.sql
--
-- (If --multiquery silently no-ops on your client version, split this file
--  into individual --query calls — see deploy notes in README.)

-- ── Landing table ──────────────────────────────────────────────────────
-- Telegraf's outputs.http with data_format=json sends a BATCH wrapper:
--   {"metrics":[{...},{...},...]}
-- rather than one flat object per line. We can't make ClickHouse's
-- JSONEachRow accept that directly, so we land the raw batch as a single
-- String column and unpack it via Materialized Views below using
-- ARRAY JOIN + JSONExtractArrayRaw.
CREATE TABLE IF NOT EXISTS iot.telemetry_raw
(
    raw String
)
ENGINE = MergeTree()
ORDER BY tuple();

-- ── Telemetry table ────────────────────────────────────────────────────
-- One row per edge-filter SDT flush (~every 10s), populated via
-- mv_telemetry_parse below.
CREATE TABLE IF NOT EXISTS iot.telemetry
(
    ts          DateTime64(3, 'UTC'),
    sensor_id   LowCardinality(String),
    value       Float32,
    window_rms  Float32,
    is_anomaly  UInt8,
    sdt_ratio   Float32
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (sensor_id, ts)
TTL toDateTime(ts) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- ── Anomaly events table ───────────────────────────────────────────────
-- One row per anomaly state transition (onset / clear).
CREATE TABLE IF NOT EXISTS iot.anomaly_events
(
    ts              DateTime64(3, 'UTC'),
    sensor_id       LowCardinality(String),
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

-- ── Materialized view: parse telemetry_raw → telemetry ────────────────
-- Telegraf batches multiple "metrics" per HTTP POST; ARRAY JOIN unrolls
-- JSONExtractArrayRaw(raw,'metrics') into one row per metric, then filters
-- by measurement name and extracts fields/tags by path.
CREATE MATERIALIZED VIEW IF NOT EXISTS iot.mv_telemetry_parse
TO iot.telemetry
AS SELECT
    toDateTime64(JSONExtractUInt(elem, 'timestamp') / 1000, 3, 'UTC') AS ts,
    JSONExtractString(JSONExtractRaw(elem, 'tags'), 'sensor_id')       AS sensor_id,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'value')          AS value,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'window_rms')     AS window_rms,
    JSONExtractUInt(JSONExtractRaw(elem, 'fields'), 'is_anomaly')      AS is_anomaly,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'sdt_ratio')      AS sdt_ratio
FROM iot.telemetry_raw
ARRAY JOIN JSONExtractArrayRaw(raw, 'metrics') AS elem
WHERE JSONExtractString(elem, 'name') = 'telemetry';

-- ── Materialized view: parse telemetry_raw → anomaly_events ───────────
CREATE MATERIALIZED VIEW IF NOT EXISTS iot.mv_anomaly_parse
TO iot.anomaly_events
AS SELECT
    toDateTime64(JSONExtractUInt(elem, 'timestamp') / 1000, 3, 'UTC') AS ts,
    JSONExtractString(JSONExtractRaw(elem, 'tags'), 'sensor_id')       AS sensor_id,
    JSONExtractString(JSONExtractRaw(elem, 'tags'), 'event_type')      AS event_type,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'rms')            AS rms,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'z_score')        AS z_score,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'baseline_mean')  AS baseline_mean,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'baseline_stddev') AS baseline_stddev
FROM iot.telemetry_raw
ARRAY JOIN JSONExtractArrayRaw(raw, 'metrics') AS elem
WHERE JSONExtractString(elem, 'name') = 'anomaly_events';

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
    toStartOfMinute(ts)  AS ts_bucket,
    sensor_id,
    avg(window_rms)      AS rms_avg,
    max(window_rms)      AS rms_max,
    count()              AS point_count,
    max(is_anomaly)      AS anomaly_any
FROM iot.telemetry
GROUP BY ts_bucket, sensor_id;
