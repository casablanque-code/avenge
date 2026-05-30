// Package fft implements a Cooley-Tukey radix-2 FFT and helpers
// for extracting the power spectrum from a time-domain window.
package fft

import (
	"math"
	"math/cmplx"
)

// IsPowerOfTwo reports whether n is a power of two.
func IsPowerOfTwo(n int) bool {
	return n > 0 && (n&(n-1)) == 0
}

// NextPow2 returns the smallest power of two >= n.
func NextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// FFT computes the forward DFT of x in-place using the Cooley-Tukey
// decimation-in-time radix-2 algorithm.
// len(x) must be a power of two; pad with zeros if needed.
func FFT(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}

	// Bit-reversal permutation.
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}

	// Butterfly stages.
	for length := 2; length <= n; length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		wn := complex(math.Cos(angle), math.Sin(angle))
		for i := 0; i < n; i += length {
			w := complex(1, 0)
			half := length / 2
			for k := 0; k < half; k++ {
				u := x[i+k]
				v := x[i+k+half] * w
				x[i+k] = u + v
				x[i+k+half] = u - v
				w *= wn
			}
		}
	}
}

// PowerSpectrum computes the one-sided power spectrum of real-valued
// samples at the given sampleRate (Hz).
//
// It returns:
//   freqs  — frequency bin centres in Hz (length N/2+1)
//   power  — power in each bin (magnitude² / N²)
//
// samples need not be a power of two; they will be zero-padded.
func PowerSpectrum(samples []float64, sampleRate int) (freqs, power []float64) {
	n := NextPow2(len(samples))

	x := make([]complex128, n)
	for i, v := range samples {
		x[i] = complex(v, 0)
	}
	FFT(x)

	half := n/2 + 1
	freqs = make([]float64, half)
	power = make([]float64, half)

	scale := 1.0 / float64(n)
	for k := 0; k < half; k++ {
		freqs[k] = float64(k) * float64(sampleRate) / float64(n)
		mag := cmplx.Abs(x[k]) * scale
		power[k] = mag * mag
	}
	return freqs, power
}

// TopPeaks returns the indices of the top-n highest power bins,
// sorted descending by power. Useful for logging dominant frequencies.
func TopPeaks(power []float64, n int) []int {
	type kv struct{ idx int; val float64 }
	ranked := make([]kv, len(power))
	for i, v := range power {
		ranked[i] = kv{i, v}
	}
	// Partial selection sort for top-n (n is small, typically ≤5).
	for i := 0; i < n && i < len(ranked); i++ {
		maxIdx := i
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].val > ranked[maxIdx].val {
				maxIdx = j
			}
		}
		ranked[i], ranked[maxIdx] = ranked[maxIdx], ranked[i]
	}
	out := make([]int, n)
	for i := range out {
		out[i] = ranked[i].idx
	}
	return out
}

// Processor holds pre-allocated buffers for repeated FFT calls on windows
// of the same size. Eliminates per-call heap allocations and GC pressure.
//
// Not safe for concurrent use — each goroutine should have its own Processor.
//
// Usage:
//
//	p := fft.NewProcessor(512, 1000)
//	freqs, power := p.PowerSpectrum(samples)  // zero allocation
type Processor struct {
	size       int
	sampleRate int
	cx         []complex128
	freqs      []float64
	power      []float64
}

// NewProcessor creates a Processor for windows of exactly `windowSize` samples
// at the given sampleRate. windowSize must be a power of two.
func NewProcessor(windowSize, sampleRate int) *Processor {
	if !IsPowerOfTwo(windowSize) {
		panic("fft.NewProcessor: windowSize must be a power of two")
	}
	half := windowSize/2 + 1
	return &Processor{
		size:       windowSize,
		sampleRate: sampleRate,
		cx:         make([]complex128, windowSize),
		freqs:      make([]float64, half),
		power:      make([]float64, half),
	}
}

// PowerSpectrum computes the one-sided power spectrum of samples in-place,
// reusing internal buffers. Returns slices that are valid until the next call.
//
// len(samples) must equal the windowSize passed to NewProcessor;
// call site is responsible for padding/truncating.
func (p *Processor) PowerSpectrum(samples []float64) (freqs, power []float64) {
	if len(samples) != p.size {
		panic("fft.Processor.PowerSpectrum: len(samples) != windowSize")
	}

	// Copy real input into complex buffer (reuse existing allocation).
	for i, v := range samples {
		p.cx[i] = complex(v, 0)
	}

	FFT(p.cx)

	scale := 1.0 / float64(p.size)
	half := p.size/2 + 1
	for k := 0; k < half; k++ {
		p.freqs[k] = float64(k) * float64(p.sampleRate) / float64(p.size)
		mag := cmplx.Abs(p.cx[k]) * scale
		p.power[k] = mag * mag
	}
	return p.freqs, p.power
}
