# E-009: the detectors on public recordings with known faults

**Question:** how does the engine do on real machine recordings made by other people, where somebody else already knows which parts are normal and which are not: does it notice the faults, and how often does it cry wolf?
**Level:** P (new: real recordings, public data, made by others; see [RESEARCH.md](../RESEARCH.md#evidence-levels))
**Status:** planned. Nothing downloaded, nothing run. The conversion tool does not exist yet.
**Hypotheses:** H9 (new), H6, H8

## Why this one

Everything measured so far is either synthetic (`S`) or one laptop in one room (`B`, E-004). Neither tells us how often the detector is right about a real fault, because nobody labelled our room. Public datasets give labels we did not produce and cannot bend.

## What the tool is for (so we judge it on the right data)

The engine learns the start of a stream as normal and reports when the sound or vibration becomes **different from that** ([E-004](E-004-laptop-microphone.md)). Machines that make a **steady** sound or vibration fit. Machines whose normal is a repeating event (a valve that clicks, a slide rail that goes back and forth) do not have a steady normal, and we expect the engine to do badly on them. We run them anyway and say so.

## The data (facts checked against the dataset papers and pages, not from memory of this project)

| Dataset | What it is | Format | Fit | Licence |
|---|---|---|---|---|
| **MIMII** (Hitachi), [Zenodo 3384388](https://zenodo.org/record/3384388) | Sound of valves, pumps, fans and slide rails, seven models each, normal and abnormal (contamination, leakage, imbalance, rail damage, ...), real factory noise mixed in | 10 s clips, 16 kHz, 16 bit (the original has 8 channels) | **Audio profile**: the same 16 kHz our profile uses. Fans and pumps are steady | Read the licence on the Zenodo page before downloading. The DCASE 2020 subset made from it is CC BY-NC-SA 4.0 (non-commercial) |
| **NASA IMS bearing** (University of Cincinnati, NASA Prognostics Data Repository) | Three run-to-failure tests, four bearings on a shaft at 2000 rpm under radial load, accelerometers | ASCII text, 20,480 points per file, 20 kHz, **one second recorded every 10 minutes** (set 2: 984 snapshots, outer race of bearing 1 fails) | **Vibration profile**, at a rate 12 times higher than the reference sensor | Check the repository's terms at download |
| CWRU bearing data (Case Western Reserve University) | Seeded bearing faults of known size, 12 kHz and 48 kHz | MATLAB `.mat` | Possible later: labelled fault classes, short recordings | Check at download |

Not used: anything we cannot cite with a link, and anything where the licence forbids using it for evaluation. **The data stays out of this repository**: only the conversion scripts, the file lists with SHA-256 and the results are committed.

## Constraints that shape the design (known before the first run)

1. **A clip is not a stream.** The audio profile learns the first 10 s as normal, which is exactly one MIMII clip. So each run feeds the engine **one constructed stream per machine model**: a block of normal clips first (the first 10 s is the baseline, the rest checks for false onsets), then a mixed block of normal and abnormal clips in a recorded order. The manifest gives the sample offset of every clip, so the truth is known by construction. The joins between clips are discontinuities the real world does not have; we count how many onsets sit on a join.
2. **IMS is not continuous either.** One second every ten minutes. Fed back to back, 6.8 days become 984 seconds, and the engine's time constants mean something else than on a real 20 kHz stream. The result says something about *change between snapshots* and nothing about continuous monitoring.
3. **The vibration profile was made for a 1 kHz-class sensor on a 50 Hz machine.** At 20,480 Hz with a 512-point window a bin is 40 Hz wide, and the built-in bands may not mean anything for a bearing. The first run uses the profile **as it is**; whether a bearing profile is needed is decided from that run and recorded as an ADR, not guessed now ([ADR-0004](../adr/0004-signal-profiles-instead-of-knobs.md)).
4. **Truth is per clip, not per millisecond.** A clip is abnormal or not. So the metrics are per clip (and per hour of normal sound), not onset timing.

## Pass line (written before the first run; Andrew accepts or changes it before any data is touched)

Per machine model, MIMII audio, SNR as in the dataset:

| Machine | Metric | Pass line |
|---|---|---|
| fan, pump (steady) | False onsets on normal clips after the baseline | at most 1 per 60 normal clips (10 minutes of sound) |
| fan, pump | Abnormal clips with an onset inside them | at least 60% of the abnormal clips |
| valve, slide rail (not steady) | all | **No pass line.** The prediction below is checked and the result is published either way |

IMS set 2, bearing 1 (fed as 984 snapshots):

| Metric | Pass line |
|---|---|
| Onset on bearing 1 before the last snapshot | yes |
| Onsets on bearings 2 to 4 in the same run | at most 5 in the whole run |
| The first onset on bearing 1 compared with the snapshot where the RMS first rises visibly (the plot of the RMS is part of the result) | recorded, not judged |

If a pass line turns out to be wrong for a reason that has nothing to do with the data, a new version is written here with the reason and the old one stays ([E-004](E-004-laptop-microphone.md) does the same).

## Predictions (also before the first run)

- **P1.** On MIMII fan and pump the engine will pass its line only in the models and SNRs where the abnormal sound has a different tone or level. In the lowest SNR the factory noise will hide most abnormal clips.
- **P2.** On valve and slide rail the engine will raise many onsets in normal clips, because every click or stroke is "different from the baseline".
- **P3.** On IMS the default vibration profile will find the late, large change and miss the early one.
- **P4.** At least one result will be worse than we like. If none is, we check the setup for a leak between the baseline and the test clips before we believe it.

## Setup (filled in at the run)

- Source revision, machine, OS, CPU, Go version: from `edge-filter --report` and the usual commands ([README](README.md#recording-the-environment))
- Profile, sample rate, window, thresholds: from `--report`; no overrides in run 1
- Data: dataset name, version or DOI, download date, SHA-256 of every archive and of every constructed stream
- Constructed streams: the script and its arguments, the random seed for the clip order, the manifest file (offsets and labels)
- Core stack: off (`--no-mqtt`); the question here is the detector, not the transport

## Commands

To be filled when the conversion tool exists. The shape: download and hash the archive, build the stream and the manifest for one machine model, run `edge-filter --no-mqtt --input stream.pcm` with the audio profile, read the events, score them against the manifest with one script, and keep the script's output in this file.

## Run 1

Not yet.

## What this will not show (said in advance)

- Anything about continuous monitoring of a machine over days, for IMS.
- Anything about our own microphone and our own rooms: that is E-004.
- General accuracy. It is accuracy on these recordings, these machine models and this constructed order of clips.
- Comparisons with other detectors: none were run. A fair comparison needs the same clips, the same windows and the same baseline rule, which is a separate experiment.

## Next

1. The conversion and scoring tool (one script per dataset, tests with synthetic input, no data in the repository).
2. Andrew accepts or changes the pass line above.
3. Run 1 on MIMII fan, model 00, then pump; IMS set 2 after that.
