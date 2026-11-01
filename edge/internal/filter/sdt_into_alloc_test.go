//go:build !race

package filter

import "testing"

// With enough capacity SDTInto must not allocate. Skipped under the race
// detector, which changes allocation behaviour.
func TestSDTIntoAllocFree(t *testing.T) {
	pts := noisySine(10240, 3)
	buf := make([]Point, 0, len(pts))
	allocs := testing.AllocsPerRun(20, func() {
		buf = SDTInto(buf, pts, 0.05)
	})
	if allocs != 0 {
		t.Fatalf("SDTInto allocated %v times per call, want 0", allocs)
	}
}
