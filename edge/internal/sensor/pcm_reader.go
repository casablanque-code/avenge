package sensor

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// PCMReader turns raw 16-bit little-endian mono PCM into samples, so any
// audio capture tool can feed the edge engine through a pipe:
//
//	arecord -q -f S16_LE -r 16000 -c 1 -t raw | edge-filter
//	ffmpeg -i input.wav -f s16le -ac 1 -ar 16000 - | edge-filter
//
// Values are scaled to [-1, 1) (divided by 32768). Like SerialReader, the
// reader has no clock of its own: timestamps are derived from the sample
// counter and the configured sample rate, so SENSOR_SAMPLE_RATE must equal
// the real rate of the stream.
type PCMReader struct {
	r          *bufio.Reader
	sampleRate float64
	n          int64
	buf        [2]byte
}

// NewPCMReader reads s16le mono samples from r at the given sample rate.
func NewPCMReader(r io.Reader, sampleRate int) *PCMReader {
	return &PCMReader{r: bufio.NewReaderSize(r, 64*1024), sampleRate: float64(sampleRate)}
}

// Next returns the next sample, or io.EOF at the end of the stream. A
// trailing odd byte (a truncated last sample) is ignored.
func (p *PCMReader) Next() (Sample, error) {
	if _, err := io.ReadFull(p.r, p.buf[:]); err != nil {
		if err == io.ErrUnexpectedEOF {
			return Sample{}, io.EOF
		}
		if err == io.EOF {
			return Sample{}, io.EOF
		}
		return Sample{}, fmt.Errorf("pcm read: %w", err)
	}
	v := int16(binary.LittleEndian.Uint16(p.buf[:]))
	s := Sample{T: float64(p.n) / p.sampleRate, Value: float64(v) / 32768}
	p.n++
	return s, nil
}

var _ SampleSource = (*PCMReader)(nil)
