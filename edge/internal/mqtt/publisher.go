// Package mqtt — publisher layer.
//
// Defines the JSON message schemas written to ClickHouse via Telegraf,
// and provides a Publisher that routes messages to the correct topics.
//
// Topic layout:
//   sm/telemetry/<sensor_id>  — SDT-compressed vibration points (frequent)
//   sm/anomaly/<sensor_id>    — anomaly state transitions (rare)
package mqtt

import (
	"encoding/json"
	"fmt"
	"time"
)

// TelemetryMessage is the JSON payload for sm/telemetry/<sensor_id>.
// Fields must match telegraf.conf json_v2 paths and clickhouse/init.sql columns.
type TelemetryMessage struct {
	// TsMs is the edge-device timestamp in Unix milliseconds.
	// Telegraf maps this to the ClickHouse `ts` column.
	TsMs      int64   `json:"ts_ms"`
	SensorID  string  `json:"sensor_id"`
	Value     float32 `json:"value"`
	WindowRMS float32 `json:"window_rms"`
	IsAnomaly int     `json:"is_anomaly"` // 0 or 1
	Severity  string  `json:"severity"`   // "normal" | "warning" | "critical"
	SDTRatio  float32 `json:"sdt_ratio"`
}

// AnomalyMessage is the JSON payload for sm/anomaly/<sensor_id>.
type AnomalyMessage struct {
	TsMs           int64   `json:"ts_ms"`
	SensorID       string  `json:"sensor_id"`
	EventType      string  `json:"event_type"` // "onset" or "clear"
	Severity       string  `json:"severity"`   // "normal" | "warning" | "critical"
	RMS            float32 `json:"rms"`
	ZScore         float32 `json:"z_score"`
	BaselineMean   float32 `json:"baseline_mean"`
	BaselineStddev float32 `json:"baseline_stddev"`
}

// Publisher sends pipeline results to the MQTT broker.
type Publisher struct {
	client   *Client
	sensorID string
	qos      byte
}

// NewPublisher creates a Publisher using an existing connected Client.
func NewPublisher(client *Client, sensorID string, qos byte) *Publisher {
	return &Publisher{client: client, sensorID: sensorID, qos: qos}
}

// PublishTelemetry sends one summary message for the most recent SDT flush.
func (p *Publisher) PublishTelemetry(
	tsMs int64,
	value, windowRMS, sdtRatio float32,
	isAnomaly bool,
	severity string,
) error {
	anomalyInt := 0
	if isAnomaly {
		anomalyInt = 1
	}
	msg := TelemetryMessage{
		TsMs:      tsMs,
		SensorID:  p.sensorID,
		Value:     value,
		WindowRMS: windowRMS,
		IsAnomaly: anomalyInt,
		Severity:  severity,
		SDTRatio:  sdtRatio,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("telemetry marshal: %w", err)
	}
	topic := "sm/telemetry/" + p.sensorID
	return p.client.Publish(topic, payload, p.qos)
}

// PublishAnomaly sends an anomaly state-transition event.
// eventType should be "onset" (NORMAL→ANOMALY) or "clear" (ANOMALY→NORMAL).
func (p *Publisher) PublishAnomaly(
	t time.Time,
	eventType string,
	severity string,
	rms, zScore, baselineMean, baselineStddev float32,
) error {
	msg := AnomalyMessage{
		TsMs:           t.UnixMilli(),
		SensorID:       p.sensorID,
		EventType:      eventType,
		Severity:       severity,
		RMS:            rms,
		ZScore:         zScore,
		BaselineMean:   baselineMean,
		BaselineStddev: baselineStddev,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("anomaly marshal: %w", err)
	}
	topic := "sm/anomaly/" + p.sensorID
	return p.client.Publish(topic, payload, p.qos)
}
