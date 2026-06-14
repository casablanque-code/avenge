// Package anomaly implements a sliding-window RMS + Z-score anomaly
// detector for vibration signals.
package anomaly

import "math"

// Config holds detector parameters.
type Config struct {
	SampleRate      int
	WindowSize      int
	BaselineWindows int

	// EnterThreshold: z-score above which a NORMAL window becomes ANOMALY.
	EnterThreshold float64
	// ExitThreshold: z-score below which an ANOMALY window becomes NORMAL.
	// Must be < EnterThreshold to provide hysteresis and avoid flapping
	// when z oscillates around a single threshold.
	ExitThreshold float64

	MinStdDev float64
}

// DefaultConfig returns production-ready defaults for 1 kHz MPU-6050.
func DefaultConfig() Config {
	return Config{
		SampleRate:      1000,
		WindowSize:      256,
		BaselineWindows: 10,
		EnterThreshold:  3.5,
		ExitThreshold:   2.5,
		MinStdDev:       0.002,
	}
}

// Severity classifies how far the current window is from baseline.
type Severity string

const (
	SeverityNormal   Severity = "normal"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Event is emitted when the detector changes state.
type Event struct {
	T              float64
	Anomaly        bool
	Severity       Severity
	RMS            float64
	ZScore         float64
	BaselineMean   float64
	BaselineStdDev float64
}

// Detector is a stateful, incremental anomaly detector.
type Detector struct {
	cfg Config

	windowRMSs []float64
	baseIdx    int

	baselineReady    bool
	baseMean         float64
	baseStdDev       float64
	lastAnomalyState bool
	lastSeverity     Severity
}

// NewDetector creates a Detector with the given configuration.
func NewDetector(cfg Config) *Detector {
	return &Detector{
		cfg:        cfg,
		windowRMSs: make([]float64, cfg.BaselineWindows),
	}
}

// IngestWindow processes one full window of samples.
// Returns a non-nil Event only when the anomaly state changes.
func (d *Detector) IngestWindow(samples []float64, windowStartT float64) *Event {
	rms := computeRMS(samples)

	// Warmup phase: collect initial baseline statistics.
	if !d.baselineReady {
		d.windowRMSs[d.baseIdx] = rms
		d.baseIdx++
		if d.baseIdx >= d.cfg.BaselineWindows {
			d.baselineReady = true
			d.baseMean, d.baseStdDev = meanStdDev(d.windowRMSs)
			if d.baseStdDev < d.cfg.MinStdDev {
				d.baseStdDev = d.cfg.MinStdDev
			}
		}
		return nil
	}

	// Detect BEFORE updating baseline.
	// This prevents the EMA from chasing a slowly-ramping fault.
	std := d.baseStdDev
	if std < d.cfg.MinStdDev {
		std = d.cfg.MinStdDev
	}
	z := (rms - d.baseMean) / std

	// Hysteresis: use a lower exit threshold than enter threshold so that
	// a z-score oscillating near the boundary doesn't cause
	// onset/clear/onset/clear flapping.
	var isAnomaly bool
	if d.lastAnomalyState {
		isAnomaly = z > d.cfg.ExitThreshold
	} else {
		isAnomaly = z > d.cfg.EnterThreshold
	}

	severity := classifySeverity(z, d.cfg)

	// Update baseline ONLY when not in anomaly state.
	// Freezing the baseline during a fault keeps z-scores meaningful
	// and prevents the detector from silently normalising a degraded machine.
	if !isAnomaly {
		const alpha = 0.1 // slower EMA = less drift on gradual fault onset

		// Compute variance using the OLD mean before updating it.
		// Using the already-updated mean here biases variance downward,
		// which makes the detector progressively less sensitive over time.
		oldMean := d.baseMean
		newMean := (1-alpha)*oldMean + alpha*rms
		variance := (1-alpha)*d.baseStdDev*d.baseStdDev + alpha*(rms-oldMean)*(rms-oldMean)

		d.baseMean = newMean
		d.baseStdDev = math.Sqrt(variance)
	}

	// Emit event on state OR severity transition.
	if isAnomaly == d.lastAnomalyState && severity == d.lastSeverity {
		return nil
	}
	d.lastAnomalyState = isAnomaly
	d.lastSeverity = severity
	return &Event{
		T:              windowStartT,
		Anomaly:        isAnomaly,
		Severity:       severity,
		RMS:            rms,
		ZScore:         z,
		BaselineMean:   d.baseMean,
		BaselineStdDev: d.baseStdDev,
	}
}

// State returns the current anomaly state.
func (d *Detector) State() bool { return d.lastAnomalyState }

// Severity returns the current severity level.
func (d *Detector) Severity() Severity { return d.lastSeverity }

// BaselineReady reports whether warmup is complete.
func (d *Detector) BaselineReady() bool { return d.baselineReady }

// classifySeverity maps a z-score to a coarse severity bucket using the
// configured thresholds:
//
//	z <= ExitThreshold          -> normal
//	ExitThreshold < z <= Enter  -> warning
//	z > EnterThreshold          -> critical
func classifySeverity(z float64, cfg Config) Severity {
	switch {
	case z > cfg.EnterThreshold:
		return SeverityCritical
	case z > cfg.ExitThreshold:
		return SeverityWarning
	default:
		return SeverityNormal
	}
}

func computeRMS(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range samples {
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(samples)))
}

func meanStdDev(xs []float64) (mean, std float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	for _, x := range xs {
		d := x - mean
		std += d * d
	}
	std = math.Sqrt(std / float64(len(xs)))
	return
}
