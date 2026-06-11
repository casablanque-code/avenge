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

type mockBroker struct {
ln      net.Listener
addr    string
mu      sync.Mutex
packets [][]byte
}

func newMockBroker(t *testing.T) *mockBroker {
t.Helper()
ln, err := net.Listen("tcp", "127.0.0.1:0")
if err != nil {
t.Fatalf("mock broker listen: %v", err)
}
mb := &mockBroker{ln: ln, addr: ln.Addr().String()}
go mb.serve()
return mb
}

func (mb *mockBroker) serve() {
for {
conn, err := mb.ln.Accept()
if err != nil {
return
}
go mb.handleConn(conn)
}
}

func (mb *mockBroker) handleConn(conn net.Conn) {
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
mb.mu.Lock()
mb.packets = append(mb.packets, body)
mb.mu.Unlock()
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

func (mb *mockBroker) close()        { mb.ln.Close() }
func (mb *mockBroker) count() int    { mb.mu.Lock(); defer mb.mu.Unlock(); return len(mb.packets) }

func TestConnect(t *testing.T) {
mb := newMockBroker(t)
defer mb.close()
c, err := mqtt.Connect(mqtt.DefaultConfig(mb.addr, "test"))
if err != nil {
t.Fatal(err)
}
c.Disconnect()
}

func TestPublishQoS0(t *testing.T) {
mb := newMockBroker(t)
defer mb.close()
c, err := mqtt.Connect(mqtt.DefaultConfig(mb.addr, "test-qos0"))
if err != nil {
t.Fatal(err)
}
defer c.Disconnect()
if err := c.Publish("test/topic", []byte(`{}`), mqtt.QoS0); err != nil {
t.Fatal(err)
}
time.Sleep(20 * time.Millisecond)
if mb.count() < 2 {
t.Errorf("expected >= 2 packets, got %d", mb.count())
}
}

func TestPublishQoS1(t *testing.T) {
mb := newMockBroker(t)
defer mb.close()
cfg := mqtt.DefaultConfig(mb.addr, "test-qos1")
cfg.PublishTimeout = 2 * time.Second
c, err := mqtt.Connect(cfg)
if err != nil {
t.Fatal(err)
}
defer c.Disconnect()
if err := c.Publish("sm/anomaly/bearing_01", []byte(`{"rms":1.2}`), mqtt.QoS1); err != nil {
t.Fatal(err)
}
}

func TestPublishConcurrent(t *testing.T) {
mb := newMockBroker(t)
defer mb.close()
c, err := mqtt.Connect(mqtt.DefaultConfig(mb.addr, "test-conc"))
if err != nil {
t.Fatal(err)
}
defer c.Disconnect()
var wg sync.WaitGroup
errs := make(chan error, 20)
for i := 0; i < 20; i++ {
wg.Add(1)
go func() {
defer wg.Done()
if err := c.Publish("sm/telemetry/bearing_01", []byte(`{"v":1.0}`), mqtt.QoS0); err != nil {
errs <- err
}
}()
}
wg.Wait()
close(errs)
for err := range errs {
t.Error(err)
}
}
