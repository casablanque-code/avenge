package sensor

import (
	"sync"
	"testing"
	"time"
)

// TestManager_StopRaceWithInFlightEvent stress-tests Stop() under
// realistic concurrent Send() traffic — a worker goroutine should never
// be mid-send on m.events when Stop() closes it. Run with -race.
func TestManager_StopRaceWithInFlightEvent(t *testing.T) {
	for iter := 0; iter < 200; iter++ {
		mgr := NewManager(1) // tiny buffer to force contention

		cfg := DefaultSensorConfig("race_test")
		cfg.FFTWindow = 4 // tiny window so events fire fast
		if err := mgr.Add(cfg); err != nil {
			t.Fatalf("Add: %v", err)
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic during concurrent Send/Stop: %v", r)
				}
			}()
			for i := 0; i < 1000; i++ {
				mgr.Send("race_test", Sample{T: float64(i) / 1000, Value: float64(i % 5)})
			}
		}()

		drainDone := make(chan struct{})
		go func() {
			defer close(drainDone)
			for range mgr.Events() {
			}
		}()

		time.Sleep(time.Microsecond * 50) // let some events flow
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic in Stop(): %v", r)
				}
			}()
			mgr.Stop()
		}()

		wg.Wait()
		<-drainDone
	}
}

// TestManager_StopWhileWorkerMidSend deliberately maximizes the chance
// that Stop() executes close(m.events) exactly while a worker goroutine
// is blocked inside `m.events <- *ev` (an unbuffered channel forces this).
// This is the scenario that originally surfaced a real data race — see
// the WaitGroup added to Manager in worker.go: without it, -race reports
// runtime.closechan() racing against runtime.chansend() at this exact
// call site.
func TestManager_StopWhileWorkerMidSend(t *testing.T) {
	for iter := 0; iter < 500; iter++ {
		mgr := NewManager(0) // unbuffered — forces the worker to block on send

		cfg := DefaultSensorConfig("race2")
		cfg.FFTWindow = 1
		cfg.SDTFlushEvery = 1
		if err := mgr.Add(cfg); err != nil {
			t.Fatalf("Add: %v", err)
		}

		mgr.Send("race2", Sample{T: 0, Value: 1.0})
		time.Sleep(time.Millisecond) // let the worker reach the blocking send

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic: %v (iteration %d)", r, iter)
				}
			}()
			mgr.Stop()
		}()
	}
}
