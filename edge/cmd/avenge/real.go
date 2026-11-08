package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/capture"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/probe"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

// The functions below touch the real machine (serial ports, arecord, a child
// process); they are the part the unit tests replace and the part a hardware
// run has to prove.

func realProbe(host string, port int, user, password string) probe.Result {
	return probe.Broker(host, port, user, password, 5*time.Second)
}

func realPorts() []serialPort { return scanSerial("/dev") }

// realOpenCheck opens the device the way the engine will, to catch a missing
// device or a missing dialout membership now and not at the first run.
func realOpenCheck(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	return f.Close()
}

// realMeasure counts the packets of the sensor for a few seconds. The first
// second is skipped (bytes that were already waiting in the buffer).
func realMeasure(port string, baud int) (float64, error) {
	r, err := sensor.OpenSerialReader(port, baud, defaultRate)
	if err != nil {
		return 0, err
	}
	var count atomic.Int64
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer r.Close()
		for !stop.Load() {
			if _, err := r.Next(); err != nil {
				return
			}
			count.Add(1)
		}
	}()
	time.Sleep(1 * time.Second)
	c1 := count.Load()
	t1 := time.Now()
	time.Sleep(measureSeconds * time.Second)
	c2 := count.Load()
	elapsed := time.Since(t1).Seconds()
	stop.Store(true) // the reader goroutine ends at its next sample
	if c2 == 0 || c2 == c1 {
		return 0, errors.New("no data from the sensor in the measuring time: wrong device, wrong baud rate, or the sensor is not streaming")
	}
	return float64(c2-c1) / elapsed, nil
}

func realListAudio(goos string) (string, error) {
	name, args, err := capture.ListCommand(goos)
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath(name); err != nil {
		return "", fmt.Errorf("%s not found", name)
	}
	// arecord prints the list on stdout; ffmpeg prints it on stderr and exits non-zero.
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", err
	}
	return string(out), nil
}

// realFindEdge looks for edge-filter next to this binary, in the current
// directory, then in PATH.
func realFindEdge() string {
	exe := "edge-filter"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	var dirs []string
	if self, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(self))
	}
	dirs = append(dirs, ".")
	for _, d := range dirs {
		p := filepath.Join(d, exe)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			if abs, err := filepath.Abs(p); err == nil {
				return abs
			}
			return p
		}
	}
	if p, err := exec.LookPath(exe); err == nil {
		return p
	}
	return ""
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// realTrial runs edge-filter --no-mqtt for d in dir and stops it politely.
func realTrial(dir, bin string, d time.Duration) trialResult {
	cmd := exec.Command(bin, "--no-mqtt")
	cmd.Dir = dir
	var buf lockedBuf
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Start(); err != nil {
		return trialResult{Err: err, Early: true}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return trialResult{Output: buf.String(), Err: err, Early: true}
	case <-time.After(d):
	}
	if runtime.GOOS == "windows" {
		cmd.Process.Kill()
	} else {
		cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		<-done
	}
	return trialResult{Output: buf.String()}
}

func realDockerPS() (string, error) {
	out, err := exec.Command("docker", "ps", "-a", "--filter", "name=^sm_", "--format", "{{.Names}}\t{{.Status}}").CombinedOutput()
	return string(out), err
}

func realIPRoute(host string) (string, error) {
	out, err := exec.Command("ip", "route", "get", host).CombinedOutput()
	return string(out), err
}
