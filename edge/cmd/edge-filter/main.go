// edge-filter: streaming pipeline with MQTT output.
//
// Data flow (per window of fftWindow samples):
//
//	stdin (JSONL) ──► sensor.Reader ──► sensor.Manager ──► SensorWorker (per sensor)
//	                                                               │
//	                                          ┌────────────────────┼────────────────────┐
//	                                          ▼                    ▼                    ▼
//	                                   FFTProcessor           Detector               SDT
//	                                 (zero-alloc)        (RMS + Z-score)       (compression)
//	                                          │                    │                    │
//	                                          └────────────────────┴────────────────────┘
//	                                                               │
//	                                                     WorkerEvent channel
//	                                                               │
//	                                                         Publisher
//	                                                               │
//	                                          ┌──────────────────┴──────────────────┐
//	                                          ▼                                     ▼
//	                              sm/telemetry/<id>                      sm/anomaly/<id>
//	                              (SDT points, ~10s)                     (state transitions)
//	                                          │
//	                                   Mosquitto ──► Telegraf ──► ClickHouse ──► Grafana
//
// Flags:
//
// Run `edge-filter --help` for the flags and settings (see help.go).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/capture"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/envfile"
	mqttclient "github.com/casablanque-code/smart-manufacturing/edge/internal/mqtt"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/profile"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

