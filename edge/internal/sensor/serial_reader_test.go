//go:build linux

package sensor

import (
	"os"
	"testing"
	"time"
)

// newTestReader builds a SerialReader backed by one end of an os.Pipe,
// returning the writable end so the test can feed it bytes as if they
// came from the microcontroller. termios setup is skipped — pipes aren't
// ttys — only the framing/resync logic in Next() is under test here.
func newTestReader(t *testing.T) (*SerialReader, *os.File) {
	t.Helper()
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		readEnd.Close()
		writeEnd.Close()
	})
	r := &SerialReader{
		f:          readEnd,
		sampleRate: 1600,
		startTime:  time.Now(),
	}
	return r, writeEnd
}

func TestSerialReader_SinglePacket(t *testing.T) {
	r, w := newTestReader(t)

	// raw = 256 -> hi=0x01, lo=0x00
	w.Write([]byte{0xAA, 0x01, 0x00})

	s, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := 256.0 * 0.0039
	if s.Value < want-1e-9 || s.Value > want+1e-9 {
		t.Errorf("Value = %v, want %v", s.Value, want)
	}
	if s.T != 0 {
		t.Errorf("T = %v, want 0 for first sample", s.T)
	}
}

func TestSerialReader_TimestampIncrementsBySampleRate(t *testing.T) {
	r, w := newTestReader(t)
	w.Write([]byte{0xAA, 0x00, 0x01, 0xAA, 0x00, 0x02})

	s1, err := r.Next()
	if err != nil {
		t.Fatalf("Next (1): %v", err)
	}
	s2, err := r.Next()
	if err != nil {
		t.Fatalf("Next (2): %v", err)
	}

	wantDelta := 1.0 / 1600.0
	gotDelta := s2.T - s1.T
	if gotDelta < wantDelta-1e-9 || gotDelta > wantDelta+1e-9 {
		t.Errorf("T delta = %v, want %v (1/sampleRate)", gotDelta, wantDelta)
	}
}

func TestSerialReader_ResyncsAfterGarbageBytes(t *testing.T) {
	r, w := newTestReader(t)

	// Garbage bytes (no 0xAA) followed by one real packet — simulates a
	// line glitch or a partial/corrupted packet ahead of a clean one.
	w.Write([]byte{0x12, 0x34, 0x56, 0x78})
	w.Write([]byte{0xAA, 0x00, 0x0A}) // raw = 10

	s, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := 10.0 * 0.0039
	if s.Value < want-1e-9 || s.Value > want+1e-9 {
		t.Errorf("Value = %v, want %v — resync after garbage failed", s.Value, want)
	}
}

func TestSerialReader_HandlesMarkerByteInPayload(t *testing.T) {
	r, w := newTestReader(t)

	// The payload's high byte is itself 0xAA (raw = -22016 as int16, i.e.
	// hi=0xAA, lo=0x00). The reader must not mistake this payload byte
	// for a new start marker — it should only look for 0xAA at the
	// position where a marker is expected after consuming a full packet.
	w.Write([]byte{0xAA, 0xAA, 0x00, 0xAA, 0x00, 0x05})

	s1, err := r.Next()
	if err != nil {
		t.Fatalf("Next (1): %v", err)
	}
	var hiByte, loByte uint16 = 0xAA, 0x00
	wantRaw1 := int16(hiByte<<8 | loByte) // matches the decode in Next()
	want1 := float64(wantRaw1) * 0.0039
	if s1.Value < want1-1e-9 || s1.Value > want1+1e-9 {
		t.Errorf("packet 1 Value = %v, want %v", s1.Value, want1)
	}

	s2, err := r.Next()
	if err != nil {
		t.Fatalf("Next (2): %v", err)
	}
	want2 := 5.0 * 0.0039
	if s2.Value < want2-1e-9 || s2.Value > want2+1e-9 {
		t.Errorf("packet 2 Value = %v, want %v", s2.Value, want2)
	}
}

func TestSerialReader_LongGarbageRunDoesNotHang(t *testing.T) {
	r, w := newTestReader(t)

	// More garbage than the internal buffer (256 bytes) can hold, with no
	// 0xAA anywhere in it, followed by one real packet. Before the fix,
	// this scenario could leave bufLen pinned at len(r.buf) and cause
	// Read to be called with a zero-length slice — see
	// TestBufferFillWithZeroLengthSlice in the bug-repro commit for the
	// isolated demonstration of why that hangs.
	garbage := make([]byte, 1000)
	for i := range garbage {
		garbage[i] = 0xFF
	}

	done := make(chan error, 1)
	go func() {
		w.Write(garbage)
		_, err := w.Write([]byte{0xAA, 0x00, 0x07})
		done <- err
	}()

	type result struct {
		s   Sample
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		s, err := r.Next()
		resultCh <- result{s, err}
	}()

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("Next: %v", res.err)
		}
		want := 7.0 * 0.0039
		if res.s.Value < want-1e-9 || res.s.Value > want+1e-9 {
			t.Errorf("Value = %v, want %v", res.s.Value, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Next() hung on a long garbage run — buffer overflow bug reproduced")
	}

	<-done
}

func TestSerialReader_ClosedPortReturnsError(t *testing.T) {
	r, w := newTestReader(t)
	w.Close() // closing the write end causes the read end to see EOF

	_, err := r.Next()
	if err == nil {
		t.Error("Next() on a closed pipe should return an error, got nil")
	}
}
