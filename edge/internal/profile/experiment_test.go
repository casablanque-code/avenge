//go:build !race

package profile_test

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"testing"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/anomaly"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/profile"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

// TestExperimentToneSweep is the harness of experiment E-002
// (docs/experiments/E-002-tone-sweep.md). It prints a table; it asserts
// nothing, because it measures. Skipped unless asked for:
//
//	AVENGE_EXPERIMENT=tone-sweep go test -run TestExperimentToneSweep -v ./internal/profile
func TestExperimentToneSweep(t *testing.T) {
	if os.Getenv("AVENGE_EXPERIMENT") != "tone-sweep" {
		t.Skip("experiment harness; set AVENGE_EXPERIMENT=tone-sweep to run")
	}
	const (
		seconds = 90.0
		toneAt  = 45.0
		toneHz  = 3000.0
		within  = 20.0 // an onset this soon after the tone counts as a detection
	)
	backgrounds := []struct {
		name string
		gen  func(rng *rand.Rand, t float64) float64
		amps []float64
	}{
		{"ambient (noise 0.010)", ambient, []float64{0.006, 0.010, 0.015, 0.020, 0.030}},
		{"quiet room (noise 0.004)", quietRoom, []float64{0.001, 0.002, 0.003, 0.004, 0.006}},
	}
	seeds := []int64{1, 2, 3, 4, 5}

	fmt.Printf("\n%-26s %7s %9s %9s %12s %7s\n", "background", "tone", "RMS rise", "detected", "median delay", "false")
	for _, bg := range backgrounds {
		for _, amp := range bg.amps {
			var delays []float64
			var detected, falseOnsets int
			var riseSum float64
			for _, seed := range seeds {
				res, err := profile.Resolve(profile.Settings{Profile: "audio", SensorID: "mic"})
				if err != nil {
					t.Fatal(err)
				}
				mgr := sensor.NewManager(1 << 16)
				if err := mgr.Add(res.Sensor); err != nil {
					t.Fatal(err)
				}
				type out struct {
					delay float64
					false int
				}
				done := make(chan out)
				go func() {
					o := out{delay: -1}
					for ev := range mgr.Events() {
						if ev.Event == nil || !ev.Event.Anomaly {
							continue
						}
						switch {
						case ev.Event.T < toneAt-1:
							o.false++
						case o.delay < 0 && ev.Event.T-toneAt <= within:
							o.delay = math.Max(0, ev.Event.T-toneAt)
						}
					}
					done <- o
				}()
				rng := rand.New(rand.NewSource(seed))
				rate := float64(res.Sensor.SampleRate)
				var sumBase, sumTone float64
				var nBase, nTone int
				for i := 0; i < int(seconds*rate); i++ {
					ts := float64(i) / rate
					v := bg.gen(rng, ts)
					if ts >= toneAt {
						v += amp * math.Sin(2*math.Pi*toneHz*ts)
					}
					switch {
					case ts < toneAt-5:
						sumBase += v * v
						nBase++
					case ts >= toneAt+5:
						sumTone += v * v
						nTone++
					}
					mgr.SendBlocking(res.Sensor.ID, sensor.Sample{T: ts, Value: v})
				}
				mgr.Stop()
				o := <-done
				if o.delay >= 0 {
					detected++
					delays = append(delays, o.delay)
				}
				falseOnsets += o.false
				riseSum += math.Sqrt(sumTone/float64(nTone))/math.Sqrt(sumBase/float64(nBase)) - 1
			}
			sort.Float64s(delays)
			med := "-"
			if len(delays) > 0 {
				med = fmt.Sprintf("%.1f s", delays[len(delays)/2])
			}
			fmt.Printf("%-26s %7.3f %8.1f%% %6d/%d %12s %7d\n",
				bg.name, amp, 100*riseSum/float64(len(seeds)), detected, len(seeds), med, falseOnsets)
		}
	}
}

// ---- E-007: a crude living room (docs/experiments/E-007-living-room-model.md) ----

// tvModel is a crude TV/speech sound: low-passed noise (one pole, about
// 3 kHz), a 4 Hz syllable-like amplitude modulation, and a slowly wandering
// level. It is a model of "non-stationary background", not of any real TV.
type tvModel struct {
	rng       *rand.Rand
	lp, level float64
}

