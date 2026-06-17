package mqtt

import (
	"encoding/json"
	"testing"
)

// mockPublishable records every Publish call for inspection, satisfying
// the Publishable interface without needing a real broker connection.
type mockPublishable struct {
	topic   string
	payload []byte
	qos     byte
	calls   int
}

func (m *mockPublishable) Publish(topic string, payload []byte, qos byte) error {
	m.topic = topic
	m.payload = payload
	m.qos = qos
	m.calls++
	return nil
}

func TestPublisher_PublishTelemetry_IncludesScoringFields(t *testing.T) {
	mock := &mockPublishable{}
	pub := NewPublisher(mock, "bearing_01", QoS1)

	err := pub.PublishTelemetry(
		1234567890,
		0.5, 1.3, 1.8,
		true,
		"critical",
		92.5,
		"band",
	)
	if err != nil {
		t.Fatalf("PublishTelemetry: %v", err)
	}

	if mock.calls != 1 {
		t.Fatalf("Publish called %d times, want 1", mock.calls)
	}
	if mock.topic != "sm/telemetry/bearing_01" {
		t.Errorf("topic = %q, want %q", mock.topic, "sm/telemetry/bearing_01")
	}

	var got TelemetryMessage
	if err := json.Unmarshal(mock.payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if got.FinalScore != 92.5 {
		t.Errorf("FinalScore = %v, want 92.5", got.FinalScore)
	}
	if got.DominantSignal != "band" {
		t.Errorf("DominantSignal = %q, want %q", got.DominantSignal, "band")
	}
	if got.Severity != "critical" {
		t.Errorf("Severity = %q, want %q", got.Severity, "critical")
	}
	if got.IsAnomaly != 1 {
		t.Errorf("IsAnomaly = %d, want 1", got.IsAnomaly)
	}
}

func TestPublisher_PublishTelemetry_NotAnomaly(t *testing.T) {
	mock := &mockPublishable{}
	pub := NewPublisher(mock, "bearing_02", QoS1)

	err := pub.PublishTelemetry(0, 0, 1.2, 1.8, false, "normal", 0.8, "rms")
	if err != nil {
		t.Fatalf("PublishTelemetry: %v", err)
	}

	var got TelemetryMessage
	if err := json.Unmarshal(mock.payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got.IsAnomaly != 0 {
		t.Errorf("IsAnomaly = %d, want 0", got.IsAnomaly)
	}
}
