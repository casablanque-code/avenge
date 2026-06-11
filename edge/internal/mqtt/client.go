// Package mqtt implements a minimal MQTT 3.1.1 client sufficient for
// the edge-filter use case: connect to a broker and publish messages.
//
// Supported: CONNECT, PUBLISH (QoS 0 and 1), DISCONNECT, PINGREQ.
// Not supported: subscribe, QoS 2, TLS (add net.Dial → tls.Dial for prod).
//
// Thread-safe: Publish may be called concurrently from multiple goroutines.
package mqtt

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// QoS levels.
const (
	QoS0 = 0 // fire-and-forget
	QoS1 = 1 // at-least-once (broker sends PUBACK)
)

// MQTT 3.1.1 packet type constants.
const (
	pktConnect    = 0x10
	pktConnAck    = 0x20
	pktPublish    = 0x30
	pktPubAck     = 0x40
	pktDisconnect = 0xE0
	pktPingReq    = 0xC0
	pktPingResp   = 0xD0
)

// Config holds connection parameters.
type Config struct {
	// Broker address, e.g. "localhost:1883".
	Broker string
	// ClientID must be unique per connected client.
	ClientID string
	// KeepAlive interval in seconds (0 = disable pings).
	KeepAlive uint16
	// ConnectTimeout for the initial TCP + CONNECT handshake.
	ConnectTimeout time.Duration
	// PublishTimeout for waiting on QoS1 PUBACK.
	PublishTimeout time.Duration
}

// DefaultConfig returns sensible defaults for a local broker.
func DefaultConfig(broker, clientID string) Config {
	return Config{
		Broker:         broker,
		ClientID:       clientID,
		KeepAlive:      60,
		ConnectTimeout: 5 * time.Second,
		PublishTimeout: 3 * time.Second,
	}
}

// Client is a connected MQTT client.
type Client struct {
	cfg    Config
	conn   net.Conn
	mu     sync.Mutex   // serialises writes to conn
	pktID  atomic.Uint32 // monotonic packet ID counter for QoS1

	// pending maps packetID → channel that receives the PUBACK.
	pendingMu sync.Mutex
	pending   map[uint16]chan struct{}

	done chan struct{}
}

// Connect establishes a TCP connection and performs the MQTT CONNECT handshake.
func Connect(cfg Config) (*Client, error) {
	conn, err := net.DialTimeout("tcp", cfg.Broker, cfg.ConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("mqtt: dial %s: %w", cfg.Broker, err)
	}

	c := &Client{
		cfg:     cfg,
		conn:    conn,
		pending: make(map[uint16]chan struct{}),
		done:    make(chan struct{}),
	}

	if err := conn.SetDeadline(time.Now().Add(cfg.ConnectTimeout)); err != nil {
		conn.Close()
		return nil, err
	}
	if err := c.sendConnect(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("mqtt: CONNECT: %w", err)
	}
	if err := c.readConnAck(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("mqtt: CONNACK: %w", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil { // clear deadline
		conn.Close()
		return nil, err
	}

	go c.readLoop()
	if cfg.KeepAlive > 0 {
		go c.pingLoop()
	}
	return c, nil
}

// Publish sends a message to the given topic.
// For QoS0 it returns immediately after the write.
// For QoS1 it blocks until PUBACK is received or PublishTimeout elapses.
func (c *Client) Publish(topic string, payload []byte, qos byte) error {
	id := uint16(0)
	var pubackCh chan struct{}

	if qos == QoS1 {
		next := c.pktID.Add(1)
		if next == 0 {
			next = c.pktID.Add(1) // skip zero — reserved by MQTT spec
		}
		id = uint16(next & 0xFFFF)
		pubackCh = make(chan struct{}, 1)
		c.pendingMu.Lock()
		c.pending[id] = pubackCh
		c.pendingMu.Unlock()
	}

	c.mu.Lock()
	err := c.sendPublish(topic, payload, qos, id)
	c.mu.Unlock()

	if err != nil {
		if qos == QoS1 {
			c.pendingMu.Lock()
			delete(c.pending, id)
			c.pendingMu.Unlock()
		}
		return fmt.Errorf("mqtt: PUBLISH: %w", err)
	}

	if qos == QoS1 {
		select {
		case <-pubackCh:
			// success
		case <-time.After(c.cfg.PublishTimeout):
			c.pendingMu.Lock()
			delete(c.pending, id)
			c.pendingMu.Unlock()
			return fmt.Errorf("mqtt: PUBACK timeout (topic=%s id=%d)", topic, id)
		case <-c.done:
			return fmt.Errorf("mqtt: client disconnected while waiting for PUBACK")
		}
	}
	return nil
}

// Disconnect sends MQTT DISCONNECT and closes the connection.
func (c *Client) Disconnect() {
	c.mu.Lock()
	_, _ = c.conn.Write([]byte{pktDisconnect, 0x00})
	c.mu.Unlock()
	close(c.done)
	_ = c.conn.Close()
}

// ── Wire format helpers ────────────────────────────────────────────────────

func (c *Client) sendConnect() error {
	clientID := []byte(c.cfg.ClientID)

	// Variable header: protocol name + level + connect flags + keep-alive.
	varHeader := []byte{
		0x00, 0x04, 'M', 'Q', 'T', 'T', // protocol name
		0x04,       // protocol level 3.1.1
		0x02,       // connect flags: clean session
		byte(c.cfg.KeepAlive >> 8),
		byte(c.cfg.KeepAlive),
	}

	// Payload: client ID (length-prefixed string).
	payload := encodeString(clientID)

	remaining := append(varHeader, payload...)
	pkt := buildFixedHeader(pktConnect, len(remaining))
	pkt = append(pkt, remaining...)
	_, err := c.conn.Write(pkt)
	return err
}

func (c *Client) readConnAck() error {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return err
	}
	if buf[0] != pktConnAck {
		return fmt.Errorf("expected CONNACK (0x20), got 0x%02x", buf[0])
	}
	if buf[3] != 0x00 {
		codes := map[byte]string{
			0x01: "unacceptable protocol version",
			0x02: "identifier rejected",
			0x03: "server unavailable",
			0x04: "bad username or password",
			0x05: "not authorized",
		}
		msg, ok := codes[buf[3]]
		if !ok {
			msg = fmt.Sprintf("unknown return code 0x%02x", buf[3])
		}
		return fmt.Errorf("broker refused connection: %s", msg)
	}
	return nil
}

func (c *Client) sendPublish(topic string, payload []byte, qos byte, id uint16) error {
	topicBytes := encodeString([]byte(topic))

	var varHeader []byte
	varHeader = append(varHeader, topicBytes...)
	if qos > 0 {
		varHeader = append(varHeader, byte(id>>8), byte(id))
	}
	varHeader = append(varHeader, payload...)

	flags := (qos & 0x03) << 1
	pkt := buildFixedHeader(pktPublish|int(flags), len(varHeader))
	pkt = append(pkt, varHeader...)
	_, err := c.conn.Write(pkt)
	return err
}

func (c *Client) sendPingReq() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.conn.Write([]byte{pktPingReq, 0x00})
	return err
}

