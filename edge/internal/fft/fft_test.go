package fft_test

import (
"math"
"testing"

"github.com/casablanque-code/smart-manufacturing/edge/internal/fft"
)

func TestFFT_DC(t *testing.T) {
x := []complex128{1, 1, 1, 1, 1, 1, 1, 1}
fft.FFT(x)
if math.Abs(real(x[0])-8) > 1e-9 {
t.Errorf("DC bin: got %v, want 8", x[0])
}
}

func TestFFT_SingleFrequency(t *testing.T) {
n := 64
k0 := 5
x := make([]complex128, n)
for i := range x {
angle := 2 * math.Pi * float64(k0) * float64(i) / float64(n)
x[i] = complex(math.Cos(angle), 0)
}
fft.FFT(x)
for k := 0; k < n; k++ {
r, im := real(x[k]), imag(x[k])
mag := math.Sqrt(r*r + im*im)
if k == k0 || k == n-k0 {
if math.Abs(mag-float64(n)/2) > 1e-6 {
t.Errorf("bin %d: got %.6f, want %.1f", k, mag, float64(n)/2)
}
} else if mag > 1e-6 {
t.Errorf("bin %d: got %.6f, want ~0", k, mag)
}
}
}

func TestPowerSpectrum_PeakFrequency(t *testing.T) {
sampleRate := 1000
n := 1024
targetHz := 50.0
samples := make([]float64, n)
for i := range samples {
samples[i] = math.Sin(2 * math.Pi * targetHz * float64(i) / float64(sampleRate))
}
freqs, power := fft.PowerSpectrum(samples, sampleRate)
peaks := fft.TopPeaks(power, 1)
if math.Abs(freqs[peaks[0]]-targetHz) > 1.0 {
t.Errorf("peak: got %.2f Hz, want %.2f Hz", freqs[peaks[0]], targetHz)
}
}

func TestNextPow2(t *testing.T) {
cases := [][2]int{{1, 1}, {2, 2}, {3, 4}, {5, 8}, {1000, 1024}, {1024, 1024}}
for _, c := range cases {
if got := fft.NextPow2(c[0]); got != c[1] {
t.Errorf("NextPow2(%d) = %d, want %d", c[0], got, c[1])
}
}
}

func TestProcessor_SameResultAsStandaloneFunc(t *testing.T) {
sampleRate := 1000
windowSize := 512
samples := make([]float64, windowSize)
for i := range samples {
samples[i] = math.Sin(2*math.Pi*50*float64(i)/float64(sampleRate)) +
0.3*math.Sin(2*math.Pi*150*float64(i)/float64(sampleRate))
}
f1, p1 := fft.PowerSpectrum(samples, sampleRate)
proc := fft.NewProcessor(windowSize, sampleRate)
proc.PowerSpectrum(samples)
f2, p2 := proc.PowerSpectrum(samples)
if len(f1) != len(f2) {
t.Fatalf("length mismatch: %d vs %d", len(f1), len(f2))
}
for i := range p1 {
if math.Abs(p1[i]-p2[i]) > 1e-12 {
t.Errorf("bin %d: standalone=%.6e processor=%.6e", i, p1[i], p2[i])
}
}
}
