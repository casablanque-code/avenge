# E-004: the audio profile in a real room

**Question:** what does the audio profile do with a real microphone in a real room: how often does it raise an alarm for nothing, and does it notice things we make happen on purpose?
**Level:** B
**Status:** planned, next
**Hypotheses:** H5, H6

## Pass line (to be fixed before the run)

To decide first, in writing, with the first recording of the room in hand but before looking at any detection result:

- an acceptable false-onset rate per hour of ordinary room sound;
- which controlled events must be noticed (a steady tone from a phone, a knock, a voice, a fan switched on) and within how long;
- how long a clear may take after the event ends.

## Plan

1. Set up the microphone with sound processing off ([OPERATIONS](../OPERATIONS.md#before-you-measure-switch-the-sound-processing-off)). Check with a steady tone that `rms` stays flat.
2. Record one hour of the ordinary room with the engine running. Count onsets: all of them are false alarms by definition. Note what the room did at those moments.
3. Controlled events, each repeated several times at a fixed distance, with times written down. Compare with `anomaly_events`.
4. Repeat step 2 with automatic gain control switched on, to see how much it matters.
5. Record the operating system, the microphone, the capture command shown by `--explain`, and the rate check result.

## Setup, commands, results

To be filled in when the run happens. Expect the plan to change on contact with the room; changes go under "What went wrong", not into a rewrite of this file.

## What this will not show

One room, one microphone, one laptop. It says nothing about industrial machines.
