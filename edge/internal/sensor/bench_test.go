package sensor

import (
	"math"
	"testing"
)

// BenchmarkIngestWindow feeds exactly one FFT window (512 samples at the
// default configuration) per iteration through the full worker pipeline:
// ring buffer, FFT, band detector, RMS detector, scoring and, every
// SDTFlushEvery windows, SDT compression. Allocations are therefore an
// amortised per-window figure that includes the periodic flush.
func BenchmarkIngestWindow(b *testing.B) {
	cfg := DefaultSensorConfig("bench")
	w := newSensorWorker(cfg)
	n := cfg.FFTWindow
	vals := make([]float64, n)
	for i := range vals {
		t := float64(i) / float64(cfg.SampleRate)
		vals[i] = 0.5*math.Sin(2*math.Pi*50*t) + 0.02*math.Sin(2*math.Pi*333*t)
	}
	var t float64
	dt := 1 / float64(cfg.SampleRate)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, v := range vals {
			w.ingestSample(Sample{T: t, Value: v})
			t += dt
		}
	}
}

// BenchmarkIngestWindowNoFlush measures the steady-state per-window path with
// a telemetry flush only every 1000 windows, so the flush cost is amortised to
// well below one allocation per window. It must report 0 allocs/op.
func BenchmarkIngestWindowNoFlush(b *testing.B) {
	cfg := DefaultSensorConfig("bench")
	cfg.SDTFlushEvery = 1000
	w := newSensorWorker(cfg)
	n := cfg.FFTWindow
	vals := make([]float64, n)
	for i := range vals {
		t := float64(i) / float64(cfg.SampleRate)
		vals[i] = 0.5*math.Sin(2*math.Pi*50*t) + 0.02*math.Sin(2*math.Pi*333*t)
	}
	var t float64
	dt := 1 / float64(cfg.SampleRate)
	feed := func() {
		for _, v := range vals {
			w.ingestSample(Sample{T: t, Value: v})
			t += dt
		}
	}
	for i := 0; i < cfg.SDTFlushEvery; i++ { // one full cycle: buffers reach steady size
		feed()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		feed()
	}
}
