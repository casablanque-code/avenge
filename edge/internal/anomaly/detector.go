// Package anomaly implements a sliding-window RMS + Z-score anomaly
// detector for vibration signals.
package anomaly

import "math"

// Config holds detector parameters.
type Config struct {
	SampleRate      int
	WindowSize      int
	BaselineWindows int
	ZScoreThreshold float64
	MinStdDev       float64
}

// DefaultConfig returns production-ready defaults for 1 kHz MPU-6050.
func DefaultConfig() Config {
	return Config{
		SampleRate:      1000,
		WindowSize:      256,
		BaselineWindows: 10,
		ZScoreThreshold: 3.5,
		MinStdDev:       0.002,
	}
}

// Event is emitted when the detector changes state.
type Event struct {
	T              float64
	Anomaly        bool
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
	isAnomaly := z > d.cfg.ZScoreThreshold

	// Update baseline ONLY when not in anomaly state.
	// Freezing the baseline during a fault keeps z-scores meaningful
	// and prevents the detector from silently normalising a degraded machine.
	if !isAnomaly {
		const alpha = 0.1 // slower EMA = less drift on gradual fault onset
		d.baseMean = (1-alpha)*d.baseMean + alpha*rms
		variance := (1-alpha)*d.baseStdDev*d.baseStdDev + alpha*(rms-d.baseMean)*(rms-d.baseMean)
		d.baseStdDev = math.Sqrt(variance)
	}

	// Emit event on state transition only.
	if isAnomaly == d.lastAnomalyState {
		return nil
	}
	d.lastAnomalyState = isAnomaly
	return &Event{
		T:              windowStartT,
		Anomaly:        isAnomaly,
		RMS:            rms,
		ZScore:         z,
		BaselineMean:   d.baseMean,
		BaselineStdDev: d.baseStdDev,
	}
}

// State returns the current anomaly state.
func (d *Detector) State() bool { return d.lastAnomalyState }

// BaselineReady reports whether warmup is complete.
func (d *Detector) BaselineReady() bool { return d.baselineReady }

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
