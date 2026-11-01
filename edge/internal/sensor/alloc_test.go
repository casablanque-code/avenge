//go:build !race

package sensor

import (
	"math"
	"testing"
)

// Allocation guards for the per-window path. They are excluded under the race
// detector (which changes allocation behaviour) and run in their own CI step.

func steadyWindow(cfg SensorConfig) []float64 {
	vals := make([]float64, cfg.FFTWindow)
	for i := range vals {
		t := float64(i) / float64(cfg.SampleRate)
		vals[i] = 0.5*math.Sin(2*math.Pi*50*t) + 0.02*math.Sin(2*math.Pi*333*t)
	}
	return vals
}

func feedWindow(w *SensorWorker, vals []float64, t *float64, dt float64) *WorkerEvent {
	var last *WorkerEvent
	for _, v := range vals {
		if ev := w.ingestSample(Sample{T: *t, Value: v}); ev != nil {
			last = ev
		}
		*t += dt
	}
	return last
}

// In steady state, with no telemetry flush and no state change, processing a
// window must not allocate at all.
func TestHotPathAllocFree(t *testing.T) {
	const flushEvery = 1000
	cfg := DefaultSensorConfig("alloc")
	cfg.SDTFlushEvery = flushEvery
	w := newSensorWorker(cfg)
	vals := steadyWindow(cfg)
	dt := 1 / float64(cfg.SampleRate)
	var clock float64

	// Run through one full flush cycle: baselines settle and the buffers grow
	// to their steady-state size. Right after the flush the next one is far
	// away, so the measured windows below contain no flush.
	for i := 0; i < flushEvery; i++ {
		feedWindow(w, vals, &clock, dt)
	}
	allocs := testing.AllocsPerRun(20, func() { feedWindow(w, vals, &clock, dt) })
	if allocs != 0 {
		t.Fatalf("per-window path allocated %v times per window, want 0", allocs)
	}
}

// A window that emits an event may allocate the event and its band snapshot,
// nothing else. The bound catches accidental per-window garbage.
func TestFlushWindowAllocsBounded(t *testing.T) {
	cfg := DefaultSensorConfig("alloc")
	cfg.SDTFlushEvery = 1 // every window flushes
	w := newSensorWorker(cfg)
	vals := steadyWindow(cfg)
	dt := 1 / float64(cfg.SampleRate)
	var clock float64

	for i := 0; i < cfg.BaselineWindows+30; i++ {
		feedWindow(w, vals, &clock, dt)
	}
	allocs := testing.AllocsPerRun(20, func() {
		if feedWindow(w, vals, &clock, dt) == nil {
			t.Fatal("expected a flush event on every window")
		}
	})
	const maxAllocs = 3
	if allocs > maxAllocs {
		t.Fatalf("flush window allocated %v times, want at most %d", allocs, maxAllocs)
	}
}
