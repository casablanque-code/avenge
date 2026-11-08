package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/capture"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/envedit"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/probe"
)

type status int

const (
	stOK status = iota
	stWarn
	stFail
)

// report collects the lines of one doctor run.
type report struct {
	e           *env
	fails, warn int
}

func (r *report) line(s status, msg string, hint ...string) {
	mark := map[status]string{stOK: "✓", stWarn: "!", stFail: "✗"}[s]
	fmt.Fprintf(r.e.Out, "  %s %s\n", mark, msg)
	switch s {
	case stFail:
		r.fails++
	case stWarn:
		r.warn++
	}
	for _, h := range hint {
		if h != "" {
			fmt.Fprintln(r.e.Out, indent(h, "      "))
		}
	}
}

func (r *report) section(title string) { fmt.Fprintf(r.e.Out, "\n%s\n", title) }

func runDoctor(args []string, e *env) int {
	return reportErr(e, doctor(args, e))
}

func doctor(args []string, e *env) error {
	which := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		which, args = args[0], args[1:]
	}
	if which != "" && which != "core" && which != "edge" {
		return fmt.Errorf("doctor checks core, edge or both (no argument); %q is none of them", which)
	}
	var dir string
	fs := flag.NewFlagSet("avenge doctor", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	fs.StringVar(&dir, "dir", ".", "repository root, or the directory edge-filter runs in")
	fs.Usage = func() {
		fmt.Fprint(e.Out, "usage: avenge doctor [core|edge] [flags]\n\nChecks what is on this machine: the core (containers, the broker login, rows\nin the database, Grafana) and/or the edge (.env, sensor or microphone, the link\nto the core). Without an argument it checks whatever it finds. Exit status 1\nwhen something failed.\n\n")
		printFlags(e.Out, fs)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return err
	}
	coreDir, coreErr := findCoreDir(dir)
	edgePath := edgeEnvPath(dir)
	_, edgeStat := os.Stat(edgePath)
	hasEdge := edgeStat == nil
	if hasEdge {
		// a core .env is not an edge one (doctor run inside core/)
		if cf, rerr := envedit.Read(edgePath); rerr == nil && (isCoreDir(dir) || looksLikeCoreEnv(cf)) {
			hasEdge = false
		}
	}
	doCore := which == "core" || (which == "" && coreErr == nil)
	doEdge := which == "edge" || (which == "" && hasEdge)
	if which == "core" && coreErr != nil {
		return coreErr
	}
	if which == "edge" && !hasEdge {
		if isCoreDir(dir) {
			return edgeScopeError(dir, edgePath, nil)
		}
		return fmt.Errorf("no edge .env under %q: run avenge edge setup first", dir)
	}
	if !doCore && !doEdge {
		return errors.New("nothing to check here: no core/compose.yaml and no edge .env under " + dir + " (use --dir)")
	}
	r := &report{e: e}
	fmt.Fprintln(e.Out, "AVENGE doctor")
	if doCore {
		doctorCore(r, coreDir)
	}
	if doEdge {
		doctorEdge(r, edgePath)
	}
	fmt.Fprintln(e.Out)
	switch {
	case r.fails > 0:
		fmt.Fprintf(e.Out, "%d problem(s), %d warning(s). Fix the first ✗ and run avenge doctor again.\n", r.fails, r.warn)
		return errors.New("doctor found problems")
	case r.warn > 0:
		fmt.Fprintf(e.Out, "No problems, %d warning(s).\n", r.warn)
	default:
		fmt.Fprintln(e.Out, "All good.")
	}
	return nil
}

// ── core ────────────────────────────────────────────────────

var containerWant = []struct {
	name    string
	oneShot bool // exits with 0 by design
	health  bool
}{
	{"sm_preflight", true, false},
	{"sm_mosquitto", false, true},
	{"sm_clickhouse", false, true},
	{"sm_clickhouse_init", true, false},
	{"sm_telegraf", false, false},
	{"sm_grafana", false, true},
	{"sm_caddy", false, false},
}

// parsePS reads "name<TAB>status" lines of docker ps.
func parsePS(out string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if name, st, ok := strings.Cut(strings.TrimSpace(l), "\t"); ok {
			m[name] = st
		}
	}
	return m
}

