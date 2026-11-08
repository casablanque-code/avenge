package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/envedit"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/pairing"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/probe"
)

func testCode(t *testing.T) string {
	t.Helper()
	c, err := pairing.Info{Host: "192.168.3.63", Port: 1883, User: "edge", Password: "EdgePw123"}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// edgeDir makes a directory shaped like an unpacked release: the binary's
// directory with .env.example next to it.
func edgeDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	ex, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, ".env.example"), ex, 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func readEdgeEnv(t *testing.T, dir string) *envedit.File {
	t.Helper()
	f, err := envedit.Read(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func get(t *testing.T, f *envedit.File, k string) string {
	t.Helper()
	v, _ := f.Get(k)
	return v
}

func TestSuggestRate(t *testing.T) {
	for in, want := range map[float64]int{1598: 1600, 1623: 1600, 1700: 1700, 802: 800, 3150: 3200, 1000: 1000, 1234: 1230} {
		if got := suggestRate(in); got != want {
			t.Errorf("suggestRate(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestEdgeEnvPath(t *testing.T) {
	d := t.TempDir()
	if got := edgeEnvPath(d); got != filepath.Join(d, ".env") {
		t.Errorf("release layout: %s", got)
	}
	os.MkdirAll(filepath.Join(d, "edge"), 0o755)
	os.WriteFile(filepath.Join(d, "edge", "compose.yaml"), []byte("x"), 0o644)
	if got := edgeEnvPath(d); got != filepath.Join(d, "edge", ".env") {
		t.Errorf("clone layout: %s", got)
	}
	os.WriteFile(filepath.Join(d, ".env"), []byte("A=1\n"), 0o600)
	if got := edgeEnvPath(d); got != filepath.Join(d, ".env") {
		t.Errorf("existing .env wins like edge-filter reads it: %s", got)
	}
	if trialDirFor(filepath.Join(d, "edge", ".env")) != d {
		t.Error("trial must start where edge-filter finds edge/.env")
	}
}

func TestScanSerial(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "serial", "by-id"), 0o755)
	for _, n := range []string{"ttyACM0", "ttyUSB0", "ttyUSB1"} {
		os.WriteFile(filepath.Join(root, n), nil, 0o600)
	}
	os.Symlink("../../ttyACM0", filepath.Join(root, "serial", "by-id", "usb-Arduino_Uno-if00"))
	got := scanSerial(root)
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	if !strings.HasSuffix(got[0].Path, "usb-Arduino_Uno-if00") || got[0].Desc != "-> ttyACM0" {
		t.Errorf("by-id first: %+v", got[0])
	}
	for _, p := range got[1:] {
		if strings.HasSuffix(p.Path, "ttyACM0") {
			t.Errorf("ttyACM0 listed twice: %+v", got)
		}
	}
}

func TestEdgeSetupVibrationInteractive(t *testing.T) {
	dir := edgeDir(t)
	h := newHarness(t, "\n\n\n\n\n\n"+testCode(t)+"\n")
	h.env.Ports = func() []serialPort {
		return []serialPort{{Path: "/dev/serial/by-id/usb-Arduino_Uno-if00", Desc: "-> ttyACM0"}}
	}
	if err := edgeSetup([]string{"--dir", dir}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	f := readEdgeEnv(t, dir)
	want := map[string]string{
		"SERIAL_INTERFACE":   "usb",
		"USB_PORT":           "/dev/serial/by-id/usb-Arduino_Uno-if00",
		"SENSOR_SAMPLE_RATE": "1600",
		"SENSOR_ID":          "bearing_01",
		"CENTRAL_HOST":       "192.168.3.63",
		"MQTT_PORT":          "1883",
		"MQTT_USER":          "edge",
		"MQTT_PASSWORD":      "EdgePw123",
	}
	for k, v := range want {
		if got := get(t, f, k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if _, on := f.Get("SENSOR_SOURCE"); on {
		t.Error("SENSOR_SOURCE active in vibration mode")
	}
	st, _ := os.Stat(filepath.Join(dir, ".env"))
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	mustContain(t, h.out.String(), "measured 1598 Hz -> 1600 Hz", "answers and the login works", "windows processed", "&& /opt/avenge/edge-filter")
	b, _ := os.ReadFile(filepath.Join(dir, ".env"))
	mustContain(t, string(b), "# ── Sensor tuning")
}

func TestEdgeSetupMicrophone(t *testing.T) {
	dir := edgeDir(t)
	h := newHarness(t, "1\n\n"+testCode(t)+"\n")
	if err := edgeSetup([]string{"--dir", dir, "--mode", "mic"}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	f := readEdgeEnv(t, dir)
	if get(t, f, "SENSOR_SOURCE") != "mic" || get(t, f, "AUDIO_DEVICE") != "plughw:CARD=Generic_1,DEV=0" {
		t.Errorf("mic settings: %q %q", get(t, f, "SENSOR_SOURCE"), get(t, f, "AUDIO_DEVICE"))
	}
	if get(t, f, "SENSOR_ID") != "mic_01" {
		t.Errorf("sensor id %q", get(t, f, "SENSOR_ID"))
	}
	if _, on := f.Get("SERIAL_INTERFACE"); on {
		t.Error("serial interface left active")
	}
	if _, on := f.Get("SENSOR_SAMPLE_RATE"); on {
		t.Error("SENSOR_SAMPLE_RATE left active; the audio profile must use 16000")
	}
	mustContain(t, h.out.String(), "automatic gain (AGC)")
}

func TestEdgeSetupSwitchModeCleansUp(t *testing.T) {
	dir := edgeDir(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	code := testCode(t)
	if err := edgeSetup([]string{"--dir", dir, "--yes", "--mode", "mic", "--device", "plughw:1,0", "--pair", code, "--no-trial"}, h.env); err != nil {
		t.Fatal(err)
	}
	h2 := newHarness(t, "")
	h2.env.Interactive = false
	if err := edgeSetup([]string{"--dir", dir, "--yes", "--mode", "vibration", "--interface", "usb", "--port", "/dev/ttyACM0", "--rate", "1600", "--no-trial"}, h2.env); err != nil {
		t.Fatalf("%v\n%s", err, h2.out)
	}
	f := readEdgeEnv(t, dir)
	for _, k := range []string{"SENSOR_SOURCE", "AUDIO_DEVICE"} {
		if _, on := f.Get(k); on {
			t.Errorf("%s still active after switching to vibration", k)
		}
	}
	if get(t, f, "SENSOR_SAMPLE_RATE") != "1600" || get(t, f, "CENTRAL_HOST") != "192.168.3.63" {
		t.Errorf("settings: rate %q host %q", get(t, f, "SENSOR_SAMPLE_RATE"), get(t, f, "CENTRAL_HOST"))
	}
	baks, _ := filepath.Glob(filepath.Join(dir, ".env.bak-*"))
	if len(baks) != 1 {
		t.Errorf("backups: %v", baks)
	}
}

func TestEdgeSetupFlagsOnly(t *testing.T) {
	dir := edgeDir(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	args := []string{"--dir", dir, "--yes", "--mode", "vibration", "--interface", "rs485", "--port", "/dev/ttyUSB0", "--baud", "115200", "--rate", "1600", "--sensor-id", "pump_3", "--pair", testCode(t), "--no-trial"}
	if err := edgeSetup(args, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	f := readEdgeEnv(t, dir)
	for k, v := range map[string]string{"SERIAL_INTERFACE": "rs485", "RS485_PORT": "/dev/ttyUSB0", "RS485_BAUD": "115200", "SENSOR_ID": "pump_3"} {
		if get(t, f, k) != v {
			t.Errorf("%s = %q, want %q", k, get(t, f, k), v)
		}
	}
	if strings.Contains(h.out.String(), "Trial run,") {
		t.Error("--no-trial ran the trial")
	}
}

func TestEdgeSetupYesNeedsLink(t *testing.T) {
	h := newHarness(t, "")
	h.env.Interactive = false
	err := edgeSetup([]string{"--dir", edgeDir(t), "--yes", "--mode", "mic", "--device", "plughw:1,0"}, h.env)
	if err == nil || !strings.Contains(err.Error(), "--pair") {
		t.Fatalf("err = %v", err)
	}
}

func TestEdgeSetupNeedsTerminalOrYes(t *testing.T) {
	h := newHarness(t, "")
	h.env.Interactive = false
	if err := edgeSetup([]string{"--dir", edgeDir(t)}, h.env); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v", err)
	}
}

func TestEdgeSetupBadPairCodeAsksAgain(t *testing.T) {
	dir := edgeDir(t)
	h := newHarness(t, "1\n\navenge1.garbage.00000000\n"+testCode(t)+"\n")
	if err := edgeSetup([]string{"--dir", dir, "--mode", "mic"}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	mustContain(t, h.out.String(), "✗")
	if get(t, readEdgeEnv(t, dir), "MQTT_PASSWORD") != "EdgePw123" {
		t.Error("the good code was not used")
	}
}

func TestEdgeSetupManualLink(t *testing.T) {
	dir := edgeDir(t)
	h := newHarness(t, "1\n\n\n10.0.0.5\nSecretPw42\n")
	if err := edgeSetup([]string{"--dir", dir, "--mode", "mic"}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	f := readEdgeEnv(t, dir)
	if get(t, f, "CENTRAL_HOST") != "10.0.0.5" || get(t, f, "MQTT_PASSWORD") != "SecretPw42" {
		t.Errorf("manual link: %q %q", get(t, f, "CENTRAL_HOST"), get(t, f, "MQTT_PASSWORD"))
	}
}

func TestEdgeSetupRerunKeepsLink(t *testing.T) {
	dir := edgeDir(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	if err := edgeSetup([]string{"--dir", dir, "--yes", "--mode", "mic", "--device", "plughw:1,0", "--pair", testCode(t), "--no-trial"}, h.env); err != nil {
		t.Fatal(err)
	}
	// second run, interactive: mic (current), microphone default, sensor name, keep link
	h2 := newHarness(t, "\n\n\n\n")
	if err := edgeSetup([]string{"--dir", dir, "--no-trial"}, h2.env); err != nil {
		t.Fatalf("%v\n%s", err, h2.out)
	}
	mustContain(t, h2.out.String(), "Keep the current link to the core (192.168.3.63:1883)")
	if get(t, readEdgeEnv(t, dir), "MQTT_PASSWORD") != "EdgePw123" {
		t.Error("link changed on a plain re-run")
	}
}

func TestEdgeSetupLinkFailureStillSavesAndExplains(t *testing.T) {
	dir := edgeDir(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	h.env.Probe = func(string, int, string, string) probe.Result {
		return probe.Result{Kind: probe.Timeout, Err: errBoom}
	}
	err := edgeSetup([]string{"--dir", dir, "--yes", "--mode", "mic", "--device", "plughw:1,0", "--pair", testCode(t), "--no-trial"}, h.env)
	if err == nil {
		t.Fatal("a failed link must not end as success")
	}
	mustContain(t, h.out.String(), "ip route get 192.168.3.63", "avenge doctor")
	if get(t, readEdgeEnv(t, dir), "CENTRAL_HOST") != "192.168.3.63" {
		t.Error(".env not saved")
	}
}

func TestEdgeSetupVibrationRefusedOffLinux(t *testing.T) {
	h := newHarness(t, "")
	h.env.Interactive = false
	h.env.GOOS = "windows"
	err := edgeSetup([]string{"--dir", edgeDir(t), "--yes", "--mode", "vibration", "--pair", testCode(t)}, h.env)
	if err == nil || !strings.Contains(err.Error(), "Linux only") {
		t.Fatalf("err = %v", err)
	}
}

func TestEdgeSetupSerialProblemsAreExplained(t *testing.T) {
	dir := edgeDir(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	h.env.OpenCheck = func(string) error { return os.ErrPermission }
	if err := edgeSetup([]string{"--dir", dir, "--yes", "--mode", "vibration", "--interface", "usb", "--port", "/dev/ttyACM0", "--rate", "1600", "--pair", testCode(t), "--no-trial"}, h.env); err != nil {
		t.Fatal(err)
	}
	mustContain(t, h.out.String(), "usermod -aG dialout")
}

func TestEdgeSetupMeasureFailureFallsBack(t *testing.T) {
	dir := edgeDir(t)
	h := newHarness(t, "\n\n\n\n\n\n"+testCode(t)+"\n")
	h.env.Ports = func() []serialPort { return []serialPort{{Path: "/dev/ttyACM0", Desc: "no stable name"}} }
	h.env.Measure = func(string, int) (float64, error) { return 0, errBoom }
	if err := edgeSetup([]string{"--dir", dir, "--no-trial"}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	if get(t, readEdgeEnv(t, dir), "SENSOR_SAMPLE_RATE") != "1600" {
		t.Error("fallback is not the reference 1600")
	}
	mustContain(t, h.out.String(), "Using 1600 Hz")
}

func TestEdgeSetupNoEdgeFilterFound(t *testing.T) {
	h := newHarness(t, "")
	h.env.Interactive = false
	h.env.FindEdge = func() string { return "" }
	if err := edgeSetup([]string{"--dir", edgeDir(t), "--yes", "--mode", "mic", "--device", "plughw:1,0", "--pair", testCode(t)}, h.env); err != nil {
		t.Fatal(err)
	}
	mustContain(t, h.out.String(), "Trial run skipped")
}

func TestSummarizeAndReportTrial(t *testing.T) {
	h := newHarness(t, "")
	if !reportTrial(h.env, trialResult{Output: goodTrial}) {
		t.Errorf("good trial failed:\n%s", h.out)
	}
	mustContain(t, h.out.String(), "rate check: samples arrive at 15983 Hz", "2 windows processed, rms between 0.3693 and 0.5581")

	for name, tr := range map[string]trialResult{
		"mismatch": {Output: "win 1 rms=0.1 x\nRATE MISMATCH\nrate check: samples arrive at 8000 Hz, SENSOR_SAMPLE_RATE is 16000 - MISMATCH\n"},
		"early":    {Output: "SENSOR_SAMPLE_RATE is required for SERIAL_INTERFACE\n", Early: true, Err: errBoom},
		"silent":   {Output: "edge-filter  sensor=mic_01\n"},
	} {
		h := newHarness(t, "")
		if reportTrial(h.env, tr) {
			t.Errorf("%s: reported as passed:\n%s", name, h.out)
		}
	}
	h = newHarness(t, "")
	reportTrial(h.env, trialResult{Output: "SENSOR_SAMPLE_RATE is required for SERIAL_INTERFACE\n", Early: true, Err: errBoom})
	mustContain(t, h.out.String(), "SENSOR_SAMPLE_RATE is required")
}

func TestEdgeRunDispatch(t *testing.T) {
	h := newHarness(t, "")
	if code := run([]string{"edge", "setup", "--help"}, h.env); code != 0 {
		t.Errorf("code %d\n%s", code, h.errb)
	}
	mustContain(t, h.out.String(), "--pair", "--no-trial", "--interface")
	_ = time.Second
}

func TestEdgeSetupRefusesCoreDirectory(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	coreDir := filepath.Join(root, "core")
	err := edgeSetup([]string{"--dir", coreDir, "--yes", "--mode", "mic", "--device", "plughw:1,0", "--pair", testCode(t), "--no-trial"}, h.env)
	if err == nil || !strings.Contains(err.Error(), "core directory") {
		t.Fatalf("err = %v", err)
	}
	if _, serr := os.Stat(filepath.Join(coreDir, ".env")); serr == nil {
		t.Error("a .env was written into the core directory")
	}
}

func TestEdgeSetupRefusesCoreEnvFile(t *testing.T) {
	dir := edgeDir(t)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("GRAFANA_ADMIN_PASSWORD=abc12345\nMQTT_BIND=127.0.0.1\n"), 0o600)
	h := newHarness(t, "")
	h.env.Interactive = false
	err := edgeSetup([]string{"--dir", dir, "--yes", "--mode", "mic", "--device", "plughw:1,0", "--pair", testCode(t), "--no-trial"}, h.env)
	if err == nil || !strings.Contains(err.Error(), "core .env") {
		t.Fatalf("err = %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if strings.Contains(string(b), "SENSOR_SOURCE") {
		t.Error("sensor settings were written into the core .env")
	}
}

func TestDoctorInsideCoreDirChecksOnlyCore(t *testing.T) {
	root, h := coreFixture(t, "mic_01\t1\t2026-11-08 14:00:01.000\n")
	coreDir := filepath.Join(root, "core")
	if err := doctor([]string{"--dir", coreDir}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	if strings.Contains(h.out.String(), "Edge (") {
		t.Errorf("the core .env was checked as an edge one:\n%s", h.out)
	}
	h2 := newHarness(t, "")
	if err := doctor([]string{"edge", "--dir", coreDir}, h2.env); err == nil || !strings.Contains(err.Error(), "core directory") {
		t.Errorf("doctor edge in core/: %v", err)
	}
}

func TestEdgeSetupSaysWhereToRun(t *testing.T) {
	dir := edgeDir(t)
	cwd, _ := os.Getwd()
	h := newHarness(t, "")
	h.env.Interactive = false
	args := []string{"--dir", dir, "--yes", "--mode", "mic", "--device", "plughw:1,0", "--pair", testCode(t), "--no-trial"}
	if err := edgeSetup(args, h.env); err != nil {
		t.Fatal(err)
	}
	mustContain(t, h.out.String(), "cd "+dir+" && /opt/avenge/edge-filter")

	// started in the folder of the .env: no cd needed
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })
	h2 := newHarness(t, "")
	h2.env.Interactive = false
	if err := edgeSetup(append([]string{"--dir", "."}, args[2:]...), h2.env); err != nil {
		t.Fatalf("%v\n%s", err, h2.out)
	}
	if strings.Contains(h2.out.String(), "cd ") {
		t.Errorf("cd printed although already in place:\n%s", h2.out)
	}
}
