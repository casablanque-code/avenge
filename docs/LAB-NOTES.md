# Lab notes: what went wrong, and what it taught us

This is where we write down being wrong. Every entry below is something that really happened in this repository, in roughly the order we hit it. The fixes are in the [CHANGELOG](../CHANGELOG.md); the lessons are here, because a lesson that lives only in someone's head is a bug waiting for its second occurrence.

A measuring instrument has one unforgivable failure mode: giving a confident wrong answer without saying so. Most of what follows is us finding that mode in our own code.

---

## The 1600 Hz trap

**What we shipped.** `edge/.env.example` came with `SENSOR_SAMPLE_RATE=1600` switched on, because the reference sensor runs at 1600 Hz. The simulator in the README runs at 1000 Hz. Copy the example, follow the README, and the engine computes every frequency as if the signal were 1.6 times faster than it is.

**What happened.** Nothing crashed. Bands shifted, time stretched, the dashboards looked entirely plausible. The only thing wrong was every number.

**How we found it.** In review, reading the profile code, not by an alarm. The serial protocol carries no clock, so the engine had no way to know. We built a check that measures how fast samples actually arrive and compares it with the configured rate.

**Its first victim was the author's own `.env`.** The check did fire: one line, around twelve seconds in, between rows of events. It was not noticed. The author asked, reasonably, why nothing had said anything. Nothing had said nothing; it had said something quietly, which in a data stream is the same.

**What we changed.** The warning became a box, shown at the first measurement. The summary at the end states the outcome of the check and prints the real run time next to the number of samples, so "38.9 s" next to a minute of wall clock is hard to miss. The example no longer pre-sets a rate, and serial input refuses to start without one. See [ADR-0008](adr/0008-trust-the-sample-rate-then-verify-it.md).

**The lesson.** A safety net nobody sees is not a safety net. And a default that is right for the hardware is wrong for the simulator; one of them has to be told.

---

## Nine windows

**What we saw.** A minute of synthetic sound, about 937 windows of 64 ms. The summary said `Windows: 9`. On the vibration run it said `Anomalous 3/3 (100%)`.

**What it was.** The counter went up once per event received, and events are only emitted on a telemetry flush or a change of state. We had named a count of events "windows" and then printed percentages of it. The detector was fine. The label lied.

**What we changed.** The worker counts windows itself. The summary now shows windows processed, windows in anomaly, windows above the warning level, and events shown, each named after what it counts.

**The lesson.** Names are claims. Any number that will end up in an experiment record has to come from the thing it is named after, or the record is decoration.

---

## The detector that couldn't hear

**What we saw.** A faint 3 kHz tone in a quiet room. It barely moved the overall RMS (about 4 percent), but the combined score read +11.8 sigma in the high band and the severity said critical. In the event stream there was no `onset`. There were only `CLEAR` events, some of them with no onset before them.

**What it was.** Two definitions of "anomaly" were living in one program. Severity came from the combined score of the RMS and every frequency band. The `onset` and `clear` events came from the RMS detector alone. A tonal fault that does not raise the RMS was visible in the telemetry every ten seconds and invisible to anyone watching events. The `CLEAR` rows were severity downgrades of the RMS detector, published under the wrong name.

**The awkward part.** The architecture document already listed this as a known limitation. Writing a limitation down is honest. It is also not a fix.

**What we changed.** One combined state: the engine is in anomaly while the RMS detector or any band detector is. Onset and clear are changes of that state, they say which signal and which band caused it, and telemetry `is_anomaly` means the same thing. See [ADR-0006](adr/0006-one-combined-anomaly-state.md).

**The lesson.** Two definitions of the same word will disagree, usually at the worst moment. A known limitation in the docs is a debt with a friendly face.

---

## The tone that wasn't there

**What we did.** To test the fix above we added a tone of amplitude 0.012 to the existing noisy ambient model. The test expected one onset. It got none.

**The tempting move.** Lower a threshold until the test passes. We did not touch a threshold. We swept the tone amplitude instead ([E-002](experiments/E-002-tone-sweep.md)).