func main() {
	// Load .env before flags are parsed, so the broker default below can
	// read from it. Tries both common working directories so the binary
	// behaves the same whether run from the repo root (as README shows:
	// ./edge/edge-filter) or from inside edge/ directly.
	// Real shell exports and Docker `environment:` always win — see
	// envfile.Load doc comment.
	envfile.Load(".env")
	envfile.Load("edge/.env")

	opts := defineFlags(flag.CommandLine)
	broker, sensorID, noMQTT, explain := opts.broker, opts.sensorID, opts.noMQTT, opts.explain
	flag.Usage = func() { fmt.Fprint(os.Stderr, usageText()) }
	flag.Parse()
	if *opts.version {
		fmt.Println(versionLine())
		return
	}

	if !*explain && !*opts.listAudio && !*opts.report {
		fmt.Fprintf(os.Stderr, "edge-filter  sensor=%s  broker=%s\n", *sensorID, *broker)
	}

	// ── Sensor configuration ─────────────────────────────────────────────
	// SENSOR_SAMPLE_RATE must match the sensor's actual output data rate
	// exactly (the reference setup runs an ADXL345, which is hardware-capped
	// at 3200 Hz, at 1600 Hz; this repository contains no sensor firmware).
	// This single value feeds both the FFT/anomaly pipeline (via
	// sensorCfg.SampleRate) and SerialReader's timestamp reconstruction —
	// previously these could silently diverge because SerialReader was
	// wired to DefaultSensorConfig's hardcoded 1000 Hz regardless of what
	// SENSOR_SAMPLE_RATE actually said, which would desync every Sample.T
	// from real elapsed time and shift every computed FFT frequency.
	//
	// SENSOR_PROFILE (vibration, audio) picks every other tunable at once:
	// FFT window, frequency bands, thresholds, time constants. Unset means
	// "vibration", which equals the historic built-in defaults.
	settings := profile.Settings{
		Profile:  os.Getenv("SENSOR_PROFILE"),
		SensorID: *sensorID,
		Input:    os.Getenv("SENSOR_INPUT"),
	}
	if r := os.Getenv("SENSOR_SAMPLE_RATE"); r != "" {
		if parsed, err := strconv.Atoi(r); err == nil && parsed > 0 {
			settings.SampleRate = parsed
		} else {
			fmt.Fprintf(os.Stderr, "warning: invalid SENSOR_SAMPLE_RATE=%q, using the profile default\n", r)
		}
	}
	if *opts.listAudio {
		os.Exit(listAudio())
	}
	source := os.Getenv("SENSOR_SOURCE")
	if err := applySource(&settings, source, os.Getenv("SERIAL_INTERFACE")); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(2)
	}
	resolved, err := profile.Resolve(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(2)
	}
	sensorCfg := resolved.Sensor
	// SENSOR_SOURCE=mic: the exact capture command is part of the
	// configuration, so --explain shows it too.
	var micName string
	var micArgs []string
	if source == "mic" {
		micName, micArgs, err = capture.Command(runtime.GOOS, os.Getenv("AUDIO_DEVICE"), sensorCfg.SampleRate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "config: %v\n", err)
			os.Exit(2)
		}
	}
	if err := checkInputFlags(*opts.input, *opts.record, source, os.Getenv("SERIAL_INTERFACE"), resolved.Input); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(2)
	}
	if *opts.report {
		in := describeInput(source, os.Getenv("SERIAL_INTERFACE"), *opts.input, resolved.Input, capture.Describe(micName, micArgs))
		fmt.Print(formatReport(time.Now(), in, *opts.record, resolved))
		return
	}
	if *explain {
		fmt.Print(resolved.Explain())
		if micName != "" {
			fmt.Printf("Microphone capture command:\n  %s\n", capture.Describe(micName, micArgs))
		}
		return
	}
	fmt.Fprintf(os.Stderr, "profile: %s (%d Hz, window %d samples)\n",
		resolved.Profile.Name, sensorCfg.SampleRate, sensorCfg.FFTWindow)
	for _, w := range resolved.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	// ── Sample source ──────────────────────────────────────────────────
	// SERIAL_INTERFACE env selects the input:
	//   unset / ""  → stdin (JSONL from firmware-sim, for local dev)
	//   "usb"       → USB serial port (Arduino, CH340 dongle)
	//   "rs485"     → RS-485 via USB dongle or HAT
	//
	// USB and RS-485 use the same binary packet protocol and SerialReader —
	// only the device path and baud rate differ.
	var src sensor.SampleSource
	var micProc *capture.Process
	var rec *recorder
	// stdin is os.Stdin, or the file named by --input.
	var stdin io.Reader = os.Stdin
	inputName := "stdin"
	if *opts.input != "" {
		f, err := os.Open(*opts.input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "input: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		stdin, inputName = f, "file "+*opts.input
	}
	// record wraps a raw PCM reader so that it is also saved, when asked.
	record := func(in io.Reader) io.Reader {
		if *opts.record == "" {
			return in
		}
		r, rc, err := startRecording(*opts.record, in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "record: %v\n", err)
			os.Exit(1)
		}
		rec = rc
		fmt.Fprint(os.Stderr, box(
			"RECORDING raw sound to "+*opts.record,
			fmt.Sprintf("16-bit mono, %d Hz. The file stays on this machine; nothing sends it anywhere.", sensorCfg.SampleRate),
			"Replay it with: edge-filter --input "+*opts.record,
		))
		return r
	}
	replay := false // true for stdin (JSONL) input: block instead of dropping
	switch iface := os.Getenv("SERIAL_INTERFACE"); iface {
	case "usb", "rs485":
		if err := requireSerialRate(settings); err != nil {
			fmt.Fprintf(os.Stderr, "config: %v\n", err)
			os.Exit(2)
		}
		port := os.Getenv("USB_PORT")
		baud := 230400
		if iface == "rs485" {
			port = os.Getenv("RS485_PORT")
			if b := os.Getenv("RS485_BAUD"); b != "" {
				if parsed, err := strconv.Atoi(b); err == nil {
					baud = parsed
				}
			}
		}
		if port == "" {
			port = "/dev/ttyUSB0"
		}
		sr, err := sensor.OpenSerialReader(port, baud, sensorCfg.SampleRate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serial: %v\n", err)
			os.Exit(1)
		}
		defer sr.Close()
		src = sr
		fmt.Fprintf(os.Stderr, "serial source: iface=%s port=%s baud=%d sampleRate=%dHz\n",
			iface, port, baud, sensorCfg.SampleRate)
	default:
		// SENSOR_INPUT (or the profile default) selects the stdin format.
		if source == "mic" {
			proc, err := capture.Start(micName, micArgs)
			if errors.Is(err, exec.ErrNotFound) {
				fmt.Fprintf(os.Stderr, "capture: %s not found: %s\n", micName, capture.InstallHint(runtime.GOOS))
				os.Exit(1)
			} else if err != nil {
				fmt.Fprintf(os.Stderr, "capture: %v\n", err)
				os.Exit(1)
			}
			micProc = proc
			defer proc.Kill()
			src = sensor.NewPCMReader(record(proc.Out), sensorCfg.SampleRate)
			fmt.Fprintf(os.Stderr, "source: microphone via %s\n", capture.Describe(micName, micArgs))
		} else if resolved.Input == profile.InputPCMS16LE {
			src = sensor.NewPCMReader(record(stdin), sensorCfg.SampleRate)
			fmt.Fprintf(os.Stderr, "source: %s (raw PCM s16le mono, %d Hz)\n", inputName, sensorCfg.SampleRate)
		} else {
			src = sensor.NewReader(stdin)
			fmt.Fprintf(os.Stderr, "source: %s (JSONL)\n", inputName)
		}
		replay = true
	}

	// ── Context: cancelled on SIGINT / SIGTERM ─────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\n[edge-filter] received %s — shutting down…\n", sig)
		cancel()
	}()

	// ── MQTT (optional — pipeline runs without it) ─────────────────────
	var (
		pub       *mqttclient.Publisher
		resilient *mqttclient.ResilientClient
	)
	if !*noMQTT {
		cfg := mqttclient.DefaultConfig(*broker, "edge-filter-"+*sensorID)
		cfg.Username = os.Getenv("MQTT_USER")
		cfg.Password = os.Getenv("MQTT_PASSWORD")
		cfg.ConnectTimeout = 3 * time.Second
		resilient = mqttclient.NewResilientClient(cfg, 512)
		resilient.Start()
		pub = mqttclient.NewPublisher(resilient, *sensorID, mqttclient.QoS1)
		fmt.Fprintln(os.Stderr, "MQTT resilient client started ✓")
	}

	// ── Sensor Manager ─────────────────────────────────────────────────
	mgr := sensor.NewManager(256)
	if err := mgr.Add(sensorCfg); err != nil {
		fmt.Fprintf(os.Stderr, "manager.Add: %v\n", err)
		os.Exit(1)
	}

	// ── Stats ──────────────────────────────────────────────────────────
	var (
		totalSamples int64
		totalEvents  int
	)

	// ── Event consumer goroutine ───────────────────────────────────────
	// Drains WorkerEvents from the Manager and publishes them via MQTT.
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for ev := range mgr.Events() {
			totalEvents++

			// Console output.
			printWorkerEvent(ev)

			if pub == nil {
				continue
			}

			// Periodic telemetry flush → sm/telemetry/<id>
			// The timestamp is the wall-clock time at which the window
			// completed (taken in the worker), not the time of publishing.
			ts := ev.WindowTime
			if ts.IsZero() {
				ts = time.Now()
			}
			if ev.IsTelemetryFlush {
				if err := pub.PublishTelemetry(
					ts.UnixMilli(),
					ev.WindowRMS, // last window RMS as the point value
					ev.WindowRMS,
					ev.SDTRatio,
					ev.IsAnomaly,
					ev.Severity,
					float32(ev.FinalScore),
					ev.DominantSignal,
				); err != nil {
					fmt.Fprintf(os.Stderr, "publish telemetry: %v\n", err)
				}
			}

			// Anomaly state transition → sm/anomaly/<id>
			if ev.Event != nil {
				eventType := "clear"
				if ev.Event.Anomaly {
					eventType = "onset"
				}
				if err := pub.PublishAnomaly(
					ts,
					eventType,
					string(ev.Event.Severity),
					float32(ev.Event.RMS),
					float32(ev.Event.ZScore),
					float32(ev.Event.BaselineMean),
					float32(ev.Event.BaselineStdDev),
					ev.Event.Signal,
					ev.Event.Band,
				); err != nil {
					fmt.Fprintf(os.Stderr, "publish anomaly: %v\n", err)
				}
			}
		}
	}()

	// Compares the real arrival rate of samples with SENSOR_SAMPLE_RATE.
	rateMon := sensor.NewRateMonitor(sensorCfg.SampleRate)
	startedAt := time.Now()

	// ── Hot loop: read source → Manager ───────────────────────────────
	for {
		select {
		case <-ctx.Done():
			goto shutdown
		default:
		}

		s, err := src.Next()
		if err != nil {
			break // EOF or read error
		}
		if replay {
			// Replayed input (file or pipe): apply backpressure instead of
			// dropping samples, so results are reproducible.
			mgr.SendBlocking(*sensorID, s)
		} else {
			mgr.Send(*sensorID, s)
		}
		totalSamples++
		if totalSamples&255 == 0 {
			if rep, ok := rateMon.Observe(time.Now(), totalSamples); ok {
				printRateReport(rep)
			}
		}
	}

