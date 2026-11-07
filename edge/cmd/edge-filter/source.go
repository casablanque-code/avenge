package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/capture"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/profile"
)

// applySource adjusts the settings for SENSOR_SOURCE and rejects
// combinations that cannot work. SENSOR_SOURCE=mic means "record the
// microphone yourself": it delivers raw PCM, so it implies the audio profile
// unless SENSOR_PROFILE says otherwise.
func applySource(s *profile.Settings, source, serialIface string) error {
	switch source {
	case "", "stdin":
		return nil
	case "mic":
	default:
		return fmt.Errorf("unknown SENSOR_SOURCE %q (use stdin or mic)", source)
	}
	if serialIface != "" {
		return fmt.Errorf("SENSOR_SOURCE=mic and SERIAL_INTERFACE=%s both select an input; unset one of them", serialIface)
	}
	if s.Input != "" && s.Input != profile.InputPCMS16LE {
		return fmt.Errorf("SENSOR_SOURCE=mic delivers raw PCM; unset SENSOR_INPUT (it is %q)", s.Input)
	}
	s.Input = profile.InputPCMS16LE
	s.Source = "mic"
	if s.Profile == "" {
		s.Profile = "audio"
	}
	return nil
}

// noInputDevices recognises the "nothing to list" answer of the listing tool.
func noInputDevices(out string) bool {
	return strings.Contains(out, "no soundcards found")
}

// listAudio prints the audio input devices of this machine and returns the
// process exit code.
func listAudio() int {
	name, args, err := capture.ListCommand(runtime.GOOS)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	out, err := exec.Command(name, args...).CombinedOutput()
	if errors.Is(err, exec.ErrNotFound) {
		fmt.Fprintf(os.Stderr, "%s not found: %s\n", name, capture.InstallHint(runtime.GOOS))
		return 1
	}
	// ffmpeg's device listing exits with an error by design; the list is
	// what matters.
	fmt.Print(string(out))
	if noInputDevices(string(out)) {
		fmt.Println("\nNo audio input devices found on this machine. A server or a virtual machine has none:")
		fmt.Println("run this on the machine that has the microphone.")
		return 1
	}
	fmt.Println("\nSet AUDIO_DEVICE in edge/.env to a name or number from this list.")
	fmt.Println("Leave it empty to use the default device (Windows has no default: it is required).")
	return 0
}
