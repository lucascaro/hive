---
issue: 382
pr: 383
title: Move the release build to CI
type: enhancement
complexity: M
priority: P2
stage: GATE
---

# Move the release build to CI

- **Issue:** #382
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/382-move-the-release-build-to-ci.md](../exec-plans/active/382-move-the-release-build-to-ci.md)

## Problem

`scripts/release.sh` performs the whole release on the maintainer's Mac: version
bump, changelog stamp, commit, tag, `build.sh --platform all`, codesign,
notarize with `notarytool --wait`, staple, checksums, push, `gh release create`.

Two things are wrong with that. Notarization is a blocking wait pinned to a
human's terminal — during the v2.7.0 release Apple's Notary Service sat
`In Progress` for over 90 minutes with nothing on the developer system-status
feed. And the release only works on one machine: the Developer ID identity and
the `hive-notary` keychain profile exist only in that login keychain, so no
other machine, and no automation, can cut a release.

## Desired behavior

Pushing a `v*` tag (or dispatching the workflow with a tag) makes a macOS
runner produce the signed, notarized, stapled artifacts and the GitHub release.
The maintainer's terminal is free the moment the tag is pushed.

Version choice, changelog stamp, release commit and tag stay local in
`release.sh` — they are cheap, they want a human, and doing them on a runner
means a bot pushing to `main`.

## Success criteria

- `scripts/release.sh <version>` runs on a Mac with **no** Developer ID
  certificate and **no** notary profile, and exits after pushing the commit and
  tag, printing the Actions run URL to watch.
- A `v*` tag push runs `.github/workflows/release.yml` on `macos-latest` and
  publishes a GitHub release carrying
  `Hive-<version>-macos-universal.zip`, `Hive-<version>-windows-amd64.zip` and
  `checksums.txt`.
- The published macOS zip is signed with the pinned Team ID, notarized and
  stapled — `spctl -a -vv -t install` accepts the unzipped `.app` with
  networking disabled.
- The build/sign/checksum/publish half exists in exactly one place
  (`scripts/release-artifacts.sh`), called by both the workflow and the local
  fallback. No step is duplicated between the workflow and a script.
- Re-running the workflow for a tag that already has a release updates that
  release's assets instead of failing — a half-failed publish is recoverable
  without deleting the tag.
- `workflow_dispatch` can publish for an existing tag, so a failed publish is
  re-runnable without re-tagging.
- The workflow never runs on `pull_request` or `pull_request_target`, so the
  signing secrets are unreachable from a fork PR.
- The signing certificate and notary key are **environment**-scoped, not
  repository-scoped, so a collaborator cannot reach them by editing a workflow
  in a pull request. The release job declares `environment: release`, that
  environment requires a reviewer, and its deployment policy admits only `v*`
  tags — so `workflow_dispatch --ref <branch>` cannot run a modified
  `release.yml` with the credentials loaded.
- `docs/releasing-signed-macos.md` documents the CI path, every required secret
  and variable, and how to still release entirely locally.

## Non-goals

- Moving the version bump, changelog stamp, commit or tag into CI.
- Splitting the build into a per-platform matrix. `build.sh --platform all`
  cross-compiles Windows on the macOS runner today; keep one job, one runner.
- Linux artifacts (still a manual native build, per README).
- Changing `scripts/sign-macos.sh`. It already reads both credentials from the
  environment and needs no modification.
- Auto-tagging or auto-releasing on a merge to `main`.

## Notes

- Prior art: spec 374 (`sign-and-notarize-macos-releases`) and
  `docs/releasing-signed-macos.md`.
- Notary credentials: App Store Connect API key, chosen over an app-specific
  password so the credential is revocable per-key and not tied to a human's
  Apple ID.
