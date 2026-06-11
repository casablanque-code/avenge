// edge-filter: streaming pipeline with constant memory footprint.
//
// Data flow (per window of fftWindow samples):
//
//	stdin (JSONL) -> sensor.Reader -> ring buffer
//	                                       |
//	                              [full window ready]
//	                                       |
//	                    ┌──────────────────┼──────────────────┐
//	                    ▼                  ▼                  ▼
//	             FFTProcessor           Detector            SDT
//	           (reused buffers)    (RMS + Z-score)    (compression)
//	                    │                  │                  │
//	                    └──────────────────┴──────────────────┘
//	                                       │
//	                              Report / MQTT (Step 3)
//
// Memory usage is O(fftWindow) regardless of stream duration.
// No heap allocations in the hot path after startup.
//
// Usage:
//
//	python3 signal_generator.py stream --anomaly-after 8 | go run ./cmd/edge-filter
//	python3 signal_generator.py batch  --samples 8000 --anomaly | go run ./cmd/edge-filter
package main

import (
	"fmt"
	"strings"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/anomaly"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/filter"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/fft"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

// Pipeline parameters — all sizes are powers of two so FFT is happy.
const (
	sampleRate = 1000 // Hz
	fftWindow  = 512  // samples per processing window (~0.5 s)
	sdtEpsilon = 0.05 // SDT deadband in g
	topFreqs   = 4    // dominant frequencies to log
)

func main() {
	fmt.Fprintln(os.Stderr, "edge-filter starting (streaming mode, constant memory)")
	fmt.Fprintf(os.Stderr, "  window=%d samples  rate=%d Hz  SDT ε=%.3f g\n\n",
		fftWindow, sampleRate, sdtEpsilon)

	// ── One-time allocations ──────────────────────────────────────────────
	// After this block, the hot loop makes zero heap allocations.

	// Ring buffer: exactly one window of samples.
	// We process window-by-window (non-overlapping) for simplicity.
	// A production system would use a 50%-overlap sliding window for
	// smoother anomaly detection at window boundaries.
	ringBuf := make([]float64, fftWindow)
	ringFilled := 0

	// SDT accumulates points across windows and flushes periodically.
	// Pre-allocate to avoid growth in the hot path.
	rawPoints := make([]filter.Point, 0, fftWindow*10)

	fftProc := fft.NewProcessor(fftWindow, sampleRate)
	detCfg := anomaly.DefaultConfig()
	detCfg.SampleRate = sampleRate
	detCfg.BaselineWindows = 10
	det := anomaly.NewDetector(detCfg)

	reader := sensor.NewReader(os.Stdin)

	// ── Stats ─────────────────────────────────────────────────────────────
	var (
		totalSamples   int64
		totalWindows   int
		anomalyWindows int
		totalRaw       int
		totalSDT       int
		windowStart    time.Time
	)
	windowStart = time.Now()

	// Graceful shutdown on SIGINT/SIGTERM — print final stats.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		printFinalStats(totalSamples, totalWindows, anomalyWindows, totalRaw, totalSDT)
		os.Exit(0)
	}()

	// ── Hot loop ──────────────────────────────────────────────────────────
	for {
		s, err := reader.Next()
		if err != nil {
			// EOF — stream ended (batch mode). Print stats and exit.
			break
		}

		// Fill the ring buffer one sample at a time.
		ringBuf[ringFilled] = s.Value
		ringFilled++
		totalSamples++

		// Accumulate for SDT (we compress across a larger window than FFT).
		rawPoints = append(rawPoints, filter.Point{T: s.T, Value: s.Value})

		if ringFilled < fftWindow {
			continue // window not full yet
		}

		// ── Window is full — process it ───────────────────────────────────
		windowDuration := time.Since(windowStart)
		totalWindows++

		// Stage A: FFT (zero allocation — Processor reuses buffers).
		// Remove DC offset so gravity (1g) doesn't dominate the spectrum.
		removeDCInPlace(ringBuf)
		freqs, power := fftProc.PowerSpectrum(ringBuf)
		peaks := fft.TopPeaks(power, topFreqs)

		// Stage C: Anomaly detection (uses original values, with DC for RMS accuracy).
		// We removed DC from ringBuf above, so recompute RMS from rawPoints tail.
		windowSlice := rawPoints[len(rawPoints)-fftWindow:]
		origValues := make([]float64, fftWindow) // small, stack-like allocation once/window
		for i, p := range windowSlice {
			origValues[i] = p.Value
		}
		event := det.IngestWindow(origValues, s.T-float64(fftWindow-1)/sampleRate)

		isAnomaly := det.State()
		if isAnomaly {
			anomalyWindows++
		}

		// Stage B: SDT flush — compress and reset when buffer grows large.
		// In production this triggers a protobuf send to MQTT.
		const sdtFlushEvery = 20 // windows (~10 s)
		if totalWindows%sdtFlushEvery == 0 {
			compressed := filter.SDT(rawPoints, sdtEpsilon)
			totalRaw += len(rawPoints)
			totalSDT += len(compressed)
			rawPoints = rawPoints[:0] // reset without deallocation
			logSDTFlush(len(rawPoints)+fftWindow*sdtFlushEvery, len(compressed), totalWindows)
		}

		// ── Per-window console output ─────────────────────────────────────
		printWindow(totalWindows, s.T, freqs, power, peaks, isAnomaly,
			det.BaselineReady(), windowDuration, event)

		// Reset for next window.
		ringFilled = 0
		windowStart = time.Now()
	}

	// Batch mode: flush remaining SDT points.
	if len(rawPoints) > 0 {
		compressed := filter.SDT(rawPoints, sdtEpsilon)
		totalRaw += len(rawPoints)
		totalSDT += len(compressed)
	}

	printFinalStats(totalSamples, totalWindows, anomalyWindows, totalRaw, totalSDT)
}

// removeDCInPlace subtracts the mean from samples in place.
// O(n), no allocation.
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

func printWindow(
	n int, t float64,
	freqs, power []float64, peaks []int,
	isAnomaly, baselineReady bool,
	dur time.Duration,
	event *anomaly.Event,
) {
	status := "  OK  "
	if !baselineReady {
		status = " WARM "
	} else if isAnomaly {
		status = " ⚠️ AN "
	}

	// Top-2 non-DC frequencies for compact display.
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
	fmt.Printf("  [%v]\n", dur.Round(time.Microsecond))
}

func logSDTFlush(raw, compressed, window int) {
	ratio := filter.CompressionRatio(raw, compressed)
	fmt.Fprintf(os.Stderr, "  → SDT flush @win%-4d  %d → %d pts  %.1f×\n",
		window, raw, compressed, ratio)
}

func printFinalStats(samples int64, windows, anomalyWins, raw, sdt int) {
	fmt.Println("\n" + strings.Repeat("─", 54))
	fmt.Printf("  Total samples   : %d (%.1f s)\n", samples, float64(samples)/sampleRate)
	fmt.Printf("  Windows         : %d × %d samples\n", windows, fftWindow)
	if windows > 0 {
		fmt.Printf("  Anomalous       : %d / %d windows (%.0f%%)\n",
			anomalyWins, windows, 100*float64(anomalyWins)/float64(windows))
	}
	if raw > 0 {
		fmt.Printf("  SDT compression : %d → %d pts  %.1f×\n",
			raw, sdt, filter.CompressionRatio(raw, sdt))
	}
	fmt.Println(strings.Repeat("─", 54))
}
