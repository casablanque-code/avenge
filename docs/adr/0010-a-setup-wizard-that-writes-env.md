# ADR-0010: A setup wizard that writes `.env`, and a doctor that reads it

**Status:** accepted. Implemented; unit tested with fakes. Not yet used on a clean machine, that is the next [E-004](../experiments/E-004-laptop-microphone.md) campaign.

## Context

Setting up a core and an edge by hand means editing two `.env` files, keeping one password equal in both, finding the right serial device or microphone name, knowing the real sample rate, and putting the LAN address in the right place. The first hardware run met exactly these mistakes: a whole `--list-audio` line pasted into `AUDIO_DEVICE`, a port that no packet could reach, a password copied wrong. Every one of them is a user error that a program can prevent or at least name.

## Options considered

1. **Keep the manual path and write better docs.** Already done; it still failed on the first real run.
2. **Make `docker compose up` interactive.** Compose cannot prompt, and the edge does not run in Docker when it listens to a microphone.
3. **A second binary, `avenge`,** that asks questions, writes the same `.env` files and checks the result. The engine (`edge-filter`) stays as it is.

## Decision

Option 3. `avenge core setup`, `avenge core pair`, `avenge edge setup`, `avenge doctor`.

- **`.env` stays the single source of truth.** The wizard edits values in place (comments and order survive), keeps a `.env.bak-<time>` of the old file, writes mode 0600. Running it again shows the current values as the defaults and keeps working passwords unless `--regenerate` is given.
- **Questions only where a person must decide.** The Grafana password is asked (the person types it to log in); the other five passwords are generated, 24 letters and digits, from `crypto/rand`.
- **A pairing code carries the link.** One line (`avenge1.<base64>.<checksum>`) holds host, port, user and the MQTT password. The checksum catches a cut or mistyped paste. It is encoded, not encrypted, and the docs say so.
- **Same words as the engine.** The wizard does not duplicate the engine's logic where it can call it: the device list is parsed with the engine's own `capture.ParseDevices`, the rate is measured with the engine's own serial reader, the trial run is a real `edge-filter --no-mqtt`.
- **The reference sensor is the default.** A serial ADXL345 at 1600 Hz is what the wizard proposes first; anything else is configured by answering differently or by editing `.env`.
- **No terminal, no guessing.** Without a TTY a command stops and asks for `--yes` and flags; with `--yes` it takes flags and defaults and never prompts.
- **The doctor says where the connection stops:** name does not resolve, nothing answers (and which interface the route takes), nothing listens, the broker does not speak MQTT, the login is refused.

## Trade-offs

A second binary to ship (it is in the same archive as `edge-filter`). Two places know the `.env` keys (the engine and the wizard); the wizard's tests read the real `.env.example` files, so a renamed key breaks a test and not a user. The core wizard cannot start Docker on a machine without Docker and says so.

## How to test it

Unit tests drive every command with fake prompts, ports, microphones and brokers. What they cannot prove is on the list in [TESTING.md](../TESTING.md): the real serial measurement, `arecord` listing, Docker, WSL. Those are proven by running the wizard on a clean machine and logging the run.

## Revisit when

Real runs show people still go around the wizard and edit by hand (then the wizard is in the way), or the number of `.env` keys grows so that the wizard falls behind the engine.
