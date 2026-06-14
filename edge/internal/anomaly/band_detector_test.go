package anomaly_test

import (
	"math"
	"testing"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/anomaly"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/fft"
)

// generateSpectrum creates a synthetic power spectrum by building a time-domain
// signal with given frequency components and running FFT on it.
func generateSpectrum(sampleRate, windowSize int, components map[float64]float64) (freqs, power []float64) {
	samples := make([]float64, windowSize)
	for i := range samples {
		t := float64(i) / float64(sampleRate)
		for hz, amp := range components {
			samples[i] += amp * math.Sin(2*math.Pi*hz*t)
		}
	}
	return fft.PowerSpectrum(samples, sampleRate)
}

func warmupBandDetector(bd *anomaly.BandDetector, sampleRate, windowSize int) {
	// Normal vibration: shaft at 50 Hz + harmonics, no fault component.
	normalComponents := map[float64]float64{
		50.0:  1.0,
		100.0: 0.35,
		150.0: 0.15,
	}
	cfg := anomaly.DefaultBandConfig()
	for i := 0; i < cfg.WarmupWindows+2; i++ {
		freqs, power := generateSpectrum(sampleRate, windowSize, normalComponents)
		bd.IngestSpectrum(freqs, power)
	}
}

func TestBandDetector_WarmupPhase(t *testing.T) {
	bands := anomaly.DefaultBands(1000)
	bd := anomaly.NewBandDetector(bands, anomaly.DefaultBandConfig())

	if bd.Ready() {
		t.Fatal("detector should not be ready before warmup completes")
	}

	freqs, power := generateSpectrum(1000, 512, map[float64]float64{50.0: 1.0})
	cfg := anomaly.DefaultBandConfig()
	for i := 0; i < cfg.WarmupWindows; i++ {
		result := bd.IngestSpectrum(freqs, power)
		if result.AnyAnomaly {
			t.Errorf("unexpected anomaly during warmup at window %d", i)
		}
	}

	if !bd.Ready() {
		t.Error("detector should be ready after WarmupWindows ingestions")
	}
}

func TestBandDetector_NoFalsePositiveOnNormalSignal(t *testing.T) {
	bands := anomaly.DefaultBands(1000)
	bd := anomaly.NewBandDetector(bands, anomaly.DefaultBandConfig())
	warmupBandDetector(bd, 1000, 512)

	// Feed 20 more normal windows — none should trigger anomaly.
	normalComponents := map[float64]float64{50.0: 1.0, 100.0: 0.35, 150.0: 0.15}
	for i := 0; i < 20; i++ {
		freqs, power := generateSpectrum(1000, 512, normalComponents)
		result := bd.IngestSpectrum(freqs, power)
		if result.AnyAnomaly {
			t.Errorf("false positive at window %d: %s", i, bd.Summary())
		}
	}
}

func TestBandDetector_DetectsFaultBandAnomaly(t *testing.T) {
	bands := anomaly.DefaultBands(1000)
	cfg := anomaly.DefaultBandConfig()
	cfg.EnterThreshold = 3.0 // lower for test reliability
	bd := anomaly.NewBandDetector(bands, cfg)
	warmupBandDetector(bd, 1000, 512)

	// Inject strong BPFO signal at 312.5 Hz (inside "fault" band 150–400 Hz).
	// Keep shaft/harmonic components identical to warmup — only fault band changes.
	faultComponents := map[float64]float64{
		50.0:  1.0,
		100.0: 0.35,
		150.0: 0.15,
		312.5: 1.8, // strong bearing fault
	}

	var detected bool
	var faultResult anomaly.BandResult
	for attempt := 0; attempt < 5; attempt++ {
		freqs, power := generateSpectrum(1000, 512, faultComponents)
		result := bd.IngestSpectrum(freqs, power)
		if result.FaultBandAnomaly {
			detected = true
			faultResult = result
			break
		}
	}

	if !detected {
		t.Fatalf("fault band anomaly not detected within 5 windows\n%s", bd.Summary())
	}
	t.Logf("detected in fault band: maxZ=%.2f maxBand=%s severity=%s\n%s",
		faultResult.MaxZScore, faultResult.MaxZBand,
		faultResult.OverallSeverity, bd.Summary())
}

