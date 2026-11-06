package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/profile"
)

// version is set at build time: -ldflags "-X main.version=v0.3.0" (the
// Docker image and the release workflow do this). A binary built from a git
// checkout without it reports "dev" and the commit it was built from.
var version = "dev"

// versionLine says exactly which build this is, for bug reports and for the
// run reports of experiments.
func versionLine() string {
	ver, commit, modified := version, "", false
	if bi, ok := debug.ReadBuildInfo(); ok {
		if ver == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			ver = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				commit = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
	}
	if len(commit) > 7 {
		commit = commit[:7]
	}
	out := "edge-filter " + ver
	if commit != "" {
		out += " (commit " + commit
		if modified {
			out += ", modified"
		}
		out += ")"
	}
	return fmt.Sprintf("%s, %s/%s, %s", out, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// checkInputFlags rejects combinations of --input, --record and the input
// settings that cannot work.
func checkInputFlags(inputFile, recordFile, source, serialIface, input string) error {
	if inputFile != "" && (source == "mic" || serialIface != "") {
		return errors.New("--input reads a file; it cannot be combined with SENSOR_SOURCE=mic or SERIAL_INTERFACE")
	}
	if recordFile != "" {
		if serialIface != "" || input != profile.InputPCMS16LE {
			return errors.New("--record works with raw PCM input only (SENSOR_SOURCE=mic, or SENSOR_PROFILE=audio with stdin or --input)")
		}
		if inputFile != "" && inputFile == recordFile {
			return errors.New("--record and --input name the same file")
		}
	}
	return nil
}

// recorder copies everything the engine reads from its raw PCM source into a
// file, for replaying and re-analysing a run. It is off unless asked for,
// local, and announced loudly: see docs/adr/0007-no-raw-signal-leaves-the-edge.md.
type recorder struct {
	f *os.File
	n int64
}

// startRecording creates the file (it must not exist, so a recording is never
// overwritten by accident) and returns a reader that tees into it.
func startRecording(path string, in io.Reader) (io.Reader, *recorder, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil, nil, fmt.Errorf("%s already exists; pick another name or delete it (a recording is never overwritten)", path)
	}
	if err != nil {
		return nil, nil, err
	}
	r := &recorder{f: f}
	return io.TeeReader(in, r), r, nil
}

func (r *recorder) Write(p []byte) (int, error) {
	n, err := r.f.Write(p)
	r.n += int64(n)
	return n, err
}

// Close finishes the file and returns how many bytes it holds.
func (r *recorder) Close() (int64, error) { return r.n, r.f.Close() }

// describeInput names the input in one line, for the run report.
func describeInput(source, serialIface, inputFile, inputFormat, micCommand string) string {
	switch {
	case serialIface != "":
		return "serial " + serialIface
	case source == "mic":
		return "microphone via `" + micCommand + "`"
	case inputFile != "":
		return "file " + inputFile + " (" + inputFormat + ")"
	}
	return "stdin (" + inputFormat + ")"
}

// formatReport renders the block to paste into an experiment record: what was
// run, on what, with which settings. It never includes passwords or hostnames.
func formatReport(now time.Time, input, recordFile string, r profile.Resolved) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Run report\n\n")
	fmt.Fprintf(&b, "- build: %s\n", versionLine())
	fmt.Fprintf(&b, "- time (UTC): %s\n", now.UTC().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- input: %s\n", input)
	if recordFile != "" {
		fmt.Fprintf(&b, "- recording: %s\n", recordFile)
	}
	fmt.Fprintf(&b, "\nConfiguration:\n\n```text\n%s```\n\n", r.Explain())
	fmt.Fprintf(&b, "Add by hand: CPU model, microphone or sensor model, room or mounting, and what was done when.\n")
	return b.String()
}
