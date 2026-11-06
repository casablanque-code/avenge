# E-004: the audio profile in a real room

**Question:** what does the audio profile do with a real microphone in a real room: how often does it raise an alarm for nothing, does it notice what we make happen on purpose, and what does it do with a background that is not steady?
**Level:** B
**Status:** planned. Linux first; Windows afterwards with the same plan.
**Hypotheses:** H5, H6, H8

## What the tool is for, and what it is not

A.V.E.N.G.E does not know what a fault is. It learns how a sound looks at the start (loudness, and how much energy sits in three bands: low 0 to 250 Hz, mid 250 to 2000 Hz, high 2000 to 8000 Hz) and reports when the sound becomes **different from that**. That suits a **steady machine sound**, a fan, a motor, a pump, a compressor, whose character changes: a new whine, a rattle, a louder or different hum.

It does not suit speech or television. They are non-stationary by nature, they have no "normal" in the engine's sense, and anything loud and different will be reported ([E-007](E-007-living-room-model.md)). So in this experiment speech and TV are not the target. They are a **stress test**: we measure what they do, and we do not expect them to pass.

## Versions of the pass line

The pass line is written before the data and is not bent to fit the result. That does not mean it can never change. If it turns out to be wrong, we write a new version here with the reason, keep the old one, and judge every run against the version that was in force when it was made.

- **v1** (drafted in conversation, never used for a run): at most 1 false onset per hour "in a room where nothing special is happening"; tones within 2 s; fan and knocks recorded. Replaced before any recording because the room that will be used has talking and a television, and [E-007](E-007-living-room-model.md) showed that a lasting change keeps the engine in anomaly. A line made for a silent room would have been judged on the wrong room.
- **v2** (below): in force from the first recording.

## Pass line v2

| Stage | What | Pass line |
|---|---|---|
| 0 | Smoke test, about 5 minutes | The chain works: the rate check says ok, a recording of N seconds has N times 32,000 bytes, a tone from a phone is visible in `rms` |
| 1 | The quiet room, 1 hour, TV off, nobody talking | At most 1 false onset in the hour |
| 2a | Steady sound running during the baseline (a fan, or the machine-like sound you have), then 5 tones of 10 s each from a phone at 1 kHz and 5 at 3 kHz, 30 cm from the microphone | In at least 4 of 5 each: onset within 2 s of the tone, clear within 10 s after the tone ends |
| 2b | Baseline in the quiet room, then the steady sound is switched on and left on for 5 minutes, then switched off | Onset when it is switched on, a clear after it is switched off. How long the engine stays in anomaly is recorded, not judged |
| 3 | Living-room stress, 30 to 60 minutes, TV and ordinary talk | No pass line. Predictions below are checked |
| 4 | Stage 1 repeated with automatic gain control on, 30 minutes | No pass line. The difference is recorded |
| Knocks (10), speech (3 times 10 s) | Done once, between stages | No pass line. What happens is recorded |

## Predictions, registered before the first recording

Made after the exploratory model ([E-007](E-007-living-room-model.md)), before any real data:

- **P1.** A television switched on after the baseline was learned in a quiet room produces one onset and **no clear** while it stays on (the baseline does not relearn). It clears after it is switched off.
- **P2.** With the TV on while the baseline is learned, loud speech or laughter produces an onset about once per loud burst. We do not predict a number.
- **P3.** With the TV on during the baseline and nothing else happening, the engine produces between none and a few onsets per hour. We do not predict more precisely.
- **P4.** Switching the steady sound on (stage 2b) behaves like P1.

If a prediction fails, that is a finding, and goes into [LAB-NOTES](../LAB-NOTES.md).

## Protocol, step by step (Linux)

Each step is written down as it happens: what was run, the output, what was heard or done, and the time. Nothing is tidied afterwards.

1. **Install** the capture tool: `sudo apt install alsa-utils`. Build: `cd edge && go build -o edge-filter ./cmd/edge-filter`.
2. **Environment:** `./edge/edge-filter --list-audio`, then choose the device. Put `SENSOR_SOURCE=mic` (and `AUDIO_DEVICE` if the default is not the right one) in `edge/.env`. Comment out `SENSOR_SAMPLE_RATE`.
3. **Sound processing off** ([OPERATIONS](../OPERATIONS.md#before-you-measure-switch-the-sound-processing-off)); the microphone gain fixed and not touched again during a run. Check with a steady tone that `rms` stays flat for a minute.
4. **Report:** `./edge/edge-filter --report`, pasted into this file with the CPU and the microphone model added by hand.
5. **Stage 0**, then stages 1 to 4 in order. Each recording: `SENSOR_SOURCE=mic ./edge/edge-filter --no-mqtt --record <name>.pcm`. Start the engine **while the room is in the state that should count as normal**, and leave the first 15 s alone.
6. **Write times as they happen** (a text file next to the recording: `12:03:10 tone 1 kHz on`, and so on), clock times that can be compared with the engine's output.
7. **After each recording:** SHA-256 and length written here; the recording is kept outside the repository.
8. **Replay** each recording with `--input` and check the result equals the live run. Then run the recordings through the full stack on one machine and compare `anomaly_events` with the written times: this is the traceability check of H5.

## Setup, commands, results

Filled in run by run, in the order they are done. Expect the plan to change on contact with the room; changes go under "What went wrong", not into a rewrite of the steps above. The pass line is versioned as described.

## What this will not show

One room, one microphone, one laptop. Nothing about industrial machines. Nothing about speech beyond "it is loud and changing".
