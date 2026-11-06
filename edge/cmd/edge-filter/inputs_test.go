package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/profile"
)

func TestVersionLine(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "v9.9.9"
	got := versionLine()
	for _, want := range []string{"edge-filter v9.9.9", runtime.GOOS + "/" + runtime.GOARCH, runtime.Version()} {
		if !strings.Contains(got, want) {
			t.Errorf("version line %q lacks %q", got, want)
		}
	}
}

func TestRecordingCopiesExactlyWhatWasRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "room.pcm")
	src := bytes.Repeat([]byte{1, 2, 3, 4, 5}, 5000)
	r, rec, err := startRecording(path, bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, src) {
		t.Fatalf("the reader must still deliver the data unchanged: %v", err)
	}
	n, err := rec.Close()
	if err != nil || n != int64(len(src)) {
		t.Fatalf("Close = %d, %v; want %d", n, err, len(src))
	}
	saved, _ := os.ReadFile(path)
	if !bytes.Equal(saved, src) {
		t.Fatal("the file differs from what was read")
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
			t.Errorf("recording permissions %v, want 0600 (it may contain private sound)", st.Mode().Perm())
		}
	}
}

func TestRecordingNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "room.pcm")
	if err := os.WriteFile(path, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := startRecording(path, bytes.NewReader(nil)); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want a refusal, got %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "precious" {
		t.Fatal("the existing file was modified")
	}
}

func TestCheckInputFlags(t *testing.T) {
	pcm, jsonl := profile.InputPCMS16LE, profile.InputJSONL
	cases := []struct {
		name                             string
		input, record, source, iface, in string
		wantErr                          string
	}{
		{"plain stdin", "", "", "", "", jsonl, ""},
		{"replay a file", "a.pcm", "", "", "", pcm, ""},
		{"record from mic", "", "r.pcm", "mic", "", pcm, ""},
		{"record from pcm stdin", "", "r.pcm", "", "", pcm, ""},
		{"input with mic", "a.pcm", "", "mic", "", pcm, "cannot be combined"},
		{"input with serial", "a.pcm", "", "", "usb", pcm, "cannot be combined"},
		{"record jsonl", "", "r.pcm", "", "", jsonl, "raw PCM input only"},
		{"record serial", "", "r.pcm", "", "usb", pcm, "raw PCM input only"},
		{"record onto input", "a.pcm", "a.pcm", "", "", pcm, "same file"},
	}
	for _, c := range cases {
		err := checkInputFlags(c.input, c.record, c.source, c.iface, c.in)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: want error containing %q, got %v", c.name, c.wantErr, err)
		}
	}
}

func TestReportHasWhatAnExperimentNeeds(t *testing.T) {
	res, err := profile.Resolve(profile.Settings{Profile: "audio", SensorID: "mic"})
	if err != nil {
		t.Fatal(err)
	}
	out := formatReport(time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), "microphone via `arecord -q`", "room.pcm", res)
	for _, want := range []string{"## Run report", "build: edge-filter", "time (UTC): 2026-11-06 12:00:00", "input: microphone via `arecord -q`",
		"recording: room.pcm", "sample rate  16000 Hz", "Add by hand: CPU model"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	for _, secret := range []string{"PASSWORD", "password", "MQTT_USER"} {
		if strings.Contains(out, secret) {
			t.Errorf("report must not mention %q", secret)
		}
	}
}
