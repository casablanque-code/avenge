# E-002: how faint a tone does the detector notice

**Question:** with the audio profile, how weak can a steady tone be before the engine stops reporting it, and what decides that?
**Level:** S
**Status:** run 1 done
**Hypotheses:** H2 (and the logic behind H6)

## Pass line (stated before the run)

- No false onsets on the background alone.
- A tone is reported within one window of appearing once it is strong enough, and the amplitude at which it starts being reported is recorded for each background.
- We do **not** set a required amplitude. The point is to find out where the limit sits and why.

## Setup

- Source revision: `main` at `307345e` plus the harness added in the same commit as this file (`edge/internal/profile/experiment_test.go`).
- Machine: Anthropic-hosted sandbox, Intel Xeon @ 2.10 GHz, 1 CPU, Linux x86-64. Go 1.22.2 (the repository asks for Go 1.27; the toolchain was not available there, so the module file was relaxed outside the repository for this run). The signal path is deterministic given the seed; please repeat on a machine with the project toolchain before quoting numbers.
- Profile: `audio`. 16,000 Hz, 1,024-sample windows (64 ms), first 10 s learned as normal, anomaly above 5.0 sigma for the RMS and 5.5 sigma for a band, clears below 3.5. Bands: low 0-250 Hz, mid 250-2000 Hz, high 2000-8000 Hz.
- Signals, 90 s each, tone switched on at 45 s (a step), 3,000 Hz, seeds 1 to 5:
  - **ambient:** 120 Hz hum 0.030 plus 360 Hz 0.010, Gaussian noise 0.010 with a slow 15 percent swell.
  - **quiet room:** 120 Hz hum 0.020 plus Gaussian noise 0.004.
- Detected means: an onset starting no later than 20 s after the tone. Delay is measured from the tone to the start of the window that reported it. False means any onset before 44 s, that is, on the background alone.

## Commands

```
cd edge
AVENGE_EXPERIMENT=tone-sweep go test -count=1 -run TestExperimentToneSweep -v ./internal/profile
```

## Run 1

```
background                    tone  RMS rise  detected median delay   false
ambient (noise 0.010)        0.006      1.4%      0/5            -       0
ambient (noise 0.010)        0.010      4.0%      0/5            -       0
ambient (noise 0.010)        0.015      8.9%      1/5        0.1 s       0
ambient (noise 0.010)        0.020     15.4%      5/5        0.0 s       0
ambient (noise 0.010)        0.030     32.2%      5/5        0.0 s       0
quiet room (noise 0.004)     0.001      0.1%      0/5            -       0
quiet room (noise 0.004)     0.002      0.4%      0/5            -       0
quiet room (noise 0.004)     0.003      1.0%      5/5        0.1 s       0
quiet room (noise 0.004)     0.004      1.8%      5/5        0.0 s       0
quiet room (noise 0.004)     0.006      4.0%      5/5        0.0 s       0
```

Related: `TestAudioProfileStaysQuietOnSteadyAmbientSound` (four seeds of 150 s of the ambient model, no false onset) and `TestAudioProfileReportsABandOnlyAnomaly` (quiet room, tone 0.004, three seeds, one onset naming the high band and one clear each) pass in CI.

## Observed vs expected

- **No false onsets:** 0 in 50 runs of 44 s background (about 37 minutes of synthetic background in total). As expected, on synthetic noise.
- **Reported within one window once strong enough:** yes. Delays are 0.0 to 0.1 s. The delay is limited by the window (64 ms), because the score jumps by many sigma at once.
- **Where the limit sits:** over the ambient noise floor, between 0.010 and 0.020 (RMS rise 4 to 15 percent). Over the quiet room, between 0.002 and 0.003 (RMS rise 0.4 to 1 percent). **The same detector, a factor of about six apart in tone amplitude.** The tone does not decide whether it is noticed; the noise in its band does.
- **The edge of the limit is ragged.** At 0.015 over ambient noise one seed in five detected it. The edge is a region, not a line.

## What went wrong

The first version of the band-only test used the ambient background with a tone of 0.012 and expected an onset. There was none. We first suspected the detector, then measured instead of tuning: this sweep is the result, and it is why the test now has its own quiet-room background. See [LAB-NOTES](../LAB-NOTES.md#the-tone-that-wasnt-there).

## What this does not show

- Anything about a real room or a real microphone. Synthetic noise is Gaussian and stationary, which real rooms are not.
- Frequency dependence. One tone frequency (3 kHz, high band) was used.
- Ramps, bursts and intermittent tones. The tone is a step.
- A usable false-alarm rate. 37 minutes of clean synthetic noise says the logic is quiet on its own model; E-004 measures it on a real room.

## Next

Repeat with the project toolchain on the target machine, then with a recorded room ([E-004](E-004-laptop-microphone.md)).
