# AVENGE Edge Engine — Dockerfile
#
# Build for Raspberry Pi 4 (ARM64):
#   docker buildx build --platform linux/arm64 \
#     -t ghcr.io/casablanque-code/avenge-edge:latest \
#     -f edge/Dockerfile edge/ --push
#
# Build for local testing (amd64):
#   docker buildx build --platform linux/amd64 \
#     -t avenge-edge:dev -f edge/Dockerfile edge/ --load
#
# Multi-platform in one go (requires buildx + QEMU, see CI workflow):
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     -t ghcr.io/casablanque-code/avenge-edge:latest \
#     -f edge/Dockerfile edge/ --push

# ── Build stage ──────────────────────────────────────────────────────────
# golang:1.22-alpine matches go.mod (go 1.22). Alpine keeps the build
# image small; CGO is disabled below so musl/glibc differences don't matter.
FROM golang:1.27-alpine AS builder

WORKDIR /src

# Cache dependency download separately from source — go.sum is absent
# (no external deps after switching serial_reader.go to raw syscalls),
# but go.mod is still copied first so adding deps later doesn't bust
# the source layer cache unnecessarily.
COPY go.mod ./
RUN go mod download 2>/dev/null || true

COPY . .

# CGO_ENABLED=0: static binary, no libc dependency at runtime —
# required since the final stage is distroless (no shared libs at all).
# GOARCH is set by buildx automatically via --platform; do not hardcode it.
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w" \
    -o /out/edge-filter \
    ./cmd/edge-filter/

# ── Runtime stage ────────────────────────────────────────────────────────
# distroless/static: no shell, no package manager, minimal attack surface.
# Fine here because edge-filter needs nothing but the serial device node
# and network — no subprocess calls, no filesystem beyond /dev/ttyUSB0.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/edge-filter /usr/local/bin/edge-filter

# nonroot user (uid 65532) from the base image. Serial device access on
# the host is granted via `group_add` + udev rule (see edge/compose.yaml
# comments) rather than running as root in the container.
USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/edge-filter"]
