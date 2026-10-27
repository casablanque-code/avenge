package mqtt

import (
	"io"
	"net"
	"testing"
	"time"
)

// readConnect accepts one connection, reads a full CONNECT packet, replies
// with CONNACK (accepted) and returns the packet body (after the fixed header).
func readConnect(t *testing.T, ln net.Listener) <-chan []byte {
	t.Helper()
	out := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		hdr := make([]byte, 2) // CONNECT bodies in these tests are < 128 bytes
		if _, err := io.ReadFull(conn, hdr); err != nil {
			return
		}
		body := make([]byte, hdr[1])
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		_, _ = conn.Write([]byte{0x20, 0x02, 0x00, 0x00})
		out <- body
		time.Sleep(50 * time.Millisecond)
	}()
	return out
}

func connectBody(t *testing.T, user, pass string) []byte {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := readConnect(t, ln)

	cfg := DefaultConfig(ln.Addr().String(), "cid")
	cfg.Username, cfg.Password = user, pass
	c, err := Connect(cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Disconnect()

	select {
	case b := <-got:
		return b
	case <-time.After(2 * time.Second):
		t.Fatal("no CONNECT received")
		return nil
	}
}

func TestConnectWithCredentials(t *testing.T) {
	b := connectBody(t, "edge", "s3cret")
	if b[7] != 0xC2 {
		t.Fatalf("connect flags = 0x%02x, want 0xC2", b[7])
	}
	want := append(append(encodeString([]byte("cid")), encodeString([]byte("edge"))...), encodeString([]byte("s3cret"))...)
	if string(b[10:]) != string(want) {
		t.Fatalf("payload = %q, want %q", b[10:], want)
	}
}

func TestConnectAnonymous(t *testing.T) {
	b := connectBody(t, "", "")
	if b[7] != 0x02 {
		t.Fatalf("connect flags = 0x%02x, want 0x02", b[7])
	}
	if string(b[10:]) != string(encodeString([]byte("cid"))) {
		t.Fatalf("unexpected payload %q", b[10:])
	}
}
