#!/usr/bin/env bash
# Repository consistency checks (run in CI, safe to run locally):
#   1. every required ${VAR:?...} in a compose file is documented in its .env.example
#   2. no real .env file is tracked by git
#   3. every repo path and relative link mentioned in README/docs exists
# Usage: tests/lint/repo-checks.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
failures=0
fail() { printf '  ✗ %s\n' "$1"; failures=$((failures + 1)); }
ok()   { printf '  ✓ %s\n' "$1"; }

echo "== compose variables are documented in .env.example"
for dir in core edge; do
  compose="$dir/compose.yaml"
  example="$dir/.env.example"
  if [ ! -f "$example" ]; then fail "$example is missing"; continue; fi
  # shellcheck disable=SC2016  # the single-quoted ${ is a literal regex, not an expansion
  while read -r var; do
    if grep -qE "^#? ?${var}=" "$example"; then
      ok "$dir: $var"
    else
      fail "$dir: $var is required by $compose but absent from $example"
    fi
  done < <(grep -oE '\$\{[A-Z0-9_]+:\?' "$compose" | tr -d '${:?' | sort -u)
done

echo "== no secret-bearing files are tracked"
# .env files, the generated MQTT password file, and key material.
if tracked=$(git ls-files | grep -E '(^|/)(\.env|passwd)$|\.(pem|key|p12)$' | grep -v '^core/grafana/plugins/'); then
  while read -r f; do fail "tracked secret-bearing file: $f"; done <<<"$tracked"
else
  ok "no tracked .env, passwd or key files"
fi

echo "== paths and links mentioned in the docs exist"
# Generated or local-only paths that legitimately do not exist in a clean checkout.
ignored='^(core/\.env|edge/\.env|edge/edge-filter)$'
docs=(README.md docs/*.md docs/*/*.md)
for doc in "${docs[@]}"; do
  base="$(dirname "$doc")"
  # Inline repo paths such as core/compose.yaml or tests/e2e/run.sh
  while read -r path; do
    path="${path%%#*}"
    [[ "$path" =~ $ignored ]] && continue
    [[ "$path" == *'*'* || "$path" == *'<'* ]] && continue
    [ -e "$path" ] || fail "$doc mentions missing path: $path"
  done < <(grep -oE '(core|edge|docs|tests|firmware-sim|firmware|\.github)/[A-Za-z0-9_./#-]*[A-Za-z0-9_/]' "$doc" | sort -u)
  # Relative markdown links: [text](target)
  while read -r target; do
    target="${target%%#*}"
    [ -z "$target" ] && continue
    case "$target" in http*|mailto:*) continue ;; esac
    [ -e "$base/$target" ] || [ -e "$target" ] || fail "$doc links to missing file: $target"
  done < <(grep -oE '\]\([^)]+\)' "$doc" | sed -E 's/^\]\(//; s/\)$//' | sort -u)
done
[ "$failures" -eq 0 ] && ok "all documented paths exist"

echo "== the sensor firmware agrees with the engine"
fw=firmware/ADXL345/adxl345.ino
if [ ! -f "$fw" ]; then
  fail "$fw is missing"
else
  fw_rate=$(sed -nE 's/^const uint32_t SAMPLE_RATE_HZ = ([0-9]+);.*/\1/p' "$fw")
  fw_baud=$(sed -nE 's/^[[:space:]]*Serial\.begin\(([0-9]+)\);.*/\1/p' "$fw")
  go_baud=$(sed -nE 's/^[[:space:]]*baud := ([0-9]+)$/\1/p' edge/cmd/edge-filter/main.go | head -n1)
  if [ -z "$fw_rate" ] || [ -z "$fw_baud" ] || [ -z "$go_baud" ]; then
    fail "$fw: could not read the rate or baud (the check needs 'const uint32_t SAMPLE_RATE_HZ = N;' and 'Serial.begin(N);')"
  else
    if grep -q "setDataRate(ADXL345_DATARATE_${fw_rate}_HZ)" "$fw"; then
      ok "firmware: SAMPLE_RATE_HZ=$fw_rate and the sensor's data rate register agree"
    else
      fail "$fw: SAMPLE_RATE_HZ is $fw_rate but setDataRate() is not ADXL345_DATARATE_${fw_rate}_HZ"
    fi
    if grep -qE "^#? ?SENSOR_SAMPLE_RATE=${fw_rate}\$" edge/.env.example; then
      ok "edge/.env.example documents SENSOR_SAMPLE_RATE=$fw_rate like the firmware"
    else
      fail "edge/.env.example does not show SENSOR_SAMPLE_RATE=$fw_rate (the firmware's rate)"
    fi
    if [ "$fw_baud" = "$go_baud" ]; then
      ok "firmware baud $fw_baud equals the engine's default"
    else
      fail "firmware baud ($fw_baud) differs from the engine's default ($go_baud)"
    fi
  fi
  if grep -q '0xAA' "$fw" && grep -q '0xAA' edge/internal/sensor/serial_reader.go; then
    ok "packet start marker 0xAA in both"
  else
    fail "0xAA start marker missing in the firmware or in serial_reader.go"
  fi
fi

echo "== CHANGELOG.md structure"
changelog=CHANGELOG.md
if [ ! -f "$changelog" ]; then
  fail "CHANGELOG.md is missing"
else
  grep -q '^## \[Unreleased\]' "$changelog" || fail "CHANGELOG.md has no [Unreleased] section"
  prev=""
  while read -r ver date; do
    [[ "$date" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || fail "CHANGELOG.md: version $ver has no ISO date"
    grep -q "^\[$ver\]: https://" "$changelog" || fail "CHANGELOG.md: no link reference for [$ver]"
    if [ -n "$prev" ] && [ "$(printf '%s\n%s\n' "$ver" "$prev" | sort -V | head -n1)" != "$ver" ]; then
      fail "CHANGELOG.md: $ver is listed after $prev (newest first, please)"
    fi
    prev="$ver"
  done < <(sed -nE 's/^## \[([0-9][^]]*)\] - ([^ ]+).*/\1 \2/p' "$changelog")
  [ "$failures" -eq 0 ] && ok "CHANGELOG.md structure is valid"
fi

echo
if [ "$failures" -ne 0 ]; then
  echo "repo checks FAILED: $failures problem(s)"
  exit 1
fi
echo "repo checks passed"
