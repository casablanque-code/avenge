package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/envedit"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/pairing"
)

func readEnv(t *testing.T, root string) *envedit.File {
	t.Helper()
	f, err := envedit.Read(filepath.Join(root, "core", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

var pairRe = regexp.MustCompile(`avenge1\.[A-Za-z0-9_-]+\.[0-9a-f]{8}`)

func codeFrom(t *testing.T, out string) pairing.Info {
	t.Helper()
	m := pairRe.FindString(out)
	if m == "" {
		t.Fatalf("no pairing code in output:\n%s", out)
	}
	info, err := pairing.Decode(m)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestCheckPassword(t *testing.T) {
	for pw, ok := range map[string]bool{"abcd1234": true, "Abc12345": true, "changeme": false, "ChangeMe": false, "short1": false, "has space1": false, "dash-1234": false} {
		if (checkPassword(pw, 8) == nil) != ok {
			t.Errorf("checkPassword(%q) = %v, want ok=%v", pw, checkPassword(pw, 8), ok)
		}
	}
}

func TestGeneratePassword(t *testing.T) {
	r := &seqReader{}
	a, _ := generatePassword(r, 24)
	b, _ := generatePassword(r, 24)
	if len(a) != 24 || a == b {
		t.Fatalf("a=%q b=%q", a, b)
	}
	if err := checkPassword(a, 24); err != nil {
		t.Errorf("generated password fails the preflight rule: %v", err)
	}
}

func TestCoreSetupFresh(t *testing.T) {
	root := repoWithCore(t)
	// choose option 1 (the LAN address), Grafana password, do not start.
	h := newHarness(t, "1\nGrafanaPass9\nn\n")
	if err := coreSetup([]string{"--dir", root}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	f := readEnv(t, root)
	if v, _ := f.Get("MQTT_BIND"); v != "192.168.3.16" {
		t.Errorf("MQTT_BIND = %q", v)
	}
	if v, _ := f.Get("GRAFANA_ADMIN_PASSWORD"); v != "GrafanaPass9" {
		t.Errorf("grafana = %q", v)
	}
	seen := map[string]bool{}
	for _, k := range servicePasswords {
		v, _ := f.Get(k)
		if len(v) != 24 || checkPassword(v, 24) != nil || seen[v] {
			t.Errorf("%s = %q", k, v)
		}
		seen[v] = true
	}
	st, _ := os.Stat(filepath.Join(root, "core", ".env"))
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	info := codeFrom(t, h.out.String())
	edgePw, _ := f.Get("MQTT_EDGE_PASSWORD")
	if info.Host != "192.168.3.16" || info.Port != 1883 || info.User != "edge" || info.Password != edgePw {
		t.Errorf("pairing %+v", info)
	}
	if len(h.runs) != 0 {
		t.Errorf("docker ran although the answer was no: %v", h.runs)
	}
	// comments of the example survive
	b, _ := os.ReadFile(filepath.Join(root, "core", ".env"))
	mustContain(t, string(b), "# ── MQTT bind address")
}

func TestCoreSetupRerunKeepsPasswordsAndBacksUp(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "1\nGrafanaPass9\nn\n")
	if err := coreSetup([]string{"--dir", root}, h.env); err != nil {
		t.Fatal(err)
	}
	first := readEnv(t, root)
	h2 := newHarness(t, "\n\nn\n") // default bind (current), Enter keeps Grafana, no start
	if err := coreSetup([]string{"--dir", root}, h2.env); err != nil {
		t.Fatalf("%v\n%s", err, h2.out)
	}
	second := readEnv(t, root)
	for _, k := range append([]string{"GRAFANA_ADMIN_PASSWORD", "MQTT_BIND"}, servicePasswords...) {
		a, _ := first.Get(k)
		b, _ := second.Get(k)
		if a != b {
			t.Errorf("%s changed on re-run: %q -> %q", k, a, b)
		}
	}
	baks, _ := filepath.Glob(filepath.Join(root, "core", ".env.bak-*"))
	if len(baks) != 1 {
		t.Fatalf("backups: %v", baks)
	}
	mustContain(t, h2.out.String(), "5 kept")
}

func TestCoreSetupRegenerate(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "")
	args := []string{"--dir", root, "--yes", "--bind", "127.0.0.1", "--grafana-password", "GrafanaPass9"}
	if err := coreSetup(args, h.env); err != nil {
		t.Fatal(err)
	}
	a, _ := readEnv(t, root).Get("MQTT_EDGE_PASSWORD")
	h2 := newHarness(t, "")
	h2.env.Rand = &seqReader{n: 3}
	if err := coreSetup(append(args, "--regenerate"), h2.env); err != nil {
		t.Fatal(err)
	}
	b, _ := readEnv(t, root).Get("MQTT_EDGE_PASSWORD")
	if a == b {
		t.Error("--regenerate kept the old password")
	}
}

func TestCoreSetupNeedsTerminalOrYes(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	err := coreSetup([]string{"--dir", root}, h.env)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v", err)
	}
	if _, serr := os.Stat(filepath.Join(root, "core", ".env")); serr == nil {
		t.Error("a .env was written without answers")
	}
}

func TestCoreSetupYesGeneratesGrafanaAndPrintsIt(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	if err := coreSetup([]string{"--dir", root, "--yes"}, h.env); err != nil {
		t.Fatal(err)
	}
	g, _ := readEnv(t, root).Get("GRAFANA_ADMIN_PASSWORD")
	mustContain(t, h.out.String(), "Grafana admin password: "+g)
	if v, _ := readEnv(t, root).Get("MQTT_BIND"); v != "127.0.0.1" {
		t.Errorf("MQTT_BIND = %q", v)
	}
	if len(h.runs) != 0 {
		t.Error("--yes alone must not start the core")
	}
}

func TestCoreSetupWSLNeedsHostForPairing(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "")
	h.env.WSL = true
	h.env.Interactive = false
	if err := coreSetup([]string{"--dir", root, "--yes", "--bind", "0.0.0.0"}, h.env); err != nil {
		t.Fatal(err)
	}
	mustContain(t, h.out.String(), "No pairing code yet", "--host")
	h2 := newHarness(t, "")
	h2.env.Interactive = false
	if err := coreSetup([]string{"--dir", root, "--yes", "--bind", "0.0.0.0", "--host", "192.168.3.63"}, h2.env); err != nil {
		t.Fatal(err)
	}
	if info := codeFrom(t, h2.out.String()); info.Host != "192.168.3.63" {
		t.Errorf("host %q", info.Host)
	}
}

func TestCoreSetupWSLDefaultsToAllAddressesAndExplains(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "\nGrafanaPass9\nn\n192.168.3.63\n")
	h.env.WSL = true
	if err := coreSetup([]string{"--dir", root}, h.env); err != nil {
		t.Fatalf("%v\n%s", err, h.out)
	}
	if v, _ := readEnv(t, root).Get("MQTT_BIND"); v != "0.0.0.0" {
		t.Errorf("MQTT_BIND = %q", v)
	}
	mustContain(t, h.out.String(), "WSL detected", "Core inside WSL2", "Windows LAN IP")
	if info := codeFrom(t, h.out.String()); info.Host != "192.168.3.63" {
		t.Errorf("host %q", info.Host)
	}
}

