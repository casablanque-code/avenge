//go:build linux

// serial_reader.go — reads the binary packet stream from a serial port.
//
// Packet format (from firmware, see RS-485.txt):
//
//	[0xAA][hi][lo]   — 3 bytes per sample
//	0xAA             — start marker
//	hi, lo           — int16 big-endian ADC value
//
// The reader reconstructs Sample.T from a monotonic counter driven by
// the configured sample rate — the microcontroller does not send timestamps.
//
// Build constraint: linux only (termios syscall). The rest of the codebase
// builds on all platforms via the stdin-based Reader.
package sensor

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// SerialReader reads the binary 3-byte packet stream from a serial port.
// It implements the same Next() interface as Reader so main.go can
// swap sources without any other changes.
type SerialReader struct {
	f          *os.File
	sampleRate int

	// Monotonic timestamp counter — incremented on every valid sample.
	sampleIdx int64
	startTime time.Time

	// Internal byte buffer for packet reassembly.
	buf [256]byte
	bufLen int
}

// OpenSerialReader opens the given port (e.g. "/dev/ttyUSB0") at the
// specified baud rate and returns a SerialReader ready to call Next().
func OpenSerialReader(port string, baud, sampleRate int) (*SerialReader, error) {
	f, err := os.OpenFile(port, os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("serial: open %s: %w", port, err)
	}

	// Switch to blocking I/O now that the port is open.
	if err := syscall.SetNonblock(int(f.Fd()), false); err != nil {
		f.Close()
		return nil, fmt.Errorf("serial: set blocking %s: %w", port, err)
	}

	if err := setTermios(f, baud); err != nil {
		f.Close()
		return nil, fmt.Errorf("serial: configure %s: %w", port, err)
	}

	return &SerialReader{
		f:          f,
		sampleRate: sampleRate,
		startTime:  time.Now(),
	}, nil
}

// Next blocks until the next valid sample arrives or an error occurs.
// It skips bytes until the 0xAA start marker is found, then reads
// the following two bytes as a big-endian int16.
func (r *SerialReader) Next() (Sample, error) {
	for {
		// Ensure we have at least 3 bytes in the buffer.
		for r.bufLen < 3 {
			n, err := r.f.Read(r.buf[r.bufLen:])
			if err != nil {
				return Sample{}, fmt.Errorf("serial read: %w", err)
			}
			r.bufLen += n
		}

		// Find start marker.
		if r.buf[0] != 0xAA {
			// Shift buffer forward by one byte to resync.
			copy(r.buf[:], r.buf[1:r.bufLen])
			r.bufLen--
			continue
		}

		// Valid packet — decode int16 big-endian.
		raw := int16(r.buf[1])<<8 | int16(r.buf[2])

		// Consume the packet.
		copy(r.buf[:], r.buf[3:r.bufLen])
		r.bufLen -= 3

		// Reconstruct monotonic timestamp from sample index.
		t := float64(r.sampleIdx) / float64(r.sampleRate)
		r.sampleIdx++

		// Normalise ADC value to g-force units.
		// ADXL345 at ±2g range: 1 LSB = 3.9 mg = 0.0039 g.
		// Adjust ScaleFactor for your sensor and range setting.
		const scaleFactor = 0.0039
		value := float64(raw) * scaleFactor

		return Sample{T: t, Value: value}, nil
	}
}

// Close releases the serial port.
func (r *SerialReader) Close() error {
	return r.f.Close()
}

// ── termios configuration ──────────────────────────────────────────────────
//
// Raw 8N1 mode — no line discipline, no flow control.
// Baud rate set via Ispeed/Ospeed directly (CBAUD not in Go's syscall package).

func setTermios(f *os.File, baud int) error {
	baudCode, err := baudRate(baud)
	if err != nil {
		return err
	}

	var t syscall.Termios

	// Raw mode: disable canonical, echo, signals.
	t.Lflag &^= syscall.ECHO | syscall.ECHOE | syscall.ECHOK |
		syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN

	// Disable input processing.
	t.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK |
		syscall.ISTRIP | syscall.IXON | syscall.IXOFF | syscall.IXANY

	// Disable output processing.
	t.Oflag &^= syscall.OPOST

	// 8N1: 8 data bits, no parity, 1 stop bit.
	t.Cflag &^= syscall.PARENB | syscall.CSTOPB | syscall.CSIZE
	t.Cflag |= syscall.CS8 | syscall.CREAD | syscall.CLOCAL

	// Baud rate via Ispeed/Ospeed (Linux termios2-compatible).
	t.Ispeed = baudCode
	t.Ospeed = baudCode

	// VMIN=1, VTIME=0: block until at least 1 byte is available.
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0

	// TCSETS = 0x5402 on Linux x86/ARM — apply settings immediately.
	const TCSETS = 0x5402
	if _, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		TCSETS,
		uintptr(unsafe.Pointer(&t)),
	); errno != 0 {
		return fmt.Errorf("ioctl TCSETS: %w", errno)
	}
	return nil
}

func baudRate(baud int) (uint32, error) {
	// Raw Linux baud constants (from ztypes_linux_*.go).
	// These values are identical across x86 and ARM64.
	switch baud {
	case 9600:
		return 0x0000000d, nil // B9600
	case 19200:
		return 0x0000000e, nil // B19200
	case 38400:
		return 0x0000000f, nil // B38400
	case 57600:
		return 0x00001001, nil // B57600
	case 115200:
		return 0x00001002, nil // B115200
	case 230400:
		return 0x00001003, nil // B230400
	case 460800:
		return 0x00001004, nil // B460800
	default:
		return 0, fmt.Errorf("serial: unsupported baud rate %d", baud)
	}
}
