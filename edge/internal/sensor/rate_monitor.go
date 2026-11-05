package sensor

import "time"

// RateKind classifies a measured sample rate against the configured one.
type RateKind int

const (
	// RateOK: the samples arrive at the configured rate, within RateTolerance.
	RateOK RateKind = iota
	// RateMismatch: the samples arrive at a clearly different rate, so time and
	// every frequency the engine computes are wrong.
	RateMismatch
	// RateReplay: the samples arrive far faster than real time, which means a
	// file or a generator, not a live source. Nothing to check.
	RateReplay
)

const (
	// RateTolerance is the relative deviation still reported as OK.
	RateTolerance = 0.05
	// replayFactor: faster than this multiple of the configured rate is a replay.
	replayFactor = 20
)

// RateReport is one measurement of the arrival rate.
type RateReport struct {
	Measured float64 // samples per second
	Want     float64 // configured sample rate
	Kind     RateKind
	First    bool // the first measurement of this run
}

// RateMonitor measures how fast samples really arrive and compares it with the
// configured sample rate. A serial device or a microphone pipe has no clock the
// engine can read, so a wrong SENSOR_SAMPLE_RATE would silently shift every
// frequency; this is the safety net.
//
// It ignores the first Settle seconds, because pipes deliver an initial burst
// from their buffers, then measures the slope over Span, and later re-checks
// every Recheck. It takes the time as an argument so tests need no clock.
type RateMonitor struct {
	want                  float64
	settle, span, recheck time.Duration

	started bool
	tFirst  time.Time
	stage   int // 0 settling, 1 first measurement, 2 periodic
	t0      time.Time
	n0      int64
	last    RateKind
}

// NewRateMonitor returns a monitor for the given configured sample rate (Hz).
func NewRateMonitor(sampleRate int) *RateMonitor {
	return &RateMonitor{
		want:    float64(sampleRate),
		settle:  2 * time.Second,
		span:    10 * time.Second,
		recheck: 60 * time.Second,
	}
}

// Observe is called with the current time and the total number of samples read
// so far. It returns a report when one should be shown: the first measurement
// always, later ones only while the rate is still wrong.
func (m *RateMonitor) Observe(now time.Time, samples int64) (RateReport, bool) {
	if !m.started {
		m.started, m.tFirst = true, now
		return RateReport{}, false
	}
	switch m.stage {
	case 0:
		if now.Sub(m.tFirst) >= m.settle {
			m.t0, m.n0, m.stage = now, samples, 1
		}
		return RateReport{}, false
	case 1, 2:
		wait := m.span
		if m.stage == 2 {
			wait = m.recheck
		}
		dt := now.Sub(m.t0)
		if dt < wait {
			return RateReport{}, false
		}
		measured := float64(samples-m.n0) / dt.Seconds()
		first := m.stage == 1
		m.t0, m.n0, m.stage = now, samples, 2

		kind := classify(measured, m.want)
		show := first || (kind == RateMismatch)
		m.last = kind
		if !show {
			return RateReport{}, false
		}
		return RateReport{Measured: measured, Want: m.want, Kind: kind, First: first}, true
	}
	return RateReport{}, false
}

func classify(measured, want float64) RateKind {
	ratio := measured / want
	switch {
	case ratio > replayFactor:
		return RateReplay
	case ratio >= 1-RateTolerance && ratio <= 1+RateTolerance:
		return RateOK
	default:
		return RateMismatch
	}
}
