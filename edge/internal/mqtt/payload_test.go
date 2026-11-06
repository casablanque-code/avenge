package mqtt

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"
)

type captureClient struct{ payloads map[string][]byte }

func (c *captureClient) Publish(topic string, payload []byte, qos byte) error {
	if c.payloads == nil {
		c.payloads = map[string][]byte{}
	}
	c.payloads[topic] = append([]byte(nil), payload...)
	return nil
}

// The privacy promise of the audio profile (docs/adr/0007-no-raw-signal-leaves-the-edge.md):
// nothing the engine publishes can carry the signal itself. Every message is
// a small flat object of named scalars; there are no arrays, no nested
// objects and no free-form strings beyond a fixed allowlist of keys. A new
// field fails this test, so adding one is a conscious, reviewed decision.
func TestPayloadsCarryNoRawSignal(t *testing.T) {
	c := &captureClient{}
	p := NewPublisher(c, "mic", QoS1)
	if err := p.PublishTelemetry(1700000000000, 0.0147, 0.0147, 1.6, true, "critical", 8.6, "band"); err != nil {
		t.Fatal(err)
	}
	if err := p.PublishAnomaly(time.UnixMilli(1700000000000), "onset", "critical", 0.0147, 8.6, 0.0146, 0.0003, "band", "high"); err != nil {
		t.Fatal(err)
	}

	allowed := map[string][]string{
		"sm/telemetry/mic": {"ts_ms", "sensor_id", "value", "window_rms", "is_anomaly", "severity", "sdt_ratio", "final_score", "dominant_signal"},
		"sm/anomaly/mic":   {"ts_ms", "sensor_id", "event_type", "severity", "rms", "z_score", "baseline_mean", "baseline_stddev", "dominant_signal", "band"},
	}
	for topic, want := range allowed {
		raw, ok := c.payloads[topic]
		if !ok {
			t.Fatalf("nothing published to %s", topic)
		}
		if len(raw) > 400 {
			t.Errorf("%s: payload is %d bytes; a summary message should be small", topic, len(raw))
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		var got []string
		for k, v := range m {
			got = append(got, k)
			switch v.(type) {
			case []any, map[string]any:
				t.Errorf("%s: field %q is an array or object; summaries must be flat scalars", topic, k)
			}
		}
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: fields changed\n got  %v\n want %v\nIf this is intentional, update the allowlist and docs/adr/0007.", topic, got, want)
		}
	}
}
