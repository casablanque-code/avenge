# Operations

How to run, configure and maintain the AVENGE stack. Concept and algorithms: [DOC.md](DOC.md).

## Ports

| Service    | Address                                       | Notes |
| ---------- | --------------------------------------------- | ----- |
| Grafana    | `http://grafana.localhost`                    | Via Caddy on `127.0.0.1:80`, same machine only. Grafana publishes no port. |
| Mosquitto  | `$MQTT_BIND:1883`                             | Auth + ACL required. Set `MQTT_BIND` to the LAN IP so edge devices can connect. |
| ClickHouse | `127.0.0.1:8123` (HTTP), `127.0.0.1:9000`     | Local only. |

## First run

Core (server or laptop):

```
cp core/.env.example core/.env         # set every password (letters and digits only)
docker compose -f core/compose.yaml up -d
```

That is all. Startup is ordered and checked automatically:

1. `preflight` rejects missing, placeholder (`changeme`) and non-alphanumeric passwords with a readable message.
2. `mosquitto` builds its password file from `.env` at every start.
3. `clickhouse-init` applies the schema as soon as ClickHouse is healthy (idempotent, safe on every `up`).
4. `telegraf` and `grafana` start once the schema exists.

Edge (Raspberry Pi):

```
cp edge/.env.example edge/.env         # SERIAL_INTERFACE, SENSOR_SAMPLE_RATE, CENTRAL_HOST, MQTT_PASSWORD, DIALOUT_GID
docker compose -f edge/compose.yaml up -d
```

## Accounts

Usernames are fixed by the configuration; only passwords live in `.env`.

| Service            | Username                          | Purpose                          | Permissions                                   | Password variable (`core/.env`)  |
| ------------------ | --------------------------------- | -------------------------------- | --------------------------------------------- | -------------------------------- |
| Grafana (web)      | `admin`                           | Dashboard login                  | Admin                                         | `GRAFANA_ADMIN_PASSWORD`         |
| ClickHouse (admin) | `default` (or `CLICKHOUSE_USER`)  | Manual queries, schema           | Everything                                    | `CLICKHOUSE_PASSWORD`            |
| ClickHouse         | `telegraf`                        | Telegraf writes telemetry        | INSERT on `iot.telemetry_raw` only            | `CLICKHOUSE_TELEGRAF_PASSWORD`   |
| ClickHouse         | `grafana`                         | Grafana reads                    | SELECT on `iot.*`, read-only                  | `CLICKHOUSE_GRAFANA_PASSWORD`    |
| MQTT               | `edge`                            | Edge devices publish             | Write `sm/telemetry/#`, `sm/anomaly/#`        | `MQTT_EDGE_PASSWORD`             |
| MQTT               | `telegraf`                        | Telegraf subscribes              | Read `sm/telemetry/#`, `sm/anomaly/#`, `$SYS` | `MQTT_TELEGRAF_PASSWORD`         |

The edge device stores the `edge` password as `MQTT_PASSWORD` (and the user as `MQTT_USER`) in `edge/.env`; the two values must match the core side.
Source of truth: `core/mosquitto/config/acl` and `core/clickhouse/users.d/avenge-users.xml`.

## Changing a password

Edit `core/.env`, then run:

```
docker compose -f core/compose.yaml up -d
```

Compose notices the changed environment and recreates only the affected services; the broker rebuilds its password file and ClickHouse reloads its accounts on start. Then:

- `MQTT_EDGE_PASSWORD`: put the same value into `MQTT_PASSWORD` in every `edge/.env` and run `docker compose -f edge/compose.yaml up -d` on each device.
- `GRAFANA_ADMIN_PASSWORD`: Grafana reads it only on its first start. For a live instance run `docker exec sm_grafana grafana cli admin reset-admin-password '<new>'` and update `.env`.

Plain `docker compose restart` does **not** pick up `.env` changes; always use `up -d`.

Rules: letters and digits only. Never commit `.env`.

## Troubleshooting