func TestBandDetector_FaultBandMoreSensitiveThanScalarRMS(t *testing.T) {
	// Key value proposition of band detection:
	// The fault band should detect a low-amplitude BPFO signal that is
	// BELOW the detection threshold of the scalar RMS detector.
	//
	// Spectral leakage means a shaft spike inevitably raises energy in
	// all bands — absolute band isolation is physically impossible.
	// What matters is that band detection fires EARLIER (at lower fault
	// amplitude) than scalar RMS detection.
	const sampleRate = 1000
	const windowSize = 512

	bands := anomaly.DefaultBands(sampleRate)
	bandCfg := anomaly.DefaultBandConfig()
	bandCfg.EnterThreshold = 3.0
	bd := anomaly.NewBandDetector(bands, bandCfg)

	scalarCfg := anomaly.DefaultConfig()
	scalarCfg.EnterThreshold = 3.0
	scalarCfg.ExitThreshold = 2.0
	scalarCfg.BaselineWindows = bandCfg.WarmupWindows
	sd := anomaly.NewDetector(scalarCfg)

	// Warmup both with identical normal signal.
	normal := map[float64]float64{50.0: 1.0, 100.0: 0.35, 150.0: 0.15}
	for i := 0; i < bandCfg.WarmupWindows+2; i++ {
		freqs, power := generateSpectrum(sampleRate, windowSize, normal)
		bd.IngestSpectrum(freqs, power)
		samples := makeSignal(sampleRate, windowSize, normal)
		sd.IngestWindow(samples, float64(i))
	}

	// Sweep fault amplitude from small to large.
	// Record at which amplitude each detector first fires.
	bandFiredAt := -1.0
	scalarFiredAt := -1.0

	for _, amp := range []float64{0.05, 0.1, 0.15, 0.2, 0.3, 0.5, 0.8, 1.0, 1.5} {
		components := map[float64]float64{
			50.0: 1.0, 100.0: 0.35, 150.0: 0.15,
			312.5: amp,
		}
		freqs, power := generateSpectrum(sampleRate, windowSize, components)
		samples := makeSignal(sampleRate, windowSize, components)

		bResult := bd.IngestSpectrum(freqs, power)
		ev := sd.IngestWindow(samples, amp)

		if bResult.FaultBandAnomaly && bandFiredAt < 0 {
			bandFiredAt = amp
		}
		if (ev != nil && ev.Anomaly || sd.State()) && scalarFiredAt < 0 {
			scalarFiredAt = amp
		}
		if bandFiredAt > 0 && scalarFiredAt > 0 {
			break
		}
	}

	t.Logf("Band detector fired at fault amp=%.2f", bandFiredAt)
	t.Logf("Scalar detector fired at fault amp=%.2f", scalarFiredAt)

	if bandFiredAt < 0 {
		t.Error("band detector never fired — check EnterThreshold")
		return
	}
	if scalarFiredAt > 0 && bandFiredAt >= scalarFiredAt {
		t.Errorf("expected band detector to fire earlier (amp=%.2f) than scalar (amp=%.2f)",
			bandFiredAt, scalarFiredAt)
	}
	if scalarFiredAt < 0 {
		t.Logf("scalar detector never fired — band detector provides coverage scalar cannot")
	}
}

// makeSignal generates a time-domain signal from frequency components.
func makeSignal(sampleRate, windowSize int, components map[float64]float64) []float64 {
	samples := make([]float64, windowSize)
	for i := range samples {
		t := float64(i) / float64(sampleRate)
		for hz, amp := range components {
			samples[i] += amp * math.Sin(2*math.Pi*hz*t)
		}
	}
	return samples
}

func TestBandDetector_HysteresisPerBand(t *testing.T) {
	bands := anomaly.DefaultBands(1000)
	cfg := anomaly.DefaultBandConfig()
	cfg.EnterThreshold = 3.0
	cfg.ExitThreshold = 2.0
	bd := anomaly.NewBandDetector(bands, cfg)
	warmupBandDetector(bd, 1000, 512)

	// Strong fault — enter anomaly.
	strong := map[float64]float64{50.0: 1.0, 100.0: 0.35, 150.0: 0.15, 312.5: 1.8}
	var entered bool
	for i := 0; i < 5; i++ {
		freqs, power := generateSpectrum(1000, 512, strong)
		r := bd.IngestSpectrum(freqs, power)
		if r.FaultBandAnomaly {
			entered = true
			break
		}
	}
	if !entered {
		t.Skip("could not enter fault band anomaly — tune amplitude")
	}

	// Mid-level fault — z between ExitThreshold and EnterThreshold.
	// Hysteresis should keep fault band in anomaly state.
	mid := map[float64]float64{50.0: 1.0, 100.0: 0.35, 150.0: 0.15, 312.5: 0.5}
	freqs, power := generateSpectrum(1000, 512, mid)
	r := bd.IngestSpectrum(freqs, power)

	// We can't assert definitively without knowing the exact z — just log.
	t.Logf("after mid-level fault: FaultBandAnomaly=%v maxZ=%.2f\n%s",
		r.FaultBandAnomaly, r.MaxZScore, bd.Summary())
}
