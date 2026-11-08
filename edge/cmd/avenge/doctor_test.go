package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/probe"
)

// coreFixture sets up a core .env and fake ClickHouse and Grafana servers.
func coreFixture(t *testing.T, rows string, bind ...string) (root string, h *harness) {
	t.Helper()
	root = repoWithCore(t)
	setup := newHarness(t, "")
	setup.env.Interactive = false
	b := "192.168.3.16"
	if len(bind) > 0 {
		b = bind[0]
	}
	if err := coreSetup([]string{"--dir", root, "--yes", "--bind", b, "--grafana-password", "GrafanaPass9"}, setup.env); err != nil {
		t.Fatal(err)
	}
	chPw, _ := readEnv(t, root).Get("CLICKHOUSE_PASSWORD")
	ch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "default" || p != chPw {
			http.Error(w, "Code: 516. Authentication failed", http.StatusUnauthorized)
			return
		}
		if !strings.Contains(r.URL.Query().Get("query"), "iot.telemetry") {
			http.Error(w, "bad query", 400)
			return
		}
		fmt.Fprint(w, rows)
	}))
	gf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "grafana.localhost" || r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"database":"ok"}`)
	}))
	t.Cleanup(ch.Close)
	t.Cleanup(gf.Close)
	h = newHarness(t, "")
	h.env.ClickHouse, h.env.GrafanaURL = ch.URL, gf.URL
	return root, h
}

func TestDoctorCoreAllGood(t *testing.T) {
	root, h := coreFixture(t, "mic_01\t120\t2026-11-08 14:00:01.000\n")
	if err := doctor([]string{"core", "--dir", root}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	mustContain(t, h.out.String(), "sm_mosquitto: Up 5 minutes (healthy)", "edge login works", "sensor mic_01, 120 in the last 5 min", "Grafana answers", "All good.")
	if strings.Contains(h.out.String(), "✗") {
		t.Errorf("a failure line in a healthy run:\n%s", h.out)
	}
}

func TestDoctorCoreProblems(t *testing.T) {
	root, h := coreFixture(t, "")
	h.env.DockerPS = func() (string, error) {
		return strings.Replace(strings.Replace(goodPS, "sm_telegraf\tUp 5 minutes\n", "", 1), "sm_mosquitto\tUp 5 minutes (healthy)", "sm_mosquitto\tUp 1 minute (unhealthy)", 1), nil
	}
	h.env.Probe = func(string, int, string, string) probe.Result {
		return probe.Result{Kind: probe.Login, Err: errBoom}
	}
	err := doctor([]string{"--dir", root}, h.env)
	if err == nil {
		t.Fatal("problems must give an error (exit status 1)")
	}
	mustContain(t, h.out.String(), "sm_telegraf: missing", "sm_mosquitto: Up 1 minute (unhealthy)", "MQTT_EDGE_PASSWORD", "no telemetry rows in the last 5 minutes", "problem(s)")
}

func TestDoctorCoreDockerDown(t *testing.T) {
	root, h := coreFixture(t, "")
	h.env.DockerPS = func() (string, error) { return "Cannot connect to the Docker daemon", errBoom }
	if err := doctor([]string{"core", "--dir", root}, h.env); err == nil {
		t.Fatal("expected failure")
	}
	mustContain(t, h.out.String(), "docker is not reachable", "Cannot connect to the Docker daemon")
}

func TestDoctorCoreClickHouseRejects(t *testing.T) {
	root, h := coreFixture(t, "")
	f, _ := os.ReadFile(filepath.Join(root, "core", ".env"))
	os.WriteFile(filepath.Join(root, "core", ".env"), []byte(strings.Replace(string(f), "CLICKHOUSE_PASSWORD=", "CLICKHOUSE_PASSWORD=Wrong", 1)), 0o600)
	if err := doctor([]string{"core", "--dir", root}, h.env); err == nil {
		t.Fatal("expected failure")
	}
	mustContain(t, h.out.String(), "ClickHouse query failed: HTTP 401")
}

func TestDoctorCoreWithoutEnv(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "")
	if err := doctor([]string{"core", "--dir", root}, h.env); err == nil {
		t.Fatal("expected failure")
	}
	mustContain(t, h.out.String(), "avenge core setup")
}

func TestDoctorCoreWarnsOnLoopbackBind(t *testing.T) {
	root, h := coreFixture(t, "mic_01\t1\t2026-11-08 14:00:01.000\n", "127.0.0.1")
	if err := doctor([]string{"core", "--dir", root}, h.env); err != nil {
		t.Fatalf("a loopback bind is a warning, got %v\n%s", err, h.out)
	}
	mustContain(t, h.out.String(), "MQTT_BIND is 127.0.0.1", "No problems, 1 warning(s).")
}

func writeEdge(t *testing.T, args ...string) string {
	t.Helper()
	dir := edgeDir(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	base := []string{"--dir", dir, "--yes", "--pair", testCode(t), "--no-trial"}
	if err := edgeSetup(append(base, args...), h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	return dir
}

func TestDoctorEdgeMicGood(t *testing.T) {
	dir := writeEdge(t, "--mode", "mic", "--device", "plughw:CARD=Generic_1,DEV=0")
	h := newHarness(t, "")
	if err := doctor([]string{"edge", "--dir", dir}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	mustContain(t, h.out.String(), "input: microphone", "microphone plughw:CARD=Generic_1,DEV=0 is present", "reachable and the login works", "All good.")
}

func TestDoctorEdgeVibrationGood(t *testing.T) {
	dir := writeEdge(t, "--mode", "vibration", "--interface", "usb", "--port", "/dev/ttyACM0", "--rate", "1600")
	h := newHarness(t, "")
	if err := doctor([]string{"--dir", dir}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	mustContain(t, h.out.String(), "input: serial sensor over usb", "/dev/ttyACM0 opens", "SENSOR_SAMPLE_RATE=1600 Hz")
}

func TestDoctorEdgeTimeoutShowsVPNRoute(t *testing.T) {
	dir := writeEdge(t, "--mode", "mic", "--device", "plughw:1,0")
	h := newHarness(t, "")
	h.env.Probe = func(string, int, string, string) probe.Result {
		return probe.Result{Kind: probe.Timeout, Err: errBoom}
	}
	h.env.IPRoute = func(string) (string, error) { return "192.168.3.63 dev amn0 table 51820 src 10.8.1.13", nil }
	if err := doctor([]string{"edge", "--dir", dir}, h.env); err == nil {
		t.Fatal("expected failure")
	}
	mustContain(t, h.out.String(), "route to the core goes through: amn0", "a VPN or tunnel interface", "ip route get 192.168.3.63")
}

func TestDoctorEdgeSerialProblems(t *testing.T) {
	dir := writeEdge(t, "--mode", "vibration", "--interface", "usb", "--port", "/dev/ttyACM0", "--rate", "1600")
	h := newHarness(t, "")
	h.env.OpenCheck = func(string) error { return os.ErrNotExist }
	if err := doctor([]string{"edge", "--dir", dir}, h.env); err == nil {
		t.Fatal("expected failure")
	}
	mustContain(t, h.out.String(), "/dev/ttyACM0 does not exist", "Plug the sensor in")

	// rate removed from .env
	b, _ := os.ReadFile(filepath.Join(dir, ".env"))
	os.WriteFile(filepath.Join(dir, ".env"), []byte(strings.Replace(string(b), "SENSOR_SAMPLE_RATE=1600", "# SENSOR_SAMPLE_RATE=1600", 1)), 0o600)
	h2 := newHarness(t, "")
	h2.env.OpenCheck = func(string) error { return os.ErrPermission }
	doctor([]string{"edge", "--dir", dir}, h2.env)
	mustContain(t, h2.out.String(), "permission denied", "dialout", "SENSOR_SAMPLE_RATE is not set")
}

func TestDoctorEdgeNotConfiguredLink(t *testing.T) {
	dir := edgeDir(t)
	ex, _ := os.ReadFile(filepath.Join(dir, ".env.example"))
	os.WriteFile(filepath.Join(dir, ".env"), ex, 0o600)
	h := newHarness(t, "")
	if err := doctor([]string{"edge", "--dir", dir}, h.env); err == nil {
		t.Fatal("expected failure")
	}
	mustContain(t, h.out.String(), "link to the core is not configured", "avenge core pair")
}

func TestDoctorEdgeMicNotInList(t *testing.T) {
	dir := writeEdge(t, "--mode", "mic", "--device", "plughw:CARD=Gone,DEV=0")
	h := newHarness(t, "")
	if err := doctor([]string{"edge", "--dir", dir}, h.env); err != nil {
		t.Fatalf("a missing microphone is a warning in the list check, got %v", err)
	}
	mustContain(t, h.out.String(), "is not in the list of microphones")
}

func TestDoctorNothingToCheckAndBadArgument(t *testing.T) {
	h := newHarness(t, "")
	if err := doctor([]string{"--dir", t.TempDir()}, h.env); err == nil || !strings.Contains(err.Error(), "nothing to check") {
		t.Errorf("err = %v", err)
	}
	if err := doctor([]string{"banana"}, h.env); err == nil {
		t.Error("bad argument accepted")
	}
	if err := doctor([]string{"edge", "--dir", t.TempDir()}, h.env); err == nil || !strings.Contains(err.Error(), "avenge edge setup") {
		t.Errorf("err = %v", err)
	}
}

func TestDoctorChecksBothWhenBothExist(t *testing.T) {
	root, h := coreFixture(t, "")
	envDir := filepath.Join(root, "edge")
	os.MkdirAll(envDir, 0o755)
	os.WriteFile(filepath.Join(envDir, "compose.yaml"), []byte("x"), 0o644)
	ex, _ := os.ReadFile("../../.env.example")
	os.WriteFile(filepath.Join(envDir, ".env.example"), ex, 0o644)
	setup := newHarness(t, "")
	setup.env.Interactive = false
	if err := edgeSetup([]string{"--dir", root, "--yes", "--mode", "mic", "--device", "plughw:1,0", "--pair", testCode(t), "--no-trial"}, setup.env); err != nil {
		t.Fatalf("%v\n%s", err, setup.out)
	}
	doctor([]string{"--dir", root}, h.env)
	mustContain(t, h.out.String(), "Core (", "Edge (")
}

func TestRouteDev(t *testing.T) {
	if routeDev("192.168.3.63 dev wlp1s0 src 192.168.3.16 uid 1000") != "wlp1s0" || routeDev("garbage") != "" {
		t.Error("routeDev")
	}
	if !isVirtualIface("tailscale0") || isVirtualIface("wlp1s0") || isVirtualIface("eth0") {
		t.Error("isVirtualIface")
	}
}