func doctorCore(r *report, coreDir string) {
	e := r.e
	r.section("Core (" + coreDir + ")")
	file, err := envedit.Read(filepath.Join(coreDir, ".env"))
	if err != nil || file.String() == "" {
		r.line(stFail, coreDir+"/.env is missing", "Run: avenge core setup")
		return
	}
	bad := 0
	for _, k := range append([]string{grafanaKey}, servicePasswords...) {
		v, _ := file.Get(k)
		if err := checkPassword(v, 1); err != nil {
			r.line(stFail, k+" in .env is not usable: "+err.Error(), "Fix it in core/.env (or run avenge core setup), then: docker compose -f "+filepath.Join(coreDir, "compose.yaml")+" up -d")
			bad++
		}
	}
	bind, _ := file.Get("MQTT_BIND")
	if bind == "" {
		bind = "127.0.0.1"
	}
	if bad == 0 {
		r.line(stOK, ".env: passwords are set, MQTT_BIND="+bind)
	}
	if bind == "127.0.0.1" {
		r.line(stWarn, "MQTT_BIND is 127.0.0.1: only this machine can reach the broker", "For an edge device on another machine set the LAN address (avenge core setup).")
	}

	// containers
	out, err := e.DockerPS()
	if err != nil {
		r.line(stFail, "docker is not reachable: "+strings.TrimSpace(firstLine(out+" "+err.Error())), "Is Docker running, and may this user use it? Try: docker ps")
		return
	}
	ps := parsePS(out)
	if len(ps) == 0 {
		r.line(stFail, "no sm_* containers exist", "Start the core: docker compose -f "+filepath.Join(coreDir, "compose.yaml")+" up -d")
		return
	}
	for _, c := range containerWant {
		st, ok := ps[c.name]
		switch {
		case !ok:
			r.line(stFail, c.name+": missing", "docker compose -f "+filepath.Join(coreDir, "compose.yaml")+" up -d")
		case c.oneShot && strings.HasPrefix(st, "Exited (0)"):
			r.line(stOK, c.name+": finished (0), as it should")
		case c.oneShot:
			r.line(stFail, c.name+": "+st, "docker logs "+c.name)
		case strings.Contains(st, "unhealthy") || strings.Contains(st, "starting"):
			r.line(stFail, c.name+": "+st, "docker logs "+c.name)
		case strings.HasPrefix(st, "Up"):
			r.line(stOK, c.name+": "+st)
		default:
			r.line(stFail, c.name+": "+st, "docker logs "+c.name)
		}
	}

	// the broker, as an edge device would log in
	pw, _ := file.Get("MQTT_EDGE_PASSWORD")
	target := bind
	if bind == "0.0.0.0" {
		target = "127.0.0.1"
	}
	res := e.Probe(target, mqttPort, mqttEdgeUser, pw)
	if res.OK() {
		r.line(stOK, fmt.Sprintf("broker %s:%d: connects and the edge login works", target, mqttPort))
	} else {
		r.line(stFail, fmt.Sprintf("broker %s:%d: %v", target, mqttPort, res.Err), probe.Hint(res, target))
	}

	// rows arriving
	chPw, _ := file.Get("CLICKHOUSE_PASSWORD")
	chUser, _ := file.Get("CLICKHOUSE_USER")
	if chUser == "" {
		chUser = "default"
	}
	rows, err := queryClickHouse(e, chUser, chPw, "SELECT sensor_id, count(), toString(max(ts)) FROM iot.telemetry WHERE ts > now() - INTERVAL 5 MINUTE GROUP BY sensor_id ORDER BY sensor_id FORMAT TSV")
	switch {
	case err != nil:
		r.line(stFail, "ClickHouse query failed: "+err.Error(), "docker logs sm_clickhouse")
	case strings.TrimSpace(rows) == "":
		r.line(stWarn, "no telemetry rows in the last 5 minutes", "Normal before the first edge device ran. Otherwise run avenge doctor on the edge device and\nwatch: docker logs sm_telegraf")
	default:
		for _, l := range strings.Split(strings.TrimSpace(rows), "\n") {
			f := strings.Split(l, "\t")
			if len(f) == 3 {
				r.line(stOK, fmt.Sprintf("rows arriving: sensor %s, %s in the last 5 min, newest %s UTC", f[0], f[1], f[2]))
			}
		}
	}

	// Grafana through Caddy
	req, _ := http.NewRequest("GET", strings.TrimRight(e.GrafanaURL, "/")+"/api/health", nil)
	req.Host = "grafana.localhost"
	resp, err := e.HTTP.Do(req)
	switch {
	case err != nil:
		r.line(stFail, "Grafana at http://grafana.localhost: "+err.Error(), "docker logs sm_caddy   and   docker logs sm_grafana")
	case resp.StatusCode != 200:
		resp.Body.Close()
		r.line(stFail, fmt.Sprintf("Grafana at http://grafana.localhost answered HTTP %d", resp.StatusCode), "docker logs sm_grafana")
	default:
		resp.Body.Close()
		r.line(stOK, "Grafana answers at http://grafana.localhost (this machine only)")
	}
}

func queryClickHouse(e *env, user, pw, q string) (string, error) {
	u := strings.TrimRight(e.ClickHouse, "/") + "/?query=" + url.QueryEscape(q)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pw)))
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(firstLine(string(b))))
	}
	return string(b), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ── edge ────────────────────────────────────────────────────

var routeDevRe = regexp.MustCompile(`\bdev (\S+)`)

