# Experiments

One file per experiment. A result without its setup is a rumour, so every file says what was run, on what, with which commands, what came out, and what the result does **not** show.

## Rules

1. **Decide the pass line before you look at the data.** Write it in the file first.
2. **One experiment, one file.** Runs are added to the file; old runs stay.
3. **Failed and inconclusive runs stay.** Delete nothing that disagreed with us.
4. **Say where it ran.** Machine, OS, Go version, source revision. A number from one machine is a number about that machine.
5. **Keep synthetic (`S`) and hardware (`B`) separate.** Evidence levels are defined in [RESEARCH.md](../RESEARCH.md#evidence-levels).
6. **Every number has a command.** If it cannot be regenerated, it does not go in.

Start from the [template](TEMPLATE.md).

## Recording the environment

Every experiment file starts from the output of the engine itself:

```
./edge/edge-filter --report        # paste the result into the experiment file
```

It gives the build (version, commit, whether the tree was modified), platform, time, input and the resolved configuration, and nothing secret. It cannot know the hardware, so add by hand the CPU and the microphone or sensor:

| | Linux | Windows (PowerShell) |
|---|---|---|
| CPU | `lscpu \| grep 'Model name'` | `(Get-CimInstance Win32_Processor).Name` |
| OS | `lsb_release -d` | `(Get-CimInstance Win32_OperatingSystem).Caption` |
| Audio devices | `edge-filter --list-audio` | `edge-filter.exe --list-audio` |
| Checksum of a recording | `sha256sum room.pcm` | `Get-FileHash room.pcm` |

## Keeping the input

Anything that depends on a recording: save it with `--record`, write its SHA-256 and its length in the experiment file, and keep the file outside the repository (it may contain private sound). A result that cannot be replayed from its recording is a result nobody can check, including us in six months. See [Recording and replaying a run](../OPERATIONS.md#recording-and-replaying-a-run).

## Index

| ID | Question | Level | Status |
|---|---|---|---|
| [E-001](E-001-data-reduction.md) | How much smaller is what we send than what we read? | S, later B | planned |
| [E-002](E-002-tone-sweep.md) | How faint a tone does the detector notice, over which noise? | S | run 1 done |
| [E-003](E-003-hot-path.md) | What does a window cost, and does it allocate? | S | run 1 done (one machine) |
| [E-004](E-004-laptop-microphone.md) | What does the audio profile do in a real room? | B | planned, next |
| [E-005](E-005-broker-outage.md) | What survives when the broker goes away? | S | planned |
| [E-006](E-006-vibration-bench.md) | Does the vibration path work on a real sensor? | B | planned |
| [E-007](E-007-living-room-model.md) | What happens with a TV, speech, a sound that stays on? | S (crude model) | run 1 done, exploratory |
| [E-008](E-008-storage-growth.md) | Does the disk stay bounded in a long run? | S | planned |
