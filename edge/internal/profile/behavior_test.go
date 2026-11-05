//go:build !race

package profile_test

import (
	"math"
	"math/rand"
	"testing"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/anomaly"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/profile"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

// Excluded under the race detector, which makes these long runs about ten
// times slower; CI runs them in their own step without -race.
//
// These tests run the audio profile on synthetic ambient sound: a hum, its
// harmonic and noise whose level drifts slowly (the same model as
// firmware-sim's audio profile). They guard the calibration against
// regressions; they do NOT prove accuracy on real recordings.

func ambient(rng *rand.Rand, t float64) float64 {
	v := 0.030*math.Sin(2*math.Pi*120*t) + 0.010*math.Sin(2*math.Pi*360*t)
	return v + rng.NormFloat64()*0.010*(1+0.15*math.Sin(2*math.Pi*0.3*t))
}

// transitions counts every detector state or severity change (warnings
// included) in `seconds` of steady ambient sound.
func transitions(t *testing.T, seed int64, seconds float64) int {
	t.Helper()
	res, err := profile.Resolve(profile.Settings{Profile: "audio", SensorID: "mic"})
	if err != nil {
		t.Fatal(err)
	}
	res.Sensor.SDTFlushEvery = 1 << 20 // count detector events only, not telemetry flushes
	mgr := sensor.NewManager(1 << 16)
	if err := mgr.Add(res.Sensor); err != nil {
		t.Fatal(err)
	}
	done := make(chan int)
	go func() {
		n := 0
		for ev := range mgr.Events() {
			if ev.Event != nil {
				n++
			}
		}
		done <- n
	}()
	rng := rand.New(rand.NewSource(seed))
	rate := float64(res.Sensor.SampleRate)
	for i := 0; i < int(seconds*rate); i++ {
		ts := float64(i) / rate
		mgr.SendBlocking(res.Sensor.ID, sensor.Sample{T: ts, Value: ambient(rng, ts)})
	}
	mgr.Stop()
	return <-done
}

// run feeds `seconds` of ambient sound, adding a 2.5 kHz tone that ramps up
// over one second from toneAt (0 = never). It returns the number of anomaly
// onsets before the tone and the delay of the first onset after it (-1 = none).
func run(t *testing.T, seed int64, seconds, toneAt, toneAmp float64) (falseOnsets int, delay float64) {
	t.Helper()
	res, err := profile.Resolve(profile.Settings{Profile: "audio", SensorID: "mic"})
	if err != nil {
		t.Fatal(err)
	}
	mgr := sensor.NewManager(1 << 16)
	if err := mgr.Add(res.Sensor); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		falseOnsets int
		delay       float64
	}
	done := make(chan outcome)
	go func() {
		o := outcome{delay: -1}
		for ev := range mgr.Events() {
			if ev.Event == nil || !ev.Event.Anomaly {
				continue
			}
			switch {
			case toneAt <= 0 || ev.Event.T < toneAt:
				o.falseOnsets++
			case o.delay < 0:
				o.delay = ev.Event.T - toneAt
			}
		}
		done <- o
	}()

	rng := rand.New(rand.NewSource(seed))
	rate := float64(res.Sensor.SampleRate)
	for i := 0; i < int(seconds*rate); i++ {
		ts := float64(i) / rate
		v := ambient(rng, ts)
		if toneAt > 0 && ts >= toneAt {
			v += math.Min(toneAmp, toneAmp*(ts-toneAt)) * math.Sin(2*math.Pi*2500*ts)
		}
		mgr.SendBlocking(res.Sensor.ID, sensor.Sample{T: ts, Value: v})
	}
	mgr.Stop()
	o := <-done
	return o.falseOnsets, o.delay
}

func TestAudioProfileStaysQuietOnSteadyAmbientSound(t *testing.T) {
	if testing.Short() {
		t.Skip("long synthetic run")
	}
	for seed := int64(1); seed <= 4; seed++ {
		if n, _ := run(t, seed, 150, 0, 0); n != 0 {
			t.Fatalf("seed %d: %d false anomaly onsets in 150 s of steady ambient sound", seed, n)
		}
	}
}

// Warnings must not churn either: every transition is an MQTT message and a
// row in the anomaly table.
func TestAudioProfileDoesNotChurnWarnings(t *testing.T) {
	if testing.Short() {
		t.Skip("long synthetic run")
	}
	for seed := int64(1); seed <= 4; seed++ {
		if n := transitions(t, seed, 120); n > 6 {
			t.Fatalf("seed %d: %d detector transitions in 120 s of steady ambient sound, want at most 6", seed, n)
		}
	}
}

