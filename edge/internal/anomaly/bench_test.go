package anomaly

import (
	"math"
	"testing"
)

func benchWindow(n, sampleRate int) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		t := float64(i) / float64(sampleRate)
		xs[i] = 0.5*math.Sin(2*math.Pi*50*t) + 0.02*math.Sin(2*math.Pi*333*t)
	}
	return xs
}

// BenchmarkDetectorIngestWindow measures the RMS detector on a 256-sample
// window (half of the default 512 FFT window, as used by the worker).
func BenchmarkDetectorIngestWindow(b *testing.B) {
	cfg := DefaultConfig()
	cfg.SampleRate = 1000
	cfg.WindowSize = 256
	d := NewDetector(cfg)
	xs := benchWindow(256, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.IngestWindow(xs, float64(i))
	}
}

// BenchmarkBandDetectorIngestSpectrum measures the per-band detector on one
// 512-point spectrum at 1 kHz.
func BenchmarkBandDetectorIngestSpectrum(b *testing.B) {
	bd := NewBandDetector(DefaultBands(1000), DefaultBandConfig())
	freqs := make([]float64, 256)
	power := make([]float64, 256)
	for i := range freqs {
		freqs[i] = float64(i) * 1000 / 512
		power[i] = 1e-3
	}
	power[26] = 1 // ~50 Hz
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bd.IngestSpectrum(freqs, power)
	}
}
