package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/capture"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/envedit"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/pairing"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/probe"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/prompt"
)

const (
	defaultRate    = 1600 // the reference ADXL345 firmware
	defaultBaud    = 230400
	trialDuration  = 15 * time.Second
	measureSeconds = 5
)

// standardRates are the ADXL345 output data rates a firmware is likely to use.
var standardRates = []int{100, 200, 400, 800, 1600, 3200}

func runEdge(args []string, e *env) int {
	if len(args) == 0 {
		fmt.Fprint(e.Err, "usage: avenge edge setup  (see avenge --help)\n")
		return 2
	}
	switch args[0] {
	case "setup":
		return reportErr(e, edgeSetup(args[1:], e))
	case "-h", "--help":
		fmt.Fprint(e.Out, usageText())
		return 0
	}
	fmt.Fprintf(e.Err, "avenge edge: unknown command %q (setup)\n", args[0])
	return 2
}

type edgeFlags struct {
	dir, pair, mode, iface, port, device, sensorID string
	rate, baud                                     int
	noMeasure, noTrial, yes                        bool
}

// suggestRate maps a measured rate to the value to configure: a standard rate
// when the measurement is within 5% of one, else the measurement rounded to 10 Hz.
func suggestRate(measured float64) int {
	best, bestDiff := 0, 1e18
	for _, r := range standardRates {
		if d := abs(measured - float64(r)); d < bestDiff {
			best, bestDiff = r, d
		}
	}
	if bestDiff <= 0.05*float64(best) {
		return best
	}
	return int(measured/10+0.5) * 10
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// edgeEnvPath finds the .env an edge-filter started in dir would read:
// ".env" first, then "edge/.env" (the order edge-filter uses). A new file goes
// to edge/.env inside a repository clone and to ./.env next to a release binary.
func edgeEnvPath(dir string) string {
	for _, c := range []string{filepath.Join(dir, ".env"), filepath.Join(dir, "edge", ".env")} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "edge", "compose.yaml")); err == nil {
		return filepath.Join(dir, "edge", ".env")
	}
	return filepath.Join(dir, ".env")
}