| Symptom                                                 | Cause                                           | Fix |
| ------------------------------------------------------- | ----------------------------------------------- | --- |
| `required variable ... is missing a value`              | `core/.env` missing or incomplete               | `cp core/.env.example core/.env`, fill in every password. |
| `preflight` prints `ERROR: ... placeholder` or `... only letters and digits` | Placeholder or unsupported character in a password | Fix the named variable in `core/.env`, run `up -d`. |
| Edge exits: `SENSOR_SAMPLE_RATE is required for SERIAL_INTERFACE` | Serial input without a sample rate | Set the real rate of the sensor in Hz in `edge/.env` (the reference setup, an ADXL345, runs at 1600), `up -d`. |
| Edge prints `warning: rate check: samples arrive at ... Hz` | The source runs at another rate than `SENSOR_SAMPLE_RATE` | Make the capture command or the sensor use the configured rate, or set `SENSOR_SAMPLE_RATE` to the measured one. |
| Telegraf log: `not authorised`                          | `.env` edited but containers not recreated      | `docker compose -f core/compose.yaml up -d`. |
| Edge log: `CONNACK: ... not authorized`                 | `MQTT_PASSWORD` differs from `MQTT_EDGE_PASSWORD` | Align the two, `up -d` on the device. |
| Telegraf log: `Not enough privileges`                   | Schema from an older version                    | `docker compose -f core/compose.yaml run --rm clickhouse-init`, then `up -d`. |
| Grafana datasource error                                | Stale Grafana password                          | `docker compose -f core/compose.yaml up -d`. |
| Edge exits with permission denied on `/dev/tty*`        | Wrong `DIALOUT_GID` or `SERIAL_PORT`            | `getent group dialout` on the host, fix `edge/.env`. |

## Health checks

```
docker compose -f core/compose.yaml ps                   # long-running services Up; grafana/clickhouse/mosquitto healthy
ss -ltnp | grep -E ':(80|1883|8123|9000)\b'              # only 127.0.0.1, 1883 on $MQTT_BIND
curl -sI http://grafana.localhost | head -1              # HTTP/1.1 302 Found
```

`preflight` and `clickhouse-init` show as `Exited (0)`; that is expected.

## Command line

```
edge-filter --help       # every flag and every setting, with defaults
edge-filter --explain    # what the current settings resolve to, with warnings
edge-filter --version    # version, commit and platform of this build
edge-filter --report     # the same plus time and input, as markdown for an experiment record
```

Settings live in `edge/.env`; an exported variable overrides the file and a flag overrides both. Run `--explain` after every change to them.

## Sample-rate check

A serial device or a microphone pipe sends no clock, so the engine trusts `SENSOR_SAMPLE_RATE` for time and for every frequency. To catch a wrong value it measures how fast samples really arrive. After about 12 seconds it prints one line, or, when the rate is wrong, a box:

```
rate check: samples arrive at 1002 Hz, SENSOR_SAMPLE_RATE is 1000 - ok

┌──────────────────────────────────────────────────────────────────────────────┐
│ RATE MISMATCH                                                                │
│ Samples arrive at 1002 Hz, but SENSOR_SAMPLE_RATE is 1600.                   │
│ Time and every frequency in this run are wrong.                              │
│ Fix the rate of the source or SENSOR_SAMPLE_RATE in edge/.env, then restart. │
└──────────────────────────────────────────────────────────────────────────────┘
```

The first 2 seconds are ignored (pipes deliver a start-up burst), a deviation above 5% is a mismatch, and a mismatch is repeated every minute. Input much faster than real time (a file or a generator) is recognised as a replay and skipped. The summary at the end of the run repeats the outcome (`Rate check : ok (...)`, `MISMATCH (...)`, `replay`, or `not measured` for runs shorter than about 12 seconds) and shows `Real time` next to the sample count, so a wrong rate is visible there as well. Do not ignore a mismatch: all frequencies, band names and detection delays of that run depend on the rate.

Which rate to expect: the simulator (`firmware-sim/signal_generator.py`) and the vibration profile default to 1000 Hz, the reference sensor (an ADXL345) runs at 1600 Hz, sound is captured at 16000 Hz. For the hardware set `SENSOR_SAMPLE_RATE=1600`; for the simulator at 1600 start it with `--rate 1600`.

**Upgrading:** older versions of `edge/.env.example` had `SENSOR_SAMPLE_RATE=1600` switched on. An `edge/.env` copied from them keeps that line, and with the 1000 Hz simulator, or with sound, it now produces the mismatch box. Check the line in your own `edge/.env`: comment it out, or set the real rate of your source.

## Console output

Each printed line is one event: a periodic telemetry flush or an anomaly state change, not one window. `win N` is the number of the window that produced the event; `N * window length` is the time since start in sample time. The summary at the end counts all processed windows and the windows whose combined severity was above normal (`Above warning`).

### Anomaly events

