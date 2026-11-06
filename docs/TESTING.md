# Testing and CI

Every pull request and every push to `main` runs the same checks. The single required status check is **`ci-ok`**; it passes only when all jobs below pass. A release tag runs the same workflow on the tagged commit before anything is published.

## What CI checks

| Job        | What it checks |
| ---------- | -------------- |
| `edge`     | `gofmt`, `go vet`, build, `go test -race` with a coverage floor on `edge/internal/...`, allocation guards (the per-window path must not allocate), a check that `--help` describes every setting of `edge/.env.example`, tests of recording (exact copy, no overwrite, private permissions), of the input flag combinations and of the run report, a payload test (published messages are flat summaries limited to an allowlist), tests of the sample-rate monitor (including its latest result and recovery), of the mismatch box and the end-of-run summary, and of the worker counters, tests of the microphone capture commands for Linux, macOS and Windows (command construction and process handling; no real microphone),  a band-only anomaly test (a faint tone must give one onset naming the band and one clear), behaviour tests of the audio profile on synthetic sound (no false onsets, a fading-in tone is detected), and cross-build plus `go vet` for `linux/arm64` (Raspberry Pi), `darwin/arm64` and `windows/amd64` (development machines; serial input is Linux-only and stubbed elsewhere). Runs on the Go version in `go.mod` and on the latest stable Go. |
| `vuln`     | `govulncheck` against the Go vulnerability database. |
| `infra`    | Compose files resolve (`docker compose config`), `yamllint`, `shellcheck`, `hadolint`, `actionlint`, dashboard JSON validity, that the simulator compiles, that every Mermaid diagram in the docs parses, and `tests/lint/repo-checks.sh` (every required `.env` variable is in `.env.example`, no tracked `.env`, `passwd` or key files, every path and link in README/docs exists). |
| `image`    | Builds the edge image, verifies it runs as non-root, replays a seeded vibration signal and a seeded sound with a tone through it (`--no-mqtt`) to check that no sample is dropped and the tone is detected, and that `--explain` works. |
| `e2e`      | Starts the real core stack with `docker compose up -d` and throwaway passwords, then checks the items listed below. |
| `secrets`  | `gitleaks` over the full git history. |

### What `e2e` verifies

- **Startup:** `preflight` rejects placeholder and non-alphanumeric passwords; one `up -d` brings every service healthy and applies the schema.
- **Exposure:** ClickHouse, Mosquitto and Caddy are published on localhost only; Grafana and the MQTT WebSocket port are not published.
- **MQTT:** anonymous and wrong-password connections are refused; `edge` can publish only to its topics.
- **ClickHouse:** `telegraf` cannot read; `grafana` can read but cannot insert or drop.
- **Data path:** a seeded simulator signal (`--seed 42`, fault after 10 s) goes through the edge engine, MQTT and Telegraf into ClickHouse: telemetry rows (including the combined-score fields `final_score` and `dominant_signal`), the per-minute aggregate and a critical anomaly onset all arrive.
- **Grafana:** reachable through Caddy, ClickHouse datasource healthy, dashboard provisioned.
- **Rotation:** changing `MQTT_EDGE_PASSWORD` in `.env` and running `up -d` rejects the old password and accepts the new one.

## Run it locally

Edge unit tests and benchmarks (Go only):

```
cd edge
go vet ./... && go test -race -count=1 ./...
go test -count=1 -run 'AllocFree|AllocsBounded|AudioProfile' ./internal/...   # allocation and audio guards, without -race
go test ./internal/... -run '^$' -bench . -benchmem
```

Repo consistency checks (seconds, no Docker):

```
tests/lint/repo-checks.sh
```

Documentation diagrams (needs Node.js 20 or newer):

```
cd tests/lint/mermaid && npm ci && npm run check
```

Full-stack end-to-end test (needs Docker with Compose v2, `python3`, `openssl`, `curl`; takes a few minutes and uses ports 80, 1883, 8123 and 9000 on localhost):

```
cd edge && go build -o edge-filter ./cmd/edge-filter && cd ..
tests/e2e/run.sh
```

The script generates random passwords, refuses to run if `core/.env` exists (set `E2E_FORCE=1` to back it up and restore it afterwards), removes the stack and its volumes when it finishes (`E2E_KEEP=1` keeps it), and prints container logs on failure.

## Coverage floor

CI fails if total statement coverage of `edge/internal/...` drops below `COVERAGE_MIN` (set in `.github/workflows/ci.yml`). The value sits about a point below the coverage measured on the CI toolchain and is meant to ratchet upwards as tests are added; it is not a quality target by itself. Each run prints the total and a per-package table in the job summary. Statement counts differ slightly between Go versions (the same code measures 80.1% on Go 1.22 and 78.2% on Go 1.27), so compare numbers from the same toolchain. `cmd/edge-filter` (wiring only) is covered by the `image` and `e2e` jobs instead of unit tests.

## Releases and dependencies

To release: rename `## [Unreleased]` in `CHANGELOG.md` to `## [X.Y.Z] - YYYY-MM-DD`, add a fresh empty `[Unreleased]` section above it and update the link references at the bottom, merge, then tag. The GitHub release notes are that section plus the image pull command. `tests/lint/repo-checks.sh` validates the file's structure on every pull request.

- Tagging `vX.Y.Z` first requires a `## [X.Y.Z]` section in [CHANGELOG.md](../CHANGELOG.md), runs the full CI on that commit, verifies the commit is on `main`, then builds and pushes a multi-architecture image (`linux/amd64`, `linux/arm64`) with provenance and SBOM attestations. `latest` moves only for stable tags; a tag with a suffix (`v1.0.0-rc1`) is published as a pre-release under its own tag.
- All GitHub Actions are pinned by commit SHA. Dependabot proposes weekly updates for the actions, the base images of `edge/Dockerfile` and the images in `core/compose.yaml`; the `e2e` job is what makes those image bumps safe to review.
- Recommended repository setting: protect `main` and require the `ci-ok` check.
