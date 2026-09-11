---
issue: 374
pr: 378
shipped: 2026-09-06
title: "Sign and notarize macOS releases"
type: enhancement
complexity: M
priority: P2
stage: DONE
---

# Sign and notarize macOS releases

- **Issue:** #374
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/374-sign-and-notarize-macos-releases.md](../exec-plans/completed/374-sign-and-notarize-macos-releases.md)

## Problem

Hive's macOS releases carry no cryptographic provenance. `scripts/release.sh:171`
writes a plain `shasum -a 256` manifest, and `stageRelease` in
`cmd/hivegui/update_apply_darwin.go` verifies the downloaded zip against it —
but the zip and the manifest come from the same GitHub release, so anyone who
can publish a release publishes a matching manifest too. The comment at
`update_apply_darwin.go:75` states the limitation outright: the checksum catches
a truncated download, not a tampered one. Everything downstream of that check
is an unpack-and-run of code the updater cannot attribute to anyone.

The same gap is visible to new users from the other direction: the bundle is
unsigned, so a first install trips Gatekeeper's "unidentified developer" prompt
and the recovery is a right-click-Open dance that trains people to bypass
exactly the warning that would matter if a build were ever malicious.

This is finding 6 (MED) of the 2026-09-05 security audit. It was held out of the
hardening plan because it needed a trust-root decision before any code; that
decision is now made (see Notes).

## Desired behavior

**Install.** A downloaded Hive release opens on a clean macOS machine with no
Gatekeeper warning and no right-click workaround. `spctl --assess` accepts the
bundle offline, because the notarization ticket is stapled to it.

**Update.** The in-app updater refuses to stage a build whose signature does not
verify against Hive's Apple **Team ID** — not merely against a valid Apple
Developer ID, which anyone with a $99 account holds. A tampered or re-zipped
bundle fails with a distinct, honest message ("this update is not signed by the
Hive developer") rather than the generic checksum-mismatch error, and the
running app is left untouched — same fail-closed shape the current checksum
path already has.

The check runs on the unpacked bundle in the temp staging directory, not on the
zip: `codesign` and `spctl` read a bundle, not an archive stream, so a
"before `ditto -x`" check is not achievable. Fail-closed is preserved where it
matters — staging happens in a temp directory that is removed unless every
check passes, so the *installed* app is never touched by an unverified build.

**Release.** `scripts/release.sh` signs, notarizes, and staples as part of the
normal release run, and refuses to publish if any of those steps fail. The
maintainer does not have to remember a manual step; credentials come from the
environment, never from the repo.

## Success criteria

- `spctl --assess --type execute --verbose hivegui.app` passes on a machine that
  has never run Hive, with networking disabled (proves the ticket is stapled,
  not merely issued).
- `codesign --verify --deep --strict` passes on the shipped bundle.
- A release run with a deliberately corrupted or re-zipped artifact fails in
  `release.sh` before anything is uploaded.
- The updater, pointed at a bundle whose signature has been broken, refuses to
  stage it and surfaces a signature-specific error; the installed app still runs.
- The `update_apply_darwin.go:75` comment disclaiming supply-chain defense is
  gone, because it is no longer true.

## Non-goals

- **Linux and Windows releases.** Different trust roots, different tooling; a
  separate spec if and when those channels ship binaries.
- **The `latest` channel.** It builds from a git checkout of the upstream remote
  (already tightened by #365), not from a published artifact, so there is no
  release signature to check. Its trust root is git + the remote check.
- **Reproducible builds.** Signing attributes a build to a key; it does not let
  a third party rebuild and compare. Worth its own spec later.
- **Replacing the SHA-256 manifest.** Keep it. It still catches the truncated
  download cheaply, and it is the error users see most.
- **Key rotation automation.** Document the rotation procedure; do not build
  tooling for it until there is a second key.
- **Gatekeeper-disabled machines.** Under `spctl --master-disable` the
  notarization assessment does not evaluate. The Team ID pin remains
  load-bearing and still refuses a bad signature; the advisory assessment is
  logged, not enforced. Refusing updates outright on such a machine is not a
  goal — its owner has already opted out of Gatekeeper globally.

## Notes

Trust root decision (2026-09-06): **Apple Developer ID + notarization**, chosen
over a self-managed minisign/cosign key. Rationale: it covers the install path
and the update path with one mechanism, the OS does the verification so there is
no bespoke verify code in the updater's critical path, and it removes the
Gatekeeper prompt that currently teaches users to click through warnings. Cost
is $99/yr for the Apple Developer account plus a notarization wait on each
release. The rejected alternative — signing `checksums.txt` with an embedded
public key — is cheaper and account-free but leaves Gatekeeper untouched and
puts key custody entirely on the maintainer.

Open questions for the exec plan:

- Where the Developer ID cert and the `notarytool` credentials live for local
  releases vs. CI, and whether releases move to CI as part of this.
- What the updater checks, given the OS already gates execution: staple
  verification before `ditto -x`, or rely on Gatekeeper at launch. Fail-closed
  before unpack is the stronger shape.
- Hardened runtime and entitlements — notarization requires the hardened
  runtime, which can break a PTY host if entitlements are wrong. Needs testing
  against `hived` spawning sessions before release.

Related: audit finding 9 (agents can spoof their own hook events) is the other
deferred item from that audit and is unrelated to this one.

Prior work: #361 (socket hardening), #365 (exact upstream-remote check),
#366 (Actions pinned to SHAs).