// readLoop handles incoming packets (PUBACK, PINGRESP) from the broker.
func (c *Client) readLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
		}

		// Read fixed header byte.
		header := make([]byte, 1)
		if _, err := io.ReadFull(c.conn, header); err != nil {
			return
		}
		pktType := header[0] & 0xF0

		// Decode remaining length (variable-length encoding).
		remaining, err := decodeRemainingLength(c.conn)
		if err != nil {
			return
		}

		// Read remaining bytes.
		body := make([]byte, remaining)
		if remaining > 0 {
			if _, err := io.ReadFull(c.conn, body); err != nil {
				return
			}
		}

		switch pktType {
		case pktPubAck:
			if len(body) >= 2 {
				id := binary.BigEndian.Uint16(body[:2])
				c.pendingMu.Lock()
				ch, ok := c.pending[id]
				if ok {
					delete(c.pending, id)
				}
				c.pendingMu.Unlock()
				if ok {
					ch <- struct{}{}
				}
			}
		case pktPingResp:
			// nothing needed
		}
	}
}

// pingLoop sends PINGREQ every KeepAlive/2 seconds to keep the connection alive.
func (c *Client) pingLoop() {
	interval := time.Duration(c.cfg.KeepAlive/2) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			_ = c.sendPingReq()
		}
	}
}

// ── Encoding helpers ───────────────────────────────────────────────────────

func encodeString(s []byte) []byte {
	out := make([]byte, 2+len(s))
	binary.BigEndian.PutUint16(out, uint16(len(s)))
	copy(out[2:], s)
	return out
}

func buildFixedHeader(pktType, remainingLen int) []byte {
	var out []byte
	out = append(out, byte(pktType))
	// Variable-length remaining length encoding (MQTT spec §2.2.3).
	x := remainingLen
	for {
		digit := x % 128
		x /= 128
		if x > 0 {
			digit |= 0x80
		}
		out = append(out, byte(digit))
		if x == 0 {
			break
		}
	}
	return out
}

func decodeRemainingLength(r io.Reader) (int, error) {
	multiplier := 1
	value := 0
	b := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, b); err != nil {
			return 0, err
		}
		value += int(b[0]&0x7F) * multiplier
		if b[0]&0x80 == 0 {
			break
		}
		multiplier *= 128
		if multiplier > 128*128*128 {
			return 0, fmt.Errorf("mqtt: malformed remaining length")
		}
	}
	return value, nil
}
