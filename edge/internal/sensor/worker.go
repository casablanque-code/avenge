// Package sensor — worker.go
//
// Defines the multi-sensor orchestration layer:
//
//	SensorConfig  — per-sensor parameters (window size, thresholds, …)
//	SensorWorker  — stateful pipeline for one sensor (FFT+SDT+anomaly)
//	Manager       — registry + lifecycle for N workers
//
// Design goals:
//   - Each sensor has its own FFTProcessor, Detector, BandDetector, and
//     SDT accumulator. Workers share nothing (no locks in the hot path).
//   - Workers communicate results via typed channels — the Manager routes
//     events to a single outbound channel for MQTT publishing.
//   - Adding a sensor is one Manager.Add(cfg) call; removing it is
//     Manager.Remove(id). No restart of the process needed.
//
// Example:
//
//	mgr := sensor.NewManager(pub)
//	mgr.Add(sensor.DefaultSensorConfig("bearing_01"))
//	mgr.Add(sensor.SensorConfig{
//	    ID: "motor_shaft", SampleRate: 2000, FFTWindow: 1024,
//	    EnterThreshold: 4.0, ExitThreshold: 2.5, SDTEpsilon: 0.03,
//	})
//	for ev := range mgr.Events() { log.Println(ev) }
package sensor

import (
	"context"
	"fmt"
	"sync"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/anomaly"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/fft"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/filter"
)

// SensorConfig holds all tunable parameters for one sensor's pipeline.
// Each sensor can have different rates, window sizes, and thresholds —
// a motor shaft runs at different frequencies than a gearbox.
type SensorConfig struct {
	// ID is the unique sensor identifier (e.g. "bearing_01", "motor_shaft").
	ID string

	// SampleRate is the ADC sampling frequency in Hz.
	SampleRate int

	// FFTWindow is the number of samples per processing window.
	// Must be a power of two. Larger windows = finer frequency resolution
	// but higher latency.
	FFTWindow int

	// SDTFlushEvery is how many windows between SDT compressions and
	// telemetry publishes.
	SDTFlushEvery int

	// SDTEpsilon is the Swinging Door Trending deadband in sensor units (g).
	SDTEpsilon float64

	// Anomaly detector thresholds.
	EnterThreshold  float64
	ExitThreshold   float64
	BaselineWindows int
	MinStdDev       float64
}

// DefaultSensorConfig returns baseline parameters for a 1 kHz MPU-6050
// monitoring a 50 Hz shaft (e.g. industrial fan or pump bearing).
func DefaultSensorConfig(id string) SensorConfig {
	return SensorConfig{
		ID:              id,
		SampleRate:      1000,
		FFTWindow:       512,
		SDTFlushEvery:   20,
		SDTEpsilon:      0.05,
		EnterThreshold:  3.5,
		ExitThreshold:   2.5,
		BaselineWindows: 10,
		MinStdDev:       0.002,
	}
}

// WorkerEvent wraps an anomaly.Event with the sensor ID that produced it.
// Using a separate struct (rather than relying on Event.SensorID alone)
// keeps the routing logic explicit and avoids mutating shared Event pointers.
//
// Two kinds of events flow through this struct:
//   - Telemetry flush (IsTelemetryFlush=true): periodic SDT summary, ~every 10s.
//     Published to sm/telemetry/<id>. Event may be nil if no state change occurred.
//   - Anomaly transition (IsTelemetryFlush=false, Event!=nil): state change only.
//     Published to sm/anomaly/<id>.
type WorkerEvent struct {
	SensorID string
	Event    *anomaly.Event
	// WindowRMS is the RMS of the window that produced this event.
	WindowRMS float32
	// BandResult holds the per-band FFT analysis for this window.
	BandResult anomaly.BandResult

	// IsTelemetryFlush is true when this event carries a periodic SDT summary.
	// The publisher should send a telemetry message regardless of anomaly state.
	IsTelemetryFlush bool
	// SDTRatio is the compression ratio from the most recent SDT flush.
	// Only meaningful when IsTelemetryFlush is true.
	SDTRatio float32
	// IsAnomaly mirrors the current detector state at flush time.
	// Only meaningful when IsTelemetryFlush is true.
	IsAnomaly bool
	// Severity mirrors the current severity at flush time.
	Severity string
}

