# ADR-0001: The edge computes, the core stores and shows

**Status:** accepted. Written retrospectively.

## Context

A sensor next to a machine can produce thousands of samples per second. Something has to compute on them and something has to keep and display the results. The two have different homes: the first belongs beside the sensor, on a small device that may be on a bad network; the second belongs on a server that is happy to run a database.

## Options considered

1. **Everything central.** Ship raw samples to a server and compute there. Simple to build, and the right choice in many places. Costs bandwidth, and analysis stops when the network does.
2. **Everything on the device.** Compute, store and draw on the edge. Robust, but a small device is a poor database and a poor dashboard server, and many devices means many islands.
3. **Split.** Compute on the edge, send summaries, store and show centrally.

## Decision

Split. `edge/` is one static Go binary (or a distroless image) configured by `.env`; `core/` is one compose file with the broker, ingestion, database and dashboards. They meet only over MQTT.

## Trade-offs

Two things to deploy and keep in version step with each other (the message schema is a contract; see the upgrade notes in the [CHANGELOG](../../CHANGELOG.md)). Raw data is gone after the window is processed: you cannot re-analyse yesterday's waveform with a better idea.

## How to test it

Run edge and core on separate hosts, break the link, and see what is lost ([E-005](../experiments/E-005-broker-outage.md)). The end-to-end test runs both on one host today.

## Revisit when

We need waveform snapshots around an anomaly (then the edge needs a bounded local buffer and a way to ship it), or a deployment cannot tolerate a central dependency at all.
