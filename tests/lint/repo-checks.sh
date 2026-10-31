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

echo "== no real .env files are tracked"
if git ls-files | grep -qE '(^|/)\.env$'; then
  fail "a .env file is tracked by git"
else
  ok "no tracked .env"
fi

echo "== paths and links mentioned in the docs exist"
# Generated or local-only paths that legitimately do not exist in a clean checkout.
ignored='^(core/\.env|edge/\.env|edge/edge-filter)$'
docs=(README.md docs/*.md)
for doc in "${docs[@]}"; do
  base="$(dirname "$doc")"
  # Inline repo paths such as core/compose.yaml or tests/e2e/run.sh
  while read -r path; do
    path="${path%%#*}"
    [[ "$path" =~ $ignored ]] && continue
    [[ "$path" == *'*'* || "$path" == *'<'* ]] && continue
    [ -e "$path" ] || fail "$doc mentions missing path: $path"
  done < <(grep -oE '(core|edge|docs|tests|firmware-sim|\.github)/[A-Za-z0-9_./#-]*[A-Za-z0-9_/]' "$doc" | sort -u)
  # Relative markdown links: [text](target)
  while read -r target; do
    target="${target%%#*}"
    [ -z "$target" ] && continue
    case "$target" in http*|mailto:*) continue ;; esac
    [ -e "$base/$target" ] || [ -e "$target" ] || fail "$doc links to missing file: $target"
  done < <(grep -oE '\]\([^)]+\)' "$doc" | sed -E 's/^\]\(//; s/\)$//' | sort -u)
done
[ "$failures" -eq 0 ] && ok "all documented paths exist"

echo
if [ "$failures" -ne 0 ]; then
  echo "repo checks FAILED: $failures problem(s)"
  exit 1
fi
echo "repo checks passed"
