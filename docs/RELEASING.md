# Releasing

Releases are made by the pipeline, never by hand. The page on GitHub, the image on ghcr.io and the binaries all come out of one run of the `Release` workflow, triggered by a tag. If you click "Create release" in the browser you get a tag and a page and none of the rest. We know. See [LAB-NOTES](LAB-NOTES.md#a-release-that-was-only-half-a-release).

## The steps

1. **Everything goes through pull requests**, with CI green. A direct push to `main` skips the checks that would have caught a bad CHANGELOG.
2. **Write the changes into `[Unreleased]`** in `CHANGELOG.md` as you go. The release notes on GitHub are this section.
3. **Prepare the release** on a branch:
   ```
   git checkout -b release/X.Y.Z
   tools/release.sh prepare X.Y.Z
   git diff CHANGELOG.md        # read it
   ```
   It renames `[Unreleased]` to `[X.Y.Z] - <today>`, leaves a new empty `[Unreleased]`, and fixes the two link lines at the bottom. Those three edits are the ones that were missed once; the script cannot forget them. It refuses to run if `[Unreleased]` is empty.
4. **Open a pull request, merge it.** Then `git checkout main && git pull`.
5. **Check before you tag**: `tools/release.sh check vX.Y.Z`. It runs what the workflow will run (the CHANGELOG section, the repository checks, the tag is free, a clean tree, `HEAD` is the tip of `origin/main`, the binaries build for every platform) and prints the tag command when all of it passes.
6. **Tag and push**:
   ```
   git tag -a vX.Y.Z -m vX.Y.Z
   git push origin vX.Y.Z
   ```
7. **Watch the run**: Actions, workflow `Release`. Four jobs: CI on the tagged commit, tag is on main, build and push the image, create the GitHub Release (which builds the binaries too).
8. **Verify the result**:
   ```
   docker run --rm ghcr.io/casablanque-code/avenge-edge:vX.Y.Z --version      # prints vX.Y.Z
   ```
   On the release page: six archives and `SHA256SUMS`. Download one, `sha256sum -c --ignore-missing SHA256SUMS`, run `edge-filter --version` and `avenge --version`.

## Version numbers

While the major version is 0, a breaking change raises the minor number (0.3.x to 0.4.0), a fix or an addition that breaks nothing raises the patch number. "Breaking" includes things that change what the data means or what a user has to edit: a changed `.env`, a changed message schema, a database migration. Put an **Upgrade notes** list at the top of such a release in the CHANGELOG, as 0.3.0 does.

## When a run fails

Read the failed job first; the tag run is the only place the whole chain is exercised.

- **CI on the tagged commit** fails: the commit has a problem the pull request did not show. Fix it on `main` through a pull request.
- **Tag is on main** fails: the CHANGELOG section is missing (run `tools/release.sh check` next time) or the commit is not on `main`.
- **Build and push** fails: nothing was published; fix and re-run.
- **A tag whose run failed and nothing was published** may be deleted and created again on the fixed commit: `git push origin :refs/tags/vX.Y.Z`, then steps 5 and 6. Do it only if no image and no release exist for that tag. If anything was published, leave the tag alone and release the next patch version.

## What the workflow produces

| Artifact | Where |
|---|---|
| Image `ghcr.io/casablanque-code/avenge-edge:vX.Y.Z` (and `latest` for stable tags), linux/amd64 and linux/arm64 | Packages |
| `avenge-edge_vX.Y.Z_<os>_<arch>` archives: Linux amd64, arm64, armv7; Windows amd64 (zip); macOS arm64, amd64. Each has `edge-filter`, `avenge`, `LICENSE`, `.env.example` | Release page |
| `SHA256SUMS` | Release page |
| Release notes: the CHANGELOG section plus how to pull the image | Release page |

The binaries are static and built with `-trimpath`; building the same commit twice with the same Go gives identical files (`tools/build-binaries.sh` is what both CI and the release use).
