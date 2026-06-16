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
//	--broker      MQTT broker address (default "localhost:1883")
//	--sensor-id   sensor identifier   (default "bearing_01")
//	--no-mqtt     disable MQTT, print to stdout only
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mqttclient "github.com/casablanque-code/smart-manufacturing/edge/internal/mqtt"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

func main() {
	broker   := flag.String("broker",    "localhost:1883", "MQTT broker address")
	sensorID := flag.String("sensor-id", "bearing_01",    "Sensor identifier")
	noMQTT   := flag.Bool("no-mqtt",     false,           "Disable MQTT output")
	flag.Parse()

	fmt.Fprintf(os.Stderr, "edge-filter  sensor=%s  broker=%s\n", *sensorID, *broker)

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
		cfg.ConnectTimeout = 3 * time.Second
		resilient = mqttclient.NewResilientClient(cfg, 512)
		resilient.Start()
		pub = mqttclient.NewPublisher(resilient, *sensorID, mqttclient.QoS1)
		fmt.Fprintln(os.Stderr, "MQTT resilient client started ✓")
	}

	// ── Sensor Manager ─────────────────────────────────────────────────
	mgr := sensor.NewManager(256)
	if err := mgr.Add(sensor.DefaultSensorConfig(*sensorID)); err != nil {
		fmt.Fprintf(os.Stderr, "manager.Add: %v\n", err)
		os.Exit(1)
	}

	// ── Stats ──────────────────────────────────────────────────────────
	var (
		totalSamples    int64
		totalWindows    int
		anomalyWindows  int
	)

	// ── Event consumer goroutine ───────────────────────────────────────
	// Drains WorkerEvents from the Manager and publishes them via MQTT.
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for ev := range mgr.Events() {
			totalWindows++
			if ev.Event.Anomaly {
				anomalyWindows++
			}

			// Console output.
			printWorkerEvent(totalWindows, ev)

			if pub == nil {
				continue
			}

			// Publish anomaly state transition.
			eventType := "clear"
			if ev.Event.Anomaly {
				eventType = "onset"
			}
			if err := pub.PublishAnomaly(
				time.Now(),
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
	}()

	// ── Hot loop: read stdin → Manager ────────────────────────────────
	reader := sensor.NewReader(os.Stdin)
	for {
		// Check for shutdown before each read so we exit cleanly on signal
		// even when stdin is a slow/blocked stream (e.g. real hardware).
		select {
		case <-ctx.Done():
			goto shutdown
		default:
		}

		s, err := reader.Next()
		if err != nil {
			break // EOF or read error
		}
		mgr.Send(*sensorID, s)
		totalSamples++
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

	printStats(totalSamples, totalWindows, anomalyWindows)
}

// ── Console output ─────────────────────────────────────────────────────────

func printWorkerEvent(n int, ev sensor.WorkerEvent) {
	e := ev.Event
	status := "  OK  "
	switch {
	case e.Severity == "critical":
		status = " CRIT "
	case e.Severity == "warning":
		status = " WARN "
	}

	fmt.Printf("win %4d  sensor=%-12s  [%s]  rms=%.4f  z=%+.1fσ",
		n, ev.SensorID, status, ev.WindowRMS, e.ZScore)

	if e.Anomaly {
		fmt.Printf("  ⚡ ANOMALY [%s]", e.Severity)
	} else {
		fmt.Printf("  ✅ CLEAR  [%s]", e.Severity)
	}

	if ev.BandResult.FaultBandAnomaly {
		fmt.Printf("  [band] ⚡ maxZ=%.1fσ band=%s",
			ev.BandResult.MaxZScore, ev.BandResult.MaxZBand)
	}
	fmt.Println()
}

func printStats(samples int64, windows, anomalyWins int) {
	const sampleRate = 1000 // matches DefaultSensorConfig
	sep := strings.Repeat("─", 54)
	fmt.Println("\n" + sep)
	fmt.Printf("  Total samples   : %d (%.1f s)\n", samples, float64(samples)/sampleRate)
	fmt.Printf("  Windows         : %d\n", windows)
	if windows > 0 {
		fmt.Printf("  Anomalous       : %d / %d (%.0f%%)\n",
			anomalyWins, windows, 100*float64(anomalyWins)/float64(windows))
	}
	fmt.Println(sep)
}
