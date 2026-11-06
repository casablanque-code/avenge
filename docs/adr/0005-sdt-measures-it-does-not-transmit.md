# ADR-0005: SDT is a measurement, not a payload

**Status:** accepted. Written retrospectively.

## Context

Swinging Door Trending is a classic way to compress a trend: keep only the points where the signal leaves a tolerance band. The project's early description speaks of "inline signal compression", which invites the reading that the compressed signal is what gets sent.

## Options considered

1. **Send the compressed points.** A reduced waveform upstream.
2. **Send nothing of the shape**, only features, and use SDT on the device to learn how compressible the recent signal is.

## Decision

Option 2. The published `sdt_ratio` is a feature: a signal that suddenly compresses worse is a signal that got busier. The reduction in data volume comes from sending summaries, not from SDT.

## Trade-offs

If you came for waveform compression over the wire, this is not it. The shape of the signal around an event is not available after the fact.

## How to test it

[E-001](../experiments/E-001-data-reduction.md) measures what is sent against what is read. Whether `sdt_ratio` helps tell faults from normal operation is not yet shown.

## Revisit when

We add event snapshots (a bounded burst of samples around an anomaly), at which point SDT might earn a second job.
