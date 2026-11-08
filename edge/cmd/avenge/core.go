package main

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/envedit"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/netinfo"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/pairing"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/prompt"
)

// errHelp is returned after --help was printed.
var errHelp = errors.New("help shown")

const (
	mqttPort       = 1883
	mqttEdgeUser   = "edge"
	generatedChars = 24
	alnum          = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// servicePasswords are generated, never asked. Grafana's is asked, the
// person logs in with it.
var servicePasswords = []string{
	"CLICKHOUSE_PASSWORD",
	"CLICKHOUSE_TELEGRAF_PASSWORD",
	"CLICKHOUSE_GRAFANA_PASSWORD",
	"MQTT_EDGE_PASSWORD",
	"MQTT_TELEGRAF_PASSWORD",
}

const grafanaKey = "GRAFANA_ADMIN_PASSWORD"

func realEnv() *env {
	return &env{
		In:          os.Stdin,
		Out:         os.Stdout,
		Err:         os.Stderr,
		Interactive: prompt.IsTerminal(os.Stdin),
		Now:         timeNow,
		Rand:        rand.Reader,
		LAN:         netinfo.LocalLAN,
		WSL:         netinfo.IsWSL(),
		GOOS:        runtime.GOOS,
		Look:        exec.LookPath,
		Probe:       realProbe,
		Ports:       realPorts,
		OpenCheck:   realOpenCheck,
		Measure:     realMeasure,
		ListAudio:   realListAudio,
		FindEdge:    realFindEdge,
		Trial:       realTrial,
		DockerPS:    realDockerPS,
		IPRoute:     realIPRoute,
		ClickHouse:  "http://127.0.0.1:8123",
		GrafanaURL:  "http://127.0.0.1",
		HTTP:        &http.Client{Timeout: 5 * time.Second},
		Run: func(dir, name string, args ...string) error {
			c := exec.Command(name, args...)
			c.Dir = dir
			c.Stdout, c.Stderr = os.Stdout, os.Stderr
			return c.Run()
		},
	}
}

// password rules mirror the preflight container: letters and digits only,
// and not the placeholder.
func checkPassword(pw string, minLen int) error {
	if strings.EqualFold(pw, "changeme") {
		return errors.New("that is the placeholder, pick a real password")
	}
	if len(pw) < minLen {
		return fmt.Errorf("at least %d characters", minLen)
	}
	for _, r := range pw {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return errors.New("letters and digits only")
		}
	}
	return nil
}

// generatePassword returns n random letters and digits, without modulo bias.
func generatePassword(r io.Reader, n int) (string, error) {
	out := make([]byte, 0, n)
	buf := make([]byte, 64)
	for len(out) < n {
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < 248 { // 248 = 62*4: keeps every character equally likely
				out = append(out, alnum[int(b)%len(alnum)])
				if len(out) == n {
					break
				}
			}
		}
	}
	return string(out), nil
}

