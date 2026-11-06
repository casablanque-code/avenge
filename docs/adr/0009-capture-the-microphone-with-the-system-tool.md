# ADR-0009: Capture the microphone with the system's own tool

**Status:** accepted. Written retrospectively.

## Context

The engine reads raw PCM. Getting it from a microphone on Linux, macOS and Windows means talking to three different audio systems.

## Options considered

1. **An audio library inside the binary** (PortAudio through cgo, or per-platform bindings). One code path, and the end of the zero-dependency, easy cross-compilation promise.
2. **Pure Go per platform.** A lot of code to write and to get wrong.
3. **Run the system's own capture tool** and read its output: `arecord` on Linux, `ffmpeg` on macOS and Windows. The engine builds the command, starts it, reads raw PCM from its standard output and stops it on exit.

## Decision

Option 3. `SENSOR_SOURCE=mic` in `.env`, `AUDIO_DEVICE` to choose, `--list-audio` to list, and `--explain` to show the exact command that will run. Anything else that can print raw PCM works through a pipe.

## Trade-offs

An external program to install. Device naming differs per platform, and Windows has no default device. The capture command's arguments are part of the contract and are tested as command lines. macOS and Windows have not been run on those systems yet.

## How to test it

Command construction and process handling are unit tested; a stand-in capture tool exercises the whole path on Linux; the real thing is [E-004](../experiments/E-004-laptop-microphone.md).

## Revisit when

Real use shows the external tool to be the main source of trouble, or a Go audio library appears that is pure Go and covers all three systems.
