# E-004: the audio profile in a real room

**Question:** what does the audio profile do with a real microphone in a real room: how often does it raise an alarm for nothing, and does it notice things we make happen on purpose?
**Level:** B
**Status:** planned, next. Two laptops, one on Linux and one on Windows, same plan on both.
**Hypotheses:** H5, H6

## Pass line

**Proposed, to be confirmed or changed before the first run, and then not touched.** The numbers are our starting expectation, not a claim about the engine.

| | Proposed pass line |
|---|---|
| False onsets on ordinary room sound | at most 1 per hour, in a room where nothing special is happening |
| Steady tone from a phone (1 kHz, then 3 kHz), 30 cm from the microphone, 10 s, 5 times each | onset within 2 s in at least 4 of 5, clear within 10 s after the tone ends in at least 4 of 5 |
| Fan or other steady broadband noise switched on | onset reported; clear after it is switched off |
| Knock on the desk, 10 times | no pass line: record what happens (a knock is shorter than the learning of a window-level detector) |
| Speech, 3 times 10 s | no pass line: record what happens |

If the room produces more false onsets than the line allows, that is a result, and we write it down before touching any threshold.

## Protocol (the same on Linux and Windows)

1. Switch off sound processing ([OPERATIONS](../OPERATIONS.md#before-you-measure-switch-the-sound-processing-off)). Check with a steady tone that `rms` stays flat for a minute.
2. Write the environment: `edge-filter --report`, plus the CPU, the microphone model and where it stands.
3. **One hour of the ordinary room.** `SENSOR_SOURCE=mic edge-filter --no-mqtt --record room-1.pcm`. Do not make special noise. Afterwards count the onsets: all of them are false alarms by definition. Note what the room did at those times.
4. **Controlled events**, one recording each, with times written down while doing it: tone from a phone, fan, knocks, speech. Start the engine in the quiet room first; the first 10 s are learned as normal.
5. **Repeat step 3 with automatic gain control switched on**, to see how much it matters.
6. Record the SHA-256 of every recording and its length. Keep them outside the repository.
7. **Replay** every recording from its file (`--input`) and check the result is the same as the live run. Then run them through the full stack on one machine and compare `anomaly_events` with the written times: this is the traceability check of H5.

Doing it on Windows and Linux is itself part of the experiment: the capture tools differ (`ffmpeg` with DirectShow against `arecord`), and a difference between them is a finding.

## Setup, commands, results

To be filled in when the runs happen. Expect the plan to change on contact with the room; changes go under "What went wrong", not into a rewrite of this file. The pass line above does not change after the first recording.

## What this will not show

One room, one microphone, one laptop per system. It says nothing about industrial machines, and nothing about speech in general: with a window-level energy detector, speech is just a loud, changing sound.
