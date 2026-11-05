package sensor

import (
	"testing"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/scoring"
)

// The default configuration must keep the historic scoring bounds.
func TestScoreThresholdsDefaultMatchesScoringDefaults(t *testing.T) {
	if got, want := scoreThresholds(DefaultSensorConfig("x")), scoring.DefaultThresholds(); got != want {
		t.Fatalf("scoreThresholds(default) = %+v, want %+v", got, want)
	}
}

func TestScoreThresholdsFollowTheConfiguration(t *testing.T) {
	cfg := DefaultSensorConfig("x")
	cfg.EnterThreshold, cfg.ExitThreshold = 4.5, 3.0
	got := scoreThresholds(cfg)
	if got.Critical != 4.5 || got.Warning != 3.0 {
		t.Fatalf("scoreThresholds = %+v, want warning 3.0 and critical 4.5", got)
	}
}
