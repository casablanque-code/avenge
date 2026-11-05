package main

import (
	"flag"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/profile"
)

func TestDefaultBroker(t *testing.T) {
	cases := []struct {
		name, host, port, want string
	}{
		{"defaults", "", "", "localhost:1883"},
		{"ipv4", "192.168.1.50", "1883", "192.168.1.50:1883"},
		{"hostname", "broker.local", "8883", "broker.local:8883"},
		{"ipv6 bare", "fe80::1", "1883", "[fe80::1]:1883"},
		{"ipv6 bracketed", "[::1]", "1883", "[::1]:1883"},
		{"default port only", "10.0.0.7", "", "10.0.0.7:1883"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CENTRAL_HOST", c.host)
			t.Setenv("MQTT_PORT", c.port)
			if got := defaultBroker(); got != c.want {
				t.Fatalf("defaultBroker() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestHelpMentionsEveryFlag(t *testing.T) {
	fs := flag.NewFlagSet("edge-filter", flag.ContinueOnError)
	defineFlags(fs)
	help := usageText()
	fs.VisitAll(func(f *flag.Flag) {
		if !strings.Contains(help, "--"+f.Name) {
			t.Errorf("help does not mention flag --%s", f.Name)
		}
	})
	if !strings.Contains(help, "--help") {
		t.Error("help does not mention --help")
	}
}

// Every setting in edge/.env.example (commented or not) must be described in
// the help, except the ones that only Docker reads.
func TestHelpMentionsEverySetting(t *testing.T) {
	data, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Skipf("cannot read .env.example: %v", err)
	}
	dockerOnly := map[string]bool{"SERIAL_PORT": true, "DIALOUT_GID": true, "AVENGE_EDGE_TAG": true}
	re := regexp.MustCompile(`(?m)^#?\s*([A-Z][A-Z0-9_]+)=`)
	help := usageText()
	found := 0
	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		found++
		if dockerOnly[m[1]] {
			continue
		}
		if !strings.Contains(help, m[1]) {
			t.Errorf(".env.example has %s but the help does not describe it", m[1])
		}
	}
	if found == 0 {
		t.Fatal("no settings found in .env.example; the pattern is broken")
	}
}

func TestRequireSerialRate(t *testing.T) {
	if err := requireSerialRate(profile.Settings{}); err == nil {
		t.Error("serial input without SENSOR_SAMPLE_RATE must be refused")
	}
	if err := requireSerialRate(profile.Settings{SampleRate: 1600}); err != nil {
		t.Errorf("explicit rate must be accepted: %v", err)
	}
}
