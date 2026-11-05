// Package profile maps a kind of signal (machine vibration, audio) to the
// full set of detector parameters, so a user picks one name instead of
// tuning a dozen numbers.
//
// A profile only fills in values that already exist in sensor.SensorConfig;
// it does not change how the pipeline works. The "vibration" profile equals
// the long-standing built-in defaults exactly, so choosing nothing keeps the
// old behaviour.
//
// Time constants of a profile are expressed in windows (vibration, to keep
// historic behaviour) or in seconds (audio). Seconds are converted to windows
// from the sample rate and the FFT window, so the same profile behaves
// sensibly at any sample rate.
package profile

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/anomaly"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

// Input formats accepted on stdin.
const (
	InputJSONL    = "jsonl"
	InputPCMS16LE = "pcm_s16le"
)

// Band is a frequency band of a profile. HiHz <= 0 means "up to the Nyquist
// frequency", which keeps a profile valid at any sample rate.
type Band struct {
	Name       string
	LoHz, HiHz float64
}

// Profile is one named preset.
type Profile struct {
	Name    string
	Summary string // one line, shown by --explain
	Unit    string // what a sample value means

	SampleRate int // default; SENSOR_SAMPLE_RATE overrides it
	FFTWindow  int // power of two

	// Exactly one of each pair is used: windows if > 0, otherwise seconds.
	FlushWindows    int
	FlushSeconds    float64
	BaselineWindows int
	BaselineSeconds float64

	// BaselineTauSeconds is how fast the baseline follows the signal, as a
	// time constant. Zero keeps the historic per-window factor (0.1). A
	// profile with short windows needs this: a per-window factor adapts
	// proportionally faster and would absorb a growing fault.
	BaselineTauSeconds float64

	SDTEpsilon    float64 // in sample units
	EnterZ, ExitZ float64 // RMS detector thresholds, in standard deviations
	MinStdDev     float64 // RMS detector stddev floor, in sample units

	Bands         []Band  // nil keeps the built-in 50 Hz machine bands
	BandMinStdDev float64 // 0 keeps the built-in floor

	Input string // default stdin format
}

var profiles = map[string]Profile{
	// vibration reproduces sensor.DefaultSensorConfig exactly (a test pins
	// this): a 1 kHz-class accelerometer on a 50 Hz machine, values in g.
	"vibration": {
		Name:            "vibration",
		Summary:         "machine vibration from an accelerometer",
		Unit:            "g",
		SampleRate:      1000,
		FFTWindow:       512,
		FlushWindows:    20,
		BaselineWindows: 10,
		SDTEpsilon:      0.05,
		EnterZ:          3.5,
		ExitZ:           2.5,
		MinStdDev:       0.002,
		Input:           InputJSONL,
	},
	// audio targets ambient sound captured by a microphone, normalised to
	// full scale = 1.0 (what PCMReader produces). First approximation:
	// calibrated against the synthetic audio of firmware-sim, not yet against
	// real recordings.
	"audio": {
		Name:            "audio",
		Summary:         "ambient sound from a microphone",
		Unit:            "full scale (1.0 = loudest)",
		SampleRate:      16000,
		FFTWindow:       1024,
		FlushSeconds:    10,
		BaselineSeconds: 10,
		// 64 ms windows: a per-window factor would adapt about 8 times
		// faster than for vibration and absorb a growing fault, so the
		// baseline gets a time constant instead.
		BaselineTauSeconds: 30,
		SDTEpsilon:         0.002,
		EnterZ:             5.0,
		ExitZ:              3.5,
		MinStdDev:          0.0001,
		Bands: []Band{
			{Name: "low", LoHz: 0, HiHz: 250},
			{Name: "mid", LoHz: 250, HiHz: 2000},
			{Name: "high", LoHz: 2000, HiHz: 0},
		},
		BandMinStdDev: 1e-7,
		Input:         InputPCMS16LE,
	},
}