**What the sweep said.** In that noise, detection starts around 0.015 to 0.020, an RMS rise of 9 to 15 percent. In a quiet room with a low noise floor, 0.003 is enough, an RMS rise of 1 percent. The tone was not too weak. The test background was too loud for the claim we wanted to test, because what a band detector can see depends on the noise in that band.

**What we changed.** The test got its own background, a quiet room, and the experiment now records both. "Detects a tone" is not a statement until it says "over this noise floor".

**The lesson.** Do not tune the detector to pass a test you wrote five minutes ago. Measure, then decide whether the detector or the test is wrong.

---

## A release that was only half a release

**What happened.** The first attempt at 0.3.1 produced a tag, a release page on GitHub, and no image. The package on ghcr.io stayed at 0.3.0.

**Why.** Three small things, none of them dramatic.

- Cutting a release means editing the CHANGELOG in three places: rename `[Unreleased]` to the new version, keep a fresh empty `[Unreleased]`, fix the two link lines at the bottom. The repository's checks insist on all three. Two of them were missed.
- Those edits were committed straight to `main`. CI skips pushes that only touch Markdown, so nothing was red.
- The tag was then created from the GitHub release form. That makes a lightweight tag and a release page, and starts the pipeline, whose first job runs the same checks. They failed on the tagged commit, so the image build never started. The page existed anyway, because it had been made by hand: the pipeline creates it only as the last step.

**What we changed.** The tag was recreated on a commit that passes the checks. A script now does the three CHANGELOG edits and refuses to run on an empty `[Unreleased]`, another runs the pipeline's checks locally before you tag, a small workflow runs the repository checks on Markdown-only pushes, and the whole procedure is written down in [RELEASING](RELEASING.md). Nobody had written the steps down; that was the actual bug.

**The lesson.** A release process that depends on remembering three edits will fail at the first release made by someone in a hurry, and that someone is always us. If a step can be forgotten, it should be a script. And a failure that only shows on the tag run is a failure in the wrong place: it should have been red on the pull request.

---

## Small victories

- **A fake microphone, a real pipeline.** With a stand-in `arecord` producing a live-paced sine, the whole Linux microphone path ran: 256 windows in 16 seconds, rate check green, and after SIGINT or SIGTERM no capture process left behind. The stand-in is not a microphone. It does prove the plumbing.
- **Zero allocations, measured.** Processing a window takes about 18 microseconds on a 2.1 GHz virtual Xeon, with no allocations per window ([E-003](experiments/E-003-hot-path.md)). Tests keep it that way.
- **Nothing leaves but summaries, and a test says so.** The messages the engine publishes are flat objects of named numbers and short strings. A test fails if a field is added, so a waveform cannot sneak into a payload by accident ([ADR-0007](adr/0007-no-raw-signal-leaves-the-edge.md)).

---

## Still open

Listed here so nobody mistakes silence for success.

- **No real microphone yet.** The laptop test ([E-004](experiments/E-004-laptop-microphone.md)) is next. Expect surprises.
- **macOS and Windows capture** are tested as command lines only, never on those systems.
- **Firmware found, not yet run by us.** A comment in the serial reader used to point at firmware source that was not in this repository. The author's ESP32 sketch has since been added as `firmware/ADXL345/adxl345.ino`; `tests/lint/repo-checks.sh` compares its rate, baud and start marker with the engine. Its comments were left over from an 8000 Hz draft and the sketch has never been timed on a board by this project: whether it really streams 1600 Hz is for [E-006](experiments/E-006-vibration-bench.md) (and `avenge edge setup` measures it). An open question for that run: the I2C clock is not set in the sketch, and a register read at the default 100 kHz may take about as long as the 625 microsecond sample period.
- **No way to declare a new normal.** The baseline is frozen while the engine is in anomaly, so a lasting change (a fan, a TV) keeps it there until restart. Found in a simulation before any real room ([E-007](experiments/E-007-living-room-model.md)); what to do about it is not decided.
- **No Raspberry Pi numbers.** Nothing about the Pi is claimed until it is measured on one.
- **Broker outages** are designed for and not yet measured ([E-005](experiments/E-005-broker-outage.md)).
