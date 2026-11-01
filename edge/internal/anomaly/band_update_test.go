package anomaly

import (
	"math/rand"
	"reflect"
	"testing"
)

func randomSpectrum(r *rand.Rand, spike bool) (freqs, power []float64) {
	freqs = make([]float64, 256)
	power = make([]float64, 256)
	for i := range freqs {
		freqs[i] = float64(i) * 1000 / 512
		power[i] = 1e-3 * (1 + 0.1*r.Float64())
	}
	power[26] = 1 // ~50 Hz
	if spike {
		power[100] = 5 // ~195 Hz, inside the fault band
	}
	return freqs, power
}

// Update followed by Bands must equal IngestSpectrum on every window,
// through warm-up, steady state, a fault and recovery.
func TestUpdateAndBandsMatchIngestSpectrum(t *testing.T) {
	a := NewBandDetector(DefaultBands(1000), DefaultBandConfig())
	b := NewBandDetector(DefaultBands(1000), DefaultBandConfig())
	r1 := rand.New(rand.NewSource(7))
	r2 := rand.New(rand.NewSource(7))

	for w := 0; w < 120; w++ {
		spike := w >= 60 && w < 80
		f1, p1 := randomSpectrum(r1, spike)
		f2, p2 := randomSpectrum(r2, spike)

		want := a.IngestSpectrum(f1, p1)
		got := b.Update(f2, p2)
		if got.Bands != nil {
			t.Fatalf("window %d: Update must not attach Bands", w)
		}
		got.Bands = b.Bands()
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("window %d: Update+Bands differs from IngestSpectrum", w)
		}
	}
}

// A copy returned by Bands must not change when the detector keeps running:
// it is handed to another goroutine inside an event.
func TestBandsReturnsIndependentCopy(t *testing.T) {
	bd := NewBandDetector(DefaultBands(1000), DefaultBandConfig())
	r := rand.New(rand.NewSource(3))
	for w := 0; w < 40; w++ {
		f, p := randomSpectrum(r, false)
		bd.Update(f, p)
	}
	snapshot := bd.Bands()
	frozen := append([]BandState(nil), snapshot...)

	for w := 0; w < 40; w++ {
		f, p := randomSpectrum(r, true)
		bd.Update(f, p)
	}
	if !reflect.DeepEqual(frozen, snapshot) {
		t.Fatal("a Bands copy changed after later windows were processed")
	}
}