shutdown:
	// ── Graceful shutdown sequence ─────────────────────────────────────
	//
	// Order matters:
	//   1. Stop Manager  — stops all worker goroutines; closes mgr.Events()
	//   2. Wait for event consumer — drains remaining WorkerEvents
	//   3. Stop ResilientClient — drains MQTT publish buffer, disconnects
	//   4. Print stats
	fmt.Fprintln(os.Stderr, "[edge-filter] stopping sensor manager…")
	mgr.Stop()

	fmt.Fprintln(os.Stderr, "[edge-filter] waiting for event consumer…")
	<-eventsDone

	if resilient != nil {
		fmt.Fprintln(os.Stderr, "[edge-filter] draining MQTT buffer…")
		resilient.Stop()
		dropped, reconnects, lost := resilient.Stats()
		fmt.Fprintf(os.Stderr, "[mqtt] stats: dropped=%d reconnects=%d lost_conns=%d\n",
			dropped, reconnects, lost)
	}

	stats, _ := mgr.Stats(*sensorID)
	var lastRate *sensor.RateReport
	if r, ok := rateMon.Latest(); ok {
		lastRate = &r
	}
	fmt.Print(formatStats(totalSamples, totalEvents, stats, sensorCfg.SampleRate, lastRate, time.Since(startedAt)))

	if rec != nil {
		n, err := rec.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "record: closing %s: %v\n", *opts.record, err)
		}
		fmt.Fprintf(os.Stderr, "recording saved: %s (%d bytes, %.1f s of sound)\n", *opts.record, n, float64(n)/2/float64(sensorCfg.SampleRate))
	}

	// The microphone command ended on its own (we were not asked to stop):
	// say why, and fail.
	if micProc != nil && ctx.Err() == nil {
		reason := "ended"
		if err := micProc.Wait(); err != nil {
			reason = "stopped: " + err.Error()
		}
		fmt.Fprintf(os.Stderr, "capture: %s %s. Check AUDIO_DEVICE (edge-filter --list-audio) and that no other program holds the microphone.\n", micName, reason)
		os.Exit(1)
	}
}

