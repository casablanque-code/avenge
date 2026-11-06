# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). While the major version is 0, a minor release may contain breaking changes; they are marked **Breaking**.

Entries for 0.1.0 to 0.2.4 were reconstructed from the git history and the contents of each tag. Release notes on GitHub are taken from the matching section of this file.

## [Unreleased]

### Added

- Signal profiles: `SENSOR_PROFILE=vibration|audio` fills in the window length, frequency bands, thresholds, baseline time constant and telemetry cadence for the kind of signal, so nothing else needs tuning. `vibration` (the default) equals the previous built-in values exactly; the output on a replayed signal is byte-identical to the previous version. `audio` is a first approximation calibrated on synthetic sound.
- `edge-filter --explain` prints what the chosen profile resolves to (sample rate, window, bin width, baseline, telemetry cadence, thresholds, bands) in a box, with warnings for settings that cannot work, such as a band above the Nyquist frequency.
- Raw PCM input for sound: `SENSOR_INPUT=pcm_s16le` (the audio default) reads 16-bit little-endian mono PCM from stdin, for example `arecord -q -f S16_LE -r 16000 -c 1 -t raw | edge-filter`.
- Simulator audio profile: `signal_generator.py --profile audio` (hum, drifting noise, a 2.5 kHz tone as the anomaly) with `--format pcm|wav|jsonl`, so a WAV file can be played through a speaker, and `--profile`, `--format` and `--seed` accepted after the sub-command.
- `edge-filter --help` lists every flag and every `.env` setting with defaults and examples; a test fails when `edge/.env.example` and the help drift apart.
- `SENSOR_SOURCE=mic` records the microphone from `edge/.env` on Linux (`arecord`), macOS and Windows (`ffmpeg`), at the profile's sample rate, and implies the audio profile. `AUDIO_DEVICE` picks the device, `edge-filter --list-audio` lists them, and `--explain` shows the exact capture command. The engine stops the capture command on exit and reports it if it fails. The Linux path is tested end to end with a stand-in `arecord`; the macOS and Windows commands are tested only as command lines and still need a run on real hardware. See `docs/OPERATIONS.md`, including how to switch off AGC and noise suppression before measuring.
- Research documentation: `docs/RESEARCH.md` (hypotheses with an honest status, the evidence boundary), `docs/LAB-NOTES.md` (what went wrong and what it taught us), `docs/experiments/` (rules, template, E-001 to E-006; E-002 and E-003 have a first run), `docs/adr/` (nine decision records, written retrospectively), and `docs/README.md` (documentation map, repository layout, data schema).
- `TestPayloadsCarryNoRawSignal`: the published messages must be flat, small, and limited to an allowlist of fields, so a waveform cannot reach a payload unnoticed.
- Experiment harness `TestExperimentToneSweep` (skipped unless `AVENGE_EXPERIMENT=tone-sweep`); it prints the table recorded in `docs/experiments/E-002-tone-sweep.md`.
- Sample-rate check: the engine measures how fast samples really arrive and prints `rate check: ... - ok` after about 12 seconds, or a warning when the rate differs from `SENSOR_SAMPLE_RATE` by more than 5% (wrong `arecord -r`, wrong firmware rate). Replays are recognised and skipped. See `docs/OPERATIONS.md`.
- Tests: profile resolution and validation, the PCM reader, and behaviour of the audio profile on synthetic sound (no false onsets, no warning churn, a fading-in tone is detected); a CI smoke test replays sound through the edge image.

- Allocation guards: tests that fail if the per-window path allocates again, a dedicated CI step that runs them without the race detector, and a `BenchmarkIngestWindowNoFlush` benchmark.
- `CHANGELOG.md`. Release notes are now generated from it, a tag without a changelog section fails the release before anything is published, and `tests/lint/repo-checks.sh` validates its structure.

### Changed

