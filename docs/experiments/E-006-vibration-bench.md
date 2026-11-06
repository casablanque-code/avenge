# E-006: vibration on a real sensor

**Question:** does the vibration path work end to end on a real accelerometer, and what does the rate check say about a real device?
**Level:** B
**Status:** planned
**Hypotheses:** H5, H7

## Pass line (to be fixed before the run)

To be written with the hardware in hand: which fault we can create safely (a loose mount, an added mass, an offset weight), what the engine must say about it, and the acceptable false-alarm rate for an idle machine.

## Plan

Record the sensor model, mounting, serial settings and the sensor firmware's real output rate. Run the engine with the rate configured, read the rate check, then create the fault. Keep the raw recording so the run can be replayed.

## Open prerequisites

A real sensor and a known firmware. The repository has no firmware; see [LAB-NOTES](../LAB-NOTES.md#still-open).

## What this will not show

Anything about other sensors, machines or mountings.