func TestAudioProfileCatchesAnAppearingTone(t *testing.T) {
	if testing.Short() {
		t.Skip("long synthetic run")
	}
	cases := []struct {
		amp, within float64
	}{
		{0.030, 2}, // loud: clearly audible
		{0.020, 3}, // RMS about 60% of the ambient sound's RMS (the practical limit: 0.015 is not reported)
	}
	for _, c := range cases {
		falseOnsets, delay := run(t, 7, 90, 45, c.amp)
		if falseOnsets != 0 {
			t.Fatalf("amp %.3f: %d onsets before the tone", c.amp, falseOnsets)
		}
		if delay < 0 || delay > c.within {
			t.Fatalf("amp %.3f: onset delay %.2f s, want within %.0f s", c.amp, delay, c.within)
		}
	}
}

// quietRoom is a hum with little broadband noise, so the high band is very
// quiet and a faint tone there stands out in the band but barely changes
// the overall RMS.
func quietRoom(rng *rand.Rand, t float64) float64 {
	return 0.020*math.Sin(2*math.Pi*120*t) + rng.NormFloat64()*0.004
}

// toneEvents feeds a quiet room with a 3 kHz tone between from and to
// (seconds) and returns the anomaly events in order.
func toneEvents(t *testing.T, seed int64, seconds, from, to, amp float64) []anomaly.Event {
	t.Helper()
	res, err := profile.Resolve(profile.Settings{Profile: "audio", SensorID: "mic"})
	if err != nil {
		t.Fatal(err)
	}
	mgr := sensor.NewManager(1 << 16)
	if err := mgr.Add(res.Sensor); err != nil {
		t.Fatal(err)
	}
	done := make(chan []anomaly.Event)
	go func() {
		var out []anomaly.Event
		for ev := range mgr.Events() {
			if ev.Event != nil {
				out = append(out, *ev.Event)
			}
		}
		done <- out
	}()
	rng := rand.New(rand.NewSource(seed))
	rate := float64(res.Sensor.SampleRate)
	for i := 0; i < int(seconds*rate); i++ {
		ts := float64(i) / rate
		v := quietRoom(rng, ts)
		if ts >= from && ts < to {
			v += amp * math.Sin(2*math.Pi*3000*ts)
		}
		mgr.SendBlocking(res.Sensor.ID, sensor.Sample{T: ts, Value: v})
	}
	mgr.Stop()
	return <-done
}

// A faint tone adds only a few percent to the RMS, so the RMS detector does
// not see it, but it stands out in its frequency band. It must produce an
// onset that names the band and a matching clear when the tone stops; a clear
// must never appear without an onset before it. (Synthetic signal; this
// guards the logic, it does not measure accuracy on real rooms.)
func TestAudioProfileReportsABandOnlyAnomaly(t *testing.T) {
	if testing.Short() {
		t.Skip("long synthetic run")
	}
	for seed := int64(1); seed <= 3; seed++ {
		evs := toneEvents(t, seed, 120, 45, 75, 0.004)
		var onsets, clears int
		for i, e := range evs {
			if e.Anomaly {
				onsets++
				if i > 0 && evs[i-1].Anomaly {
					t.Errorf("seed %d: two onsets in a row at T=%.1f", seed, e.T)
				}
			} else {
				clears++
				if i == 0 || !evs[i-1].Anomaly {
					t.Errorf("seed %d: clear without a preceding onset at T=%.1f", seed, e.T)
				}
			}
		}
		if onsets != 1 || clears != 1 {
			t.Fatalf("seed %d: want exactly one onset and one clear, got %d and %d: %+v", seed, onsets, clears, evs)
		}
		on, off := evs[0], evs[1]
		if on.T < 44 || on.T > 49 { // T is the start of the window that first contained the tone
			t.Errorf("seed %d: onset at T=%.1f, want within 4 s of the tone starting at 45", seed, on.T)
		}
		if on.Signal != "band" || on.Band != "high" {
			t.Errorf("seed %d: onset should name the high band, got signal=%q band=%q", seed, on.Signal, on.Band)
		}
		if off.T < 75 || off.T > 80 {
			t.Errorf("seed %d: clear at T=%.1f, want within 5 s after the tone stops at 75", seed, off.T)
		}
		if off.Signal != on.Signal || off.Band != on.Band {
			t.Errorf("seed %d: clear should repeat the onset's source (%s %s), got %s %s", seed, on.Signal, on.Band, off.Signal, off.Band)
		}
	}
}
