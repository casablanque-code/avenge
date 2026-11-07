#!/usr/bin/env bash
# Release helper. Does the error-prone CHANGELOG edits for you and runs, on
# your machine, the same checks the Release workflow runs on the tag.
#
#   tools/release.sh prepare X.Y.Z [YYYY-MM-DD]   turn [Unreleased] into a release
#   tools/release.sh check vX.Y.Z                 everything the workflow checks
#
# See docs/RELEASING.md for the whole procedure.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
changelog=CHANGELOG.md

die() { echo "error: $*" >&2; exit 1; }

prepare() {
  local ver=${1:-} date=${2:-$(date +%F)}
  [[ $ver =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "usage: prepare X.Y.Z [YYYY-MM-DD] (no leading v)"
  [[ $date =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || die "the date must look like 2026-11-07"
  grep -q '^## \[Unreleased\]' "$changelog" || die "$changelog has no [Unreleased] section"
  if grep -q "^## \[$ver\]" "$changelog"; then die "$ver already has a section"; fi

  local body
  body=$(awk '/^## \[Unreleased\]/{f=1;next} /^## \[/{f=0} f' "$changelog" | grep -v '^[[:space:]]*$' || true)
  [[ -n $body ]] || die "[Unreleased] is empty: nothing to release"

  local prev
  prev=$(sed -nE 's/^## \[([0-9][^]]*)\] - .*/\1/p' "$changelog" | head -n1)
  [[ -n $prev ]] || die "cannot find the previous release in $changelog"

  local link base
  link=$(grep -E '^\[Unreleased\]: ' "$changelog") || die "$changelog has no [Unreleased]: link line"
  base=${link#\[Unreleased\]: }
  base=${base%/compare/*}

  local tmp
  tmp=$(mktemp)
  awk -v ver="$ver" -v date="$date" -v prev="$prev" -v base="$base" '
    /^## \[Unreleased\]/ { print; print ""; print "## [" ver "] - " date; next }
    /^\[Unreleased\]: /  { print "[Unreleased]: " base "/compare/v" ver "...HEAD"
                           print "[" ver "]: " base "/compare/v" prev "...v" ver; next }
    { print }
  ' "$changelog" > "$tmp"
  mv "$tmp" "$changelog"

  tests/lint/repo-checks.sh > /dev/null || { tests/lint/repo-checks.sh; die "the checks fail after the edit; see above"; }
  echo "Prepared $ver (previous release: $prev, date: $date)."
  echo "Read the result:  git diff $changelog"
  echo "Next: commit it in a pull request. After the merge: tools/release.sh check v$ver"
}

# run "description" command...: prints a tick, or the output of the failure.
run() {
  local description=$1 out
  shift
  if out=$("$@" 2>&1); then
    echo "  ✓ $description"
  else
    echo "  ✗ $description" >&2
    while IFS= read -r line; do echo "      $line" >&2; done <<< "$out"
    exit 1
  fi
}

tag_is_free() {
  if git rev-parse -q --verify "refs/tags/$tag" > /dev/null; then echo "the tag exists here"; return 1; fi
  if git ls-remote --exit-code --tags origin "refs/tags/$tag" > /dev/null 2>&1; then echo "the tag exists on origin"; return 1; fi
}
tree_is_clean() {
  [[ -z $(git status --porcelain) ]] || { git status --short; return 1; }
}
head_is_origin_main() {
  git fetch -q origin main
  [[ $(git rev-parse HEAD) == "$(git rev-parse origin/main)" ]] ||
    { echo "HEAD is not the tip of origin/main: merge the pull request, then git checkout main && git pull"; return 1; }
}

check() {
  tag=${1:-}
  [[ $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || die "usage: check vX.Y.Z"
  echo "Checking $tag"
  run "CHANGELOG.md has a section for $tag" tests/lint/changelog-section.sh "$tag"
  run "repository checks (docs, links, CHANGELOG structure)" tests/lint/repo-checks.sh
  run "the tag does not exist yet" tag_is_free
  run "the working tree is clean" tree_is_clean
  run "HEAD is the tip of origin/main" head_is_origin_main
  if [[ -z ${NO_BUILD:-} ]]; then
    out=$(mktemp -d)
    run "release binaries build for every platform" tools/build-binaries.sh "$tag" "$out"
    rm -rf "$out"
  fi
  echo
  echo "Ready. Create the tag on this commit and push it:"
  echo "  git tag -a $tag -m $tag && git push origin $tag"
}

case ${1:-} in
  prepare) shift; prepare "$@" ;;
  check) shift; check "$@" ;;
  *) sed -n '2,9p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
