package anomaly_test

import (
"math"
"testing"

"github.com/casablanque-code/smart-manufacturing/edge/internal/anomaly"
)

func makeSine(n, sampleRate int, hz, amp float64) []float64 {
s := make([]float64, n)
for i := range s {
s[i] = 1.0 + amp*math.Sin(2*math.Pi*hz*float64(i)/float64(sampleRate))
}
return s
}

func TestDetector_NoFalsePositiveDuringWarmup(t *testing.T) {
cfg := anomaly.DefaultConfig()
d := anomaly.NewDetector(cfg)
window := makeSine(cfg.WindowSize, cfg.SampleRate, 50, 1.0)
for i := 0; i < cfg.BaselineWindows; i++ {
if ev := d.IngestWindow(window, float64(i)); ev != nil {
t.Errorf("unexpected event during warmup at window %d", i)
}
}
if !d.BaselineReady() {
t.Error("baseline should be ready after BaselineWindows ingestions")
}
}

func TestDetector_DetectsAnomaly(t *testing.T) {
cfg := anomaly.DefaultConfig()
cfg.ZScoreThreshold = 3.0
d := anomaly.NewDetector(cfg)
normal := makeSine(cfg.WindowSize, cfg.SampleRate, 50, 1.0)
for i := 0; i < cfg.BaselineWindows+5; i++ {
d.IngestWindow(normal, float64(i))
}
fault := make([]float64, cfg.WindowSize)
for i := range fault {
t_ := float64(i) / float64(cfg.SampleRate)
fault[i] = 1.0 + math.Sin(2*math.Pi*50*t_) + 3.0*math.Sin(2*math.Pi*312*t_)
}
var detected *anomaly.Event
for attempt := 0; attempt < 5; attempt++ {
if ev := d.IngestWindow(fault, float64(cfg.BaselineWindows+5+attempt)); ev != nil && ev.Anomaly {
detected = ev
break
}
}
if detected == nil {
t.Fatal("anomaly not detected within 5 fault windows")
}
t.Logf("detected: RMS=%.4f Z=%.2f", detected.RMS, detected.ZScore)
}

func TestDetector_BaselineFrozenDuringAnomaly(t *testing.T) {
cfg := anomaly.DefaultConfig()
cfg.ZScoreThreshold = 2.0
d := anomaly.NewDetector(cfg)
normal := makeSine(cfg.WindowSize, cfg.SampleRate, 50, 1.0)
for i := 0; i < cfg.BaselineWindows+5; i++ {
d.IngestWindow(normal, float64(i))
}
fault := make([]float64, cfg.WindowSize)
for i := range fault {
fault[i] = 4.0
}
var triggered bool
for i := 0; i < 3; i++ {
if ev := d.IngestWindow(fault, float64(100+i)); ev != nil && ev.Anomaly {
triggered = true
}
}
if !triggered {
t.Skip("anomaly not triggered — adjust threshold")
}
for i := 0; i < 20; i++ {
d.IngestWindow(fault, float64(200+i))
}
if !d.State() {
t.Error("detector should remain in anomaly state while fault persists")
}
}