- The README is reduced to what the project is, the idea, and how to run it; the status checklist, layout, data schema and stack moved to `docs/README.md` and the research documents. `tests/lint/repo-checks.sh` now also checks documents in `docs/` subfolders.
- Documentation no longer refers to sensor firmware as part of the repository: it is not, and the reference sensor setup (an ADXL345 at 1600 Hz) is described as such.
- The sample-rate mismatch warning is a box printed at the first measurement, and the end-of-run summary states the outcome of the rate check (`ok`, `MISMATCH`, `replay`, `not measured`) and shows the real run time next to the sample count (`38.9 s at 1600 Hz` versus `Real time: 62.0 s`). A wrong rate is no longer easy to miss.
- **Upgrade note:** an `edge/.env` copied from an older `edge/.env.example` still has `SENSOR_SAMPLE_RATE=1600` switched on. Comment it out (simulator, sound) or leave it only if your source really runs at 1600 Hz.
- **Anomaly events follow the combined state.** `onset` and `clear` are now published when the RMS detector or any band detector enters or leaves anomaly, not when the RMS detector alone changes. A tonal anomaly that barely moves the RMS now produces an onset; before, it showed up only in the 10-second telemetry and its transitions were published as `clear`. Severity changes during an anomaly are no longer events. Telemetry `is_anomaly` is the same combined state (it used to mean "severity above normal", which flapped near the warning threshold).
- Anomaly messages and `iot.anomaly_events` carry `dominant_signal` and `band`; the Grafana table shows them as `source`. Existing databases are migrated by `clickhouse-init` (see `docs/OPERATIONS.md`); run the edge and core of the same version.
- Console `win N` is now the real window number of the event (it used to count printed events), and the end summary reports processed windows and windows above warning level; it used to count events as windows (9 "windows" for 60 s of sound). Output of a replayed signal therefore differs from the previous version in these two places only.
- MQTT timestamps are taken when the window completes in the worker, not when the event is published.
- **Breaking:** a serial interface (`SERIAL_INTERFACE`) now requires `SENSOR_SAMPLE_RATE` and exits with an error without it. `edge/.env.example` ships the line commented out and `edge/compose.yaml` no longer defaults it to 1600, so a copied example can no longer start with a guessed rate. Set `SENSOR_SAMPLE_RATE=1600` for the ADXL345 firmware.
- For raw PCM input, a `SENSOR_SAMPLE_RATE` that differs from the profile default produces a warning, and `--explain` states the rate the capture command must use.
- `--explain` no longer prints the start-up banner before the box.
- The combined score uses the detectors' thresholds instead of fixed 2.5 and 3.5 (identical for the default configuration), so profiles with other thresholds classify consistently. `anomaly.Config` gained `Alpha`, the baseline adaptation factor that was a hard-coded 0.1 (zero keeps 0.1), and `SensorConfig` gained optional `Bands`, `BandMinStdDev` and `BaselineAlpha`.
- The edge engine's per-window path (ring buffer, FFT, both detectors, scoring) no longer allocates after initialization: 0 allocations per window instead of 2 (about 10.6 KB), measured on amd64 with Go 1.22. Output is byte-identical to the previous version on a replayed signal with two faults. A window that emits an event still allocates the event and a copy of the band states, because they are handed to another goroutine. New APIs: `filter.SDTInto`, `BandDetector.Update` and `BandDetector.Bands`; `SDT` and `IngestSpectrum` behave as before.
- SDT and worker buffers are preallocated for at most 64 windows; a longer flush interval grows them once and reuses them.
- Documentation claims narrowed to what is tested: "zero-heap-allocation hot path" became "allocation-free per-window processing", and the unmeasured "scalable to thousands of sensor streams" was removed. The README technology table no longer lists component versions; `core/compose.yaml` is the source of truth.

### Fixed

- `CLEAR` events without a preceding `ANOMALY` (severity downgrades of the RMS detector were published as `clear`).
- README and `docs/OPERATIONS.md` did not mention that serial input needs `SENSOR_SAMPLE_RATE`; the old `edge/.env.example` pre-set 1600 Hz, which shifted all frequencies when it was used with the 1000 Hz simulator.
- Documentation said telemetry carried SDT turning points; it carries only the compression ratio, the compressed points are not transmitted. The description of band scoring said "weighted", but the larger of the RMS and band z-scores decides; the claim that bands detect bearing defects, imbalance and misalignment now says they are symptoms, not diagnoses.

## [0.2.4] - 2026-11-02

### Added