func edgeSetup(args []string, e *env) error {
	var f edgeFlags
	fs := flag.NewFlagSet("avenge edge setup", flag.ContinueOnError)
	fs.SetOutput(e.Err)
	fs.StringVar(&f.dir, "dir", ".", "directory where edge-filter will run (its .env is read from here)")
	fs.StringVar(&f.pair, "pair", "", "pairing code from the core (avenge core pair)")
	fs.StringVar(&f.mode, "mode", "", "vibration or mic")
	fs.StringVar(&f.iface, "interface", "", "usb or rs485 (vibration)")
	fs.StringVar(&f.port, "port", "", "serial device path (vibration)")
	fs.IntVar(&f.baud, "baud", 0, "RS-485 baud rate (default 230400)")
	fs.IntVar(&f.rate, "rate", 0, "sample rate of the sensor in Hz (vibration; default: measured, else 1600)")
	fs.StringVar(&f.device, "device", "", "microphone value from edge-filter --list-audio (mic)")
	fs.StringVar(&f.sensorID, "sensor-id", "", "sensor name (default bearing_01 or mic_01)")
	fs.BoolVar(&f.noMeasure, "no-measure", false, "do not measure the sample rate")
	fs.BoolVar(&f.noTrial, "no-trial", false, "skip the 15 s trial run at the end")
	fs.BoolVar(&f.yes, "yes", false, "do not ask; take flags and defaults")
	fs.Usage = func() {
		fmt.Fprint(e.Out, "usage: avenge edge setup [flags]\n\nAsks what is connected (ADXL345 sensor or microphone), finds the device,\nmeasures the sample rate, takes the pairing code from the core, writes .env\n(mode 0600, the old file is kept as .env.bak-<time>), checks the link to the\ncore and ends with a 15 s trial run of edge-filter.\n\n")
		printFlags(e.Out, fs)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return err
	}
	if !f.yes && !e.Interactive {
		return errors.New("this needs a terminal to ask its questions; run it in one, or give the answers as flags and add --yes")
	}
	if f.mode != "" && f.mode != "vibration" && f.mode != "mic" {
		return errors.New("--mode must be vibration or mic")
	}
	if f.iface != "" && f.iface != "usb" && f.iface != "rs485" {
		return errors.New("--interface must be usb or rs485")
	}

	envPath := edgeEnvPath(f.dir)
	file, err := envedit.Read(envPath)
	if err != nil {
		return err
	}
	if err := edgeScopeError(f.dir, envPath, file); err != nil {
		return err
	}
	hadEnv := file.String() != ""
	if !hadEnv {
		if file, err = envedit.Read(filepath.Join(filepath.Dir(envPath), ".env.example")); err != nil {
			return err
		}
	}
	in := e.ask()
	fmt.Fprintf(e.Out, "AVENGE edge setup  (%s)\n\n", envPath)

	// ── what is connected ────────────────────────────────────
	mode := f.mode
	if mode == "" {
		if mode, err = chooseMode(e, f.yes, file, hadEnv); err != nil {
			return err
		}
	}
	if mode == "vibration" && e.GOOS != "linux" {
		return fmt.Errorf("a serial sensor is read on Linux only (this is %s); use --mode mic here, or run the edge on a Linux machine", e.GOOS)
	}
	notes := []string{}
	if mode == "vibration" {
		n, err := setupVibration(e, in, file, hadEnv, f)
		if err != nil {
			return err
		}
		notes = append(notes, n...)
	} else {
		n, err := setupMic(e, in, file, hadEnv, f)
		if err != nil {
			return err
		}
		notes = append(notes, n...)
	}
	sid := f.sensorID
	if sid == "" {
		def := "bearing_01"
		if mode == "mic" {
			def = "mic_01"
		}
		cur, _ := file.Get("SENSOR_ID")
		if hadEnv && cur != "" && !(cur == "bearing_01" && mode == "mic") {
			def = cur
		}
		sid = def
		if !f.yes {
			if sid, err = in.Ask("Sensor name (shows up in Grafana)", def, validSensorID); err != nil {
				return err
			}
		}
	}
	if err := validSensorID(sid); err != nil {
		return fmt.Errorf("--sensor-id: %w", err)
	}
	file.Set("SENSOR_ID", sid)

	// ── link to the core ─────────────────────────────────────
	info, err := linkToCore(e, in, file, hadEnv, f)
	if err != nil {
		return err
	}
	file.Set("CENTRAL_HOST", info.Host)
	file.Set("MQTT_PORT", strconv.Itoa(info.Port))
	file.Set("MQTT_USER", info.User)
	file.Set("MQTT_PASSWORD", info.Password)

	// ── write ────────────────────────────────────────────────
	backup := ""
	if hadEnv {
		if backup, err = envedit.Backup(envPath, e.Now()); err != nil {
			return fmt.Errorf("could not back up the old .env: %w", err)
		}
	}
	if err := file.Write(envPath); err != nil {
		return err
	}
	summary := []string{
		"file:     " + envPath + "  (mode 0600)",
		"mode:     " + map[string]string{"vibration": "vibration sensor", "mic": "microphone"}[mode],
		"sensor:   " + sid,
		fmt.Sprintf("core:     %s:%d  (user %s)", info.Host, info.Port, info.User),
	}
	if backup != "" {
		summary = append(summary, "old file: "+backup)
	}
	fmt.Fprintln(e.Out)
	box(e.Out, "written", summary)
	for _, n := range notes {
		fmt.Fprintln(e.Out, n)
	}

	// ── check the link, then try the engine ──────────────────
	fmt.Fprintln(e.Out, "\nChecking the link to the core ...")
	res := e.Probe(info.Host, info.Port, info.User, info.Password)
	linkOK := res.OK()
	if linkOK {
		fmt.Fprintf(e.Out, "  ✓ %s:%d answers and the login works\n", info.Host, info.Port)
	} else {
		fmt.Fprintf(e.Out, "  ✗ %v\n%s\n", res.Err, indent(probe.Hint(res, info.Host), "    "))
		fmt.Fprintln(e.Out, "  The .env is saved; fix the link and run: avenge doctor")
	}

	bin := e.FindEdge()
	runCmd := "./edge-filter"
	if bin != "" {
		runCmd = bin
	}
	// edge-filter reads .env from the folder it is started in, not from the
	// folder of its binary: say so when the two differ.
	if runDir, derr := filepath.Abs(trialDirFor(envPath)); derr == nil {
		if cwd, cerr := os.Getwd(); cerr == nil && cwd != runDir {
			runCmd = fmt.Sprintf("cd %s && %s", runDir, runCmd)
		}
	}
	trialOK := true
	if !f.noTrial {
		switch {
		case bin == "":
			fmt.Fprintln(e.Out, "\nTrial run skipped: edge-filter was not found next to avenge, here or in PATH.")
			fmt.Fprintln(e.Out, "  Download it from the same release, then run: ./edge-filter --explain")
		default:
			fmt.Fprintf(e.Out, "\nTrial run, %d s (no data is sent to the core) ...\n", int(trialDuration/time.Second))
			trialOK = reportTrial(e, e.Trial(trialDirFor(envPath), bin, trialDuration))
		}
	}
	fmt.Fprintln(e.Out)
	next := []string{"Run it:   " + runCmd}
	if mode == "vibration" {
		next = append(next, "Or with Docker (vibration only): docker compose -f edge/compose.yaml up -d")
	}
	next = append(next, "Check:    avenge doctor")
	box(e.Out, "next", next)
	if !linkOK || !trialOK {
		return errors.New("setup finished with problems, see above")
	}
	return nil
}

var sensorIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validSensorID(s string) error {
	if !sensorIDRe.MatchString(s) {
		return errors.New("letters, digits, _ and - only, up to 64 characters (it becomes part of the MQTT topic)")
	}
	return nil
}

func indent(s, pad string) string {
	return pad + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n"+pad)
}

// trialDirFor is the directory edge-filter must start in to read this .env.
func trialDirFor(envPath string) string {
	d := filepath.Dir(envPath)
	if filepath.Base(d) == "edge" {
		return filepath.Dir(d)
	}
	return d
}

func chooseMode(e *env, yes bool, file *envedit.File, hadEnv bool) (string, error) {
	def := 0 // vibration: the recommended setup
	if hadEnv {
		if v, _ := file.Get("SENSOR_SOURCE"); v == "mic" {
			def = 1
		}
	}
	opts := []string{
		"Vibration sensor (ADXL345 on USB or RS-485): the main use",
		"Microphone (sound of a machine or a room)",
	}
	if e.GOOS != "linux" {
		def = 1
		opts[0] += "  [Linux only, not available here]"
	}
	if yes {
		return []string{"vibration", "mic"}[def], nil
	}
	i, err := e.ask().Choose("What is connected to this device?", opts, def)
	if err != nil {
		return "", err
	}
	return []string{"vibration", "mic"}[i], nil
}

// ── vibration ───────────────────────────────────────────────

type serialPort struct{ Path, Desc string }

// scanSerial lists serial devices under devRoot (normally /dev). Stable
// /dev/serial/by-id names come first, and a device already listed through its
// by-id name is not repeated.
func scanSerial(devRoot string) []serialPort {
	var out []serialPort
	covered := map[string]bool{}
	byID, _ := filepath.Glob(filepath.Join(devRoot, "serial", "by-id", "*"))
	sort.Strings(byID)
	for _, p := range byID {
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			continue
		}
		covered[real] = true
		out = append(out, serialPort{Path: p, Desc: "-> " + filepath.Base(real)})
	}
	for _, pat := range []string{"ttyACM*", "ttyUSB*"} {
		list, _ := filepath.Glob(filepath.Join(devRoot, pat))
		sort.Strings(list)
		for _, p := range list {
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				real = p
			}
			if covered[real] {
				continue
			}
			out = append(out, serialPort{Path: p, Desc: "no stable name"})
		}
	}
	return out
}