func TestCoreSetupStartRunsCompose(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	if err := coreSetup([]string{"--dir", root, "--yes", "--start"}, h.env); err != nil {
		t.Fatal(err)
	}
	if len(h.runs) != 1 || h.runs[0].name != "docker" || h.runs[0].args[len(h.runs[0].args)-2] != "up" {
		t.Fatalf("runs: %+v", h.runs)
	}
}

func TestCoreSetupStartWithoutDocker(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	h.env.Look = func(string) (string, error) { return "", errBoom }
	if err := coreSetup([]string{"--dir", root, "--yes", "--start"}, h.env); err != nil {
		t.Fatal(err)
	}
	mustContain(t, h.out.String(), "docker is not installed")
	if len(h.runs) != 0 {
		t.Error("ran docker although it is missing")
	}
}

func TestCoreSetupComposeFailureKeepsEnv(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "")
	h.env.Interactive = false
	h.env.Run = func(string, string, ...string) error { return errBoom }
	err := coreSetup([]string{"--dir", root, "--yes", "--start"}, h.env)
	if err == nil || !strings.Contains(err.Error(), "the .env is written") {
		t.Fatalf("err = %v", err)
	}
	if _, serr := os.Stat(filepath.Join(root, "core", ".env")); serr != nil {
		t.Error(".env missing after a compose failure")
	}
}

func TestCoreSetupRejectsBadInput(t *testing.T) {
	root := repoWithCore(t)
	for name, args := range map[string][]string{
		"bind":      {"--yes", "--bind", "not-an-ip"},
		"grafana":   {"--yes", "--grafana-password", "changeme"},
		"retention": {"--yes", "--retention", "99999"},
		"both":      {"--yes", "--start", "--no-start"},
	} {
		h := newHarness(t, "")
		if err := coreSetup(append([]string{"--dir", root}, args...), h.env); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "core", ".env")); err == nil {
		t.Error("a .env appeared although every call was refused")
	}
}

func TestCoreSetupOutsideRepo(t *testing.T) {
	h := newHarness(t, "")
	err := coreSetup([]string{"--dir", t.TempDir(), "--yes"}, h.env)
	if err == nil || !strings.Contains(err.Error(), "core/compose.yaml") {
		t.Fatalf("err = %v", err)
	}
}

func TestCorePair(t *testing.T) {
	root := repoWithCore(t)
	h := newHarness(t, "")
	if err := coreSetup([]string{"--dir", root, "--yes", "--bind", "192.168.3.16"}, h.env); err != nil {
		t.Fatal(err)
	}
	h2 := newHarness(t, "")
	if err := corePair([]string{"--dir", root}, h2.env); err != nil {
		t.Fatal(err)
	}
	pw, _ := readEnv(t, root).Get("MQTT_EDGE_PASSWORD")
	if info := codeFrom(t, h2.out.String()); info.Host != "192.168.3.16" || info.Password != pw {
		t.Errorf("%+v", info)
	}
	// without a prepared .env it says what to do
	h3 := newHarness(t, "")
	if err := corePair([]string{"--dir", repoWithCore(t)}, h3.env); err == nil || !strings.Contains(err.Error(), "core setup") {
		t.Errorf("err = %v", err)
	}
}

func TestRunDispatch(t *testing.T) {
	h := newHarness(t, "")
	if code := run([]string{"--version"}, h.env); code != 0 || !strings.HasPrefix(h.out.String(), "avenge ") {
		t.Errorf("version: %d %q", code, h.out)
	}
	h = newHarness(t, "")
	if code := run([]string{"--help"}, h.env); code != 0 {
		t.Errorf("help code %d", code)
	}
	mustContain(t, h.out.String(), "avenge core setup", "avenge edge setup", "avenge doctor")
	h = newHarness(t, "")
	if code := run([]string{"bogus"}, h.env); code != 2 {
		t.Errorf("unknown: %d", code)
	}
	h = newHarness(t, "")
	if code := run([]string{"core", "setup", "--help"}, h.env); code != 0 {
		t.Errorf("setup --help: %d\n%s", code, h.errb)
	}
	mustContain(t, h.out.String(), "--grafana-password")
}
