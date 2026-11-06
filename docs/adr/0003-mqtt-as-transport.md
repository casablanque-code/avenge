# ADR-0003: MQTT 3.1.1 with QoS 1 as the transport

**Status:** accepted. Written retrospectively.

## Context

Small messages, from many possible devices, over networks that come and go, into one ingestion point.

## Options considered

1. **HTTP POST per message.** Easy, and every language has it. No built-in queueing semantics, and a connection per message or an awkward long one.
2. **MQTT.** Made for this: small framing, persistent connection, a broker that decouples producers from consumers, topics for routing, per-user access control.
3. **A log such as Kafka.** Powerful, and far too much machine for a Raspberry Pi and a handful of sensors.

## Decision

MQTT 3.1.1 at QoS 1 (at least once), with a small client written against the standard library to keep the zero-dependency promise, wrapped in a resilient layer that reconnects and queues up to 512 messages. The broker requires authentication and enforces per-user topic ACLs.

## Trade-offs

At-least-once means possible duplicates. A queue in memory means messages are lost if the edge process dies during an outage. QoS 1 is not durable application-level delivery, and we do not pretend otherwise.

## How to test it

[E-005](../experiments/E-005-broker-outage.md): stop the broker for different durations and compare what arrives with what was injected.

## Revisit when

Outages longer than the queue are normal in a deployment (then a disk-backed queue), or MQTT 5 features become necessary.
