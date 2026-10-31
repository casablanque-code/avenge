package filter

import (
	"math"
	"testing"
)

// BenchmarkSDT compresses one default flush interval: 20 windows x 512
// samples of a noisy 50 Hz signal.
func BenchmarkSDT(b *testing.B) {
	pts := make([]Point, 20*512)
	for i := range pts {
		t := float64(i) / 1000
		pts[i] = Point{T: t, Value: math.Sin(2*math.Pi*50*t) + 0.02*math.Sin(2*math.Pi*333*t)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SDT(pts, 0.05)
	}
}
