# ADR-0008: Trust the configured sample rate, then verify it

**Status:** accepted. Written retrospectively.

## Context

A serial sensor or a microphone pipe delivers numbers without a clock. Time and every frequency the engine computes rest on a configured sample rate. If it is wrong, nothing crashes and every result is quietly off ([LAB-NOTES](../LAB-NOTES.md#the-1600-hz-trap)).

## Options considered

1. **A default rate everywhere.** Right for some sources, silently wrong for others.
2. **Make the user configure it, and trust them.**
3. **Make the user configure it, and measure the real arrival rate to check.**

## Decision

Option 3. Serial input refuses to start without `SENSOR_SAMPLE_RATE`. For every source the engine measures how fast samples arrive (ignoring the first two seconds, because pipes deliver a start-up burst) and reports a mismatch above 5 percent in a box, again every minute, and in the summary at the end. Input far faster than real time is recognised as a replay and left alone.

## Trade-offs

One more thing to set for a serial sensor. The check needs about twelve seconds of live input, so short runs go unchecked and say so. A source that really runs at the wrong rate and is consistent about it is still reported as a mismatch, which is correct.

## How to test it

Unit tests with a virtual clock (steady, wrong, double, replay, start-up burst, recovery), live-paced runs with a generator and a stand-in capture tool, and, still open, a real device ([E-006](../experiments/E-006-vibration-bench.md)).

## Revisit when

The sensor protocol gains a header with its own rate, at which point the engine should read it and the setting becomes an override.
