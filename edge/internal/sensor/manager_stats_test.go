package sensor

import (
	"testing"
	"time"
)

func TestManagerStatsAndWindowFields(t *testing.T) {
	cfg := DefaultSensorConfig("s1")
	cfg.FFTWindow = 64
	cfg.SampleRate = 1000
	cfg.SDTFlushEvery = 2
	m := NewManager(64)
	if err := m.Add(cfg); err != nil {
		t.Fatal(err)
	}
	const windows = 6
	for i := 0; i < windows*cfg.FFTWindow; i++ {
		m.SendBlocking("s1", Sample{T: float64(i) / 1000, Value: 0.01 * float64(i%7)})
	}
	time.Sleep(200 * time.Millisecond)
	m.Stop()

	st, ok := m.Stats("s1")
	if !ok || st.Windows != windows {
		t.Fatalf("Stats = %+v ok=%v, want %d windows", st, ok, windows)
	}
	var last WorkerEvent
	n := 0
	for ev := range m.Events() {
		n++
		last = ev
		if ev.WindowTime.IsZero() {
			t.Error("event without WindowTime")
		}
		if ev.Window < 1 || ev.Window > windows {
			t.Errorf("Window = %d out of range", ev.Window)
		}
	}
	if n != windows/cfg.SDTFlushEvery {
		t.Fatalf("got %d events, want %d (one per flush)", n, windows/cfg.SDTFlushEvery)
	}
	if last.Window != windows {
		t.Fatalf("last event Window = %d, want %d", last.Window, windows)
	}
	if _, ok := m.Stats("nope"); ok {
		t.Error("Stats of an unknown sensor must report false")
	}
}