- One-command start: `docker compose up -d` is the only step. `preflight` rejects missing, `changeme` and non-alphanumeric passwords with a readable message; Mosquitto builds its password file from `.env` at every start; `clickhouse-init` applies the schema idempotently once ClickHouse is healthy; `depends_on` orders everything else. Changing a password is: edit `.env`, run `up -d`.
- CI pipeline with a single required check, `ci-ok`: Go formatting, vet, build and `test -race` with a coverage floor on the toolchain from `go.mod` and on stable Go; cross-build and vet for `linux/arm64`, `darwin/arm64` and `windows/amd64`; `govulncheck`; compose resolution, `yamllint`, `shellcheck`, `hadolint`, `actionlint` and dashboard JSON checks; edge image build, non-root check and smoke test; secret scan with `gitleaks` over the full history.
- End-to-end test of the whole stack (`tests/e2e/run.sh`): startup, network exposure, MQTT authentication and ACL, ClickHouse least privilege, the data path from a seeded simulator to ClickHouse, Grafana datasource health and password rotation.
- Release gating: a tag runs the full CI on the tagged commit, must be on `main`, and publishes a multi-architecture image (`linux/amd64`, `linux/arm64`) with provenance and SBOM. `latest` moves only for stable tags; tags with a suffix are pre-releases.
- Dependabot for GitHub Actions (pinned by commit SHA), the edge Docker base image, the core compose images and the docs checker.
- `tests/lint/repo-checks.sh`: every required `.env` variable is documented in `.env.example`, no secret-bearing file is tracked, and every path and link mentioned in the docs exists.
- Documentation: `docs/ARCHITECTURE.md` (eight Mermaid diagrams, parsed in CI), `docs/TESTING.md`, and in `docs/OPERATIONS.md` the account table, secret rotation and data retention.
- A stub for the serial reader on non-Linux platforms, so the module builds on macOS and Windows (serial input stays Linux-only; use stdin there).
- Tests for the broker address helper (IPv4, hostname, IPv6).
- `LICENSE` (GPL-3.0).

### Changed

- **Breaking:** `core/clickhouse/init.sh` and `core/mosquitto/gen-passwd.sh` are gone; their work is automatic. Passwords must be alphanumeric and not `changeme`, or startup stops at `preflight`.
- Go 1.22 to 1.27 (`edge/Dockerfile`, `edge/go.mod`); Grafana 10.4.2 to 13.3.0; ClickHouse 24.3 to 26.10; Telegraf 1.30 to 1.40; Alpine 3.20 to 3.24. CI passes on fresh volumes. Upgrading existing ClickHouse and Grafana data volumes across these major versions was not tested; back them up first.
- The broker address is built with `net.JoinHostPort`, which handles IPv6.
- `govulncheck` runs at `@latest`; the pinned version panicked on Go 1.27.

### Fixed

- `final_score` and `dominant_signal` were published by the edge and parsed by ClickHouse, but Telegraf did not forward them, so both columns were always empty. They are stored now, and the e2e test checks it.
- `iot.telemetry_raw` grew without bound; it now expires after 7 days (a `received_at` column and a TTL, applied to existing databases as well).
- The module did not build on macOS and Windows (`undefined: sensor.OpenSerialReader`).

### Removed

- `core/clickhouse/init.sh` and `core/mosquitto/gen-passwd.sh`.

### Security

- The hashed MQTT password file `core/mosquitto/config/passwd` was committed in `6562d94` and removed in `805024d`; the hashes remain in the history. Rotate `MQTT_EDGE_PASSWORD` and `MQTT_TELEGRAF_PASSWORD`. The file is gitignored again and CI fails if it, a `.env` file or key material is tracked.

## [0.2.3] - 2026-10-31

### Added

- MQTT authentication and per-user ACL: anonymous access is refused; `edge` may only publish to `sm/telemetry/#` and `sm/anomaly/#`, `telegraf` may only read them. The edge MQTT client sends `MQTT_USER` and `MQTT_PASSWORD`.
- ClickHouse least-privilege accounts: `telegraf` can only insert into `iot.telemetry_raw`, `grafana` is read-only. Materialized views run with `SQL SECURITY DEFINER`.
- Caddy reverse proxy: Grafana is served at `http://grafana.localhost`.
- Helper scripts `core/mosquitto/gen-passwd.sh` and `core/clickhouse/init.sh` (removed in 0.2.4).
- Go benchmarks for the FFT processor, SDT, both detectors and the worker pipeline; `--seed` for the simulator, so runs are reproducible.
- `docs/OPERATIONS.md`; a refreshed `docs/DOC.md`.

### Changed

