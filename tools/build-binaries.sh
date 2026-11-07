#!/usr/bin/env bash
# Builds the release archives of edge-filter for every supported platform.
#
#   tools/build-binaries.sh VERSION OUTDIR
#
# VERSION is stamped into the binary (edge-filter --version) and used in the
# file names. Builds are static (CGO off) and use -trimpath; archives carry a
# fixed timestamp (the commit time) and no host information, so the same commit
# gives the same files. SHA256SUMS is written next to the archives.
# Needs: go, tar (GNU), gzip, zip, sha256sum, git.
set -euo pipefail

usage="usage: tools/build-binaries.sh VERSION OUTDIR"
version=${1:?$usage}
outdir=${2:?$usage}
[[ $version =~ ^[A-Za-z0-9._-]+$ ]] || { echo "error: bad version '$version'" >&2; exit 1; }

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
mkdir -p "$outdir"
outdir=$(cd "$outdir" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

# os/arch[/goarm]
targets=(linux/amd64 linux/arm64 linux/arm/7 windows/amd64 darwin/arm64 darwin/amd64)

epoch=$(git -C "$root" log -1 --format=%ct 2>/dev/null || date +%s)

for target in "${targets[@]}"; do
  IFS=/ read -r goos goarch goarm <<<"$target"
  suffix=$goarch
  [[ -n ${goarm:-} ]] && suffix="${goarch}v${goarm}"
  name="avenge-edge_${version}_${goos}_${suffix}"
  dir="$stage/$name"
  mkdir -p "$dir"

  bin=edge-filter
  [[ $goos == windows ]] && bin=edge-filter.exe

  echo "building $name"
  (
    cd "$root/edge"
    CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch GOARM=${goarm:-} \
      go build -trimpath -ldflags "-s -w -X main.version=$version" \
      -o "$dir/$bin" ./cmd/edge-filter
  )
  cp "$root/LICENSE" "$dir/LICENSE"
  cp "$root/edge/.env.example" "$dir/.env.example"
  find "$dir" -exec touch -d "@$epoch" {} +

  if [[ $goos == windows ]]; then
    (cd "$stage" && zip -X -q -r "$outdir/$name.zip" "$name")
  else
    tar --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner \
      -C "$stage" -cf - "$name" | gzip -n -9 > "$outdir/$name.tar.gz"
  fi
done

(cd "$outdir" && sha256sum "avenge-edge_${version}_"* > SHA256SUMS)

# Self-test: the linux/amd64 binary must report the version it was stamped with.
if [[ "$(go env GOOS)/$(go env GOARCH)" == linux/amd64 ]]; then
  mkdir -p "$stage/selftest"
  tar -xzf "$outdir/avenge-edge_${version}_linux_amd64.tar.gz" -C "$stage/selftest"
  got=$("$stage/selftest/avenge-edge_${version}_linux_amd64/edge-filter" --version)
  if [[ $got != "edge-filter $version"* ]]; then
    echo "error: the binary reports '$got', expected it to start with 'edge-filter $version'" >&2
    exit 1
  fi
  echo "self-test: $got"
fi
echo "wrote $(wc -l < "$outdir/SHA256SUMS") archives and SHA256SUMS to $outdir"