func setupVibration(e *env, in *prompt.Prompter, file *envedit.File, hadEnv bool, f edgeFlags) ([]string, error) {
	var notes []string
	// the microphone settings must not stay active next to a serial sensor
	file.Unset("SENSOR_SOURCE")
	file.Unset("AUDIO_DEVICE")

	iface := f.iface
	if iface == "" {
		def := 0
		if v, _ := file.Get("SERIAL_INTERFACE"); hadEnv && v == "rs485" {
			def = 1
		}
		if f.yes {
			iface = []string{"usb", "rs485"}[def]
		} else {
			i, err := in.Choose("How is the sensor connected?", []string{"USB (Arduino-type board, ttyACM / ttyUSB)", "RS-485 (HAT or USB-RS485 dongle)"}, def)
			if err != nil {
				return nil, err
			}
			iface = []string{"usb", "rs485"}[i]
		}
	}
	portKey, defPath := "USB_PORT", "/dev/ttyACM0"
	if iface == "rs485" {
		portKey, defPath = "RS485_PORT", "/dev/ttyUSB0"
	}

	port := f.port
	if port == "" {
		ports := e.Ports()
		cur, _ := file.Get(portKey)
		if f.yes {
			switch {
			case len(ports) == 1:
				port = ports[0].Path
			case hadEnv && cur != "":
				port = cur
			default:
				port = defPath
			}
		} else {
			var err error
			if port, err = pickPort(e, in, ports, defPath, cur, hadEnv); err != nil {
				return nil, err
			}
		}
	}
	if port == "" || !strings.HasPrefix(port, "/") {
		return nil, fmt.Errorf("serial device %q is not a path", port)
	}
	if err := e.OpenCheck(port); err != nil {
		if os.IsPermission(err) {
			notes = append(notes, fmt.Sprintf("! %s: permission denied. Add yourself to the dialout group, then log out and in:\n    sudo usermod -aG dialout $USER", port))
		} else if os.IsNotExist(err) {
			notes = append(notes, fmt.Sprintf("! %s does not exist yet: plug the sensor in before you start it (ls /dev/ttyACM* /dev/ttyUSB*)", port))
		} else {
			notes = append(notes, fmt.Sprintf("! %s cannot be opened: %v", port, err))
		}
	}
	file.Set("SERIAL_INTERFACE", iface)
	file.Set(portKey, port)

	baud := defaultBaud
	if iface == "rs485" {
		if f.baud > 0 {
			baud = f.baud
		} else if v, ok := file.Get("RS485_BAUD"); ok && hadEnv {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				baud = n
			}
		}
		file.Set("RS485_BAUD", strconv.Itoa(baud))
	}

	// sample rate: measured if possible, else the reference value
	rate := f.rate
	if rate == 0 {
		suggest := defaultRate
		if v, ok := file.Get("SENSOR_SAMPLE_RATE"); ok && hadEnv {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				suggest = n
			}
		}
		measure := !f.noMeasure && !notesHaveProblem(notes)
		if measure && !f.yes {
			var err error
			if measure, err = in.Confirm(fmt.Sprintf("Measure the real sample rate now (%d s, the sensor must be streaming)?", measureSeconds), true); err != nil {
				return nil, err
			}
		}
		if measure {
			fmt.Fprintf(e.Out, "Measuring %s for %d s ...\n", port, measureSeconds)
			got, err := e.Measure(port, baud)
			if err != nil {
				fmt.Fprintf(e.Out, "  ✗ %v\n  Using %d Hz (the reference ADXL345 firmware); the rate check at the first run will tell if it is wrong.\n", err, suggest)
			} else {
				suggest = suggestRate(got)
				fmt.Fprintf(e.Out, "  measured %.0f Hz -> %d Hz\n", got, suggest)
			}
		}
		rate = suggest
		if !f.yes {
			var err error
			rs, err := in.Ask("Sample rate of the sensor, Hz", strconv.Itoa(suggest), validRate)
			if err != nil {
				return nil, err
			}
			rate, _ = strconv.Atoi(rs)
		}
	}
	if err := validRate(strconv.Itoa(rate)); err != nil {
		return nil, fmt.Errorf("--rate: %w", err)
	}
	file.Set("SENSOR_SAMPLE_RATE", strconv.Itoa(rate))
	return notes, nil
}

func notesHaveProblem(n []string) bool { return len(n) > 0 }

func validRate(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 100000 {
		return errors.New("a whole number of Hz, 1 to 100000")
	}
	return nil
}

