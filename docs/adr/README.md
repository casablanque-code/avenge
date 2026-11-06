# Architecture decision records

Short records of the decisions that shape A.V.E.N.G.E: what we were facing, what else we could have done, what we chose and what it costs, how to test the decision, and what would make us change our minds.

These were written after the fact, from the code, the reviews and the mistakes in [LAB-NOTES](../LAB-NOTES.md). Where a decision came out of a mistake, the record says so. New decisions get a record when they are made: copy the [template](TEMPLATE.md).

| ADR | Decision |
|---|---|
| [0001](0001-split-edge-and-core.md) | The edge computes, the core stores and shows |
| [0002](0002-statistics-not-machine-learning.md) | Statistics against a learned baseline, not a model |
| [0003](0003-mqtt-as-transport.md) | MQTT 3.1.1 with QoS 1 as the transport |
| [0004](0004-signal-profiles-instead-of-knobs.md) | One profile setting instead of a wall of knobs |
| [0005](0005-sdt-measures-it-does-not-transmit.md) | SDT is a measurement, not a payload |
| [0006](0006-one-combined-anomaly-state.md) | One combined anomaly state |
| [0007](0007-no-raw-signal-leaves-the-edge.md) | No raw signal leaves the edge |
| [0008](0008-trust-the-sample-rate-then-verify-it.md) | Trust the configured sample rate, then verify it |
| [0009](0009-capture-the-microphone-with-the-system-tool.md) | Capture the microphone with the system's own tool |
