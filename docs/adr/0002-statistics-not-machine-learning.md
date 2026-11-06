# ADR-0002: Statistics against a learned baseline, not a model

**Status:** accepted. Written retrospectively.

## Context

We want to know when a machine sounds or shakes differently from how it did a minute ago. Many opaque solutions exist. This project wants something deterministic, debuggable on the device, and cheap enough to run without an accelerator.

## Options considered

1. **A trained model.** Needs labelled examples of failures, which we do not have, and is hard to explain when it fires.
2. **Fixed thresholds.** Trivial, and wrong on every machine except the one they were tuned on.
3. **Z-scores against a baseline learned at start-up**, for the overall RMS and for each frequency band, with hysteresis (enter above one threshold, leave below a lower one) and a baseline that stops adapting while an anomaly is active.

## Decision

Option 3. Thresholds are in units of standard deviations, so they carry over between machines; the baseline is learned from the first seconds and then drifts slowly. The profile sets the numbers ([ADR-0004](0004-signal-profiles-instead-of-knobs.md)).

## Trade-offs

It detects "different from what was learned", not "which fault". It is sensitive to what the machine was doing during start-up. A slow drift is absorbed, which is a feature until the failure itself is the slow drift. While the engine is in anomaly the baseline is frozen, so a lasting change that is meant to be the new normal is never accepted: the engine stays in anomaly until it is restarted ([E-007](../experiments/E-007-living-room-model.md)).

## How to test it

[E-002](../experiments/E-002-tone-sweep.md) measures what the detector notices over a given noise. The honest test is false alarms per hour on real recordings ([E-004](../experiments/E-004-laptop-microphone.md), [E-006](../experiments/E-006-vibration-bench.md)).

## Revisit when

We have labelled real faults in quantity and a baseline comparison that beats this one on them, **or** a way to declare a new normal (an explicit re-learn command, or a re-learn after a long sustained anomaly) is decided and tested: a separate record.
