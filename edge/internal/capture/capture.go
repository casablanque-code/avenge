// Package capture builds and runs the command that records a microphone as
// raw PCM (signed 16-bit little-endian, mono) on its standard output, which
// is what the engine's audio input reads. It uses the tool each platform
// already has: arecord on Linux, ffmpeg on macOS and Windows. Nothing is
// hidden: the exact command line is shown by `edge-filter --explain`.
package capture

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Command returns the program and arguments that capture the microphone at
// rate Hz. device is the AUDIO_DEVICE setting; empty means the default
// device, except on Windows, which has no default and needs a name.
func Command(goos, device string, rate int) (string, []string, error) {
	if rate <= 0 {
		return "", nil, fmt.Errorf("invalid sample rate %d", rate)
	}
	if err := CheckDevice(goos, device); err != nil {
		return "", nil, err
	}
	r := strconv.Itoa(rate)
	switch goos {
	case "linux":
		args := []string{"-q", "-f", "S16_LE", "-r", r, "-c", "1", "-t", "raw"}
		if device != "" {
			args = append(args, "-D", device)
		}
		return "arecord", args, nil
	case "darwin":
		if device == "" {
			device = "default"
		}
		return "ffmpeg", []string{"-hide_banner", "-loglevel", "error", "-f", "avfoundation",
			"-i", ":" + device, "-ac", "1", "-ar", r, "-f", "s16le", "-"}, nil
	case "windows":
		if device == "" {
			return "", nil, errors.New("AUDIO_DEVICE is required on Windows: run `edge-filter --list-audio` and set the microphone name in edge/.env")
		}
		return "ffmpeg", []string{"-hide_banner", "-loglevel", "error", "-f", "dshow",
			"-i", "audio=" + device, "-ac", "1", "-ar", r, "-f", "s16le", "-"}, nil
	}
	return "", nil, fmt.Errorf("SENSOR_SOURCE=mic is not supported on %s; pipe raw PCM into edge-filter instead (see docs/OPERATIONS.md)", goos)
}

// ListCommand returns the command that lists the audio input devices.
func ListCommand(goos string) (string, []string, error) {
	switch goos {
	case "linux":
		return "arecord", []string{"-l"}, nil
	case "darwin":
		return "ffmpeg", []string{"-hide_banner", "-f", "avfoundation", "-list_devices", "true", "-i", ""}, nil
	case "windows":
		return "ffmpeg", []string{"-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy"}, nil
	}
	return "", nil, fmt.Errorf("listing audio devices is not supported on %s", goos)
}

// InstallHint says how to get the capture tool on this platform.
func InstallHint(goos string) string {
	switch goos {
	case "linux":
		return "install alsa-utils (sudo apt install alsa-utils)"
	case "darwin":
		return "install ffmpeg (brew install ffmpeg)"
	case "windows":
		return "install ffmpeg (winget install Gyan.FFmpeg) and make sure it is on PATH"
	}
	return "install the capture tool"
}

// Describe renders a command for display, quoting arguments with spaces.
func Describe(name string, args []string) string {
	parts := []string{name}
	for _, a := range args {
		if strings.ContainsAny(a, " \t\"") {
			a = strconv.Quote(a)
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

// Process is a running capture command.
type Process struct {
	// Out delivers the raw PCM the command writes.
	Out io.Reader
	cmd *exec.Cmd
	pr  *os.File

	once sync.Once
	err  error
}

// Start runs the command. Its standard error is passed through to ours.
// If the program is not installed the returned error wraps exec.ErrNotFound.
func Start(name string, args []string) (*Process, error) {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	pw.Close() // the child holds the write end; EOF arrives when it exits
	return &Process{Out: pr, cmd: cmd, pr: pr}, nil
}

// Wait waits for the command to exit and returns its exit error (nil when it
// exited cleanly). It may be called any number of times.
func (p *Process) Wait() error {
	p.once.Do(func() { p.err = p.cmd.Wait() })
	return p.err
}

// Kill stops the command and releases the pipe. It is safe to call twice.
func (p *Process) Kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.Wait()
	_ = p.pr.Close()
}

// Device is one audio input as the listing tool reports it, with the value to
// put into AUDIO_DEVICE.
type Device struct {
	Value string
	Desc  string
}

var (
	alsaLine   = regexp.MustCompile(`^card (\d+): (\S+) \[([^\]]*)\], device (\d+): (.*?) \[([^\]]*)\]`)
	dshowLine  = regexp.MustCompile(`^\[dshow @ [^\]]*\]\s+"(.+)" \(audio\)`)
	avfAudio   = regexp.MustCompile(`^\[AVFoundation indev @ [^\]]*\]\s+\[(\d+)\] (.+)$`)
	avfSection = "AVFoundation audio devices"
)

// ParseDevices extracts the input devices from the output of the listing
// command, so the user can copy a value that is known to work instead of
// guessing the name. The Linux form is tested against real arecord output;
// the macOS and Windows forms follow the documented ffmpeg output and still
// need a run on those systems.
func ParseDevices(goos, out string) []Device {
	var devs []Device
	switch goos {
	case "linux":
		for _, line := range strings.Split(out, "\n") {
			if m := alsaLine.FindStringSubmatch(line); m != nil {
				devs = append(devs, Device{
					Value: fmt.Sprintf("plughw:CARD=%s,DEV=%s", m[2], m[4]),
					Desc:  fmt.Sprintf("%s, %s (same as plughw:%s,%s)", m[3], m[6], m[1], m[4]),
				})
			}
		}
	case "windows":
		for _, line := range strings.Split(out, "\n") {
			if m := dshowLine.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
				devs = append(devs, Device{Value: m[1], Desc: "DirectShow audio input"})
			}
		}
	case "darwin":
		inAudio := false
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimRight(line, "\r")
			if strings.Contains(line, avfSection) {
				inAudio = true
				continue
			}
			if m := avfAudio.FindStringSubmatch(line); m != nil && inAudio {
				devs = append(devs, Device{Value: m[1], Desc: m[2]})
			}
		}
	}
	return devs
}

// CheckDevice rejects AUDIO_DEVICE values that cannot be a device name, most
// often a whole line pasted from the listing.
func CheckDevice(goos, device string) error {
	if goos != "linux" || device == "" {
		return nil
	}
	if m := alsaLine.FindStringSubmatch(device); m != nil {
		return fmt.Errorf("AUDIO_DEVICE looks like a line copied from the list (%q); use plughw:CARD=%s,DEV=%s (or plughw:%s,%s) instead",
			device, m[2], m[4], m[1], m[4])
	}
	if strings.ContainsAny(device, "[]") || strings.HasPrefix(device, "card ") {
		return fmt.Errorf("AUDIO_DEVICE %q is not an ALSA device name; run `edge-filter --list-audio` and copy one of the printed values (for example plughw:1,0)", device)
	}
	return nil
}
