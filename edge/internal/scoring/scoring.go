// Package scoring combines independent anomaly signals — currently the
// RMS-based Detector and the per-band BandDetector — into a single final
// decision. Before this package existed, the two detectors ran in
// parallel with no combining logic: a worker could publish "severity:
// normal" from the RMS detector in the same window where the band
// detector saw a 90σ spike in the fault band, with no single value an
// operator (or an alert rule) could act on.
//
// FinalScore takes the position that the more alarming of the two
// signals should win — a real fault is more likely to be missed by
// under-reacting to a strong band-specific signal than to be
// over-reacted to. This mirrors the same "max wins" choice already made
// inside BandDetector itself (anomaly.BandResult.MaxZScore takes the
// max across bands, not an average).
package scoring

import "github.com/casablanque-code/smart-manufacturing/edge/internal/anomaly"

// Thresholds for classifying a combined Z-score into a Severity.
// Defaults match anomaly.DefaultConfig()'s EnterThreshold/ExitThreshold —
// kept as a separate struct (not reusing anomaly.Config) so the scoring
// layer can be tuned independently of the RMS detector's hysteresis
// thresholds if that's ever needed.
type Thresholds struct {
	// Warning: above this z-score, severity is at least "warning".
	Warning float64
	// Critical: above this z-score, severity is "critical".
	Critical float64
}

// DefaultThresholds mirrors anomaly.DefaultConfig()'s hysteresis bounds.
func DefaultThresholds() Thresholds {
	return Thresholds{
		Warning:  2.5,
		Critical: 3.5,
	}
}

// Result is the combined output of the scoring layer for one window.
type Result struct {
	// FinalScore is the combined z-score: max(rmsZ, bandZ).
	FinalScore float64
	// Severity is derived from FinalScore via the configured thresholds.
	Severity anomaly.Severity
	// DominantSignal names which input produced FinalScore — "rms" or
	// "band" — so a human or a downstream rule can tell which detector
	// is actually driving the alert, instead of just seeing one number.
	DominantSignal string
}

// Combine produces a single Result from the RMS detector's z-score and
// the band detector's max z-score for the same window.
//
//	finalScore = max(rmsZ, bandZ)
//	severity   = classify(finalScore, thresholds)
//
// This is deliberately a pure function — no state, no I/O — so it's
// trivial to unit test against the threshold boundaries and to call from
// any context (a worker mid-pipeline, a replay tool, a test).
func Combine(rmsZ, bandZ float64, t Thresholds) Result {
	finalScore := rmsZ
	dominant := "rms"
	if bandZ > rmsZ {
		finalScore = bandZ
		dominant = "band"
	}

	return Result{
		FinalScore:     finalScore,
		Severity:       classify(finalScore, t),
		DominantSignal: dominant,
	}
}

// classify maps a combined z-score to a severity bucket, mirroring
// anomaly.classifySeverity's bucket boundaries:
//
//	score <= Warning            -> normal
//	Warning < score <= Critical -> warning
//	score > Critical            -> critical
func classify(score float64, t Thresholds) anomaly.Severity {
	switch {
	case score > t.Critical:
		return anomaly.SeverityCritical
	case score > t.Warning:
		return anomaly.SeverityWarning
	default:
		return anomaly.SeverityNormal
	}
}
