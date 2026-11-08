// Command avenge is the setup and diagnostics tool of AVENGE. It does not run
// the engine (that is edge-filter); it asks a few questions, writes .env for
// you and checks that the pieces can reach each other.
//
//	avenge core setup     configure and start the core (the broker and the database)
//	avenge core pair      print the pairing code for an edge device
//	avenge edge setup     configure an edge device (sensor or microphone)
//	avenge doctor         check the core, the edge or both
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/netinfo"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/probe"
	"github.com/casablanque-code/smart-manufacturing/edge/internal/prompt"
)

// version is stamped at build time: -ldflags "-X main.version=v0.5.0".
var version = "dev"

// env is everything the commands take from the outside world, so tests can
// replace it.
type env struct {
	In          io.Reader
	Out, Err    io.Writer
	Interactive bool
	Now         func() time.Time
	Rand        io.Reader
	LAN         func() []netinfo.Addr
	WSL         bool
	GOOS        string
	Run         func(dir, name string, args ...string) error
	Look        func(name string) (string, error)
	Probe       func(host string, port int, user, password string) probe.Result
	Ports       func() []serialPort
	OpenCheck   func(path string) error
	Measure     func(port string, baud int) (float64, error)
	ListAudio   func(goos string) (string, error)
	FindEdge    func() string
	DockerPS    func() (string, error)
	IPRoute     func(host string) (string, error)
	ClickHouse  string // base URL of the ClickHouse HTTP interface
	GrafanaURL  string // base URL of Grafana behind Caddy
	HTTP        *http.Client
	Trial       func(dir, bin string, d time.Duration) trialResult
	prompter    *prompt.Prompter
}

func (e *env) ask() *prompt.Prompter {
	if e.prompter == nil {
		e.prompter = prompt.New(e.In, e.Out)
	}
	return e.prompter
}

func main() {
	e := realEnv()
	os.Exit(run(os.Args[1:], e))
}

func run(args []string, e *env) int {
	if len(args) == 0 {
		fmt.Fprint(e.Err, usageText())
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(e.Out, usageText())
		return 0
	case "--version", "version":
		fmt.Fprintf(e.Out, "avenge %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return 0
	case "core":
		return runCore(args[1:], e)
	case "edge":
		return runEdge(args[1:], e)
	case "doctor":
		return runDoctor(args[1:], e)
	}
	fmt.Fprintf(e.Err, "avenge: unknown command %q\n\n%s", args[0], usageText())
	return 2
}

func runCore(args []string, e *env) int {
	if len(args) == 0 {
		fmt.Fprint(e.Err, "usage: avenge core setup | avenge core pair  (see avenge --help)\n")
		return 2
	}
	switch args[0] {
	case "setup":
		return reportErr(e, coreSetup(args[1:], e))
	case "pair":
		return reportErr(e, corePair(args[1:], e))
	case "-h", "--help":
		fmt.Fprint(e.Out, usageText())
		return 0
	}
	fmt.Fprintf(e.Err, "avenge core: unknown command %q (setup, pair)\n", args[0])
	return 2
}

// reportErr prints an error the same way everywhere and gives the exit code.
func reportErr(e *env, err error) int {
	if err == nil {
		return 0
	}
	if err == errHelp {
		return 0
	}
	fmt.Fprintf(e.Err, "\navenge: %v\n", err)
	return 1
}

func usageText() string {
	return strings.TrimLeft(`
avenge - setup and diagnostics for AVENGE.

USAGE
  avenge core setup    configure the core (passwords, bind address), offer to start it
  avenge core pair     print the pairing code that an edge device needs
  avenge edge setup    configure an edge device: sensor or microphone, link to the core
  avenge doctor        check the core, the edge, or both (it looks at what is here)
  avenge --version     print the version
  avenge --help        this text

Every command asks questions on a terminal. For scripts, give the answers as
flags and add --yes; without a terminal and without --yes a command stops with
a clear message instead of guessing.

  avenge core setup --help     the flags of one command

The .env file stays the single source of truth: run a command again to change
a value (it shows the current ones), or edit the file by hand. A changed .env
takes effect at the next start: "docker compose -f core/compose.yaml up -d" for
the core, a restart of edge-filter for the edge.

DOCS
  docs/OPERATIONS.md  setup, profiles, troubleshooting
`, "\n")
}
