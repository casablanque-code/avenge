// edge-filter: streaming pipeline with MQTT output.
package main

import (
"flag"
"fmt"
"os"
"os/signal"
"strings"
"syscall"
"time"

"github.com/casablanque-code/smart-manufacturing/edge/internal/anomaly"
"github.com/casablanque-code/smart-manufacturing/edge/internal/filter"
"github.com/casablanque-code/smart-manufacturing/edge/internal/fft"
mqttclient "github.com/casablanque-code/smart-manufacturing/edge/internal/mqtt"
"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

const (
sampleRate = 1000
fftWindow  = 512
sdtEpsilon = 0.05
topFreqs   = 4
)

func main() {
broker   := flag.String("broker",    "localhost:1883", "MQTT broker address")
sensorID := flag.String("sensor-id", "bearing_01",    "Sensor identifier")
noMQTT   := flag.Bool("no-mqtt",     false,           "Disable MQTT output")
flag.Parse()

fmt.Fprintf(os.Stderr, "edge-filter  sensor=%s  broker=%s\n", *sensorID, *broker)

var pub *mqttclient.Publisher
if !*noMQTT {
cfg := mqttclient.DefaultConfig(*broker, "edge-filter-"+*sensorID)
cfg.ConnectTimeout = 3 * time.Second
client, err := mqttclient.Connect(cfg)
if err != nil {
fmt.Fprintf(os.Stderr, "MQTT connect failed (%v) — running in console-only mode\n", err)
} else {
pub = mqttclient.NewPublisher(client, *sensorID, mqttclient.QoS1)
fmt.Fprintln(os.Stderr, "MQTT connected ✓")
defer client.Disconnect()
}
}

ringBuf   := make([]float64, fftWindow)
ringFilled := 0
rawPoints := make([]filter.Point, 0, fftWindow*20)

fftProc := fft.NewProcessor(fftWindow, sampleRate)
detCfg  := anomaly.DefaultConfig()
detCfg.SampleRate      = sampleRate
detCfg.BaselineWindows = 10
det := anomaly.NewDetector(detCfg)

reader := sensor.NewReader(os.Stdin)

var (
totalSamples    int64
totalWindows    int
anomalyWindows  int
totalRaw        int
totalCompressed int
lastSDTRatio    float32 = 1.0
)

sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
go func() {
<-sigCh
printStats(totalSamples, totalWindows, anomalyWindows, totalRaw, totalCompressed)
os.Exit(0)
}()

const sdtFlushEvery = 20

for {
s, err := reader.Next()
if err != nil {
break
}

ringBuf[ringFilled] = s.Value
rawPoints = append(rawPoints, filter.Point{T: s.T, Value: s.Value})
ringFilled++
totalSamples++

if ringFilled < fftWindow {
continue
}

totalWindows++
windowT := s.T

removeDCInPlace(ringBuf)
freqs, power := fftProc.PowerSpectrum(ringBuf)
peaks := fft.TopPeaks(power, topFreqs)

tail := rawPoints[len(rawPoints)-fftWindow:]
origVals := make([]float64, fftWindow)
for i, p := range tail {
origVals[i] = p.Value
}
windowStartT := windowT - float64(fftWindow-1)/sampleRate
event := det.IngestWindow(origVals, windowStartT)
isAnomaly := det.State()
if isAnomaly {
anomalyWindows++
}
windowRMS := float32(rmsOf(origVals))

if totalWindows%sdtFlushEvery == 0 {
compressed := filter.SDT(rawPoints, sdtEpsilon)
lastSDTRatio = float32(filter.CompressionRatio(len(rawPoints), len(compressed)))
totalRaw += len(rawPoints)
totalCompressed += len(compressed)

if pub != nil {
tsMs := time.Now().UnixMilli()
for _, pt := range compressed {
if err := pub.PublishTelemetry(
tsMs,
float32(pt.Value),
windowRMS,
lastSDTRatio,
isAnomaly,
); err != nil {
fmt.Fprintf(os.Stderr, "publish telemetry: %v\n", err)
}
}
}
rawPoints = rawPoints[:0]
fmt.Fprintf(os.Stderr, "  → SDT flush @win%-4d  ratio=%.1f×  mqtt=%v\n",
totalWindows, lastSDTRatio, pub != nil)
}

if event != nil && pub != nil {
eventType := "clear"
if event.Anomaly {
eventType = "onset"
}
if err := pub.PublishAnomaly(
time.Now(),
eventType,
float32(event.RMS),
float32(event.ZScore),
float32(event.BaselineMean),
float32(event.BaselineStdDev),
); err != nil {
fmt.Fprintf(os.Stderr, "publish anomaly: %v\n", err)
}
}

printWindow(totalWindows, windowT, freqs, power, peaks,
isAnomaly, det.BaselineReady(), event)

ringFilled = 0
}

if len(rawPoints) > 0 {
compressed := filter.SDT(rawPoints, sdtEpsilon)
lastSDTRatio = float32(filter.CompressionRatio(len(rawPoints), len(compressed)))
totalRaw += len(rawPoints)
totalCompressed += len(compressed)
}

printStats(totalSamples, totalWindows, anomalyWindows, totalRaw, totalCompressed)
}

func removeDCInPlace(xs []float64) {
mean := 0.0
for _, x := range xs {
mean += x
}
mean /= float64(len(xs))
for i := range xs {
xs[i] -= mean
}
}

func rmsOf(xs []float64) float64 {
sum := 0.0
for _, x := range xs {
sum += x * x
}
if len(xs) == 0 {
return 0
}
s := sum / float64(len(xs))
if s <= 0 {
return 0
}
z := s
for i := 0; i < 10; i++ {
z = (z + s/z) / 2
}
return z
}

func printWindow(
n int, t float64,
freqs, power []float64, peaks []int,
isAnomaly, baselineReady bool,
event *anomaly.Event,
) {
status := "  OK  "
switch {
case !baselineReady:
status = " WARM "
case isAnomaly:
status = " ⚠️ AN "
}

f1, f2 := 0.0, 0.0
p1, p2 := 0.0, 0.0
nonDC := 0
for _, idx := range peaks {
if freqs[idx] < 5.0 {
continue
}
if nonDC == 0 {
f1, p1 = freqs[idx], power[idx]
} else if nonDC == 1 {
f2, p2 = freqs[idx], power[idx]
}
nonDC++
if nonDC == 2 {
break
}
}

fmt.Printf("win %4d  t=%7.3fs  [%s]  fft: %6.1fHz(%.1e) %6.1fHz(%.1e)",
n, t, status, f1, p1, f2, p2)

if event != nil {
if event.Anomaly {
fmt.Printf("  ⚡ ANOMALY z=%+.1fσ rms=%.4f", event.ZScore, event.RMS)
} else {
fmt.Printf("  ✅ CLEAR  z=%+.1fσ rms=%.4f", event.ZScore, event.RMS)
}
}
fmt.Println()
}

func printStats(samples int64, windows, anomalyWins, raw, compressed int) {
sep := strings.Repeat("─", 54)
fmt.Println("\n" + sep)
fmt.Printf("  Total samples   : %d (%.1f s)\n", samples, float64(samples)/sampleRate)
fmt.Printf("  Windows         : %d × %d samples\n", windows, fftWindow)
if windows > 0 {
fmt.Printf("  Anomalous       : %d / %d (%.0f%%)\n",
anomalyWins, windows, 100*float64(anomalyWins)/float64(windows))
}
if raw > 0 {
fmt.Printf("  SDT compression : %d → %d pts  %.1f×\n",
raw, compressed, filter.CompressionRatio(raw, compressed))
}
fmt.Println(sep)
}
