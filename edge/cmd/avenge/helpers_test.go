package main

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/netinfo"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/probe"
)

const arecordList = `**** List of CAPTURE Hardware Devices ****
card 1: Generic_1 [HD-Audio Generic], device 0: ALC256 Analog [ALC256 Analog]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
`

// the shape of real edge-filter console output (see the E-004 run log)
const goodTrial = `edge-filter  sensor=mic_01  broker=192.168.3.63:1883
win   156  sensor=mic_01        [  OK  ]  rms=0.3693  score=+0.0σ(rms)  [flush sdt=1.1×]
rate check: samples arrive at 15983 Hz, SENSOR_SAMPLE_RATE is 16000 - ok
win   312  sensor=mic_01        [  OK  ]  rms=0.5581  score=+1.2σ(band)  [flush sdt=1.0×]
`

// seqReader gives bytes 0,1,2,... wrapping at 251: different on every call
// and fully deterministic.
type seqReader struct{ n byte }

func (r *seqReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.n
		r.n = (r.n + 7) % 251
	}
	return len(p), nil
}

type runCall struct {
	dir  string
	name string
	args []string
}

type harness struct {
	env  *env
	out  *bytes.Buffer
	errb *bytes.Buffer
	runs []runCall
}

func newHarness(t *testing.T, stdin string) *harness {
	t.Helper()
	h := &harness{out: &bytes.Buffer{}, errb: &bytes.Buffer{}}
	h.env = &env{
		In:          strings.NewReader(stdin),
		Out:         h.out,
		Err:         h.errb,
		Interactive: true,
		Now:         func() time.Time { return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC) },
		Rand:        &seqReader{},
		LAN:         func() []netinfo.Addr { return []netinfo.Addr{{Iface: "wlp1s0", IP: "192.168.3.16"}} },
		GOOS:        "linux",
		Look:        func(string) (string, error) { return "/usr/bin/docker", nil },
		Run: func(dir, name string, args ...string) error {
			h.runs = append(h.runs, runCall{dir, name, args})
			return nil
		},
		Probe:     func(string, int, string, string) probe.Result { return probe.Result{Kind: probe.OK} },
		Ports:     func() []serialPort { return nil },
		OpenCheck: func(string) error { return nil },
		Measure:   func(string, int) (float64, error) { return 1598, nil },
		ListAudio: func(string) (string, error) { return arecordList, nil },
		FindEdge:  func() string { return "/opt/avenge/edge-filter" },
		DockerPS:  func() (string, error) { return goodPS, nil },
		IPRoute:   func(string) (string, error) { return "192.168.3.63 dev wlp1s0 src 192.168.3.16 uid 0", nil },
		HTTP:      &http.Client{Timeout: time.Second},
		Trial: func(string, string, time.Duration) trialResult {
			return trialResult{Output: goodTrial}
		},
	}
	return h
}

// repoWithCore builds a minimal checkout: core/compose.yaml, core/Caddyfile and
// the real core/.env.example.
func repoWithCore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	core := filepath.Join(root, "core")
	if err := os.MkdirAll(core, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"compose.yaml", "Caddyfile"} {
		if err := os.WriteFile(filepath.Join(core, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ex, err := os.ReadFile("../../../core/.env.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(core, ".env.example"), ex, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func mustContain(t *testing.T, got string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(got, p) {
			t.Errorf("output lacks %q:\n%s", p, got)
		}
	}
}

var errBoom = errors.New("boom")

const goodPS = "sm_preflight\tExited (0) 5 minutes ago\n" +
	"sm_mosquitto\tUp 5 minutes (healthy)\n" +
	"sm_clickhouse\tUp 5 minutes (healthy)\n" +
	"sm_clickhouse_init\tExited (0) 5 minutes ago\n" +
	"sm_telegraf\tUp 5 minutes\n" +
	"sm_grafana\tUp 5 minutes (healthy)\n" +
	"sm_caddy\tUp 5 minutes\n"