func (s *tvModel) next(t, gain float64) float64 {
	s.lp = 0.55*s.lp + 0.45*s.rng.NormFloat64()
	if int(math.Round(t*16000))%1600 == 0 { // every 0.1 s the level wanders
		s.level = math.Max(0.6, math.Min(1.4, s.level+0.04*s.rng.NormFloat64()))
	}
	env := 0.6 + 0.4*math.Abs(math.Sin(2*math.Pi*4*t))
	return gain * s.level * env * s.lp
}

type roomRun struct {
	events []anomaly.Event
	stats  sensor.WorkerStats
}

func playRoom(seed int64, seconds float64, gen func(rng *rand.Rand, ts float64, tv *tvModel) float64) roomRun {
	res, _ := profile.Resolve(profile.Settings{Profile: "audio", SensorID: "mic"})
	mgr := sensor.NewManager(1 << 16)
	mgr.Add(res.Sensor)
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
	tv := &tvModel{rng: rand.New(rand.NewSource(seed + 100)), level: 1}
	rate := float64(res.Sensor.SampleRate)
	for i := 0; i < int(seconds*rate); i++ {
		ts := float64(i) / rate
		mgr.SendBlocking("mic", sensor.Sample{T: ts, Value: gen(rng, ts, tv)})
	}
	mgr.Stop()
	st, _ := mgr.Stats("mic")
	return roomRun{<-done, st}
}

func countOnsets(r roomRun) (n int) {
	for _, e := range r.events {
		if e.Anomaly {
			n++
		}
	}
	return
}

// AVENGE_EXPERIMENT=living-room go test -run TestExperimentLivingRoom -v ./internal/profile
func TestExperimentLivingRoom(t *testing.T) {
	if os.Getenv("AVENGE_EXPERIMENT") != "living-room" {
		t.Skip("experiment harness; set AVENGE_EXPERIMENT=living-room to run")
	}
	fmt.Println("A: quiet baseline, TV on from 30 s and left on, 330 s total")
	for seed := int64(1); seed <= 3; seed++ {
		r := playRoom(seed, 330, func(rng *rand.Rand, ts float64, s *tvModel) float64 {
			v := quietRoom(rng, ts)
			if ts >= 30 {
				v += s.next(ts, 0.02)
			}
			return v
		})
		fmt.Printf("  seed %d: in anomaly %d/%d windows  ", seed, r.stats.InAnomaly, r.stats.Windows)
		for _, e := range r.events {
			k := "clear"
			if e.Anomaly {
				k = "ONSET"
			}
			fmt.Printf("[%s@%.0fs %s %s] ", k, e.T, e.Signal, e.Band)
		}
		fmt.Println()
	}
	fmt.Println("B: TV on during baseline; 3 s louder burst (speech, x2) every 25-45 s, 600 s")
	for seed := int64(1); seed <= 3; seed++ {
		br := rand.New(rand.NewSource(seed + 500))
		next, until, bursts := 40.0, 0.0, 0
		r := playRoom(seed, 600, func(rng *rand.Rand, ts float64, s *tvModel) float64 {
			if ts >= next && until == 0 {
				until = ts + 3
				bursts++
			}
			g := 0.02
			if until > 0 {
				g = 0.04
				if ts >= until {
					until, next, g = 0, ts+25+20*br.Float64(), 0.02
				}
			}
			return quietRoom(rng, ts)*0.3 + s.next(ts, g)
		})
		fmt.Printf("  seed %d: bursts=%d onsets=%d  in anomaly %d/%d windows\n", seed, bursts, countOnsets(r), r.stats.InAnomaly, r.stats.Windows)
	}
	fmt.Println("C: TV on during baseline, nothing else, 600 s")
	for seed := int64(1); seed <= 3; seed++ {
		r := playRoom(seed, 600, func(rng *rand.Rand, ts float64, s *tvModel) float64 {
			return quietRoom(rng, ts)*0.3 + s.next(ts, 0.02)
		})
		fmt.Printf("  seed %d: onsets=%d  in anomaly %d/%d windows\n", seed, countOnsets(r), r.stats.InAnomaly, r.stats.Windows)
	}
}
