// Package anomaly — band_detector.go
//
// BandDetector extends the scalar RMS detector with frequency-band-specific
// anomaly detection. Instead of collapsing the entire spectrum to a single
// RMS value, it computes band-limited RMS for N frequency ranges and
// maintains an independent EMA baseline per band.
//
// Why this matters:
//   A bearing fault (e.g. BPFO at 312.5 Hz) raises energy in the 150–400 Hz
//   band while leaving the shaft-rotation band (0–50 Hz) unchanged.
//   A scalar RMS detector dilutes this signal across all frequencies and
//   needs a much larger amplitude before it crosses the threshold.
//   A band detector fires as soon as the fault band alone exceeds its
//   per-band threshold, giving earlier and more precise detection.
//
// Usage:
//
//	bands := anomaly.DefaultBands(1000) // sampleRate = 1000 Hz
//	bd := anomaly.NewBandDetector(bands, anomaly.DefaultBandConfig())
//	result := bd.IngestSpectrum(freqs, power)

package anomaly

import (
	"fmt"
	"math"
)

// FreqBand defines a named frequency range.
type FreqBand struct {
	Name   string
	LoHz   float64
	HiHz   float64
}

// DefaultBands returns the four standard bands for a 1 kHz sensor
// monitoring a 50 Hz shaft with a bearing fault zone around 312 Hz.
func DefaultBands(sampleRate int) []FreqBand {
	return []FreqBand{
		{"shaft",   0,   50},
		{"harmonic", 50,  150},
		{"fault",   150, 400}, // BPFO 312.5 Hz lives here
		{"hf_noise", 400, float64(sampleRate / 2)},
	}
}

// BandConfig holds per-band detection parameters.
type BandConfig struct {
	// WarmupWindows: number of windows to collect baseline per band.
	WarmupWindows int
	// EnterThreshold: z-score above which a band is flagged as anomalous.
	EnterThreshold float64
	// ExitThreshold: z-score below which an anomalous band returns to normal.
	ExitThreshold float64
	// Alpha: EMA smoothing factor for baseline updates (0 < α ≤ 1).
	Alpha float64
	// MinStdDev: floor for per-band stddev to avoid division by zero on
	// very stable bands during warmup.
	MinStdDev float64
}

// DefaultBandConfig returns conservative defaults suitable for PoC.
func DefaultBandConfig() BandConfig {
	return BandConfig{
		WarmupWindows:  10, // matches scalar detector default; ~5s at 1kHz/512 window
		EnterThreshold: 4.0,
		ExitThreshold:  2.5,
		Alpha:          0.1,
		MinStdDev:      1e-6,
	}
}

// BandState holds the running baseline for one frequency band.
type BandState struct {
	Band        FreqBand
	Mean        float64
	StdDev      float64
	ZScore      float64
	BandRMS     float64
	IsAnomaly   bool
	Severity    Severity
	warmupCount int
	ready       bool
	warmupSum   float64
	warmupSumSq float64
}

// BandResult is returned by BandDetector.IngestSpectrum for each window.
type BandResult struct {
	// Bands holds per-band state after processing the current window.
	Bands []BandState
	// AnyAnomaly is true if at least one band is in anomaly state.
	AnyAnomaly bool
	// FaultBandAnomaly is true if specifically the "fault" band is anomalous.
	// This is the primary bearing-degradation indicator.
	FaultBandAnomaly bool
	// MaxZScore across all bands.
	MaxZScore float64
	// MaxZBand is the name of the band with the highest z-score.
	MaxZBand string
	// OverallSeverity is the highest severity across all bands.
	OverallSeverity Severity
}

// BandDetector maintains per-band EMA baselines and detects anomalies
// in specific frequency ranges of the power spectrum.
type BandDetector struct {
	cfg    BandConfig
	states []BandState
}

// NewBandDetector creates a BandDetector for the given frequency bands.
func NewBandDetector(bands []FreqBand, cfg BandConfig) *BandDetector {
	states := make([]BandState, len(bands))
	for i, b := range bands {
		states[i] = BandState{Band: b}
	}
	return &BandDetector{cfg: cfg, states: states}
}

