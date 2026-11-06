# ADR-0007: No raw signal leaves the edge

**Status:** accepted. Written retrospectively.

## Context

With a vibration sensor this is an efficiency choice. With a microphone it is a privacy choice: sound can contain speech. A system that listens must be able to say, and prove, what it keeps.

## Options considered

1. **Promise in the documentation.**
2. **Make the promise structural and tested**: the engine publishes only flat summaries, and a test fails when the set of published fields changes.

## Decision

Option 2. Telemetry and anomaly messages are flat objects of named numbers and short strings, a few hundred bytes. `TestPayloadsCarryNoRawSignal` checks that no field is an array or an object, that the set of keys equals an allowlist, and that payloads stay small. Adding a field means editing the allowlist and this record, on purpose. The engine does not write samples to disk either.

## Trade-offs

No audio for debugging, no waveform around an event. If we ever add snapshots, this record has to be superseded first, in the open.

## How to test it

The test above, plus reading `internal/mqtt/publisher.go`: it is short on purpose.

## Revisit when

A deployment needs evidence recordings, with consent, a bounded size and an explicit switch that is off by default.
