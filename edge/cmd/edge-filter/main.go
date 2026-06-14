// edge-filter: streaming pipeline with MQTT output.
//
// Data flow (per window of fftWindow samples):
//
//	stdin (JSONL) ──► sensor.Reader ──► ring buffer
//	                                         │
//	                                [window full: 512 samples]
//	                                         │
//	                    ┌────────────────────┼────────────────────┐
//	                    ▼                    ▼                    ▼
//	             FFTProcessor           Detector               SDT
//	           (zero-alloc)        (RMS + Z-score)       (compression)
//	                    │                    │                    │
//	                    └────────────────────┴────────────────────┘
//	                                         │
//	                                   Publisher
//	                                         │
//	                          ┌──────────────┴──────────────┐
//	                          ▼                             ▼
//	              sm/telemetry/<id>              sm/anomaly/<id>
//	              (SDT points, ~10s)             (state transitions)
//	                          │
//	                       Mosquitto ──► Telegraf ──► ClickHouse ──► Grafana
//
// Flags:
//
//	--broker      MQTT broker address (default "localhost:1883")
//	--sensor-id   sensor identifier (default "bearing_01")
//	--no-mqtt     disable MQTT, print to stdout only (default when broker unreachable)
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

	// ── MQTT connection (optional — pipeline runs without it) ─────────────
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

	// ── One-time allocations (zero heap allocs in hot loop after this) ────
	ringBuf    := make([]float64, fftWindow)
	ringFilled := 0
	rawPoints  := make([]filter.Point, 0, fftWindow*20) // SDT accumulator

	fftProc := fft.NewProcessor(fftWindow, sampleRate)

	detCfg := anomaly.DefaultConfig()
	detCfg.SampleRate      = sampleRate
	detCfg.BaselineWindows = 10
	det := anomaly.NewDetector(detCfg)

	// Band detector runs in parallel with the scalar detector.
	// It is more sensitive to narrow-band faults (e.g. BPFO at 312.5 Hz)
	// and fires independently — either detector can raise an alert.
	bandDet := anomaly.NewBandDetector(
		anomaly.DefaultBands(sampleRate),
		anomaly.DefaultBandConfig(),
	)

	reader := sensor.NewReader(os.Stdin)

	// ── Stats ─────────────────────────────────────────────────────────────
	var (
		totalSamples   int64
		totalWindows   int
		anomalyWindows int
		totalRaw       int
		totalCompressed int
		lastSDTRatio   float32 = 1.0
	)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		printStats(totalSamples, totalWindows, anomalyWindows, totalRaw, totalCompressed)
		os.Exit(0)
	}()

	// ── Hot loop ──────────────────────────────────────────────────────────
	const sdtFlushEvery = 20 // windows ≈ 10 s at 1 kHz / 512 window

	for {
		s, err := reader.Next()
		if err != nil {
			break // EOF
		}

		ringBuf[ringFilled] = s.Value
		rawPoints = append(rawPoints, filter.Point{T: s.T, Value: s.Value})
		ringFilled++
		totalSamples++

		if ringFilled < fftWindow {
			continue
		}

		// ── Process window ────────────────────────────────────────────────
		totalWindows++
		windowT := s.T

		// Stage A: FFT (zero-alloc via Processor).
		removeDCInPlace(ringBuf)
		freqs, power := fftProc.PowerSpectrum(ringBuf)
		peaks := fft.TopPeaks(power, topFreqs)

		// Stage C: Anomaly detection on original (DC-included) values.
		tail := rawPoints[len(rawPoints)-fftWindow:]
		origVals := make([]float64, fftWindow)
		for i, p := range tail {
			origVals[i] = p.Value
		}
		windowStartT := windowT - float64(fftWindow-1)/sampleRate

		// Scalar detector (RMS + Z-score).
		event := det.IngestWindow(origVals, windowStartT)
		isAnomaly := det.State()
		severity := string(det.Severity())
		if isAnomaly {
			anomalyWindows++
		}
		windowRMS := float32(rmsOf(origVals))

		// Band detector (per-band FFT RMS + Z-score).
		// Runs on the DC-removed power spectrum already computed for display.
		// More sensitive to narrow-band faults than the scalar detector.
		bandResult := bandDet.IngestSpectrum(freqs, power)

		// Stage B: SDT flush every sdtFlushEvery windows.
		if totalWindows%sdtFlushEvery == 0 {
			compressed := filter.SDT(rawPoints, sdtEpsilon)
			lastSDTRatio = float32(filter.CompressionRatio(len(rawPoints), len(compressed)))
			totalRaw += len(rawPoints)
			totalCompressed += len(compressed)

			// Publish ONE aggregated telemetry message per flush.
			//
			// Rationale: SDT can produce thousands of turning points during
			// sustained anomalies (low compression ratio). Publishing each
			// point as a separate QoS1 MQTT message overwhelms the broker
			// connection — the client has no reconnect logic, so a single
			// dropped connection then causes every subsequent publish to
			// fail with "broken pipe" for the rest of the run.
			//
			// The sm/telemetry/<id> topic is a periodic summary channel
			// (~every 10s), not a per-sample firehose — raw points stay
			// local on the edge device (e.g. written to disk for offline
			// analysis) and only the summary goes upstream.
			if pub != nil {
				lastVal := float32(0)
				if len(compressed) > 0 {
					lastVal = float32(compressed[len(compressed)-1].Value)
				}
				if err := pub.PublishTelemetry(
					time.Now().UnixMilli(),
					lastVal,
					windowRMS,
					lastSDTRatio,
					isAnomaly,
					severity,
				); err != nil {
					fmt.Fprintf(os.Stderr, "publish telemetry: %v\n", err)
				}
			}
			rawPoints = rawPoints[:0]
			fmt.Fprintf(os.Stderr, "  → SDT flush @win%-4d  ratio=%.1f×  mqtt=%v\n",
				totalWindows, lastSDTRatio, pub != nil)
		}

		// Publish anomaly event on state transition.
		if event != nil && pub != nil {
			eventType := "clear"
			if event.Anomaly {
				eventType = "onset"
			}
			if err := pub.PublishAnomaly(
				time.Now(),
				eventType,
				string(event.Severity),
				float32(event.RMS),
				float32(event.ZScore),
				float32(event.BaselineMean),
				float32(event.BaselineStdDev),
			); err != nil {
				fmt.Fprintf(os.Stderr, "publish anomaly: %v\n", err)
			}
		}

		// ── Console output ────────────────────────────────────────────────
		printWindow(totalWindows, windowT, freqs, power, peaks,
			det.Severity(), det.BaselineReady(), event)

		// Print band breakdown when fault band fires or periodically for debug.
		if bandResult.FaultBandAnomaly {
			fmt.Printf("         [band] ⚡ fault-band ANOMALY  maxZ=%.1fσ  band=%s  severity=%s\n",
				bandResult.MaxZScore, bandResult.MaxZBand, bandResult.OverallSeverity)
		} else if bandDet.Ready() && bandResult.MaxZScore > 2.0 {
			// Print band warning even before crossing EnterThreshold —
			// useful to observe fault energy rising before scalar detector fires.
			fmt.Printf("         [band]    fault-band rising   maxZ=%.1fσ  band=%s\n",
				bandResult.MaxZScore, bandResult.MaxZBand)
		}

		ringFilled = 0
	}

	// Final SDT flush for batch mode.
	if len(rawPoints) > 0 {
		compressed := filter.SDT(rawPoints, sdtEpsilon)
		lastSDTRatio = float32(filter.CompressionRatio(len(rawPoints), len(compressed)))
		totalRaw += len(rawPoints)
		totalCompressed += len(compressed)
	}

	printStats(totalSamples, totalWindows, anomalyWindows, totalRaw, totalCompressed)
}

// ── Helpers ────────────────────────────────────────────────────────────────

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
	if s < 0 {
		return 0
	}
	// manual sqrt to keep math import out
	// use Newton's method — fast enough for a single value
	if s == 0 {
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
	severity anomaly.Severity, baselineReady bool,
	event *anomaly.Event,
) {
	status := "  OK  "
	switch {
	case !baselineReady:
		status = " WARM "
	case severity == anomaly.SeverityCritical:
		status = " CRIT "
	case severity == anomaly.SeverityWarning:
		status = " WARN "
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
			fmt.Printf("  ⚡ ANOMALY [%s] z=%+.1fσ rms=%.4f", event.Severity, event.ZScore, event.RMS)
		} else {
			fmt.Printf("  ✅ CLEAR  [%s] z=%+.1fσ rms=%.4f", event.Severity, event.ZScore, event.RMS)
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