The engine is in anomaly while the RMS detector or any frequency-band detector is in anomaly. Each change of that state is one event, shown as `ANOMALY [severity] via rms|band <name>` and `CLEAR (was ...)`, and sent to `iot.anomaly_events` as `onset` or `clear` with the columns `dominant_signal` and `band`. A tone that barely changes the overall RMS but stands out in its band is reported with the band's name.

Upgrading an existing installation: `docker compose -f core/compose.yaml up -d` is enough; `clickhouse-init` adds the two columns and recreates the parsing view. Older rows keep empty values in the new columns. Update the edge at the same time: an old edge sends no `band`.

Timestamps sent over MQTT are the wall-clock time at which the window completed on the edge machine. When the core runs on another host, keep both clocks in sync (NTP) before comparing events.

## Signal profiles

One setting tells the edge engine what kind of signal it is watching. Everything that depends on it (window length, frequency bands, thresholds, adaptation speed, telemetry cadence) is filled in automatically.

```
SENSOR_PROFILE=vibration    # default
SENSOR_PROFILE=audio
```

| | `vibration` | `audio` |
| --- | --- | --- |
| For | accelerometer on a machine, values in g | microphone, full scale = 1.0 |
| Default sample rate | 1000 Hz with stdin; serial input requires `SENSOR_SAMPLE_RATE` (e.g. 1600) | 16000 Hz |
| Window | 512 samples | 1024 samples (64 ms) |
| Baseline learned from | first 10 windows | first 10 s |
| Telemetry | every 20 windows | every 10 s |
| Bands | shaft 0-50, harmonic 50-150, fault 150-400, hf_noise 400-Nyquist (Hz) | low 0-250, mid 250-2000, high 2000-Nyquist (Hz) |
| Default input on stdin | JSONL `{t, value}` | raw PCM, 16-bit little-endian, mono |

`SENSOR_SAMPLE_RATE` must always equal the real sample rate of your source, because the engine derives time and frequencies from it. It is required for a serial interface (the engine refuses to start without it) and optional for stdin, where the profile default applies; for sound the capture command must use exactly that rate (`arecord -r`, `ffmpeg -ar`). `SENSOR_INPUT` (`jsonl` or `pcm_s16le`) overrides the stdin format; a serial interface (`SERIAL_INTERFACE`) overrides it too.

To see what a profile resolves to, with warnings for settings that cannot work (for example a frequency band above the Nyquist frequency):

```
SENSOR_PROFILE=audio edge-filter --explain
```

### Sound from a microphone

Run the binary on the host (the Docker image has no sound device). Three steps:

```
# 1. install the capture tool once
#    Linux: sudo apt install alsa-utils    macOS: brew install ffmpeg    Windows: winget install Gyan.FFmpeg
# 2. in edge/.env
SENSOR_SOURCE=mic          # implies SENSOR_PROFILE=audio
# AUDIO_DEVICE=            # empty = default device; required on Windows
# 3. check, then run
./edge/edge-filter --list-audio      # microphones, to fill AUDIO_DEVICE
./edge/edge-filter --explain         # ends with the exact capture command that will run
./edge/edge-filter --broker localhost:1883 --sensor-id mic
```

The engine starts the capture command itself, at the profile's sample rate (16000 Hz), and stops it on exit. If the command ends or fails, the engine says so and exits with status 1. Linux uses `arecord` (`AUDIO_DEVICE` is an ALSA name such as `default` or `plughw:1,0`), macOS and Windows use `ffmpeg` (`AUDIO_DEVICE` is a number or name from `--list-audio`). Start the engine while the sound is in its normal state: the first 10 seconds are learned as normal.

Any other raw PCM source works through a pipe, for example `ffmpeg -i recording.wav -f s16le -ac 1 -ar 16000 - | SENSOR_PROFILE=audio edge-filter`.

#### Before you measure: switch the sound processing off

Laptop microphones and many drivers apply automatic gain control (AGC), noise suppression, echo cancellation or "voice isolation". These change the level and the spectrum of the signal, so a constant sound looks different from minute to minute and the learned baseline means little. In short:

- Use a plain input: turn off audio enhancements, noise suppression, AGC and voice isolation in the system sound settings for that microphone, and in the driver's own control panel if it has one.
- On Linux avoid the PulseAudio/PipeWire echo-cancel or noise-suppression modules; `AUDIO_DEVICE=plughw:X,Y` (see `arecord -l`) goes around the sound server.
- Check it: play a steady tone from a phone at a fixed distance. `rms` in the output should stay almost constant for a minute and should not creep up or down. If it does, something is still adjusting the level.

