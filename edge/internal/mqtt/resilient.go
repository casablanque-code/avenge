// resilient.go — ResilientClient wraps Client with:
//   - Automatic reconnect with exponential backoff on connection loss
//   - In-memory publish buffer (channel) so callers don't block during reconnect
//   - Drop logging when the buffer is full (explicit, never silent data loss)
//
// Usage:
//
//	rc := mqtt.NewResilientClient(mqtt.DefaultConfig("localhost:1883", "edge"))
//	rc.Start()
//	defer rc.Stop()
//	rc.Publish("sm/telemetry/bearing_01", payload, mqtt.QoS0)
package mqtt

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// publishRequest is an item in the outbound buffer.
type publishRequest struct {
	topic   string
	payload []byte
	qos     byte
}

// ResilientClient is a reconnecting MQTT client with a buffered publish queue.
// It satisfies the same Publish / Disconnect interface as Client.
type ResilientClient struct {
	cfg      Config
	buf      chan publishRequest
	stopCh   chan struct{}
	stopped  chan struct{}
	wg       sync.WaitGroup

	// Stats — read with atomic, written by the publish goroutine.
	dropped    atomic.Int64
	reconnects atomic.Int64
	lostConns  atomic.Int64
}

// reconnectPolicy defines backoff parameters.
const (
	reconnectInitial = 1 * time.Second
	reconnectMax     = 60 * time.Second
	reconnectFactor  = 2.0
)

// NewResilientClient creates a ResilientClient with a publish buffer of
// bufSize messages. A size of 512 can absorb ~5 minutes of telemetry at
// one message per 10 s; increase for longer expected outage windows.
func NewResilientClient(cfg Config, bufSize int) *ResilientClient {
	return &ResilientClient{
		cfg:     cfg,
		buf:     make(chan publishRequest, bufSize),
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Start launches the background reconnect+publish loop.
// Must be called before Publish.
func (rc *ResilientClient) Start() {
	rc.wg.Add(1)
	go rc.loop()
}

// Stop shuts down the client gracefully.
// Blocks until the background loop exits.
func (rc *ResilientClient) Stop() {
	close(rc.stopCh)
	rc.wg.Wait()
}

// Publish enqueues a message for delivery.
// Non-blocking: if the buffer is full the message is dropped and logged.
// This is the correct behaviour for a real-time sensor pipeline where
// dropping old data is safer than blocking the caller.
func (rc *ResilientClient) Publish(topic string, payload []byte, qos byte) {
	req := publishRequest{topic: topic, payload: payload, qos: qos}
	select {
	case rc.buf <- req:
	default:
		n := rc.dropped.Add(1)
		fmt.Printf("[mqtt] DROP publish to %s — buffer full (total drops: %d)\n",
			topic, n)
	}
}

// Stats returns cumulative drop, reconnect, and lost-connection counts.
func (rc *ResilientClient) Stats() (dropped, reconnects, lost int64) {
	return rc.dropped.Load(), rc.reconnects.Load(), rc.lostConns.Load()
}

// loop is the background goroutine: connects, drains the buffer, reconnects on loss.
func (rc *ResilientClient) loop() {
	defer rc.wg.Done()
	defer close(rc.stopped)

	backoff := reconnectInitial

	for {
		select {
		case <-rc.stopCh:
			return
		default:
		}

		// Connect (or reconnect).
		client, err := Connect(rc.cfg)
		if err != nil {
			fmt.Printf("[mqtt] connect failed (%v) — retry in %s\n", err, backoff)
			select {
			case <-time.After(backoff):
			case <-rc.stopCh:
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		n := rc.reconnects.Add(1)
		if n > 1 {
			fmt.Printf("[mqtt] reconnected (attempt %d)\n", n)
		} else {
			fmt.Printf("[mqtt] connected to %s\n", rc.cfg.Broker)
		}
		backoff = reconnectInitial // reset on success

		// Drain buffer until connection is lost or stop requested.
		rc.drain(client)
		client.Disconnect()

		select {
		case <-rc.stopCh:
			return
		default:
			rc.lostConns.Add(1)
			fmt.Printf("[mqtt] connection lost — reconnecting in %s\n", backoff)
			select {
			case <-time.After(backoff):
			case <-rc.stopCh:
				return
			}
			backoff = nextBackoff(backoff)
		}
	}
}

// drain reads from rc.buf and publishes to client until the connection
// is lost (client.Disconnected() fires) or stop is requested.
func (rc *ResilientClient) drain(client *Client) {
	for {
		select {
		case <-rc.stopCh:
			return
		case <-client.Disconnected():
			return
		case req := <-rc.buf:
			if err := client.Publish(req.topic, req.payload, req.qos); err != nil {
				// Connection failed mid-publish — re-queue if possible, else drop.
				select {
				case rc.buf <- req: // re-queue at back
				default:
					n := rc.dropped.Add(1)
					fmt.Printf("[mqtt] DROP on publish error (total drops: %d): %v\n", n, err)
				}
				return // trigger reconnect
			}
		}
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := time.Duration(float64(current) * reconnectFactor)
	if next > reconnectMax {
		next = reconnectMax
	}
	return next
}

// ExportedBackoffSequence is the exported alias of backoffSequence for tests.
var ExportedBackoffSequence = backoffSequence

// backoffSequence returns the sequence of backoff durations for testing.
func backoffSequence(n int) []time.Duration {
	durations := make([]time.Duration, n)
	d := reconnectInitial
	for i := range durations {
		durations[i] = d
		d = time.Duration(math.Min(float64(d)*reconnectFactor, float64(reconnectMax)))
	}
	return durations
}
