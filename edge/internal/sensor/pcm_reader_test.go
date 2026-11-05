package sensor

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"
)

func pcmBytes(vals ...int16) []byte {
	var b bytes.Buffer
	for _, v := range vals {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	return b.Bytes()
}

func TestPCMReaderValuesAndTimestamps(t *testing.T) {
	r := NewPCMReader(bytes.NewReader(pcmBytes(0, 16384, -16384, 32767, -32768)), 8000)
	want := []float64{0, 0.5, -0.5, 32767.0 / 32768, -1}
	for i, w := range want {
		s, err := r.Next()
		if err != nil {
			t.Fatalf("sample %d: %v", i, err)
		}
		if math.Abs(s.Value-w) > 1e-12 {
			t.Fatalf("sample %d: value %v, want %v", i, s.Value, w)
		}
		if wantT := float64(i) / 8000; math.Abs(s.T-wantT) > 1e-12 {
			t.Fatalf("sample %d: t %v, want %v", i, s.T, wantT)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("expected io.EOF at the end, got %v", err)
	}
}

func TestPCMReaderIgnoresTruncatedLastSample(t *testing.T) {
	data := append(pcmBytes(100, 200), 0x7f) // one stray byte
	r := NewPCMReader(bytes.NewReader(data), 16000)
	for i := 0; i < 2; i++ {
		if _, err := r.Next(); err != nil {
			t.Fatalf("sample %d: %v", i, err)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("expected io.EOF after a truncated sample, got %v", err)
	}
}

func TestPCMReaderEmptyStream(t *testing.T) {
	if _, err := NewPCMReader(bytes.NewReader(nil), 16000).Next(); err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}
