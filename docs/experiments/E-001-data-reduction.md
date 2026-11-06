# E-001: data reduction

**Question:** how much smaller is what the edge sends than what it reads, for the same input?
**Level:** S first, B once sensors exist
**Status:** planned
**Hypotheses:** H1

## Pass line

Decided before running: for the same input, bytes on the wire (MQTT payloads plus framing, retransmissions counted) are at least 100 times fewer than the bytes of the raw samples in the source format. Below 100 times, we say so and look for why.

## What we have so far (arithmetic, not a measurement)

Telemetry is one JSON message of 177 bytes (measured from the publisher with a typical message). With about 30 bytes of MQTT framing:

| Profile | Samples per telemetry message | Raw size | On the wire | Ratio |
|---|---|---|---|---|
| vibration | 20 windows x 512 = 10,240 | 30,720 B at 3 B/sample (serial format) | about 207 B | about 150x |
| audio | 10 s x 16,000 = 160,000 | 320,000 B at 2 B/sample (16-bit PCM) | about 207 B | about 1,500x |

Anomaly messages (205 bytes) are added on top, only when the state changes.

This ignores everything that is not telemetry: keep-alives, reconnects, retransmissions. That is why it is arithmetic and not a result.

## Setup (to be run)

Same synthetic input, generated with a fixed seed, through the engine with a real broker; count bytes at the broker or with a packet capture on the loopback interface.

## Commands (to be written when the run happens)

## What this will not show

Anything about real machines. Reduction is a property of the message design plus the cadence, not of the signal, apart from how many anomaly events a real signal produces.
