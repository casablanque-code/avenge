package sensor

import (
	"math"
	"reflect"
	"testing"
)

// The band states inside a WorkerEvent cross goroutines through the events
// channel, so later windows must never modify them.
func TestEventBandStatesAreNotAliased(t *testing.T) {
	cfg := DefaultSensorConfig("alias")
	cfg.SDTFlushEvery = 1
	w := newSensorWorker(cfg)
	dt := 1 / float64(cfg.SampleRate)
	var clock float64

	window := func(amp float64) []float64 {
		vals := make([]float64, cfg.FFTWindow)
		for i := range vals {
			ts := float64(i) / float64(cfg.SampleRate)
			vals[i] = 0.5*math.Sin(2*math.Pi*50*ts) + amp*math.Sin(2*math.Pi*200*ts)
		}
		return vals
	}
	run := func(vals []float64) *WorkerEvent {
		var last *WorkerEvent
		for _, v := range vals {
			if ev := w.ingestSample(Sample{T: clock, Value: v}); ev != nil {
				last = ev
			}
			clock += dt
		}
		return last
	}

	var first *WorkerEvent
	for i := 0; i < cfg.BaselineWindows+5; i++ {
		first = run(window(0.01))
	}
	if first == nil || len(first.BandResult.Bands) == 0 {
		t.Fatal("expected a flush event carrying band states")
	}
	frozen := append(first.BandResult.Bands[:0:0], first.BandResult.Bands...)

	for i := 0; i < 20; i++ { // a strong fault changes every later window
		run(window(2.0))
	}
	if !reflect.DeepEqual(frozen, first.BandResult.Bands) {
		t.Fatal("band states of an earlier event changed after later windows")
	}
}