// findCoreDir accepts the repository root, the core directory itself, or a
// directory that holds compose.yaml.
func findCoreDir(dir string) (string, error) {
	for _, c := range []string{filepath.Join(dir, "core"), dir} {
		if _, err := os.Stat(filepath.Join(c, "compose.yaml")); err == nil {
			if _, err := os.Stat(filepath.Join(c, "Caddyfile")); err == nil {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("no core/compose.yaml under %q: run this from a clone of the repository, or pass --dir", dir)
}

type coreFlags struct {
	dir, bind, host, grafana string
	retention                int
	regenerate, yes          bool
	start, noStart           bool
}

func coreSetup(args []string, e *env) error {
	var f coreFlags
	fs := flag.NewFlagSet("avenge core setup", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	fs.StringVar(&f.dir, "dir", ".", "repository root (or the core directory)")
	fs.StringVar(&f.bind, "bind", "", "address Mosquitto listens on (MQTT_BIND)")
	fs.StringVar(&f.host, "host", "", "address edge devices use to reach this core (for the pairing code)")
	fs.StringVar(&f.grafana, "grafana-password", "", "Grafana admin password (letters and digits)")
	fs.IntVar(&f.retention, "retention", 0, "days of data to keep (RETENTION_DAYS, 1-3650)")
	fs.BoolVar(&f.regenerate, "regenerate", false, "make new service passwords even if valid ones exist (edge devices must be paired again)")
	fs.BoolVar(&f.yes, "yes", false, "do not ask; take flags and defaults")
	fs.BoolVar(&f.start, "start", false, "start the core when done")
	fs.BoolVar(&f.noStart, "no-start", false, "do not start the core")
	fs.Usage = func() {
		fmt.Fprint(e.Out, "usage: avenge core setup [flags]\n\nAsks the bind address and the Grafana password, generates the other passwords,\nwrites core/.env (mode 0600, the old file is kept as .env.bak-<time>), offers to\nstart the core and prints the pairing code for the edge devices.\n\n")
		printFlags(e.Out, fs)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return err
	}
	if f.start && f.noStart {
		return errors.New("--start and --no-start together make no sense")
	}
	if !f.yes && !e.Interactive {
		return errors.New("this needs a terminal to ask its questions; run it in one, or give the answers as flags and add --yes")
	}

	coreDir, err := findCoreDir(f.dir)
	if err != nil {
		return err
	}
	envPath := filepath.Join(coreDir, ".env")
	cur, err := envedit.Read(envPath)
	if err != nil {
		return err
	}
	hadEnv := cur.String() != ""
	file := cur
	if !hadEnv {
		file, err = envedit.Read(filepath.Join(coreDir, ".env.example"))
		if err != nil || file.String() == "" {
			return fmt.Errorf("%s/.env.example not found: run this from a clone of the repository", coreDir)
		}
	}
	in := e.ask()
	fmt.Fprintf(e.Out, "AVENGE core setup  (%s)\n\n", envPath)

	// ── bind address ─────────────────────────────────────────
	bind := f.bind
	if bind == "" {
		bind, err = chooseBind(e, in, file, hadEnv, f.yes)
		if err != nil {
			return err
		}
	}
	if err := checkIPv4(bind, true); err != nil {
		return fmt.Errorf("MQTT bind address: %w", err)
	}
	if err := file.Set("MQTT_BIND", bind); err != nil {
		return err
	}

	// ── retention (flag only: the default is right for most) ───
	if f.retention != 0 {
		if f.retention < 1 || f.retention > 3650 {
			return errors.New("--retention must be between 1 and 3650")
		}
		file.Set("RETENTION_DAYS", strconv.Itoa(f.retention))
	}

	// ── passwords ────────────────────────────────────────────
	var generated []string
	gpw, gGenerated, err := grafanaPassword(e, in, file, hadEnv, f)
	if err != nil {
		return err
	}
	file.Set(grafanaKey, gpw)
	if gGenerated {
		generated = append(generated, fmt.Sprintf("Grafana admin password: %s   (login: admin)", gpw))
	}
	kept := 0
	for _, k := range servicePasswords {
		if v, ok := file.Get(k); ok && hadEnv && checkPassword(v, 1) == nil && !f.regenerate {
			kept++
			continue
		}
		pw, err := generatePassword(e.Rand, generatedChars)
		if err != nil {
			return err
		}
		file.Set(k, pw)
	}

	// ── write ───────────────────────────────────────────────
	backup := ""
	if hadEnv {
		if backup, err = envedit.Backup(envPath, e.Now()); err != nil {
			return fmt.Errorf("could not back up the old .env: %w", err)
		}
	}
	if err := file.Write(envPath); err != nil {
		return err
	}
	lines := []string{
		"file:       " + envPath + "  (mode 0600)",
		"MQTT_BIND:  " + bind,
		fmt.Sprintf("passwords:  %d generated (%d characters), %d kept", len(servicePasswords)-kept, generatedChars, kept),
	}
	if v, ok := file.Get("RETENTION_DAYS"); ok {
		lines = append(lines, "retention:  "+v+" days")
	}
	if backup != "" {
		lines = append(lines, "old file:   "+backup)
	}
	fmt.Fprintln(e.Out)
	box(e.Out, "written", lines)
	for _, g := range generated {
		fmt.Fprintln(e.Out, g)
	}

	// ── start ────────────────────────────────────────────────
	started := false
	start := f.start
	if !f.start && !f.noStart && !f.yes {
		fmt.Fprintln(e.Out)
		if start, err = in.Confirm("Start the core now (docker compose up -d)?", true); err != nil {
			return err
		}
	}
	composeCmd := fmt.Sprintf("docker compose -f %s up -d", filepath.Join(coreDir, "compose.yaml"))
	if start {
		if _, lerr := e.Look("docker"); lerr != nil {
			fmt.Fprintf(e.Out, "\ndocker is not installed or not in PATH, so the core was not started.\nInstall Docker with Compose v2, then run: %s\n", composeCmd)
		} else if rerr := e.Run(".", "docker", "compose", "-f", filepath.Join(coreDir, "compose.yaml"), "up", "-d"); rerr != nil {
			return fmt.Errorf("docker compose failed: %w\n(the .env is written; fix the cause and run: %s)", rerr, composeCmd)
		} else {
			started = true
		}
	}
	if !started {
		fmt.Fprintf(e.Out, "\nTo start the core: %s\n", composeCmd)
	}

	// ── pairing code ─────────────────────────────────────────
	pw, _ := file.Get("MQTT_EDGE_PASSWORD")
	host, err := pairHost(e, in, bind, f.host, f.yes)
	if err != nil {
		fmt.Fprintf(e.Out, "\nNo pairing code yet: %v\nRun later: avenge core pair --host <address of this machine>\n", err)
		return nil
	}
	printPairing(e, host, pw)
	return nil
}

func chooseBind(e *env, in *prompt.Prompter, file *envedit.File, hadEnv, yes bool) (string, error) {
	lan := e.LAN()
	cur, _ := file.Get("MQTT_BIND")
	if !hadEnv || cur == "" {
		cur = ""
	}
	type opt struct{ label, value string }
	var opts []opt
	if e.WSL {
		opts = append(opts, opt{"0.0.0.0  (all addresses; this is what WSL2 needs behind a port forward)", "0.0.0.0"})
	}
	for _, a := range lan {
		opts = append(opts, opt{fmt.Sprintf("%s  (%s: an edge device on the LAN can connect)", a.IP, a.Iface), a.IP})
	}
	opts = append(opts, opt{"127.0.0.1  (this machine only: core and edge on one computer)", "127.0.0.1"})
	def := 0
	if cur != "" {
		found := false
		for i, o := range opts {
			if o.value == cur {
				def, found = i, true
			}
		}
		if !found {
			opts = append(opts, opt{cur + "  (current value)", cur})
			def = len(opts) - 1
		}
	}
	if yes {
		if cur != "" {
			return cur, nil
		}
		return "127.0.0.1", nil
	}
	if e.WSL {
		fmt.Fprintln(e.Out, "WSL detected: its network is NAT, so a device on the LAN cannot reach it directly.")
		fmt.Fprint(e.Out, "See \"Core inside WSL2\" in docs/OPERATIONS.md for the port forward from Windows.\n\n")
	}
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = o.label
	}
	i, err := in.Choose("Where should the broker (MQTT, port 1883) listen?", labels, def)
	if err != nil {
		return "", err
	}
	return opts[i].value, nil
}

func checkIPv4(s string, allowAny bool) error {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("%q is not an IPv4 address", s)
	}
	if ip.IsUnspecified() && !allowAny {
		return errors.New("0.0.0.0 is not an address a device can connect to")
	}
	return nil
}

func grafanaPassword(e *env, in *prompt.Prompter, file *envedit.File, hadEnv bool, f coreFlags) (pw string, generated bool, err error) {
	if f.grafana != "" {
		if err := checkPassword(f.grafana, 8); err != nil {
			return "", false, fmt.Errorf("--grafana-password: %w", err)
		}
		return f.grafana, false, nil
	}
	existing, _ := file.Get(grafanaKey)
	have := hadEnv && checkPassword(existing, 1) == nil
	if f.yes {
		if have {
			return existing, false, nil
		}
		pw, err = generatePassword(e.Rand, generatedChars)
		return pw, true, err
	}
	hint := "Enter generates one"
	if have {
		hint = "Enter keeps the current one"
	}
	ans, err := in.Ask(fmt.Sprintf("Grafana admin password, letters and digits, 8+ (%s)", hint), "", func(s string) error {
		if s == "" {
			return nil
		}
		return checkPassword(s, 8)
	})
	if err != nil {
		return "", false, err
	}
	switch {
	case ans != "":
		return ans, false, nil
	case have:
		return existing, false, nil
	}
	pw, err = generatePassword(e.Rand, generatedChars)
	return pw, true, err
}

// pairHost picks the address an edge device should use. A bind address that
// is a real address is that address; 0.0.0.0 and 127.0.0.1 need a decision.
func pairHost(e *env, in *prompt.Prompter, bind, flagHost string, yes bool) (string, error) {
	if flagHost != "" {
		if err := checkIPv4(flagHost, false); err != nil {
			if net.ParseIP(flagHost) == nil && !strings.ContainsAny(flagHost, " /") {
				return flagHost, nil // a host name is fine
			}
			return "", fmt.Errorf("--host: %w", err)
		}
		return flagHost, nil
	}
	switch bind {
	case "127.0.0.1":
		return "127.0.0.1", nil
	case "0.0.0.0":
		if yes || !e.Interactive {
			return "", errors.New("the core listens on all addresses, so --host must say which one the edge devices use")
		}
		def := ""
		if lan := e.LAN(); len(lan) > 0 {
			def = lan[0].IP
		}
		label := "Address edge devices use to reach this core"
		if e.WSL {
			label += " (the Windows LAN IP, not the WSL one)"
		}
		return in.Ask(label, def, func(s string) error { return checkIPv4(s, false) })
	}
	return bind, nil
}

func printPairing(e *env, host, edgePassword string) {
	code, err := pairing.Info{Host: host, Port: mqttPort, User: mqttEdgeUser, Password: edgePassword}.Encode()
	if err != nil {
		fmt.Fprintf(e.Out, "\nCould not build a pairing code: %v\n", err)
		return
	}
	// The code is printed bare, outside any frame, so a triple-click selects
	// exactly the code and nothing else.
	fmt.Fprintf(e.Out, "\nPairing code for the edge devices (copy the whole line):\n\n%s\n\n", code)
	fmt.Fprintln(e.Out, "On the edge device run: avenge edge setup   and paste it when asked.")
	fmt.Fprintln(e.Out, "It contains the MQTT password (encoded, not encrypted): handle it like a password.")
	fmt.Fprintln(e.Out, "Print it again any time: avenge core pair")
}

func corePair(args []string, e *env) error {
	var dir, host string
	var yes bool
	fs := flag.NewFlagSet("avenge core pair", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	fs.StringVar(&dir, "dir", ".", "repository root (or the core directory)")
	fs.StringVar(&host, "host", "", "address edge devices use to reach this core")
	fs.BoolVar(&yes, "yes", false, "do not ask")
	fs.Usage = func() {
		fmt.Fprint(e.Out, "usage: avenge core pair [flags]\n\nPrints the pairing code built from core/.env.\n\n")
		printFlags(e.Out, fs)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return err
	}
	coreDir, err := findCoreDir(dir)
	if err != nil {
		return err
	}
	file, err := envedit.Read(filepath.Join(coreDir, ".env"))
	if err != nil {
		return err
	}
	pw, ok := file.Get("MQTT_EDGE_PASSWORD")
	if !ok || checkPassword(pw, 1) != nil {
		return fmt.Errorf("%s/.env has no usable MQTT_EDGE_PASSWORD: run avenge core setup first", coreDir)
	}
	bind, _ := file.Get("MQTT_BIND")
	if bind == "" {
		bind = "127.0.0.1"
	}
	h, err := pairHost(e, e.ask(), bind, host, yes)
	if err != nil {
		return err
	}
	printPairing(e, h, pw)
	return nil
}
