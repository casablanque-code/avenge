# ADR-0006: One combined anomaly state

**Status:** accepted. Written retrospectively.

## Context

The engine runs several detectors: one on the overall RMS and one per frequency band. For a while, severity came from their combined score, while the `onset` and `clear` events came from the RMS detector alone. A faint tone that barely moved the RMS showed up as critical in the telemetry and never as an onset ([LAB-NOTES](../LAB-NOTES.md#the-detector-that-couldnt-hear)).

## Options considered

1. **Keep two definitions** and document the difference.
2. **Events on every severity change.** Chatty, and noisy around thresholds.
3. **One state**: in anomaly while the RMS detector or any band detector is, each with its own hysteresis. Events are the changes of that state.

## Decision

Option 3. `onset` and `clear` are published when the combined state changes, with the signal (`rms` or `band`) and the band that caused it; a `clear` repeats what its onset said. Telemetry `is_anomaly` is the same state. Severity changes during an anomaly are visible in telemetry, not as events.

## Trade-offs

Peak severity of an anomaly is not in the event. A band detector's false alarm is now an onset like any other.

## How to test it

`TestAudioProfileReportsABandOnlyAnomaly` (one onset naming the band, one clear, no clear without an onset), the end-to-end test's check that `dominant_signal` reaches the database, and the false-onset counts of [E-002](../experiments/E-002-tone-sweep.md).

## Revisit when

Someone needs the peak severity of each anomaly, or wants separate alerts per band.
