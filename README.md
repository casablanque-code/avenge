# A.V.E.N.G.E

**Anomaly Vibration Edge Network Go Engine.** Listen to a machine at the edge. Send the verdict, not the waveform.

[![CI](https://github.com/casablanque-code/avenge/actions/workflows/ci.yml/badge.svg)](https://github.com/casablanque-code/avenge/actions/workflows/ci.yml)

A vibration sensor or a microphone produces a firehose, and almost all of it says "nothing is happening". A.V.E.N.G.E sits next to the sensor, learns what normal looks like from the first seconds, watches the signal window by window (energy and frequency bands), and sends upstream a few hundred bytes: a periodic summary, and a message when the machine goes into or out of anomaly. The raw signal never leaves the device.

It is an open research project. What works, what is only measured on synthetic data, and what is merely claimed are kept apart on purpose: see [docs/RESEARCH.md](docs/RESEARCH.md).

## The idea

- **Compute where the signal is.** FFT, RMS, band energies and z-scores against a baseline the engine learns by itself, on the device.
- **One verdict.** The engine is either in anomaly or not (RMS or any frequency band, with hysteresis), and it says which band did it.
- **Summaries, not streams.** Telemetry every few seconds, an event on every change of state. No waveform, no audio.
- **Boring on purpose.** Zero runtime dependencies, a per-window hot path that does not allocate (tests enforce it), statistics you can read instead of a model you have to trust.
- **Two signals, one engine.** Vibration from a serial sensor, sound from a microphone. One setting (`SENSOR_PROFILE`) chooses the defaults.

## Run it

You need Go 1.27+, Docker with Compose 24+, and Python 3.10+.

```
# 1. Configure: set every password in core/.env, MQTT_USER=edge and MQTT_PASSWORD in edge/.env
cp core/.env.example core/.env
cp edge/.env.example edge/.env

# 2. Core stack (broker, ingestion, database, dashboards)
docker compose -f core/compose.yaml up -d

# 3. Edge binary
cd edge && go build -o edge-filter ./cmd/edge-filter/ && cd ..

# 4. Feed it a synthetic signal with a fault injected after 15 seconds
python3 firmware-sim/signal_generator.py stream --anomaly-after 15 \
  | ./edge/edge-filter --broker localhost:1883 --sensor-id bearing_01
```

Dashboards: `http://grafana.localhost` (user `admin`, password `GRAFANA_ADMIN_PASSWORD` from `core/.env`).

No broker at hand? Add `--no-mqtt` and the engine just prints. A microphone instead of the simulator: set `SENSOR_SOURCE=mic` in `edge/.env` and run `./edge/edge-filter --explain` to see exactly what will happen. Everything the binary understands: `./edge/edge-filter --help`.

Real sensors, secrets, ports, troubleshooting: [docs/OPERATIONS.md](docs/OPERATIONS.md). For a real core and edge, `avenge core setup` and `avenge edge setup` ask the questions and write the `.env` files (same document).

## Where to read next

| If you want to know... | Read |
|---|---|
| What is proven, what is not, and why | [docs/RESEARCH.md](docs/RESEARCH.md) |
| What went wrong along the way | [docs/LAB-NOTES.md](docs/LAB-NOTES.md) |
| The measurements, with the data | [docs/experiments/](docs/experiments/README.md) |
| Why it is built this way | [docs/adr/](docs/adr/README.md) |
| The concept and the algorithms | [docs/DOC.md](docs/DOC.md) |
| How the parts fit together | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| How to run and fix it | [docs/OPERATIONS.md](docs/OPERATIONS.md) |
| What CI checks | [docs/TESTING.md](docs/TESTING.md) |
| What changed | [CHANGELOG.md](CHANGELOG.md) |
| The layout of the repository | [docs/README.md](docs/README.md) |

Licensed under GPL-3.0, see [LICENSE](LICENSE).
