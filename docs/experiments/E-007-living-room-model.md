# E-007: a crude living room

**Question:** what does the audio profile do when the background is not steady: a television, speech, or a sound that is switched on and then simply stays on?
**Level:** S (a deliberately crude model)
**Status:** run 1 done. Exploratory: written down after the first look, because the result changed the plan of [E-004](E-004-laptop-microphone.md).
**Hypotheses:** H8

## Pass line

None. This is exploratory, and it is honest to say so. The scenarios were chosen from the question "what will happen in a normal room with a TV and people talking", and the outcomes were not predicted before the first run. E-004 registers its predictions before it starts.

## Setup

- Source revision: `main` at `c063cfc` plus the harness added in the same commit as this file (`edge/internal/profile/experiment_test.go`).
- Machine and toolchain: as in [E-002](E-002-tone-sweep.md) (Anthropic-hosted sandbox, Go 1.22.2). The result is deterministic given the seeds.
- Profile: `audio`, 16,000 Hz, 1,024-sample windows (64 ms), the first 10 s are learned as normal, anomaly above 5.0 sigma (RMS) or 5.5 sigma (a band).
- The "TV" is a model, not a recording: noise passed through a one-pole low-pass (about 3 kHz), a 4 Hz syllable-like amplitude modulation, and a level that wanders slowly (between 0.6 and 1.4 of nominal). Nominal amplitude 0.02, against a quiet-room background of a 120 Hz hum (0.020) and noise (0.004). "Speech" is the same model at twice the amplitude for 3 s.
- Scenarios, seeds 1 to 3:
  - **A:** quiet room for the baseline, the TV is switched on at 30 s and **left on** (330 s in total).
  - **B:** the TV is already on while the baseline is learned; every 25 to 45 s a 3 s louder burst (speech), 600 s in total.
  - **C:** the TV is on during the baseline, nothing else happens, 600 s.

## Commands

```
cd edge
AVENGE_EXPERIMENT=living-room go test -count=1 -run TestExperimentLivingRoom -v ./internal/profile
```

## Run 1

```
A: quiet baseline, TV on from 30 s and left on, 330 s total
  seed 1: in anomaly 4687/5155 windows  [ONSET@30s band high]
  seed 2: in anomaly 4686/5154 windows  [ONSET@30s band high]
  seed 3: in anomaly 4686/5154 windows  [ONSET@30s band high]
B: TV on during baseline; 3 s louder burst (speech, x2) every 25-45 s, 600 s
  seed 1: bursts=16 onsets=18  in anomaly 338/9374 windows
  seed 2: bursts=15 onsets=13  in anomaly 383/9374 windows
  seed 3: bursts=15 onsets=16  in anomaly 340/9374 windows
C: TV on during baseline, nothing else, 600 s
  seed 1: onsets=0  in anomaly 0/9374 windows
  seed 2: onsets=2  in anomaly 2/9373 windows
  seed 3: onsets=1  in anomaly 1/9374 windows
```

## Observed

- **A: a lasting change is never absorbed.** One onset when the TV arrives, in the high band, and no clear for the remaining 300 s: the engine spends about 91 percent of all windows in anomaly. The reason is in the code and in [ADR-0002](../adr/0002-statistics-not-machine-learning.md): while the engine is in anomaly its baseline is frozen, so that a degrading machine is not quietly accepted as normal. The price is that a change which is *meant* to be the new normal is never accepted either, until the engine is restarted. There is no way to say "this is normal now" other than restarting.
- **B: loud bursts are noticed.** 13 to 18 onsets for 15 or 16 bursts: nearly one per burst, sometimes two for one burst, sometimes none. About 4 percent of the windows are in anomaly. By design a louder sound is different; in a living room that is the speech.
- **C: a lively but constant background is mostly quiet.** 0, 2 and 1 onsets in 10 minutes, a rough 0 to 12 per hour. Three seeds are not a rate; it says the false-alarm rate on a non-stationary background is not zero.

## What this does not show

- Anything about a real television or real speech. The model has no formants, no music, no scene changes, no hard cuts.
- That the behaviour in A is wrong. For a machine it can be exactly right. It shows that the tool has no notion of "normal has changed".
- Anything about other levels. One TV amplitude, one burst gain.

## Consequences

- E-004 gets a staged plan, a statement of what the tool is for, and registered predictions (A and C above).
- A decision is open on re-learning: restart only (today), an explicit re-learn command, or an automatic re-learn after a long sustained anomaly, off by default. Nothing is decided here; it needs an ADR and a test of its own.
