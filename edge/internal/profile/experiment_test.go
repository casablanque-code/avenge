//go:build !race

package profile_test

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"testing"

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
