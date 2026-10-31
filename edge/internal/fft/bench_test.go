package fft

import (
	"math"
	"testing"
)

func benchSignal(n, sampleRate int) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		t := float64(i) / float64(sampleRate)
		xs[i] = math.Sin(2*math.Pi*50*t) + 0.3*math.Sin(2*math.Pi*120*t)
	}
	return xs
}

// BenchmarkProcessorPowerSpectrum measures the reusable Processor on a
// 512-sample window at 1 kHz (the default sensor configuration).
func BenchmarkProcessorPowerSpectrum(b *testing.B) {
	p := NewProcessor(512, 1000)
	xs := benchSignal(512, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.PowerSpectrum(xs)
	}
}

// BenchmarkPowerSpectrumFunc measures the stateless function for comparison.
func BenchmarkPowerSpectrumFunc(b *testing.B) {
	xs := benchSignal(512, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PowerSpectrum(xs, 1000)
	}
}
