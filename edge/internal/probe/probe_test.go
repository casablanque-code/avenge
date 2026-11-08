package probe

import (
	"net"
	"strings"
	"testing"
	"time"
)

// fakeBroker answers one CONNECT with the given CONNACK return code.
func fakeBroker(t *testing.T, code byte) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen:", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 512)
				c.SetReadDeadline(time.Now().Add(2 * time.Second))
				if n, _ := c.Read(buf); n == 0 {
					return // the plain TCP probe: it connects and closes
				}
				c.Write([]byte{0x20, 0x02, 0x00, code})
				time.Sleep(100 * time.Millisecond)
			}(c)
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func TestBrokerOK(t *testing.T) {
	h, p := fakeBroker(t, 0)
	if r := Broker(h, p, "edge", "pw", time.Second); !r.OK() {
		t.Fatalf("%+v", r)
	}
}

func TestBrokerLoginRefused(t *testing.T) {
	h, p := fakeBroker(t, 0x04)
	r := Broker(h, p, "edge", "wrong", time.Second)
	if r.Kind != Login {
		t.Fatalf("kind %v err %v", r.Kind, r.Err)
	}
	if !strings.Contains(Hint(r, h), "MQTT_EDGE_PASSWORD") {
		t.Error("hint does not name the password setting")
	}
}

func TestBrokerRefusedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if r := Broker("127.0.0.1", port, "e", "p", time.Second); r.Kind != Refused {
		t.Fatalf("kind %v err %v", r.Kind, r.Err)
	}
}

func TestBrokerDNS(t *testing.T) {
	r := Broker("no-such-host.invalid", 1883, "e", "p", time.Second)
	if r.Kind != DNS && r.Kind != Network {
		t.Fatalf("kind %v err %v", r.Kind, r.Err)
	}
}

func TestHintsExistForEveryFailure(t *testing.T) {
	for _, k := range []Kind{DNS, Timeout, Refused, NoReply, Login, Network} {
		if Hint(Result{Kind: k}, "10.0.0.1") == "" {
			t.Errorf("no hint for %s", k)
		}
	}
	if !strings.Contains(Hint(Result{Kind: Timeout}, "10.0.0.1"), "ip route get 10.0.0.1") {
		t.Error("timeout hint lacks the route check")
	}
}
