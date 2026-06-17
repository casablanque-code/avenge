package scoring

import (
	"testing"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/anomaly"
)

func TestCombine_RMSDominant(t *testing.T) {
	r := Combine(5.0, 1.0, DefaultThresholds())
	if r.FinalScore != 5.0 {
		t.Errorf("FinalScore = %v, want 5.0", r.FinalScore)
	}
	if r.DominantSignal != "rms" {
		t.Errorf("DominantSignal = %q, want %q", r.DominantSignal, "rms")
	}
	if r.Severity != anomaly.SeverityCritical {
		t.Errorf("Severity = %v, want critical", r.Severity)
	}
}

func TestCombine_BandDominant(t *testing.T) {
	// This is the exact scenario the scoring layer exists to fix: RMS
	// detector sees nothing alarming, but the band detector has a sharp
	// spike (e.g. a bearing fault frequency) that RMS alone would miss
	// because it's a small fraction of total signal energy.
	r := Combine(1.0, 90.0, DefaultThresholds())
	if r.FinalScore != 90.0 {
		t.Errorf("FinalScore = %v, want 90.0", r.FinalScore)
	}
	if r.DominantSignal != "band" {
		t.Errorf("DominantSignal = %q, want %q", r.DominantSignal, "band")
	}
	if r.Severity != anomaly.SeverityCritical {
		t.Errorf("Severity = %v, want critical", r.Severity)
	}
}

func TestCombine_BothNormal(t *testing.T) {
	r := Combine(0.5, 1.2, DefaultThresholds())
	if r.Severity != anomaly.SeverityNormal {
		t.Errorf("Severity = %v, want normal", r.Severity)
	}
}

func TestCombine_ThresholdBoundaries(t *testing.T) {
	thr := DefaultThresholds()

	cases := []struct {
		name  string
		score float64
		want  anomaly.Severity
	}{
		{"exactly at warning boundary is normal", thr.Warning, anomaly.SeverityNormal},
		{"just above warning boundary", thr.Warning + 0.01, anomaly.SeverityWarning},
		{"exactly at critical boundary is warning", thr.Critical, anomaly.SeverityWarning},
		{"just above critical boundary", thr.Critical + 0.01, anomaly.SeverityCritical},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Combine(c.score, 0, thr)
			if r.Severity != c.want {
				t.Errorf("score=%v: Severity = %v, want %v", c.score, r.Severity, c.want)
			}
		})
	}
}

func TestCombine_EqualScores_PrefersRMSLabel(t *testing.T) {
	// When both signals tie exactly, dominant signal attribution defaults
	// to "rms" — an arbitrary but deterministic tie-break, documented
	// here so a future change to this behavior is a visible diff, not a
	// silent regression.
	r := Combine(3.0, 3.0, DefaultThresholds())
	if r.DominantSignal != "rms" {
		t.Errorf("DominantSignal = %q, want %q on tie", r.DominantSignal, "rms")
	}
}