// Names returns the available profile names, sorted.
func Names() []string {
	out := make([]string, 0, len(profiles))
	for n := range profiles {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Settings are the user-facing inputs; empty or zero values mean "use the
// profile's default".
type Settings struct {
	Profile    string // SENSOR_PROFILE
	SensorID   string // SENSOR_ID or --sensor-id
	SampleRate int    // SENSOR_SAMPLE_RATE
	Input      string // SENSOR_INPUT
}

// Resolved is a validated configuration ready for sensor.Manager.Add.
type Resolved struct {
	Profile  Profile
	Sensor   sensor.SensorConfig
	Input    string
	Warnings []string
}

// Resolve applies the chosen profile and validates the result. A returned
// error is fatal; Warnings are advice that does not stop the run.
func Resolve(s Settings) (Resolved, error) {
	name := strings.ToLower(strings.TrimSpace(s.Profile))
	if name == "" {
		name = "vibration"
	}
	p, ok := profiles[name]
	if !ok {
		return Resolved{}, fmt.Errorf("unknown SENSOR_PROFILE %q (available: %s)",
			s.Profile, strings.Join(Names(), ", "))
	}
	if s.SampleRate < 0 {
		return Resolved{}, fmt.Errorf("SENSOR_SAMPLE_RATE must be positive, got %d", s.SampleRate)
	}

	rate := p.SampleRate
	if s.SampleRate > 0 {
		rate = s.SampleRate
	}
	window := p.FFTWindow
	if window < 2 || window&(window-1) != 0 {
		return Resolved{}, fmt.Errorf("profile %q: FFT window %d is not a power of two", p.Name, window)
	}
	windowSec := float64(window) / float64(rate)

	input := p.Input
	if in := strings.ToLower(strings.TrimSpace(s.Input)); in != "" {
		input = in
	}
	if input != InputJSONL && input != InputPCMS16LE {
		return Resolved{}, fmt.Errorf("unknown SENSOR_INPUT %q (use %s or %s)", input, InputJSONL, InputPCMS16LE)
	}

	cfg := sensor.DefaultSensorConfig(s.SensorID)
	cfg.SampleRate = rate
	cfg.FFTWindow = window
	cfg.SDTFlushEvery = windowsFor(p.FlushWindows, p.FlushSeconds, windowSec, 1)
	cfg.BaselineWindows = windowsFor(p.BaselineWindows, p.BaselineSeconds, windowSec, 5)
	cfg.SDTEpsilon = p.SDTEpsilon
	cfg.EnterThreshold = p.EnterZ
	cfg.ExitThreshold = p.ExitZ
	cfg.MinStdDev = p.MinStdDev
	cfg.BandMinStdDev = p.BandMinStdDev
	if p.BaselineTauSeconds > 0 {
		cfg.BaselineAlpha = math.Min(0.5, windowSec/p.BaselineTauSeconds)
	}

	res := Resolved{Profile: p, Input: input}
	nyquist := float64(rate) / 2
	binHz := float64(rate) / float64(window)

	if p.Bands != nil {
		for _, b := range p.Bands {
			lo, hi := b.LoHz, b.HiHz
			if hi <= 0 || hi > nyquist {
				hi = nyquist
			}
			if lo >= nyquist {
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"band %q (from %g Hz) is above the Nyquist frequency %g Hz of a %d Hz sample rate and was dropped",
					b.Name, lo, nyquist, rate))
				continue
			}
			if hi-lo < 2*binHz {
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"band %q is only %.1f Hz wide but an FFT bin is %.1f Hz; its readings will be coarse",
					b.Name, hi-lo, binHz))
			}
			cfg.Bands = append(cfg.Bands, anomaly.FreqBand{Name: b.Name, LoHz: lo, HiHz: hi})
		}
		if len(cfg.Bands) == 0 {
			return Resolved{}, fmt.Errorf("no frequency band fits below the Nyquist frequency %g Hz; raise SENSOR_SAMPLE_RATE", nyquist)
		}
	} else {
		for _, b := range anomaly.DefaultBands(rate) {
			if b.LoHz >= nyquist {
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"built-in band %q (from %g Hz) is above the Nyquist frequency %g Hz of a %d Hz sample rate and will stay empty",
					b.Name, b.LoHz, nyquist, rate))
			}
		}
	}

	if every := float64(cfg.SDTFlushEvery) * windowSec; every < 1 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"telemetry would be published every %.2f s; that is a lot of MQTT traffic per sensor", every))
	}
	if windowSec > 5 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"each window lasts %.1f s, so anomalies are detected slowly; check SENSOR_SAMPLE_RATE", windowSec))
	}

	if input == InputPCMS16LE && s.SampleRate > 0 && s.SampleRate != p.SampleRate {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"SENSOR_SAMPLE_RATE=%d differs from the %s default %d Hz; the capture command must produce exactly %d Hz",
			s.SampleRate, p.Name, p.SampleRate, s.SampleRate))
	}

	res.Sensor = cfg
	return res, nil
}

