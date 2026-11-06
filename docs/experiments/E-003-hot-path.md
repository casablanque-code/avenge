# E-003: the cost of a window

**Question:** what does processing one window cost, and does the hot path allocate after start-up?
**Level:** S
**Status:** run 1 done, on one machine that is not a target device
**Hypotheses:** H3

## Pass line

Zero allocations per window on the steady path (the tests already enforce this). A time per window small enough that one core of the target device is nowhere near saturated; the target and the budget are to be fixed when a device is chosen.

## Setup

- Source revision: `main` at `307345e`.
- Machine: Anthropic-hosted sandbox, Intel Xeon @ 2.10 GHz, 1 CPU, Linux x86-64, Go 1.22.2 (see the note on toolchain in [E-002](E-002-tone-sweep.md)).
- Benchmarks as they exist in the repository: one window of the vibration profile (512 samples) through the sensor worker.

## Commands

```
cd edge
go test -run xxx -bench . -benchmem -count 3 ./internal/sensor ./internal/fft
go test -count=1 -run 'AllocFree|AllocsBounded' ./internal/...
```

## Run 1

| Benchmark | Time per op | Allocations per op |
|---|---|---|
| `BenchmarkIngestWindow` (with telemetry flush), 3 runs | 18.4, 17.9, 19.0 µs | 0 |
| `BenchmarkIngestWindowNoFlush`, 3 runs | 18.9, 19.5, 19.1 µs | 0 |
| `BenchmarkProcessorPowerSpectrum` (preallocated), 3 runs | 10.8, 10.7, 10.9 µs | 0 |
| `BenchmarkPowerSpectrumFunc` (allocating variant, for contrast), 3 runs | 13.0, 13.8, 12.7 µs | 3 (12,800 B) |

The `-benchmem` figure for the flush benchmark reads 33 B/op with 0 allocs/op; we have not investigated what that residue is, and do not claim to know.

At 1,600 Hz a 512-sample window is 320 ms of signal, so about 18 µs per window is under 0.01 percent of one core on this machine.

## Observed vs expected

Zero allocations per window, as expected, and the preallocated spectrum is faster than the allocating one. The allocation tests pass.

## What this does not show

- Anything about a Raspberry Pi or any other edge device. A virtual Xeon is not a Pi, and the ratio between them is not something we should guess.
- The audio profile (1,024-sample windows at 16 kHz); only the vibration profile is benchmarked.
- Memory use, garbage collector behaviour over hours, or many sensors at once.
- Noise from a shared virtual machine: three runs per row, no controlled frequency scaling.

## Next

Repeat on the machine that will run the laptop test, add the audio profile, then on a Raspberry Pi once there is one.