// SensorWorker runs the full processing pipeline for a single sensor.
// It reads Sample values from a channel, processes them window-by-window,
// and sends WorkerEvents to an output channel on state/severity changes.
type SensorWorker struct {
	cfg SensorConfig

	fftProc *fft.Processor
	det     *anomaly.Detector
	bandDet *anomaly.BandDetector
	sdt     []filter.Point

	ringBuf    []float64
	ringFilled int

	totalWindows    int
	totalRaw        int
	totalCompressed int
}

// newSensorWorker creates an initialised SensorWorker for the given config.
// All buffers are allocated here — the hot loop is allocation-free.
func newSensorWorker(cfg SensorConfig) *SensorWorker {
	detCfg := anomaly.Config{
		SampleRate:      cfg.SampleRate,
		WindowSize:      cfg.FFTWindow / 2, // use half the FFT window for RMS
		BaselineWindows: cfg.BaselineWindows,
		EnterThreshold:  cfg.EnterThreshold,
		ExitThreshold:   cfg.ExitThreshold,
		MinStdDev:       cfg.MinStdDev,
	}

	bandCfg := anomaly.DefaultBandConfig()
	bandCfg.WarmupWindows = cfg.BaselineWindows
	bandCfg.EnterThreshold = cfg.EnterThreshold + 0.5 // slightly stricter for bands
	bandCfg.ExitThreshold = cfg.ExitThreshold

	return &SensorWorker{
		cfg:     cfg,
		fftProc: fft.NewProcessor(cfg.FFTWindow, cfg.SampleRate),
		det:     anomaly.NewDetector(detCfg),
		bandDet: anomaly.NewBandDetector(
			anomaly.DefaultBands(cfg.SampleRate),
			bandCfg,
		),
		sdt:     make([]filter.Point, 0, cfg.FFTWindow*20),
		ringBuf: make([]float64, cfg.FFTWindow),
	}
}

// ingestSample adds one sample to the ring buffer.
// Returns a WorkerEvent if a full window was processed AND a state change
// occurred; returns nil otherwise.
func (w *SensorWorker) ingestSample(s Sample) *WorkerEvent {
	w.ringBuf[w.ringFilled] = s.Value
	w.sdt = append(w.sdt, filter.Point{T: s.T, Value: s.Value})
	w.ringFilled++

	if w.ringFilled < w.cfg.FFTWindow {
		return nil
	}

	// Full window — process it.
	w.totalWindows++
	w.ringFilled = 0

	windowT := s.T

	// FFT (zero-alloc via Processor, in-place DC removal).
	removeDCInPlace(w.ringBuf)
	freqs, power := w.fftProc.PowerSpectrum(w.ringBuf)
	bandResult := w.bandDet.IngestSpectrum(freqs, power)

	// Anomaly detection on original values (DC-included).
	origVals := make([]float64, w.cfg.FFTWindow)
	tail := w.sdt[len(w.sdt)-w.cfg.FFTWindow:]
	for i, p := range tail {
		origVals[i] = p.Value
	}
	windowStartT := windowT - float64(w.cfg.FFTWindow-1)/float64(w.cfg.SampleRate)
	event := w.det.IngestWindow(origVals, windowStartT)
	windowRMS := float32(rmsOf(origVals))

	// SDT flush — emit a telemetry event every SDTFlushEvery windows.
	// This is independent of anomaly state transitions: we always want
	// periodic data in ClickHouse so Grafana has something to draw.
	var flushEvent *WorkerEvent
	if w.totalWindows%w.cfg.SDTFlushEvery == 0 {
		compressed := filter.SDT(w.sdt, w.cfg.SDTEpsilon)
		ratio := float32(filter.CompressionRatio(len(w.sdt), len(compressed)))
		w.totalRaw += len(w.sdt)
		w.totalCompressed += len(compressed)
		w.sdt = w.sdt[:0]

		flushEvent = &WorkerEvent{
			SensorID:         w.cfg.ID,
			Event:            event, // may be nil — that's fine
			WindowRMS:        windowRMS,
			BandResult:       bandResult,
			IsTelemetryFlush: true,
			SDTRatio:         ratio,
			IsAnomaly:        w.det.State(),
			Severity:         string(w.det.Severity()),
		}
	}

	// Anomaly state transition — emit regardless of flush cycle.
	if event != nil {
		event.SensorID = w.cfg.ID
		if flushEvent != nil {
			// Flush event already carries this transition — no duplicate needed.
			return flushEvent
		}
		return &WorkerEvent{
			SensorID:   w.cfg.ID,
			Event:      event,
			WindowRMS:  windowRMS,
			BandResult: bandResult,
			IsAnomaly:  w.det.State(),
			Severity:   string(w.det.Severity()),
		}
	}

	return flushEvent // nil when no flush and no transition
}