// ── Console output ─────────────────────────────────────────────────────────

// requireSerialRate refuses to start a serial source without an explicit
// SENSOR_SAMPLE_RATE. A serial device sends no clock, so time and every
// frequency are derived from this value; guessing it would silently shift
// all results.
func requireSerialRate(s profile.Settings) error {
	if s.SampleRate > 0 {
		return nil
	}
	return fmt.Errorf("SENSOR_SAMPLE_RATE is required for SERIAL_INTERFACE: set the real sample rate of the device in Hz (ADXL345 firmware: 1600)")
}

// envOr returns the value of environment variable key, or def if unset/empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// defaultBroker builds the --broker default from CENTRAL_HOST/MQTT_PORT
// (set via edge/.env or the environment). Falls back to localhost:1883
// for local dev with firmware-sim when neither is set.
// An explicit --broker flag always overrides this.
func defaultBroker() string {
	host := os.Getenv("CENTRAL_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("MQTT_PORT")
	if port == "" {
		port = "1883"
	}
	// JoinHostPort brackets IPv6 literals; accept both "::1" and "[::1]".
	return net.JoinHostPort(strings.Trim(host, "[]"), port)
}

func printWorkerEvent(ev sensor.WorkerEvent) {
	severity := ev.Severity
	if severity == "" {
		severity = "normal"
	}

	status := "  OK  "
	switch severity {
	case "critical":
		status = " CRIT "
	case "warning":
		status = " WARN "
	}

	tag := ""
	if ev.IsTelemetryFlush {
		tag = fmt.Sprintf("  [flush sdt=%.1f×]", ev.SDTRatio)
	}

	fmt.Printf("win %5d  sensor=%-12s  [%s]  rms=%.4f  score=%+.1fσ(%s)%s",
		ev.Window, ev.SensorID, status, ev.WindowRMS, ev.FinalScore, ev.DominantSignal, tag)

	if ev.Event != nil {
		src := ev.Event.Signal
		if ev.Event.Band != "" {
			src += " " + ev.Event.Band
		}
		if ev.Event.Anomaly {
			fmt.Printf("  ⚡ ANOMALY [%s] via %s", ev.Event.Severity, src)
		} else {
			fmt.Printf("  ✅ CLEAR  (was %s)", src)
		}
	}

	if ev.BandResult.FaultBandAnomaly {
		fmt.Printf("  [band] ⚡ maxZ=%.1fσ band=%s",
			ev.BandResult.MaxZScore, ev.BandResult.MaxZBand)
	}
	fmt.Println()
}

// formatStats renders the summary printed at the end of a run. rate is the
// latest result of the sample-rate check (nil if the run was too short to
// measure); wall is the real time the run took.
func formatStats(samples int64, events int, st sensor.WorkerStats, sampleRate int, rate *sensor.RateReport, wall time.Duration) string {
	sep := strings.Repeat("─", 54)
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n", sep)
	fmt.Fprintf(&b, "  Total samples   : %d (%.1f s at %d Hz)\n", samples, float64(samples)/float64(sampleRate), sampleRate)
	if rate != nil && rate.Kind != sensor.RateReplay {
		fmt.Fprintf(&b, "  Real time       : %.1f s\n", wall.Seconds())
	}
	fmt.Fprintf(&b, "  Windows         : %d\n", st.Windows)
	if st.Windows > 0 {
		fmt.Fprintf(&b, "  In anomaly      : %d / %d (%.1f%%)\n",
			st.InAnomaly, st.Windows, 100*float64(st.InAnomaly)/float64(st.Windows))
		fmt.Fprintf(&b, "  Above warning   : %d / %d (%.1f%%)\n",
			st.AboveWarning, st.Windows, 100*float64(st.AboveWarning)/float64(st.Windows))
	}
	fmt.Fprintf(&b, "  Events shown    : %d (periodic telemetry and state changes)\n", events)
	switch {
	case rate == nil:
		fmt.Fprintf(&b, "  Rate check      : not measured (needs about 12 s of live input)\n")
	case rate.Kind == sensor.RateOK:
		fmt.Fprintf(&b, "  Rate check      : ok (%.0f Hz measured, %.0f Hz configured)\n", rate.Measured, rate.Want)
	case rate.Kind == sensor.RateReplay:
		fmt.Fprintf(&b, "  Rate check      : replay, faster than real time\n")
	default:
		fmt.Fprintf(&b, "  Rate check      : MISMATCH (%.0f Hz measured, %.0f Hz configured): all times and frequencies above are wrong\n",
			rate.Measured, rate.Want)
	}
	fmt.Fprintf(&b, "%s\n", sep)
	return b.String()
}

// box draws lines in a frame.
func box(lines ...string) string {
	w := 0
	for _, l := range lines {
		if n := len([]rune(l)); n > w {
			w = n
		}
	}
	var b strings.Builder
	b.WriteString("┌" + strings.Repeat("─", w+2) + "┐\n")
	for _, l := range lines {
		b.WriteString("│ " + l + strings.Repeat(" ", w-len([]rune(l))) + " │\n")
	}
	b.WriteString("└" + strings.Repeat("─", w+2) + "┘\n")
	return b.String()
}

// formatRateReport renders the result of the sample-rate check. A mismatch is
// a box: it is the one message that must not be missed in a stream of events.
func formatRateReport(r sensor.RateReport) string {
	switch r.Kind {
	case sensor.RateOK:
		return fmt.Sprintf("rate check: samples arrive at %.0f Hz, SENSOR_SAMPLE_RATE is %.0f - ok\n", r.Measured, r.Want)
	case sensor.RateReplay:
		return "rate check: input is much faster than real time (replay), skipped\n"
	}
	return box(
		"RATE MISMATCH",
		fmt.Sprintf("Samples arrive at %.0f Hz, but SENSOR_SAMPLE_RATE is %.0f.", r.Measured, r.Want),
		"Time and every frequency in this run are wrong.",
		"Fix the rate of the source or SENSOR_SAMPLE_RATE in edge/.env, then restart.",
	)
}

func printRateReport(r sensor.RateReport) { fmt.Fprint(os.Stderr, formatRateReport(r)) }
