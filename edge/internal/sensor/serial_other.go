//go:build !linux

// serial_other.go — stub for platforms without termios serial support.
//
// The real implementation (serial_reader.go) is Linux-only. This stub keeps
// the whole module building on macOS and Windows for development: the edge
// engine still runs there with the stdin (JSONL) source, and asking for a
// hardware serial port fails with a clear error instead of a build failure.
package sensor

import (
	"errors"
	"fmt"
	"runtime"
)

// errSerialUnsupported is returned by every SerialReader operation on
// platforms without serial support.
var errSerialUnsupported = errors.New("serial input is not supported on this platform")

// SerialReader is a placeholder with the same method set as the Linux type.
type SerialReader struct{}

var _ SampleSource = (*SerialReader)(nil)

// OpenSerialReader always fails on this platform.
func OpenSerialReader(port string, baud, sampleRate int) (*SerialReader, error) {
	return nil, fmt.Errorf("%w (%s): serial ports are Linux-only; pipe JSONL samples via stdin instead",
		errSerialUnsupported, runtime.GOOS)
}

// Next always fails on this platform.
func (r *SerialReader) Next() (Sample, error) { return Sample{}, errSerialUnsupported }

// Close is a no-op.
func (r *SerialReader) Close() error { return nil }
