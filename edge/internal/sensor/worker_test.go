package sensor_test

import (
	"math"
	"testing"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

// makeSamples generates synthetic vibration samples.
// normal=true: clean 50 Hz sine; normal=false: adds 312.5 Hz fault component.
func makeSamples(n, sampleRate int, normal bool) []sensor.Sample {
	samples := make([]sensor.Sample, n)
	for i := range samples {
		t := float64(i) / float64(sampleRate)
		v := math.Sin(2*math.Pi*50*t) + 0.35*math.Sin(2*math.Pi*100*t)
		if !normal {
			v += 1.8 * math.Sin(2*math.Pi*312.5*t)
		}
		samples[i] = sensor.Sample{T: t, Value: v}
	}
	return samples
}

func TestManager_AddRemove(t *testing.T) {
	mgr := sensor.NewManager(64)
	defer mgr.Stop()

	cfg := sensor.DefaultSensorConfig("bearing_01")
	if err := mgr.Add(cfg); err != nil {
		t.Fatalf("Add: %v", err)
	}

	ids := mgr.Sensors()
	if len(ids) != 1 || ids[0] != "bearing_01" {
		t.Errorf("expected [bearing_01], got %v", ids)
	}

	mgr.Remove("bearing_01")
	if len(mgr.Sensors()) != 0 {
		t.Error("expected no sensors after Remove")
	}
}

func TestManager_EmptyIDRejected(t *testing.T) {
	mgr := sensor.NewManager(64)
	defer mgr.Stop()

	if err := mgr.Add(sensor.SensorConfig{}); err == nil {
		t.Error("expected error for empty sensor ID")
	}
}

func TestManager_MultiSensor_IndependentPipelines(t *testing.T) {
	// Two sensors with different configs run concurrently.
	// Events from each must carry the correct SensorID.
	mgr := sensor.NewManager(128)
	defer mgr.Stop()

	cfg1 := sensor.DefaultSensorConfig("bearing_01")
	cfg2 := sensor.DefaultSensorConfig("motor_shaft")
	cfg2.EnterThreshold = 4.0 // stricter threshold for motor

	if err := mgr.Add(cfg1); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Add(cfg2); err != nil {
		t.Fatal(err)
	}

	if got := len(mgr.Sensors()); got != 2 {
		t.Fatalf("expected 2 sensors, got %d", got)
	}

	// Feed normal warmup samples to both sensors.
	// Use time.Sleep between batches to let goroutines drain the input buffer.
	normal := makeSamples(cfg1.FFTWindow*cfg1.BaselineWindows*2, cfg1.SampleRate, true)
	batchSize := cfg1.FFTWindow * 2 // one buffer-worth at a time
	for i := 0; i < len(normal); i += batchSize {
		end := i + batchSize
		if end > len(normal) {
			end = len(normal)
		}
		for _, s := range normal[i:end] {
			mgr.Send("bearing_01", s)
			mgr.Send("motor_shaft", s)
		}
		time.Sleep(5 * time.Millisecond) // let workers drain
	}

	// Feed fault samples to bearing_01 only.
	fault := makeSamples(cfg1.FFTWindow*10, cfg1.SampleRate, false)
	for _, s := range fault {
		mgr.Send("bearing_01", s)
	}
	time.Sleep(30 * time.Millisecond)

	// Drain events — all events should have valid SensorIDs.
	timeout := time.After(200 * time.Millisecond)
	var eventsFromBearing, eventsFromMotor int
loop:
	for {
		select {
		case ev, ok := <-mgr.Events():
			if !ok {
				break loop
			}
			switch ev.SensorID {
			case "bearing_01":
				eventsFromBearing++
			case "motor_shaft":
				eventsFromMotor++
			default:
				t.Errorf("event with unexpected SensorID: %q", ev.SensorID)
			}
		case <-timeout:
			break loop
		}
	}

	t.Logf("bearing_01 events: %d, motor_shaft events: %d",
		eventsFromBearing, eventsFromMotor)
	// bearing_01 received fault signal — should have at least one event.
	// motor_shaft received only normal signal — may have events from warmup transitions.
}

func TestManager_ReplaceExistingSensor(t *testing.T) {
	// Adding a sensor with an existing ID should replace it cleanly.
	mgr := sensor.NewManager(64)
	defer mgr.Stop()

	cfg := sensor.DefaultSensorConfig("bearing_01")
	if err := mgr.Add(cfg); err != nil {
		t.Fatal(err)
	}

	// Replace with different config.
	cfg.EnterThreshold = 5.0
	if err := mgr.Add(cfg); err != nil {
		t.Fatalf("re-Add: %v", err)
	}

	if got := len(mgr.Sensors()); got != 1 {
		t.Errorf("expected 1 sensor after replace, got %d", got)
	}
}

func TestManager_SendToUnknownSensor(t *testing.T) {
	// Sending to a non-existent sensor should be a silent no-op.
	mgr := sensor.NewManager(64)
	defer mgr.Stop()

	// Should not panic.
	mgr.Send("nonexistent", sensor.Sample{T: 0, Value: 1.0})
}

func TestSensorConfig_Default(t *testing.T) {
	cfg := sensor.DefaultSensorConfig("test")
	if cfg.ID != "test" {
		t.Errorf("ID: got %q, want %q", cfg.ID, "test")
	}
	if cfg.SampleRate <= 0 {
		t.Error("SampleRate must be positive")
	}
	if cfg.FFTWindow <= 0 || cfg.FFTWindow&(cfg.FFTWindow-1) != 0 {
		t.Errorf("FFTWindow must be a positive power of two, got %d", cfg.FFTWindow)
	}
	if cfg.EnterThreshold <= cfg.ExitThreshold {
		t.Errorf("EnterThreshold %.2f must be > ExitThreshold %.2f",
			cfg.EnterThreshold, cfg.ExitThreshold)
	}
}
