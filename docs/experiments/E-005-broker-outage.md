# E-005: when the broker goes away

**Question:** what is delivered, what is buffered and what is lost when the broker disappears and comes back?
**Level:** S
**Status:** planned
**Hypotheses:** H4

## Pass line (to be fixed before the run)

Decide first which losses are acceptable. The design has a queue of 512 messages in the client; the experiment finds out whether that is what happens, in which order messages arrive afterwards, and what is lost beyond the queue.

## Plan

Synthetic input with injected events at known times. Stop the broker for different durations (shorter and longer than the queue allows), restart it, then compare the stored telemetry and anomaly events with the injected timeline. Also: kill the edge process during an outage, and restart the whole stack.

## What this will not show

Durable application-level delivery. MQTT QoS 1 does not provide it, and the engine keeps no disk queue.
