package filter_test

import (
"math"
"testing"

"github.com/casablanque-code/smart-manufacturing/edge/internal/filter"
)

func TestSDT_PerfectLine_MaxCompression(t *testing.T) {
pts := make([]filter.Point, 100)
for i := range pts {
pts[i] = filter.Point{T: float64(i), Value: float64(i) * 0.01}
}
got := filter.SDT(pts, 0.05)
if len(got) != 2 {
t.Errorf("linear signal: expected 2 points, got %d", len(got))
}
}

func TestSDT_PreservesEndpoints(t *testing.T) {
pts := []filter.Point{{0, 1.0}, {1, 2.0}, {2, 1.5}, {3, 3.0}}
got := filter.SDT(pts, 0.01)
if got[0] != pts[0] {
t.Errorf("first point not preserved")
}
if got[len(got)-1] != pts[len(pts)-1] {
t.Errorf("last point not preserved")
}
}

func TestSDT_Sinewave(t *testing.T) {
n := 4000
pts := make([]filter.Point, n)
for i := range pts {
t_ := float64(i) / 1000.0
pts[i] = filter.Point{T: t_, Value: math.Sin(2 * math.Pi * t_)}
}
got := filter.SDT(pts, 0.05)
ratio := filter.CompressionRatio(n, len(got))
if ratio < 20 {
t.Errorf("compression ratio %.1f× too low", ratio)
}
t.Logf("sine 1Hz/1kHz deadband=0.05: %d → %d (%.1f×)", n, len(got), ratio)
}
