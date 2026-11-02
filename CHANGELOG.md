# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). While the major version is 0, a minor release may contain breaking changes; they are marked **Breaking**.

Entries for 0.1.0 to 0.2.4 were reconstructed from the git history and the contents of each tag. Release notes on GitHub are taken from the matching section of this file.

## [Unreleased]

### Added

- Allocation guards: tests that fail if the per-window path allocates again, a dedicated CI step that runs them without the race detector, and a `BenchmarkIngestWindowNoFlush` benchmark.
- `CHANGELOG.md`. Release notes are now generated from it, a tag without a changelog section fails the release before anything is published, and `tests/lint/repo-checks.sh` validates its structure.

### Changed

- The edge engine's per-window path (ring buffer, FFT, both detectors, scoring) no longer allocates after initialization: 0 allocations per window instead of 2 (about 10.6 KB), measured on amd64 with Go 1.22. Output is byte-identical to the previous version on a replayed signal with two faults. A window that emits an event still allocates the event and a copy of the band states, because they are handed to another goroutine. New APIs: `filter.SDTInto`, `BandDetector.Update` and `BandDetector.Bands`; `SDT` and `IngestSpectrum` behave as before.
- SDT and worker buffers are preallocated for at most 64 windows; a longer flush interval grows them once and reuses them.
- Documentation claims narrowed to what is tested: "zero-heap-allocation hot path" became "allocation-free per-window processing", and the unmeasured "scalable to thousands of sensor streams" was removed. The README technology table no longer lists component versions; `core/compose.yaml` is the source of truth.

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
