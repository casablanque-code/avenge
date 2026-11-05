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
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

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

	if !*explain {
		fmt.Fprintf(os.Stderr, "edge-filter  sensor=%s  broker=%s\n", *sensorID, *broker)
	}

	// ── Sensor configuration ─────────────────────────────────────────────
	// SENSOR_SAMPLE_RATE must match the firmware's actual output data rate
	// exactly (see edge/firmware/adxl345_esp32/adxl345_esp32.ino — ADXL345
	// is hardware-capped at 3200 Hz, this project runs it at 1600 Hz).
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
	resolved, err := profile.Resolve(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(2)
	}
	if *explain {
		fmt.Print(resolved.Explain())
		return
	}
	sensorCfg := resolved.Sensor
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
		if resolved.Input == profile.InputPCMS16LE {
			src = sensor.NewPCMReader(os.Stdin, sensorCfg.SampleRate)
			fmt.Fprintf(os.Stderr, "source: stdin (raw PCM s16le mono, %d Hz)\n", sensorCfg.SampleRate)
		} else {
			src = sensor.NewReader(os.Stdin)
			fmt.Fprintln(os.Stderr, "serial source: stdin (JSONL)")
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
				); err != nil {
					fmt.Fprintf(os.Stderr, "publish anomaly: %v\n", err)
				}
			}
		}
	}()

	// Compares the real arrival rate of samples with SENSOR_SAMPLE_RATE.
	rateMon := sensor.NewRateMonitor(sensorCfg.SampleRate)

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
	printStats(totalSamples, totalEvents, stats, sensorCfg.SampleRate)
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
		if ev.Event.Anomaly {
			fmt.Printf("  ⚡ ANOMALY [%s]", ev.Event.Severity)
		} else {
			fmt.Printf("  ✅ CLEAR  [%s]", ev.Event.Severity)
		}
	}

	if ev.BandResult.FaultBandAnomaly {
		fmt.Printf("  [band] ⚡ maxZ=%.1fσ band=%s",
			ev.BandResult.MaxZScore, ev.BandResult.MaxZBand)
	}
	fmt.Println()
}

func printStats(samples int64, events int, st sensor.WorkerStats, sampleRate int) {
	sep := strings.Repeat("─", 54)
	fmt.Println("\n" + sep)
	fmt.Printf("  Total samples   : %d (%.1f s)\n", samples, float64(samples)/float64(sampleRate))
	fmt.Printf("  Windows         : %d\n", st.Windows)
	if st.Windows > 0 {
		fmt.Printf("  Above warning   : %d / %d (%.1f%%)\n",
			st.AboveWarning, st.Windows, 100*float64(st.AboveWarning)/float64(st.Windows))
	}
	fmt.Printf("  Events shown    : %d (periodic telemetry and state changes)\n", events)
	fmt.Println(sep)
}

// printRateReport shows the result of the sample-rate check.
func printRateReport(r sensor.RateReport) {
	switch r.Kind {
	case sensor.RateOK:
		fmt.Fprintf(os.Stderr, "rate check: samples arrive at %.0f Hz, SENSOR_SAMPLE_RATE is %.0f - ok\n", r.Measured, r.Want)
	case sensor.RateReplay:
		fmt.Fprintln(os.Stderr, "rate check: input is much faster than real time (replay), skipped")
	default:
		fmt.Fprintf(os.Stderr, "warning: rate check: samples arrive at %.0f Hz but SENSOR_SAMPLE_RATE is %.0f; "+
			"time and every frequency are wrong. Fix the capture rate or SENSOR_SAMPLE_RATE.\n", r.Measured, r.Want)
	}
}