// windowsFor returns a window count: the explicit one if set, else the
// duration converted to windows (rounded, never below min).
func windowsFor(windows int, seconds, windowSec float64, min int) int {
	if windows > 0 {
		return windows
	}
	n := int(math.Round(seconds / windowSec))
	if n < min {
		n = min
	}
	return n
}

// Explain renders the resolved configuration as a boxed, human-readable
// summary plus any warnings.
func (r Resolved) Explain() string {
	c := r.Sensor
	windowSec := float64(c.FFTWindow) / float64(c.SampleRate)
	binHz := float64(c.SampleRate) / float64(c.FFTWindow)
	bandEnter := c.EnterThreshold + 0.5 // the worker keeps bands slightly stricter

	bands := c.Bands
	if bands == nil {
		bands = anomaly.DefaultBands(c.SampleRate)
	}
	input := "JSONL {t, value} on stdin"
	if r.Input == InputPCMS16LE {
		input = "raw PCM, 16-bit LE mono, on stdin"
	}

	rows := [][2]string{
		{"profile", fmt.Sprintf("%s - %s", r.Profile.Name, r.Profile.Summary)},
		{"unit", r.Profile.Unit},
		{"input", input},
		{"sample rate", fmt.Sprintf("%d Hz", c.SampleRate)},
		{"window", fmt.Sprintf("%d samples = %s, %.1f Hz per bin", c.FFTWindow, dur(windowSec), binHz)},
		{"baseline", fmt.Sprintf("first %d windows (%s) = normal", c.BaselineWindows, dur(float64(c.BaselineWindows)*windowSec))},
		{"telemetry", fmt.Sprintf("every %d windows (%s)", c.SDTFlushEvery, dur(float64(c.SDTFlushEvery)*windowSec))},
		{"thresholds", fmt.Sprintf("anomaly > %.1f sigma, clears < %.1f (bands %.1f / %.1f)",
			c.EnterThreshold, c.ExitThreshold, bandEnter, c.ExitThreshold)},
		{"adaptation", adaptation(c, windowSec)},
	}
	for i, b := range bands {
		label := ""
		if i == 0 {
			label = "bands"
		}
		rows = append(rows, [2]string{label, fmt.Sprintf("%s %g-%g Hz", b.Name, b.LoHz, b.HiHz)})
	}

	var lines []string
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("%-12s %s", row[0], row[1]))
	}
	out := box("AVENGE edge configuration", lines)
	out += "\nA serial interface (SERIAL_INTERFACE) overrides the input shown above.\n"
	if r.Input == InputPCMS16LE {
		out += fmt.Sprintf("Capture the sound at exactly %d Hz, mono (arecord -r %d, ffmpeg -ar %d); the engine cannot detect a different rate.\n",
			c.SampleRate, c.SampleRate, c.SampleRate)
	}
	out += "Start the monitor while the signal is in its normal state: the baseline is learned at start.\n"
	for _, w := range r.Warnings {
		out += "warning: " + w + "\n"
	}
	return out
}

func adaptation(c sensor.SensorConfig, windowSec float64) string {
	alpha := c.BaselineAlpha
	if alpha <= 0 {
		alpha = 0.1
	}
	return fmt.Sprintf("baseline time constant ~%s (slower drift is absorbed)", dur(windowSec/alpha))
}

func dur(seconds float64) string {
	if seconds < 1 {
		return fmt.Sprintf("%.0f ms", seconds*1000)
	}
	return fmt.Sprintf("%.1f s", seconds)
}

// box draws lines inside a titled frame.
func box(title string, lines []string) string {
	width := utf8.RuneCountInString(title) + 2
	for _, l := range lines {
		if n := utf8.RuneCountInString(l); n > width {
			width = n
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "┌─ %s %s┐\n", title, strings.Repeat("─", width-utf8.RuneCountInString(title)-1))
	for _, l := range lines {
		fmt.Fprintf(&b, "│ %s%s │\n", l, strings.Repeat(" ", width-utf8.RuneCountInString(l)))
	}
	fmt.Fprintf(&b, "└%s┘\n", strings.Repeat("─", width+2))
	return b.String()
}