func pickPort(e *env, in interface {
	Ask(string, string, func(string) error) (string, error)
	Choose(string, []string, int) (int, error)
}, ports []serialPort, defPath, cur string, hadEnv bool) (string, error) {
	if len(ports) == 0 {
		fmt.Fprintln(e.Out, "No serial devices found (/dev/serial/by-id, ttyACM*, ttyUSB*). Plug the sensor in;")
		fmt.Fprintln(e.Out, "you can still type the path now and plug it in before the first run.")
		if hadEnv && cur != "" {
			defPath = cur
		}
		return in.Ask("Serial device path", defPath, nil)
	}
	labels := make([]string, 0, len(ports)+1)
	def := 0
	for i, p := range ports {
		labels = append(labels, fmt.Sprintf("%s  (%s)", p.Path, p.Desc))
		if hadEnv && p.Path == cur {
			def = i
		}
	}
	labels = append(labels, "type another path")
	i, err := in.Choose("Which serial device is the sensor?", labels, def)
	if err != nil {
		return "", err
	}
	if i < len(ports) {
		return ports[i].Path, nil
	}
	return in.Ask("Serial device path", defPath, nil)
}

// ── microphone ──────────────────────────────────────────────

func setupMic(e *env, in *prompt.Prompter, file *envedit.File, hadEnv bool, f edgeFlags) ([]string, error) {
	// serial settings must not stay active next to the microphone
	file.Unset("SERIAL_INTERFACE")
	file.Unset("SENSOR_SAMPLE_RATE") // the audio profile uses 16000
	file.Set("SENSOR_SOURCE", "mic")

	device := f.device
	if device == "" && !f.yes {
		out, err := e.ListAudio(e.GOOS)
		devs := capture.ParseDevices(e.GOOS, out)
		cur, _ := file.Get("AUDIO_DEVICE")
		switch {
		case err != nil && len(devs) == 0:
			fmt.Fprintf(e.Out, "Could not list the audio inputs (%v).\n  %s\n", err, capture.InstallHint(e.GOOS))
			device, err = in.Ask("Microphone (empty = default device)", cur, nil)
			if err != nil {
				return nil, err
			}
		case len(devs) == 0:
			fmt.Fprintln(e.Out, "No audio input devices found: this machine has no microphone, or it is not visible to it.")
			var aerr error
			device, aerr = in.Ask("Microphone (empty = default device)", cur, nil)
			if aerr != nil {
				return nil, aerr
			}
		default:
			labels := make([]string, 0, len(devs)+1)
			def := 0
			for i, d := range devs {
				labels = append(labels, fmt.Sprintf("%s  (%s)", d.Value, d.Desc))
				if hadEnv && d.Value == cur {
					def = i
				}
			}
			labels = append(labels, "the default device (leave AUDIO_DEVICE empty)")
			i, cerr := in.Choose("Which microphone?", labels, def)
			if cerr != nil {
				return nil, cerr
			}
			if i < len(devs) {
				device = devs[i].Value
			}
		}
	}
	if err := capture.CheckDevice(e.GOOS, device); err != nil {
		return nil, err
	}
	if device == "" && e.GOOS == "windows" {
		return nil, errors.New("on Windows the microphone name is required (edge-filter --list-audio prints it)")
	}
	if device == "" {
		file.Unset("AUDIO_DEVICE")
	} else {
		file.Set("AUDIO_DEVICE", device)
	}
	notes := []string{
		"Before you measure: switch off automatic gain (AGC) and noise suppression for this microphone,",
		"fix the input level and do not touch it during a run. Start the engine while the room is in its",
		"normal state: the first 10 s are learned as normal.",
	}
	return notes, nil
}

// ── link to the core ────────────────────────────────────────

