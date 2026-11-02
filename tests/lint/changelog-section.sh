#!/usr/bin/env bash
# Print the body of one release section of CHANGELOG.md.
#
# Usage: tests/lint/changelog-section.sh <version> [changelog-file]
#   <version> may carry a leading "v" (v0.2.4 or 0.2.4).
# Exits 1 if the section does not exist or is empty. Used by the release
# workflow to fail fast and to build the GitHub release notes.
set -euo pipefail

version="${1:?usage: changelog-section.sh <version> [file]}"
version="${version#v}"
file="${2:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/CHANGELOG.md}"

body="$(awk -v v="$version" '
  index($0, "## [" v "]") == 1 { found = 1; next }
  found && /^## \[/            { exit }
  found && /^\[[^]]+\]: /       { exit }
  found                         { print }
' "$file")"

if [ -z "$(printf '%s' "$body" | tr -d '[:space:]')" ]; then
  echo "CHANGELOG.md has no (or an empty) section for ${version}" >&2
  exit 1
fi
printf '%s\n' "$body"
