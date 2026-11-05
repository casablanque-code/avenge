package main

import (
	"strings"
	"testing"
	"time"

	"github.com/casablanque-code/smart-manufacturing/edge/internal/sensor"
)

func TestRateMismatchIsABox(t *testing.T) {
	out := formatRateReport(sensor.RateReport{Measured: 1001, Want: 1600, Kind: sensor.RateMismatch})
	for _, want := range []string{"RATE MISMATCH", "1001 Hz", "1600", "edge/.env", "┌", "└"} {
		if !strings.Contains(out, want) {
			t.Errorf("box lacks %q:\n%s", want, out)
		}
	}
	// every line of the frame has the same width
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for _, l := range lines {
		if len([]rune(l)) != len([]rune(lines[0])) {
			t.Fatalf("ragged box:\n%s", out)
		}
	}
}

func TestSummaryStatesTheRateCheck(t *testing.T) {
	st := sensor.WorkerStats{Windows: 121, InAnomaly: 90, AboveWarning: 91}
	mismatch := &sensor.RateReport{Measured: 1001, Want: 1600, Kind: sensor.RateMismatch}
	out := formatStats(62300, 7, st, 1600, mismatch, 62*time.Second)
	for _, want := range []string{"38.9 s at 1600 Hz", "Real time       : 62.0 s", "MISMATCH", "1001 Hz measured"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	ok := &sensor.RateReport{Measured: 1002, Want: 1000, Kind: sensor.RateOK}
	if out := formatStats(10000, 1, st, 1000, ok, 10*time.Second); !strings.Contains(out, "ok (1002 Hz measured, 1000 Hz configured)") {
		t.Errorf("ok line missing:\n%s", out)
	}
	if out := formatStats(10000, 1, st, 1000, nil, time.Second); !strings.Contains(out, "not measured") || strings.Contains(out, "Real time") {
		t.Errorf("short run must say 'not measured' and show no real time:\n%s", out)
	}
	replay := &sensor.RateReport{Kind: sensor.RateReplay}
	if out := formatStats(10000, 1, st, 1000, replay, time.Second); !strings.Contains(out, "replay") || strings.Contains(out, "Real time") {
		t.Errorf("replay must be labelled and show no real time:\n%s", out)
	}
}