- **Breaking:** MQTT requires credentials, and the core `.env` needs the new password variables (see `core/.env.example`).
- **Breaking:** ClickHouse (8123, 9000) is bound to localhost, Grafana no longer publishes port 3000, the MQTT WebSocket listener (9001) is removed, and port 1883 is bound to `MQTT_BIND` (default `127.0.0.1`; set it to the LAN address so edge devices can connect).
- `edge/docker-compose.yml` is now `edge/compose.yaml`.

### Fixed

- Offline replay (`signal_generator.py batch | edge-filter --no-mqtt`) dropped most of the input (74% in a measured run) because the worker's input was never blocking. Replayed input now applies backpressure (`Manager.SendBlocking`); live sources still drop on overflow.

### Removed

- `docs/run.txt` and the committed `firmware-sim/__pycache__`.

## [0.2.2] - 2026-10-27

### Changed

- `core/docker-compose.yml` renamed to `core/compose.yaml` (no content change).

## [0.2.1] - 2026-10-19

### Fixed

- Edge container could not reach the broker: the compose file passed `MQTT_BROKER_URL`, which the binary never read; it now passes `CENTRAL_HOST` and `MQTT_PORT`.
- `SENSOR_ID` from the environment is honored (before, only `--sensor-id` worked, so every sensor was `bearing_01`).
- Serial access from the container: the non-root user could not open `/dev/tty*`. The compose file now adds the host's dialout group (`DIALOUT_GID`) and passes exactly one device (`SERIAL_PORT`); listing devices that do not exist makes Docker refuse to start.
- Removed a healthcheck that could never pass (the distroless image has no shell) and the unused `FFT_WINDOW_SIZE`.
- Run statistics counted only anomaly onset transitions; they now count every anomalous window.

### Changed

- **Breaking:** the edge `.env` uses `CENTRAL_HOST`, `MQTT_PORT`, `SERIAL_PORT` and `DIALOUT_GID`; the image tag is selectable with `AVENGE_EDGE_TAG`.

## [0.2.0] - 2026-06-18

### Added

- Scoring layer (`scoring.Combine`): the RMS detector's z-score and the band detector's maximum z-score are combined into one decision, so a strong band-specific signal cannot be outvoted by a calm overall RMS. The edge publishes `final_score` and `dominant_signal`, and the ClickHouse schema carries them.

### Fixed

- `edge/.env.example` values.

### Removed

- The compiled `edge-filter` binary is no longer tracked.

## [0.1.1] - 2026-06-17

### Changed

- `.env` is loaded from the current directory and from `edge/`, so the README commands work from the repository root. Configuration priority is verified by tests: `--broker` flag, then exported environment, then `.env`, then the default.

### Fixed

- Serial reader hung on noisy lines (resynchronization reworked); the RS-485 interface was chosen precisely for that scenario.
- Data race between closing the events channel and the workers in `Manager.Stop`, found by the race detector.
- `SENSOR_SAMPLE_RATE` from `.env` now reaches both the serial reader and the pipeline; before, both silently fell back to 1000 Hz, which would have shifted every computed frequency.

## [0.1.0] - 2026-06-16

First tagged release of the proof of concept (development since 2026-05-28).

### Added

- Edge engine in Go with no external dependencies: radix-2 Cooley-Tukey FFT, Swinging Door Trending compression, RMS and z-score anomaly detector, per-frequency-band detector, constant-memory streaming pipeline.
- MQTT 3.1.1 client on the standard library, a multi-sensor manager, a resilient client with reconnect and graceful shutdown through `context`.
- Sensor input: JSONL on stdin, and a serial reader for USB and RS-485.
- Configuration through `.env` files; a distroless ARM64-ready Dockerfile.
- Synthetic vibration signal generator (`firmware-sim`).
- Core stack in Docker Compose: Mosquitto, Telegraf, ClickHouse, Grafana, with a provisioned dashboard; severity travels from the detector to Grafana.
- CI build and test workflow and a tag-triggered release.

### Fixed

- End-to-end pipeline from MQTT to ClickHouse and Grafana stabilized; EMA bias, hysteresis and severity levels in the detector; telemetry flush and statistics sample rate; the ClickHouse password reaches the Grafana datasource.

[Unreleased]: https://github.com/casablanque-code/avenge/compare/v0.2.4...HEAD
[0.2.4]: https://github.com/casablanque-code/avenge/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/casablanque-code/avenge/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/casablanque-code/avenge/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/casablanque-code/avenge/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/casablanque-code/avenge/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/casablanque-code/avenge/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/casablanque-code/avenge/releases/tag/v0.1.0
