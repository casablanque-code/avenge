package main

import (
	"strings"
	"testing"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/profile"
)

func TestApplySourceStdinChangesNothing(t *testing.T) {
	for _, src := range []string{"", "stdin"} {
		s := profile.Settings{Profile: "vibration"}
		if err := applySource(&s, src, ""); err != nil || s.Profile != "vibration" || s.Input != "" {
			t.Fatalf("source %q: %+v %v", src, s, err)
		}
	}
}

func TestApplySourceMicImpliesAudioPCM(t *testing.T) {
	var s profile.Settings
	if err := applySource(&s, "mic", ""); err != nil {
		t.Fatal(err)
	}
	if s.Profile != "audio" || s.Input != profile.InputPCMS16LE {
		t.Fatalf("got %+v", s)
	}
}

func TestApplySourceMicKeepsAnExplicitProfile(t *testing.T) {
	s := profile.Settings{Profile: "vibration"}
	if err := applySource(&s, "mic", ""); err != nil || s.Profile != "vibration" {
		t.Fatalf("an explicit SENSOR_PROFILE must win: %+v %v", s, err)
	}
}

func TestApplySourceRejectsConflicts(t *testing.T) {
	cases := []struct {
		s      profile.Settings
		source string
		iface  string
		want   string
	}{
		{profile.Settings{}, "tape", "", "unknown SENSOR_SOURCE"},
		{profile.Settings{}, "mic", "usb", "SERIAL_INTERFACE=usb"},
		{profile.Settings{Input: "jsonl"}, "mic", "", "unset SENSOR_INPUT"},
	}
	for _, c := range cases {
		err := applySource(&c.s, c.source, c.iface)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("source %q iface %q: want error containing %q, got %v", c.source, c.iface, c.want, err)
		}
	}
}

func TestNoInputDevicesIsRecognised(t *testing.T) {
	if !noInputDevices("arecord: device_list:277: no soundcards found...\n") {
		t.Error("arecord's empty list must be recognised")
	}
	if noInputDevices("**** List of CAPTURE Hardware Devices ****\ncard 0: PCH [HDA Intel PCH]") {
		t.Error("a real device list must not be taken for an empty one")
	}
}

func TestApplySourceMarksTheMicrophone(t *testing.T) {
	var s profile.Settings
	if err := applySource(&s, "mic", ""); err != nil || s.Source != "mic" {
		t.Fatalf("got %+v %v", s, err)
	}
}