// routeDev takes the interface out of `ip route get` output.
func routeDev(out string) string {
	if m := routeDevRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func isVirtualIface(name string) bool {
	for _, p := range []string{"tailscale", "amn", "tun", "tap", "wg", "zt", "utun"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func doctorEdge(r *report, envPath string) {
	e := r.e
	r.section("Edge (" + envPath + ")")
	file, err := envedit.Read(envPath)
	if err != nil {
		r.line(stFail, "cannot read "+envPath+": "+err.Error())
		return
	}
	get := func(k string) string { v, _ := file.Get(k); return v }

	mic := get("SENSOR_SOURCE") == "mic"
	switch {
	case mic:
		r.line(stOK, "input: microphone")
		dev := get("AUDIO_DEVICE")
		if err := capture.CheckDevice(e.GOOS, dev); err != nil {
			r.line(stFail, err.Error())
		} else if dev == "" && e.GOOS == "windows" {
			r.line(stFail, "AUDIO_DEVICE is empty; Windows needs the microphone name", "edge-filter --list-audio")
		} else if out, lerr := e.ListAudio(e.GOOS); lerr == nil {
			devs := capture.ParseDevices(e.GOOS, out)
			found := dev == ""
			for _, d := range devs {
				if d.Value == dev || strings.HasPrefix(dev, "plughw:") && strings.Contains(d.Desc, "same as "+dev) {
					found = true
				}
			}
			if len(devs) == 0 {
				r.line(stFail, "no audio input devices are visible on this machine", capture.InstallHint(e.GOOS))
			} else if !found {
				r.line(stWarn, fmt.Sprintf("AUDIO_DEVICE=%s is not in the list of microphones", dev), "edge-filter --list-audio")
			} else if dev == "" {
				r.line(stOK, "microphone: the default device")
			} else {
				r.line(stOK, "microphone "+dev+" is present")
			}
		} else {
			r.line(stWarn, "could not list the audio devices: "+lerr.Error(), capture.InstallHint(e.GOOS))
		}
	case get("SERIAL_INTERFACE") != "":
		iface := get("SERIAL_INTERFACE")
		portKey := "USB_PORT"
		if iface == "rs485" {
			portKey = "RS485_PORT"
		}
		port := get(portKey)
		r.line(stOK, "input: serial sensor over "+iface)
		if port == "" {
			r.line(stFail, portKey+" is not set")
		} else if err := e.OpenCheck(port); err == nil {
			r.line(stOK, port+" opens")
		} else if os.IsPermission(err) {
			r.line(stFail, port+": permission denied", "sudo usermod -aG dialout $USER   then log out and in")
		} else if os.IsNotExist(err) {
			r.line(stFail, port+" does not exist", "Plug the sensor in; ls /dev/ttyACM* /dev/ttyUSB*  (or /dev/serial/by-id/)")
		} else {
			r.line(stFail, port+": "+err.Error())
		}
		if rate := get("SENSOR_SAMPLE_RATE"); rate == "" {
			r.line(stFail, "SENSOR_SAMPLE_RATE is not set (required for a serial sensor)", "avenge edge setup measures it; the reference ADXL345 firmware runs at 1600")
		} else if n, err := strconv.Atoi(rate); err != nil || n < 1 {
			r.line(stFail, "SENSOR_SAMPLE_RATE="+rate+" is not a rate in Hz")
		} else {
			r.line(stOK, "SENSOR_SAMPLE_RATE="+rate+" Hz")
		}
	default:
		r.line(stWarn, "no sensor configured (neither SENSOR_SOURCE=mic nor SERIAL_INTERFACE): the engine would read stdin", "avenge edge setup")
	}

	host, pw := get("CENTRAL_HOST"), get("MQTT_PASSWORD")
	user := get("MQTT_USER")
	if user == "" {
		user = mqttEdgeUser
	}
	port := mqttPort
	if p := get("MQTT_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	if host == "" || host == "192.168.1.50" || pw == "" || pw == "changeme" {
		r.line(stFail, "the link to the core is not configured (CENTRAL_HOST or MQTT_PASSWORD is still the example value)", "avenge edge setup   (paste the code printed by avenge core pair)")
		return
	}
	res := e.Probe(host, port, user, pw)
	if res.OK() {
		r.line(stOK, fmt.Sprintf("core %s:%d: reachable and the login works", host, port))
		return
	}
	hint := probe.Hint(res, host)
	if res.Kind == probe.Timeout || res.Kind == probe.Network {
		if out, err := e.IPRoute(host); err == nil {
			if dev := routeDev(out); dev != "" {
				extra := "route to the core goes through: " + dev
				if isVirtualIface(dev) {
					extra += "   <- a VPN or tunnel interface, probably not the LAN"
				}
				hint = extra + "\n" + hint
			}
		}
	}
	if net.ParseIP(host) == nil {
		hint += "\n  (CENTRAL_HOST is a name: try the IP address to rule out DNS)"
	}
	r.line(stFail, fmt.Sprintf("core %s:%d: %v", host, port, res.Err), hint)
}
