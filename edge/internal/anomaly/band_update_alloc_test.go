//go:build !race

package anomaly

import (
	"math/rand"
	"testing"
)

func TestBandUpdateAllocFree(t *testing.T) {
	bd := NewBandDetector(DefaultBands(1000), DefaultBandConfig())
	f, p := randomSpectrum(rand.New(rand.NewSource(1)), false)
	for i := 0; i < 50; i++ { // get past warm-up
		bd.Update(f, p)
	}
	allocs := testing.AllocsPerRun(50, func() { bd.Update(f, p) })
	if allocs != 0 {
		t.Fatalf("BandDetector.Update allocated %v times per call, want 0", allocs)
	}
}
