# ADR-0004: One profile setting instead of a wall of knobs

**Status:** accepted. Written retrospectively.

## Context

Window length, frequency bands, thresholds, adaptation speed and telemetry cadence all depend on what kind of signal is being watched. A vibration sensor at 1.6 kHz and a microphone at 16 kHz want very different values.

## Options considered

1. **Expose every parameter** as an environment variable. Honest and flexible, and the user must understand a dozen interacting numbers before the first run.
2. **A config file per sensor.** Same problem, moved.
3. **A profile**: one setting (`SENSOR_PROFILE`) fills in all of them, and `--explain` prints what it resolved to, in plain words, with the numbers.

## Decision

Profiles. Nothing is hidden: `--explain` shows every value the profile chose, and the profile code is the documentation of what each one means. Advanced overrides can be added when someone needs them; nobody has yet.

## Trade-offs

A profile is an opinion. The audio profile's defaults are validated on synthetic sound only until [E-004](../experiments/E-004-laptop-microphone.md) is done. Users who need something unusual have to change code or wait for an override.

## How to test it

Behaviour tests per profile on synthetic signals (they exist), and the real recordings of E-004 and E-006 for the real defaults.

## Revisit when

Two real use cases want the same profile with different thresholds.