// IngestSpectrum processes one power spectrum window.
// freqs and power must come from fft.PowerSpectrum (same length, same order).
// Returns a BandResult describing the anomaly state of each band.
func (bd *BandDetector) IngestSpectrum(freqs, power []float64) BandResult {
	// Compute band-limited RMS for each band.
	for i := range bd.states {
		bd.states[i].BandRMS = bandRMS(freqs, power, bd.states[i].Band)
	}

	// Update baselines and detect anomalies per band.
	for i := range bd.states {
		st := &bd.states[i]
		rms := st.BandRMS

		if !st.ready {
			// Warmup: accumulate samples for initial mean/stddev estimate.
			st.warmupSum += rms
			st.warmupSumSq += rms * rms
			st.warmupCount++
			if st.warmupCount >= bd.cfg.WarmupWindows {
				n := float64(st.warmupCount)
				st.Mean = st.warmupSum / n
				variance := st.warmupSumSq/n - st.Mean*st.Mean
				if variance < 0 {
					variance = 0
				}
				st.StdDev = math.Sqrt(variance)
				if st.StdDev < bd.cfg.MinStdDev {
					st.StdDev = bd.cfg.MinStdDev
				}
				st.ready = true
			}
			st.ZScore = 0
			st.IsAnomaly = false
			st.Severity = SeverityNormal
			continue
		}

		// Compute z-score BEFORE updating baseline (same pattern as Detector).
		std := st.StdDev
		if std < bd.cfg.MinStdDev {
			std = bd.cfg.MinStdDev
		}
		z := (rms - st.Mean) / std
		st.ZScore = z

		// Hysteresis.
		if st.IsAnomaly {
			st.IsAnomaly = z > bd.cfg.ExitThreshold
		} else {
			st.IsAnomaly = z > bd.cfg.EnterThreshold
		}

		st.Severity = classifySeverity(z, Config{
			EnterThreshold: bd.cfg.EnterThreshold,
			ExitThreshold:  bd.cfg.ExitThreshold,
		})

		// Freeze baseline during anomaly (same rationale as scalar detector).
		if !st.IsAnomaly {
			oldMean := st.Mean
			st.Mean = (1-bd.cfg.Alpha)*oldMean + bd.cfg.Alpha*rms
			variance := (1-bd.cfg.Alpha)*st.StdDev*st.StdDev +
				bd.cfg.Alpha*(rms-oldMean)*(rms-oldMean)
			st.StdDev = math.Sqrt(variance)
			if st.StdDev < bd.cfg.MinStdDev {
				st.StdDev = bd.cfg.MinStdDev
			}
		}
	}

	return bd.buildResult()
}

// Ready reports whether all bands have completed warmup.
func (bd *BandDetector) Ready() bool {
	for i := range bd.states {
		if !bd.states[i].ready {
			return false
		}
	}
	return true
}

// Summary returns a compact human-readable string for console logging.
func (bd *BandDetector) Summary() string {
	s := ""
	for _, st := range bd.states {
		if !st.ready {
			s += fmt.Sprintf("  %-10s WARM\n", st.Band.Name)
			continue
		}
		marker := " "
		if st.IsAnomaly {
			marker = "!"
		}
		s += fmt.Sprintf("  %s %-10s rms=%.4f z=%+.2fσ [%s]\n",
			marker, st.Band.Name, st.BandRMS, st.ZScore, st.Severity)
	}
	return s
}

// ── Private helpers ────────────────────────────────────────────────────────

// bandRMS computes the RMS of the power spectrum within a frequency band.
// Power values are already magnitude² so we sum them and take sqrt,
// giving a measure of total energy in the band.
func bandRMS(freqs, power []float64, band FreqBand) float64 {
	sum := 0.0
	count := 0
	for i, f := range freqs {
		if f >= band.LoHz && f < band.HiHz {
			sum += power[i]
			count++
		}
	}
	if count == 0 {
		return 0
	}
	v := sum / float64(count)
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}

func (bd *BandDetector) buildResult() BandResult {
	r := BandResult{
		Bands:           make([]BandState, len(bd.states)),
		OverallSeverity: SeverityNormal,
	}
	copy(r.Bands, bd.states)

	for _, st := range bd.states {
		if st.IsAnomaly {
			r.AnyAnomaly = true
			if st.Band.Name == "fault" {
				r.FaultBandAnomaly = true
			}
		}
		if st.ZScore > r.MaxZScore {
			r.MaxZScore = st.ZScore
			r.MaxZBand = st.Band.Name
		}
		if severityRank(st.Severity) > severityRank(r.OverallSeverity) {
			r.OverallSeverity = st.Severity
		}
	}
	return r
}

func severityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 2
	case SeverityWarning:
		return 1
	default:
		return 0
	}
}
