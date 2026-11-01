//go:build !linux

package sensor

import (
	"errors"
	"testing"
)

func TestOpenSerialReaderUnsupported(t *testing.T) {
	sr, err := OpenSerialReader("/dev/ttyUSB0", 230400, 1600)
	if sr != nil {
		t.Fatalf("expected nil reader, got %v", sr)
	}
	if !errors.Is(err, errSerialUnsupported) {
		t.Fatalf("expected errSerialUnsupported, got %v", err)
	}
}
