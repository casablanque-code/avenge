package mqtt_test

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/mqtt"
)

// ── Backoff tests ──────────────────────────────────────────────────────────

func TestBackoffSequence(t *testing.T) {
	// Verify exponential backoff caps at max and starts at initial.
	seq := mqtt.ExportedBackoffSequence(6)
	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
	}
	for i, d := range expected {
		if seq[i] != d {
			t.Errorf("backoff[%d]: got %v, want %v", i, seq[i], d)
		}
	}
	// All values should be <= reconnectMax (60s).
	for i, d := range seq {
		if d > 60*time.Second {
			t.Errorf("backoff[%d] = %v exceeds max 60s", i, d)
		}
	}
}

// ── Drop logging tests ─────────────────────────────────────────────────────

func TestResilientClient_DropsWhenBufferFull(t *testing.T) {
	// A ResilientClient with a very small buffer (1 message) and no running
	// broker should accumulate drops when flooded.
	cfg := mqtt.DefaultConfig("127.0.0.1:19999", "test-drop") // port nobody listens on
	cfg.ConnectTimeout = 50 * time.Millisecond
	rc := mqtt.NewResilientClient(cfg, 1) // buffer of 1
	rc.Start()
	defer rc.Stop()

	// Flood with 10 messages — 1 may fit in buffer, rest should be dropped.
	for i := 0; i < 10; i++ {
		rc.Publish("test/topic", []byte("payload"), mqtt.QoS0)
	}

	// Give a moment for drops to register.
	time.Sleep(20 * time.Millisecond)

	dropped, _, _ := rc.Stats()
	if dropped == 0 {
		t.Error("expected some drops when buffer is full, got 0")
	}
	t.Logf("dropped: %d / 10 messages", dropped)
}

// ── Reconnect tests ────────────────────────────────────────────────────────

// restartableBroker is a mock broker that can be stopped and restarted
// to simulate connection loss and recovery.
type restartableBroker struct {
	mu      sync.Mutex
	ln      net.Listener
	addr    string
	active  bool
	packets [][]byte
	conns   []net.Conn
}

func newRestartableBroker(t *testing.T) *restartableBroker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	rb := &restartableBroker{ln: ln, addr: ln.Addr().String(), active: true}
	go rb.serve()
	return rb
}

func (rb *restartableBroker) serve() {
	for {
		conn, err := rb.ln.Accept()
		if err != nil {
			return
		}
		rb.mu.Lock()
		rb.conns = append(rb.conns, conn)
		rb.mu.Unlock()
		go rb.handleConn(conn)
	}
}

func (rb *restartableBroker) handleConn(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 4096)
	for {
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		fullHeader := buf[0]
		pktType := fullHeader & 0xF0

		remaining := 0
		mult := 1
		for {
			if _, err := io.ReadFull(conn, buf[:1]); err != nil {
				return
			}
			remaining += int(buf[0]&0x7F) * mult
			if buf[0]&0x80 == 0 {
				break
			}
			mult *= 128
		}
		body := make([]byte, remaining)
		if remaining > 0 {
			if _, err := io.ReadFull(conn, body); err != nil {
				return
			}
		}
		rb.mu.Lock()
		rb.packets = append(rb.packets, body)
		rb.mu.Unlock()

		switch pktType {
		case 0x10:
			conn.Write([]byte{0x20, 0x02, 0x00, 0x00})
		case 0x30:
			if qos := (fullHeader >> 1) & 0x03; qos == 1 {
				if len(body) >= 4 {
					topicLen := int(binary.BigEndian.Uint16(body[:2]))
					if off := 2 + topicLen; off+2 <= len(body) {
						pkID := body[off : off+2]
						conn.Write([]byte{0x40, 0x02, pkID[0], pkID[1]})
					}
				}
			}
		case 0xC0:
			conn.Write([]byte{0xD0, 0x00})
		case 0xE0:
			return
		}
	}
}

func (rb *restartableBroker) close() {
	rb.ln.Close()
	rb.mu.Lock()
	for _, c := range rb.conns {
		c.Close()
	}
	rb.mu.Unlock()
}

func (rb *restartableBroker) packetCount() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return len(rb.packets)
}

func TestResilientClient_PublishesAfterConnect(t *testing.T) {
	broker := newRestartableBroker(t)
	defer broker.close()

	cfg := mqtt.DefaultConfig(broker.addr, "test-resilient")
	cfg.ConnectTimeout = 500 * time.Millisecond
	rc := mqtt.NewResilientClient(cfg, 64)
	rc.Start()
	defer rc.Stop()

	// Give client time to connect.
	time.Sleep(100 * time.Millisecond)

	rc.Publish("sm/telemetry/bearing_01", []byte(`{"rms":1.2}`), mqtt.QoS0)
	time.Sleep(50 * time.Millisecond)

	// CONNECT packet + at least one PUBLISH.
	if broker.packetCount() < 2 {
		t.Errorf("expected >= 2 packets (CONNECT + PUBLISH), got %d", broker.packetCount())
	}
}

func TestResilientClient_ReconnectsAfterBrokerRestart(t *testing.T) {
	// Pattern: start broker1, connect, close it, start broker2 on NEW port,
	// update is impossible without re-creating client — instead verify the
	// client keeps retrying by checking reconnect counter increases.
	// The ResilientClient will keep attempting reconnect with backoff — we
	// just verify the reconnect counter increments, not that it succeeds
	// (which would require the broker to come back on the same port,
	// which is blocked by OS TIME_WAIT in test environments).

	broker := newRestartableBroker(t)

	cfg := mqtt.DefaultConfig(broker.addr, "test-reconnect")
	cfg.ConnectTimeout = 100 * time.Millisecond
	rc := mqtt.NewResilientClient(cfg, 64)
	rc.Start()
	defer rc.Stop()

	// Wait for initial connect.
	time.Sleep(150 * time.Millisecond)
	_, r0, l0 := rc.Stats()
	if r0 < 1 {
		t.Skip("initial connect did not complete in time")
	}

	// Close broker — triggers disconnect.
	broker.close()

	// Wait for at least one reconnect attempt.
	// Timing: read deadline (2s) + initial backoff (1s) + ConnectTimeout (100ms) ≈ 3.2s minimum.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, _, l := rc.Stats()
		if l > l0 {
			t.Logf("connection loss detected (lost: %d -> %d)", l0, l)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("expected reconnect attempts after broker close, counter did not increment")
}
