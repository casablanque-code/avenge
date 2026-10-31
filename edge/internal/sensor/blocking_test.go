package sensor

import (
	"testing"
	"time"
)

// TestSendBlockingDeliversEverySample feeds far more samples than the input
// buffer holds, as fast as possible, and checks that no window is lost.
func TestSendBlockingDeliversEverySample(t *testing.T) {
	cfg := DefaultSensorConfig("blk")
	cfg.SDTFlushEvery = 1 // one flush event per window
	mgr := NewManager(1024)
	if err := mgr.Add(cfg); err != nil {
		t.Fatal(err)
	}

	const windows = 60
	done := make(chan int)
	go func() {
		n := 0
		for range mgr.Events() {
			n++
		}
		done <- n
	}()

	for i := 0; i < windows*cfg.FFTWindow; i++ {
		mgr.SendBlocking("blk", Sample{T: float64(i) / float64(cfg.SampleRate), Value: 0.1})
	}
	// Give the worker time to drain its input buffer, then stop.
	time.Sleep(200 * time.Millisecond)
	mgr.Stop()

	if got := <-done; got != windows {
		t.Fatalf("got %d window events, want %d", got, windows)
	}
}

// TestSendBlockingReturnsAfterRemove makes sure a blocked sender is released
// when the sensor is removed, instead of hanging forever.
func TestSendBlockingReturnsAfterRemove(t *testing.T) {
	cfg := DefaultSensorConfig("rm")
	mgr := NewManager(16)
	if err := mgr.Add(cfg); err != nil {
		t.Fatal(err)
	}
	mgr.Remove("rm")

	finished := make(chan struct{})
	go func() {
		for i := 0; i < 10*cfg.FFTWindow; i++ {
			mgr.SendBlocking("rm", Sample{Value: 1}) // unknown sensor: must be a no-op
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("SendBlocking hung after the sensor was removed")
	}
	mgr.Stop()
}