// CompressionRatio returns the current SDT compression ratio.
func (w *SensorWorker) CompressionRatio() float64 {
	return filter.CompressionRatio(w.totalRaw, w.totalCompressed)
}

// ── Manager ────────────────────────────────────────────────────────────────

// Manager maintains a registry of SensorWorkers and routes their events
// to a shared output channel.
type Manager struct {
	mu      sync.RWMutex
	workers map[string]*workerHandle
	events  chan WorkerEvent
	wg      sync.WaitGroup
}

type workerHandle struct {
	worker *SensorWorker
	input  chan Sample
	cancel context.CancelFunc
}

// NewManager creates a Manager with an output event buffer of bufSize.
// A bufSize of 256 is sufficient for PoC; increase for high-sensor-count
// deployments where event processing may lag behind ingestion.
func NewManager(bufSize int) *Manager {
	return &Manager{
		workers: make(map[string]*workerHandle),
		events:  make(chan WorkerEvent, bufSize),
	}
}

// Add registers a new sensor and starts its processing goroutine.
// If a sensor with the same ID already exists, it is removed first.
func (m *Manager) Add(cfg SensorConfig) error {
	if cfg.ID == "" {
		return fmt.Errorf("sensor: SensorConfig.ID must not be empty")
	}
	m.Remove(cfg.ID) // idempotent — stop existing worker if any

	ctx, cancel := context.WithCancel(context.Background())
	h := &workerHandle{
		worker: newSensorWorker(cfg),
		input:  make(chan Sample, cfg.FFTWindow*2), // buffer ~2 windows
		cancel: cancel,
	}

	m.mu.Lock()
	m.workers[cfg.ID] = h
	m.mu.Unlock()

	m.wg.Add(1)
	go m.runWorker(ctx, h)
	return nil
}

// Remove stops and removes the sensor with the given ID.
// Safe to call on a non-existent ID (no-op).
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	h, ok := m.workers[id]
	if ok {
		delete(m.workers, id)
	}
	m.mu.Unlock()

	if ok {
		h.cancel()
	}
}

// Send delivers a sample to the sensor with the given ID.
// If the sensor's input buffer is full, the sample is dropped and the
// drop is logged — this is the correct backpressure behaviour for a
// real-time pipeline (dropping old data > blocking the producer).
func (m *Manager) Send(id string, s Sample) {
	m.mu.RLock()
	h, ok := m.workers[id]
	m.mu.RUnlock()
	if !ok {
		return
	}

	select {
	case h.input <- s:
	default:
		// Input buffer full — drop sample and log.
		// In production this would increment a Prometheus counter.
		fmt.Printf("[manager] DROP sample for sensor %s — input buffer full\n", id)
	}
}

// Events returns the read-only channel of WorkerEvents.
// The caller should range over this channel in a dedicated goroutine.
func (m *Manager) Events() <-chan WorkerEvent {
	return m.events
}

// Sensors returns the IDs of all currently registered sensors.
func (m *Manager) Sensors() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.workers))
	for id := range m.workers {
		ids = append(ids, id)
	}
	return ids
}

// Stop removes all sensors and closes the events channel.
// Call once, at shutdown.
//
// Waits for every runWorker goroutine to actually exit before closing
// m.events — cancelling a context only signals a goroutine to stop, it
// doesn't guarantee it has stopped. Without this wait, a worker still
// mid-way through `m.events <- *ev` could race with close(m.events) and
// panic with "send on closed channel". Confirmed via -race in
// manager_race2_test.go before this fix.
func (m *Manager) Stop() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.workers))
	for id := range m.workers {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.Remove(id)
	}
	m.wg.Wait()
	close(m.events)
}

// runWorker is the goroutine body for one SensorWorker.
// It reads from h.input and routes WorkerEvents to m.events.
func (m *Manager) runWorker(ctx context.Context, h *workerHandle) {
	defer m.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case s, ok := <-h.input:
			if !ok {
				return
			}
			ev := h.worker.ingestSample(s)
			if ev == nil {
				continue
			}
			select {
			case m.events <- *ev:
			default:
				// Output buffer full — drop event and log.
				fmt.Printf("[manager] DROP event from sensor %s — output buffer full\n",
					ev.SensorID)
			}
		}
	}
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
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x * x
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
