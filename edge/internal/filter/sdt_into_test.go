package filter

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func noisySine(n int, seed int64) []Point {
	r := rand.New(rand.NewSource(seed))
	pts := make([]Point, n)
	for i := range pts {
		t := float64(i) / 1000
		pts[i] = Point{T: t, Value: math.Sin(2*math.Pi*5*t) + 0.05*r.NormFloat64()}
	}
	return pts
}

// SDTInto must produce exactly what SDT produces, for every input size.
func TestSDTIntoMatchesSDT(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 10, 500, 10240} {
		pts := noisySine(n, int64(n)+1)
		want := SDT(pts, 0.05)
		got := SDTInto(nil, pts, 0.05)
		if len(want) == 0 && len(got) == 0 {
			continue
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("n=%d: SDTInto differs from SDT (%d vs %d points)", n, len(got), len(want))
		}
	}
}

// A reused destination buffer must not leak points from a previous call.
func TestSDTIntoReusesBuffer(t *testing.T) {
	first := noisySine(2000, 1)
	second := noisySine(300, 2)
	buf := make([]Point, 0, 2000)

	buf = SDTInto(buf, first, 0.05)
	buf = SDTInto(buf, second, 0.05)

	if want := SDT(second, 0.05); !reflect.DeepEqual(want, buf) {
		t.Fatalf("reused buffer holds %d points, want %d", len(buf), len(want))
	}
}
