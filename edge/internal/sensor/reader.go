// Package sensor reads vibration samples from an io.Reader.
//
// Each line must be a JSON object with fields:
//   {"t": 0.001, "value": 1.234, "anomaly": false}
//
// In production this will be replaced by a serial/SPI reader
// talking to the ESP32 firmware. The interface stays identical.
package sensor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Sample is one ADC reading from the vibration sensor.
type Sample struct {
	T        float64 `json:"t"`
	Value    float64 `json:"value"`
	// GroundTruth is set by the Python generator for testing only.
	// Real hardware never sends this field.
	GroundTruth bool `json:"anomaly"`
}

// Reader streams samples from an io.Reader line by line.
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader wraps r in a buffered line scanner.
func NewReader(r io.Reader) *Reader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	return &Reader{scanner: sc}
}

// Next returns the next sample, or (zero, io.EOF) when the stream ends.
func (r *Reader) Next() (Sample, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return Sample{}, fmt.Errorf("sensor read: %w", err)
		}
		return Sample{}, io.EOF
	}
	var s Sample
	if err := json.Unmarshal(r.scanner.Bytes(), &s); err != nil {
		return Sample{}, fmt.Errorf("sensor parse: %w", err)
	}
	return s, nil
}

// ReadAll drains r into a slice. Useful for batch processing in tests.
func ReadAll(r io.Reader) ([]Sample, error) {
	rd := NewReader(r)
	var out []Sample
	for {
		s, err := rd.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, s)
	}
}
