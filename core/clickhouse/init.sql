-- ClickHouse schema for AVENGE — Smart Manufacturing PoC
--
-- Applied automatically by the clickhouse-init service on every
-- `docker compose up -d`; safe to re-run. (Not mounted into
-- docker-entrypoint-initdb.d: multi-statement init scripts there trigger a
-- double-process startup bug — EADDRINUSE on 8123/9000/9009 — in the
-- 24.3-alpine image.)

-- ── Landing table ──────────────────────────────────────────────────────
-- Telegraf inserts whole JSON batches here; materialized views parse them into
-- the typed tables below. The raw copy is only a buffer, so it expires fast.
CREATE TABLE IF NOT EXISTS iot.telemetry_raw
(
    raw         String,
    received_at DateTime MATERIALIZED now()
)
ENGINE = MergeTree()
ORDER BY tuple()
TTL received_at + INTERVAL 7 DAY;

-- ── Telemetry table ────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS iot.telemetry
(
    ts          DateTime64(3, 'UTC'),
    sensor_id   LowCardinality(String),
    value       Float32,
    window_rms  Float32,
    is_anomaly  UInt8,
    severity    LowCardinality(String),
    sdt_ratio   Float32,
    -- final_score / dominant_signal come from internal/scoring: the
    -- combined max(rmsZ, bandZ) decision and which detector produced it.
    final_score      Float32 DEFAULT 0,
    dominant_signal  LowCardinality(String) DEFAULT ''
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (sensor_id, ts)
TTL toDateTime(ts) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;

-- ── Anomaly events table ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS iot.anomaly_events
(
    ts              DateTime64(3, 'UTC'),
    sensor_id       LowCardinality(String),
    event_type      LowCardinality(String),
    severity        LowCardinality(String),
    rms             Float32,
    z_score         Float32,
    baseline_mean   Float32,
    baseline_stddev Float32,
    -- what drove the anomaly: 'rms' or 'band'; band names the frequency band
    dominant_signal LowCardinality(String) DEFAULT '',
    band            LowCardinality(String) DEFAULT ''
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (sensor_id, ts)
TTL toDateTime(ts) + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;

-- Upgrade of an existing database: older tables lack the two columns.
-- Old rows keep the empty default.
ALTER TABLE iot.anomaly_events ADD COLUMN IF NOT EXISTS dominant_signal LowCardinality(String) DEFAULT '';
ALTER TABLE iot.anomaly_events ADD COLUMN IF NOT EXISTS band LowCardinality(String) DEFAULT '';

-- ── MV: telemetry_raw → telemetry ─────────────────────────────────────
CREATE MATERIALIZED VIEW IF NOT EXISTS iot.mv_telemetry_parse
TO iot.telemetry
AS SELECT
    toDateTime64(JSONExtractUInt(elem, 'timestamp') / 1000, 3, 'UTC') AS ts,
    JSONExtractString(JSONExtractRaw(elem, 'tags'), 'sensor_id')       AS sensor_id,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'value')          AS value,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'window_rms')     AS window_rms,
    JSONExtractUInt(JSONExtractRaw(elem, 'fields'), 'is_anomaly')      AS is_anomaly,
    JSONExtractString(JSONExtractRaw(elem, 'fields'), 'severity')      AS severity,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'sdt_ratio')      AS sdt_ratio,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'final_score')    AS final_score,
    JSONExtractString(JSONExtractRaw(elem, 'fields'), 'dominant_signal') AS dominant_signal
FROM iot.telemetry_raw
ARRAY JOIN JSONExtractArrayRaw(raw, 'metrics') AS elem
WHERE JSONExtractString(elem, 'name') = 'telemetry';

-- ── MV: telemetry_raw → anomaly_events ────────────────────────────────
-- Dropped and recreated on every run so that an existing database picks up
-- the new columns; the stored rows are in iot.anomaly_events and stay.
DROP VIEW IF EXISTS iot.mv_anomaly_parse;
CREATE MATERIALIZED VIEW iot.mv_anomaly_parse
TO iot.anomaly_events
AS SELECT
    toDateTime64(JSONExtractUInt(elem, 'timestamp') / 1000, 3, 'UTC') AS ts,
    JSONExtractString(JSONExtractRaw(elem, 'tags'), 'sensor_id')       AS sensor_id,
    JSONExtractString(JSONExtractRaw(elem, 'tags'), 'event_type')      AS event_type,
    JSONExtractString(JSONExtractRaw(elem, 'tags'), 'severity')        AS severity,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'rms')            AS rms,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'z_score')        AS z_score,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'baseline_mean')  AS baseline_mean,
    JSONExtractFloat(JSONExtractRaw(elem, 'fields'), 'baseline_stddev') AS baseline_stddev,
    JSONExtractString(JSONExtractRaw(elem, 'fields'), 'dominant_signal') AS dominant_signal,
    JSONExtractString(JSONExtractRaw(elem, 'fields'), 'band')           AS band
FROM iot.telemetry_raw
ARRAY JOIN JSONExtractArrayRaw(raw, 'metrics') AS elem
WHERE JSONExtractString(elem, 'name') = 'anomaly_events';

-- ── Aggregates: per-minute RMS ─────────────────────────────────────────
CREATE TABLE IF NOT EXISTS iot.telemetry_1m
(
    ts_bucket       DateTime('UTC'),
    sensor_id       LowCardinality(String),
    rms_avg         Float32,
    rms_max         Float32,
    point_count     UInt32,
    anomaly_any     UInt8,
    critical_count  UInt32,
    warning_count   UInt32
)
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(ts_bucket)
ORDER BY (sensor_id, ts_bucket);

CREATE MATERIALIZED VIEW IF NOT EXISTS iot.mv_telemetry_1m
TO iot.telemetry_1m
AS SELECT
    toStartOfMinute(ts)                    AS ts_bucket,
    sensor_id,
    avg(window_rms)                        AS rms_avg,
    max(window_rms)                        AS rms_max,
    count()                                AS point_count,
    max(is_anomaly)                        AS anomaly_any,
    countIf(severity = 'critical')         AS critical_count,
    countIf(severity = 'warning')          AS warning_count
FROM iot.telemetry
GROUP BY ts_bucket, sensor_id;

-- ── Security: run materialized views as their owner ─────────────────────
-- The telegraf account may only INSERT into iot.telemetry_raw. With
-- DEFINER security the views read and write as the admin user that ran this
-- script, so telegraf needs no access to the target tables.
-- (Idempotent; also upgrades views created by older versions of this file.)
ALTER TABLE iot.mv_telemetry_parse MODIFY DEFINER = CURRENT_USER SQL SECURITY DEFINER;
ALTER TABLE iot.mv_anomaly_parse   MODIFY DEFINER = CURRENT_USER SQL SECURITY DEFINER;
ALTER TABLE iot.mv_telemetry_1m    MODIFY DEFINER = CURRENT_USER SQL SECURITY DEFINER;

-- ── Upgrade path for databases created by older versions ───────────────
-- (no-ops on a fresh database)
ALTER TABLE iot.telemetry_raw ADD COLUMN IF NOT EXISTS received_at DateTime MATERIALIZED now();
ALTER TABLE iot.telemetry_raw MODIFY TTL received_at + INTERVAL 7 DAY;
