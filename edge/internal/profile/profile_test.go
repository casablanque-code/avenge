package profile

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

// The vibration profile must equal the built-in defaults exactly: choosing
// nothing keeps the behaviour that existed before profiles.
func TestVibrationEqualsBuiltInDefaults(t *testing.T) {
	for _, name := range []string{"", "vibration", "  VIBRATION "} {
		r, err := Resolve(Settings{Profile: name, SensorID: "bearing_01"})
		if err != nil {
			t.Fatalf("profile %q: %v", name, err)
		}
		want := sensor.DefaultSensorConfig("bearing_01")
		if !reflect.DeepEqual(r.Sensor, want) {
			t.Fatalf("profile %q differs from DefaultSensorConfig:\n got %+v\nwant %+v", name, r.Sensor, want)
		}
		if r.Input != InputJSONL {
			t.Fatalf("vibration input = %q, want %q", r.Input, InputJSONL)
		}
	}
}

func TestVibrationSampleRateOverrideChangesOnlyTheRate(t *testing.T) {
	r, err := Resolve(Settings{SensorID: "x", SampleRate: 1600})
	if err != nil {
		t.Fatal(err)
	}
	want := sensor.DefaultSensorConfig("x")
	want.SampleRate = 1600
	if !reflect.DeepEqual(r.Sensor, want) {
		t.Fatalf("got %+v, want %+v", r.Sensor, want)
	}
}

func TestAudioDefaults(t *testing.T) {
	r, err := Resolve(Settings{Profile: "audio", SensorID: "mic"})
	if err != nil {
		t.Fatal(err)
	}
	c := r.Sensor
	if c.SampleRate != 16000 || c.FFTWindow != 1024 {
		t.Fatalf("rate/window = %d/%d", c.SampleRate, c.FFTWindow)
	}
	// 64 ms windows: 10 s telemetry = 156 windows, 10 s baseline = 156 windows.
	if c.SDTFlushEvery != 156 || c.BaselineWindows != 156 {
		t.Fatalf("flush/baseline windows = %d/%d, want 156/156", c.SDTFlushEvery, c.BaselineWindows)
	}
	if c.BaselineAlpha <= 0 || c.BaselineAlpha >= 0.01 {
		t.Fatalf("BaselineAlpha = %v, want a small per-window factor", c.BaselineAlpha)
	}
	if len(c.Bands) != 3 || c.Bands[2].HiHz != 8000 {
		t.Fatalf("bands = %+v, want three bands ending at the Nyquist frequency", c.Bands)
	}
	if r.Input != InputPCMS16LE {
		t.Fatalf("audio input = %q, want %q", r.Input, InputPCMS16LE)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", r.Warnings)
	}
}

// Time constants are in seconds, so a lower sample rate means fewer windows.
func TestAudioAdaptsToSampleRate(t *testing.T) {
	r, err := Resolve(Settings{Profile: "audio", SensorID: "mic", SampleRate: 8000})
	if err != nil {
		t.Fatal(err)
	}
	// 1024 samples at 8 kHz = 128 ms, so 10 s = 78 windows.
	if r.Sensor.SDTFlushEvery != 78 {
		t.Fatalf("flush windows = %d, want 78", r.Sensor.SDTFlushEvery)
	}
	if got := r.Sensor.Bands[2].HiHz; got != 4000 {
		t.Fatalf("top band ends at %v Hz, want 4000 (Nyquist)", got)
	}
}

func TestBandsAboveNyquistAreDroppedWithAWarning(t *testing.T) {
	r, err := Resolve(Settings{Profile: "audio", SensorID: "mic", SampleRate: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sensor.Bands) != 2 {
		t.Fatalf("bands = %+v, want low and mid only", r.Sensor.Bands)
	}
	if len(r.Warnings) == 0 || !strings.Contains(r.Warnings[0], "Nyquist") {
		t.Fatalf("expected a Nyquist warning, got %v", r.Warnings)
	}
}

func TestVibrationWarnsWhenBuiltInBandsAreUnreachable(t *testing.T) {
	r, err := Resolve(Settings{SensorID: "phone", SampleRate: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) == 0 {
		t.Fatal("expected warnings about built-in bands above the Nyquist frequency")
	}
}

func TestInvalidSettings(t *testing.T) {
	cases := []struct {
		name string
		s    Settings
		want string
	}{
		{"unknown profile", Settings{Profile: "ultrasound"}, "available: audio, vibration"},
		{"unknown input", Settings{Input: "wav"}, "SENSOR_INPUT"},
		{"negative rate", Settings{SampleRate: -5}, "positive"},
	}
	for _, c := range cases {
		_, err := Resolve(c.s)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: error = %v, want it to mention %q", c.name, err, c.want)
		}
	}
}

func TestExplainIsAlignedAndInformative(t *testing.T) {
	r, err := Resolve(Settings{Profile: "audio", SensorID: "mic"})
	if err != nil {
		t.Fatal(err)
	}
	out := r.Explain()
	for _, want := range []string{"audio", "16000 Hz", "1024 samples = 64 ms", "15.6 Hz per bin", "10.0 s", "low 0-250 Hz", "normal state", "SERIAL_INTERFACE"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Explain() lacks %q:\n%s", want, out)
		}
	}
	// Every line of the box has the same width.
	width := -1
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "┌") && !strings.HasPrefix(line, "│") && !strings.HasPrefix(line, "└") {
			continue
		}
		if width < 0 {
			width = utf8.RuneCountInString(line)
		} else if n := utf8.RuneCountInString(line); n != width {
			t.Fatalf("box line width %d, want %d: %q", n, width, line)
		}
	}
}

func TestPCMRateDifferentFromProfileWarns(t *testing.T) {
	r, err := Resolve(Settings{Profile: "audio", SampleRate: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(r.Warnings, "\n"), "SENSOR_SAMPLE_RATE=8000") {
		t.Fatalf("expected a warning naming the rate, got %v", r.Warnings)
	}
	if r, _ = Resolve(Settings{Profile: "audio"}); len(r.Warnings) != 0 {
		t.Fatalf("profile default rate must not warn: %v", r.Warnings)
	}
	if r, _ = Resolve(Settings{Profile: "vibration", SampleRate: 1600}); len(r.Warnings) != 0 {
		t.Fatalf("jsonl input at 1600 Hz must not warn: %v", r.Warnings)
	}
}

func TestExplainTellsCaptureRate(t *testing.T) {
	r, _ := Resolve(Settings{Profile: "audio"})
	if !strings.Contains(r.Explain(), "arecord -r 16000") {
		t.Fatal("explain for PCM input must state the capture rate")
	}
}
