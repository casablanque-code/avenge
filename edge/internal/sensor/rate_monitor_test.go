package sensor

import (
	"testing"
	"time"
)

// feed simulates a source delivering `rate` samples per second, after an
// initial burst of `burst` samples, and returns every report shown in `secs`.
func feed(m *RateMonitor, rate float64, burst int64, secs int) []RateReport {
	t0 := time.Unix(1000, 0)
	var out []RateReport
	for s := 0; s <= secs; s++ {
		n := burst + int64(rate*float64(s))
		if r, ok := m.Observe(t0.Add(time.Duration(s)*time.Second), n); ok {
			out = append(out, r)
		}
	}
	return out
}

func TestRateMonitorOK(t *testing.T) {
	got := feed(NewRateMonitor(1600), 1598, 0, 20)
	if len(got) != 1 || got[0].Kind != RateOK || !got[0].First {
		t.Fatalf("want one OK report, got %+v", got)
	}
}

func TestRateMonitorIgnoresStartupBurst(t *testing.T) {
	// 64k samples arrive at once (pipe buffers), then the real rate.
	got := feed(NewRateMonitor(16000), 16000, 64000, 20)
	if len(got) != 1 || got[0].Kind != RateOK {
		t.Fatalf("a startup burst must not cause a warning, got %+v", got)
	}
}

func TestRateMonitorWrongRateWarnsAndRepeats(t *testing.T) {
	got := feed(NewRateMonitor(16000), 8000, 0, 130)
	if len(got) < 2 {
		t.Fatalf("want a first report and a repeat while the rate stays wrong, got %+v", got)
	}
	for _, r := range got {
		if r.Kind != RateMismatch || r.Measured < 7900 || r.Measured > 8100 {
			t.Fatalf("bad report %+v", r)
		}
	}
}

func TestRateMonitorDoubleRateWarns(t *testing.T) {
	got := feed(NewRateMonitor(16000), 32000, 0, 20)
	if len(got) != 1 || got[0].Kind != RateMismatch {
		t.Fatalf("twice the configured rate must be a mismatch, got %+v", got)
	}
}

func TestRateMonitorReplayIsNotAMismatch(t *testing.T) {
	got := feed(NewRateMonitor(16000), 2_000_000, 0, 20)
	if len(got) != 1 || got[0].Kind != RateReplay {
		t.Fatalf("want one replay report, got %+v", got)
	}
}

func TestRateMonitorQuietWhenOKAfterFirst(t *testing.T) {
	got := feed(NewRateMonitor(1000), 1000, 0, 200)
	if len(got) != 1 {
		t.Fatalf("OK must be reported once, got %d reports", len(got))
	}
}