func linkToCore(e *env, in *prompt.Prompter, file *envedit.File, hadEnv bool, f edgeFlags) (pairing.Info, error) {
	if f.pair != "" {
		info, err := pairing.Decode(f.pair)
		if err != nil {
			return pairing.Info{}, fmt.Errorf("--pair: %w", err)
		}
		return info, nil
	}
	host, _ := file.Get("CENTRAL_HOST")
	pw, _ := file.Get("MQTT_PASSWORD")
	port, _ := file.Get("MQTT_PORT")
	user, _ := file.Get("MQTT_USER")
	have := hadEnv && host != "" && host != "192.168.1.50" && pw != "" && pw != "changeme"
	pnum, perr := strconv.Atoi(port)
	if perr != nil {
		pnum = mqttPort
	}
	if user == "" {
		user = mqttEdgeUser
	}
	if have {
		if f.yes {
			return pairing.Info{Host: host, Port: pnum, User: user, Password: pw}, nil
		}
		keep, err := in.Confirm(fmt.Sprintf("Keep the current link to the core (%s:%d)?", host, pnum), true)
		if err != nil {
			return pairing.Info{}, err
		}
		if keep {
			return pairing.Info{Host: host, Port: pnum, User: user, Password: pw}, nil
		}
	}
	if f.yes {
		return pairing.Info{}, errors.New("no link to the core: give --pair CODE (print it on the core with: avenge core pair)")
	}
	fmt.Fprintln(e.Out, "\nPaste the pairing code printed by the core (avenge core pair); Enter alone = type the address by hand.")
	var info pairing.Info
	code, err := in.Ask("Pairing code", "", func(s string) error {
		if s == "" {
			return nil
		}
		var derr error
		info, derr = pairing.Decode(s)
		return derr
	})
	if err != nil {
		return pairing.Info{}, err
	}
	if code != "" {
		return info, nil
	}
	h, err := in.Ask("Core address (the IP of the machine running the core)", "", func(s string) error {
		if s == "" || strings.ContainsAny(s, " /") {
			return errors.New("an IP address or a host name")
		}
		return nil
	})
	if err != nil {
		return pairing.Info{}, err
	}
	pwd, err := in.Ask("MQTT password (MQTT_EDGE_PASSWORD from core/.env)", "", func(s string) error {
		if s == "" || s == "changeme" {
			return errors.New("the real password")
		}
		return nil
	})
	if err != nil {
		return pairing.Info{}, err
	}
	return pairing.Info{Host: h, Port: mqttPort, User: mqttEdgeUser, Password: pwd}, nil
}

// ── trial run ───────────────────────────────────────────────

type trialResult struct {
	Output string
	Err    error // the process ended by itself, with an error
	Early  bool  // it ended before the time was up
}

var (
	rmsRe  = regexp.MustCompile(`rms=([0-9.]+)`)
	rateRe = regexp.MustCompile(`(?i)rate check:.*`)
)

// summarizeTrial reads the console output of a trial run.
func summarizeTrial(out string) (rateLine string, mismatch bool, minRMS, maxRMS float64, windows int) {
	minRMS = -1
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "RATE MISMATCH") {
			mismatch = true
		}
		if m := rateRe.FindString(line); m != "" && rateLine == "" {
			rateLine = strings.TrimSpace(m)
		}
		if m := rmsRe.FindStringSubmatch(line); m != nil {
			v, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				continue
			}
			windows++
			if minRMS < 0 || v < minRMS {
				minRMS = v
			}
			if v > maxRMS {
				maxRMS = v
			}
		}
	}
	return
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// reportTrial prints the verdict and says whether the trial passed.
func reportTrial(e *env, r trialResult) bool {
	if r.Early || r.Err != nil {
		fmt.Fprintln(e.Out, "  ✗ edge-filter stopped by itself. Its last lines:")
		fmt.Fprintln(e.Out, indent(tail(r.Output, 8), "    "))
		return false
	}
	rateLine, mismatch, lo, hi, n := summarizeTrial(r.Output)
	ok := true
	switch {
	case mismatch:
		fmt.Fprintln(e.Out, "  ✗ RATE MISMATCH: the source does not run at the configured rate.")
		if rateLine != "" {
			fmt.Fprintln(e.Out, "    "+rateLine)
		}
		fmt.Fprintln(e.Out, "    Fix the sensor or set SENSOR_SAMPLE_RATE to the measured value, then run this again.")
		ok = false
	case rateLine != "" && strings.Contains(rateLine, "- ok"):
		fmt.Fprintln(e.Out, "  ✓ "+rateLine)
	case rateLine != "":
		fmt.Fprintln(e.Out, "  ! "+rateLine)
		ok = false
	default:
		fmt.Fprintln(e.Out, "  ! no rate check line yet (it appears after about 12 s); no verdict on the rate")
	}
	if n == 0 {
		fmt.Fprintln(e.Out, "  ✗ no windows were processed: the engine is not getting a signal")
		fmt.Fprintln(e.Out, indent(tail(r.Output, 6), "    "))
		return false
	}
	fmt.Fprintf(e.Out, "  ✓ %d windows processed, rms between %.4f and %.4f\n", n, lo, hi)
	if hi > 0 && lo >= 0 && hi-lo == 0 && hi < 1e-6 {
		fmt.Fprintln(e.Out, "  ! the signal is flat zero: a muted microphone or a sensor that sends nothing")
	}
	return ok
}