### Recording and replaying a run

Experiments must be repeatable, so a run can be saved and played again with different settings or on another machine.

```
SENSOR_SOURCE=mic ./edge/edge-filter --no-mqtt --record room.pcm     # saves the raw sound while it runs
SENSOR_PROFILE=audio ./edge/edge-filter --no-mqtt --input room.pcm   # replays it, as fast as it can be processed
```

`--record` is off unless you ask for it, writes to a **new** file (an existing file is never overwritten), creates it readable by you only, and says so in a box when it starts. The file is raw 16-bit mono PCM at the profile's rate, a little over 110 MB per hour at 16 kHz. It stays on the machine; the engine never sends it anywhere, and the MQTT messages are unchanged ([ADR-0007](adr/0007-no-raw-signal-leaves-the-edge.md)). Treat it as what it is: a recording of a room, possibly with speech in it. Do not commit recordings; keep them next to your notes and write down their SHA-256.

A recording replayed with the same settings and the same build gives the same output. That is checked in the tests and on the synthetic sound in the repository.

To listen to a recording: `ffmpeg -f s16le -ar 16000 -ac 1 -i room.pcm room.wav`.

### Windows and macOS

The microphone path is the same everywhere; serial sensors are Linux-only.

- Build on the machine (`cd edge`, then `go build -o edge-filter.exe ./cmd/edge-filter` on Windows), or cross-compile with `GOOS=windows`.
- Run from the repository root, so that `edge/.env` is found: `edge\edge-filter.exe --list-audio`.
- Windows needs `ffmpeg` and an explicit `AUDIO_DEVICE` (the name from `--list-audio`, for example `Microphone (Realtek Audio)`).
- **Do not push PCM through PowerShell**: its `|` and `<` treat the data as text and damage it. Use `SENSOR_SOURCE=mic` (the engine starts the capture itself) and `--input file.pcm` (the engine reads the file itself).
- The engine does not need the core stack to be tested: run with `--no-mqtt` and `--record`, then replay the recording through the full stack on another machine.

### Testing with a speaker

The simulator can produce a sound with a known fault: a 120 Hz hum with noise and a 2.5 kHz tone that fades in halfway through.

```
# replay without any audio hardware
python3 firmware-sim/signal_generator.py --profile audio --seed 1 batch --samples 960000 --anomaly > sound.pcm
SENSOR_PROFILE=audio ./edge/edge-filter --no-mqtt < sound.pcm

# or play it through a speaker and let a microphone capture it
python3 firmware-sim/signal_generator.py --profile audio batch --samples 960000 --anomaly --format wav --file test.wav
aplay test.wav            # in one terminal, while the line from the previous section runs in another
```

The file is 60 s long; the tone starts at 30 s, after the 10 s learning period. Real rooms add their own noise and echo, so expect the onset to be less crisp than in a replay.

### What to expect from the audio profile

- It is a first approximation, calibrated on the simulator's synthetic sound, not on real recordings. Treat the numbers as a starting point.
- A new tone about 60% as strong as the existing background (by RMS) is reported within about a second; a tone as strong as the background within about half a second. Quieter additions are not reported, by design: the thresholds favour few false alarms.
- The baseline follows slow changes with a time constant of about 30 s, so a sound that grows slower than that is absorbed into "normal" instead of reported.
- Steady ambient sound produced about one warning per two minutes in the synthetic tests.

## Data retention

| Table                  | Contents                                   | Kept |
| ---------------------- | ------------------------------------------ | ---- |
| `iot.telemetry_raw`    | Raw JSON batches from Telegraf (a buffer)  | 7 days |
| `iot.telemetry`        | Parsed telemetry points                    | 30 days |
| `iot.anomaly_events`   | Anomaly state transitions                  | 90 days |
| `iot.telemetry_1m`     | Per-minute aggregates (one row per sensor per minute) | Indefinitely |

Retention is set in `core/clickhouse/init.sql` and applied by `clickhouse-init` on every `up -d`. Raw rows written before the 7-day TTL existed are not expired automatically; to reclaim that space once, run `docker exec sm_clickhouse clickhouse-client --user default --password "$CLICKHOUSE_PASSWORD" --query "TRUNCATE TABLE iot.telemetry_raw"` (the parsed tables are unaffected).
