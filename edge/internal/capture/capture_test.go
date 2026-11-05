package capture

import (
	"errors"
	"io"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCommandLinux(t *testing.T) {
	name, args, err := Command("linux", "", 16000)
	want := []string{"-q", "-f", "S16_LE", "-r", "16000", "-c", "1", "-t", "raw"}
	if err != nil || name != "arecord" || !reflect.DeepEqual(args, want) {
		t.Fatalf("got %s %v %v", name, args, err)
	}
	_, args, _ = Command("linux", "plughw:1,0", 16000)
	if got := strings.Join(args[len(args)-2:], " "); got != "-D plughw:1,0" {
		t.Fatalf("device not passed: %v", args)
	}
}

func TestCommandDarwin(t *testing.T) {
	name, args, err := Command("darwin", "", 16000)
	line := Describe(name, args)
	if err != nil || name != "ffmpeg" ||
		!strings.Contains(line, "-f avfoundation -i :default") ||
		!strings.Contains(line, "-ac 1 -ar 16000 -f s16le -") {
		t.Fatalf("got %q %v", line, err)
	}
	_, args, _ = Command("darwin", "1", 8000)
	if !strings.Contains(Describe("ffmpeg", args), "-i :1 ") || !strings.Contains(Describe("ffmpeg", args), "-ar 8000") {
		t.Fatalf("device or rate not passed: %v", args)
	}
}

func TestCommandWindows(t *testing.T) {
	if _, _, err := Command("windows", "", 16000); err == nil || !strings.Contains(err.Error(), "AUDIO_DEVICE") {
		t.Fatalf("Windows without a device must explain what to do, got %v", err)
	}
	name, args, err := Command("windows", "Microphone (Realtek Audio)", 16000)
	if err != nil || name != "ffmpeg" {
		t.Fatalf("got %s %v", name, err)
	}
	// The device name must stay ONE argument, spaces and all.
	found := false
	for _, a := range args {
		if a == "audio=Microphone (Realtek Audio)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("device name was split: %v", args)
	}
	if !strings.Contains(Describe(name, args), `"audio=Microphone (Realtek Audio)"`) {
		t.Fatal("Describe must quote arguments with spaces")
	}
}

func TestUnsupportedPlatform(t *testing.T) {
	if _, _, err := Command("plan9", "", 16000); err == nil {
		t.Fatal("expected an error")
	}
	if _, _, err := Command("linux", "", 0); err == nil {
		t.Fatal("expected an error for rate 0")
	}
}

func TestListCommands(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if n, a, err := ListCommand(goos); err != nil || n == "" || len(a) == 0 {
			t.Errorf("%s: %s %v %v", goos, n, a, err)
		}
		if InstallHint(goos) == "" {
			t.Errorf("%s: empty install hint", goos)
		}
	}
}

func TestStartReadsOutputUntilExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	p, err := Start("sh", []string{"-c", "head -c 4000 /dev/zero"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Kill()
	data, err := io.ReadAll(p.Out)
	if err != nil || len(data) != 4000 {
		t.Fatalf("read %d bytes, err %v", len(data), err)
	}
}

func TestWaitReportsAFailingCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	p, err := Start("sh", []string{"-c", "exit 3"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Kill()
	if _, err := io.ReadAll(p.Out); err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("want exit status 3, got %v", err)
	}
	if p.Wait() == nil {
		t.Fatal("Wait must keep returning the same error")
	}
}

func TestKillStopsALongRunningCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sleep")
	}
	p, err := Start("sleep", []string{"30"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { p.Kill(); p.Kill(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Kill did not return")
	}
}

func TestMissingProgramIsNotFound(t *testing.T) {
	_, err := Start("avenge-no-such-tool", nil)
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("want exec.ErrNotFound, got %v", err)
	}
}
