# A.V.E.N.G.E: research notes

Short version: we are testing whether you can listen to a machine at the edge, keep only the verdict, and still know afterwards what happened. This file says what we believe, what we have actually shown, and where the line between the two runs. If a claim is not backed by something you can re-run, it is marked as such. Nobody is trying to impress you here.

## 1. What this is

**Area:** applied systems R&D, edge computing, industrial signal analytics.
**Artifact:** a public, runnable proof of concept: a Go edge engine and a separately deployable ingestion and dashboard stack.
**Signals:** vibration (serial sensor) and sound (microphone).

The engine takes fixed-size windows of signal, computes features (RMS, spectrum, frequency bands), scores them against a baseline it learns by itself, keeps a single anomaly state, and publishes compact MQTT messages. The core stack stores them and draws them. That is the whole machine.

## 2. The itch

Continuous monitoring produces a continuous stream. Shipping every raw sample to a central system costs bandwidth and makes the analysis hostage to the network. Central processing is still the right answer in plenty of places; this is not a crusade. The engineering question is narrower:

> Can useful features and an anomaly state be computed at the edge, sent as a few hundred bytes, and still leave enough on record to investigate later?

This is an architecture question, not a belief that the edge always wins.

## 3. Hypotheses and where they stand

Statuses are deliberately blunt. **Supported** means a recorded test passed on the data named in the last column. It does not mean "works in general".

| ID | Hypothesis | Status | Evidence |
|---|---|---|---|
| H1 | Summaries cut upstream volume by orders of magnitude compared with raw samples | **Estimated, not measured.** A telemetry message is 177 bytes of JSON per 10,240 samples (vibration profile); with MQTT framing that is roughly 150 times less than the 3-byte serial samples, and about 1,500 times less than 16-bit sound over the same 10 s. This is arithmetic. | [E-001](experiments/E-001-data-reduction.md) (planned) |
| H2 | A statistical detector finds defined deviations in synthetic signals | **Supported on synthetic data.** Detection limit depends on the noise floor of the band, not on the tone alone. | [E-002](experiments/E-002-tone-sweep.md) |
| H3 | The per-window path does not allocate after start-up | **Supported on one machine.** Zero allocations per window, enforced by tests. Needs a repeat on the target device. | [E-003](experiments/E-003-hot-path.md) |
| H4 | Edge and core deploy independently | **Partly.** End-to-end test runs both on one host. Separate hosts and broker outages not yet exercised. | `tests/e2e/run.sh`, [E-005](experiments/E-005-broker-outage.md) (planned) |
| H5 | The message schema is enough to review an incident afterwards | **Partly.** An anomaly event names its source and band and reaches the database. Tracing an injected event to a dashboard row on real input is not done. | `tests/e2e/run.sh`, [E-004](experiments/E-004-laptop-microphone.md) (planned) |
| H6 | The audio profile works on a real room, not only on generated sound | **Not tested.** Only synthetic sound so far. The laptop microphone is next. | [E-004](experiments/E-004-laptop-microphone.md) (planned) |
| H7 | A wrong sample rate is caught while running | **Supported on live-paced synthetic pipes.** Not yet seen on real hardware. | unit tests, [LAB-NOTES](LAB-NOTES.md) |

A result counts as demonstrated only when the test and its outcome are written down.

## 4. The evidence boundary

This is the part where it is easy to cheat, so here it is in plain words.

**Version 1.** A first version of A.V.E.N.G.E was built, validated on vibration hardware, and sold. That work was done under NDA. Its details, data and configuration cannot be published, and this repository contains none of it.

**Version 2 (this repository).** A new, open-source implementation. The decision for it is the opposite: every experiment is published, with its configuration, so anybody can repeat it. Vibration experiments on real sensors are coming. The microphone is next, and numbers for it will appear here.

What that means for you as a reader:

- **Publicly reproducible.** Code, architecture, synthetic input, commands, tests and recorded experiments in this repository. Evidence levels `S` (synthetic) and, once they exist, `B` (bench hardware).
- **Context only.** The claim that version 1 was validated on hardware. You cannot check it. Treat it as background about where the ideas come from, not as evidence for anything in this repository.
- **Not claimed at all.** General fault-detection accuracy, safety certification, behaviour at arbitrary sensor counts, performance on any device we have not measured, anything about sound on real rooms until E-004 is done.

We only publish a hardware, performance or reliability claim when it is both permitted by the NDA and backed by evidence that can be shared. When we cannot, we say nothing rather than hint.

### Evidence levels

| Level | Meaning |
|---|---|
| `S` | Synthetic signal, fixed seed, anyone can reproduce it |
| `B` | Bench hardware, public data and configuration |
| `F` | Field deployment, public data |
| `N` | Under NDA, unverifiable by readers, never used as evidence here |

## 5. How we run an experiment

Decide the pass line before looking at the data. Then write down, per experiment ([template](experiments/TEMPLATE.md)): the source revision, OS, CPU and Go version, profile, sample rate, window and thresholds, the signal and its seed or a description of the recording, duration, the exact commands, the raw output, the expected and the observed result, and everything that went wrong. Keep synthetic and physical results in separate files. Failed and inconclusive runs stay in the record.

The index and the rules: [docs/experiments/](experiments/README.md).

## 6. Known limits

- Synthetic signals are cleaner than any real machine or room. They test the logic, not the world.
- A z-score against a learned baseline is sensitive to start-up conditions, drifting load and non-stationary signals. The engine learns the first seconds as normal: start it while things are normal.
- A transition to anomaly is not a diagnosis. It says "different from what I learned", not "bearing outer race".
- What a tone has to look like to be noticed is set by the noise floor in its band, not by the tone alone ([E-002](experiments/E-002-tone-sweep.md)).
- Laptop microphones apply gain control and noise suppression that change the signal. See the setup notes in [OPERATIONS.md](OPERATIONS.md).
- The serial protocol carries no clock. Time and frequencies rest on the configured sample rate, which the engine checks at run time but cannot know in advance ([ADR-0008](adr/0008-trust-the-sample-rate-then-verify-it.md)).
- MQTT QoS 1 is not durable application-level delivery. What is lost when the broker is gone is not yet measured ([E-005](experiments/E-005-broker-outage.md)).
- Resource numbers exist for one x86 machine. Nothing is claimed for a Raspberry Pi until it is measured on one.

## 7. Decisions

Significant decisions are written down as short records with their alternatives, trade-offs, a way to test them and the conditions under which we would reverse them: [docs/adr/](adr/README.md).

## 8. Where it stands

A.V.E.N.G.E v2 is a working public proof of concept for streaming vibration and sound processing, statistical anomaly detection with one combined state, summary telemetry, and an ingestion path from sensor to dashboard. The edge side is tested on synthetic data and, for sound, through a stand-in capture tool. Real sensors and a real microphone are the next thing to do, and the next thing to write down, including whatever breaks.

The mistakes made so far are in [LAB-NOTES.md](LAB-NOTES.md). They are part of the evidence.
