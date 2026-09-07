# Sign and notarize macOS releases

- **Spec:** [docs/product-specs/374-sign-and-notarize-macos-releases.md](../../product-specs/374-sign-and-notarize-macos-releases.md)
- **Issue:** #374
- **PR:** #378
- **Branch:** `feature/374-sign-and-notarize-macos-releases`
- **Status:** active

## Summary

Add Apple Developer ID codesigning, notarization and stapling to the macOS
release pipeline, and make the in-app updater refuse to stage a bundle whose
signature does not verify against Hive's Team ID. The trust-root decision is
already made in the spec; this plan is about where each step slots into
`build.sh` / `scripts/release.sh` and what the updater actually checks.

## Research

### Release path

- `scripts/release.sh` runs **locally, never in CI**. `.github/workflows/`
  holds only `ci.yml`, `changesets.yml`, `pages.yml` — none package or upload.
  The script needs a local authenticated `gh` (`scripts/release.sh:60`) and
  pushes with `git push origin HEAD "$TAG"` (`scripts/release.sh:189`).
- Flow: clean-tree/tag validation (`:60-68`) → pin `RELEASE_SHA` (`:73`) →
  changelog bump (`:100-121`) → commit+tag (`:125-145`) →
  `./build.sh --zip --version "$VERSION" --platform all` (`:156`) → assert both
  zips exist (`:157-162`) → `shasum -a 256` into `release/checksums.txt`
  (`:169-172`) → re-verify `origin/main` has not advanced (`:177-186`) → push →
  `gh release create "$TAG" ... "${ARTIFACTS[@]}"` (`:195`).
- **Signing seam:** after `build.sh --zip` (`:156-162`), before the checksum
  step (`:171`). The checksum must be computed over the *final* signed,
  notarized, stapled artifact or the manifest will not match what ships. Any
  failure must `exit 1` before `gh release create`.
- No credential convention exists yet. The script's existing style is plain
  env vars with `${VAR:-default}` (`:23-39`); signing should follow it.

### Bundle production

- `build.sh` `build_macos()` (`:113-176`): `wails build -platform
  darwin/universal -clean -ldflags …` (`:120-121`) builds the GUI; `hived` is
  built separately per-arch and `lipo`'d (`:123-132`); `hivebar` (cgo/AppKit
  menu-bar helper) is `lipo`'d into
  `Contents/Library/LoginItems/hivebar.app` (`:134-155`). Zip at `:165-171`.
- `cmd/hivegui/wails.json` is minimal — **no `mac.info`, no entitlements, no
  hardened-runtime config**. No entitlements file exists anywhere in the repo.
  Hardened runtime (`codesign --options runtime`), required by notarization,
  must be added fresh.
- Three Mach-O binaries need signing before the `.app` is sealed: `hivegui`,
  `Contents/MacOS/hived`, and the nested `hivebar.app`. Inside-out order.

### Updater path

- `cmd/hivegui/update_apply_darwin.go`: `bundleName = "hivegui.app"`,
  `checksumsAsset = "checksums.txt"` (`:24,28`).
- `stageRelease` (`:83-154`): resolve asset URLs through `assetURL`, which
  enforces the `updateURLPrefix` allowlist (`:183-194`) → download checksums →
  download zip → SHA-256 verify (`:131-138`) → `extractZipFn` = `dittoExtract`
  (`ditto -x -k`, `:578-586`) → `verifyBundle` checks `hivegui`/`hived` are
  present and executable (`:564-576`).
- The comment the spec wants deleted is at `:75-82` — it states outright that
  the checksum is not a supply-chain defense.
- **A signature cannot be checked before unpack.** `codesign`, `spctl` and
  `stapler` all operate on an unpacked `.app`, not on a zip stream. The only
  real seam is *after* `extractZipFn` and *before* the bundle is considered
  staged — the existing `ok=false → os.RemoveAll(dir)` cleanup (`:108-113`)
  already gives that path fail-closed semantics against the installed app.
- `applyStagedBundle` / `swapBundle` (`:509-556`) copy the staged bundle to a
  sibling `.new`, rename installed → `.old`, rename `.new` → installed, with
  rollback. The stapled ticket must survive that copy; needs an explicit test.
- `stageLatest` (`:313-362`) is a spec non-goal — its trust root is git plus
  `verifyUpstreamRemote` (`:373-390`).
- Existing seams to mirror: `extractZipFn` / `copyBundleFn` / `runBuildFn`
  package vars (`:44-56`), so tests need no real Apple signature.

### Hardened-runtime risk to the PTY host

- `hived` is a separate binary spawned by the GUI (`cmd/hivegui/locate.go:18-43`
  resolves it; `restart_unix.go:39-95` treats it as a distinct child, noting at
  `:90-93` that it is never `Wait()`ed on). `update_restart_kind.go:32-61` runs
  the **staged** `hived --version --json` as a probe — that exec is subject to
  Gatekeeper on a freshly staged bundle.
- PTY sessions: `internal/session/session.go:122-170` uses
  `github.com/aymanbagabas/go-pty` and spawns ordinary child processes (login
  shell, agent CLIs). No JIT, no `dlopen` of unsigned code, no in-memory object
  images — so no `allow-unsigned-executable-memory` or debugger entitlement is
  indicated.
- cgo/ObjC files link **system frameworks only**:
  `cmd/hivegui/loginitem_darwin.go` (ServiceManagement / SMAppService — already
  notes it is expected to fail on an unsigned build),
  `internal/notify/notify_darwin.go` (Cocoa),
  `internal/activity/activity_darwin.go` (Foundation, App Nap).
  Library validation should hold, but this needs a real smoke test.
- App Sandbox is *not* required for Developer ID distribution and must not be
  conflated with hardened runtime, which is.

### Constraints / dependencies

- **A paid Apple Developer account ($99/yr) is a hard external dependency.**
  Nothing in the success criteria can be demonstrated without a `Developer ID
  Application` certificate and `notarytool` credentials.
- `xcrun stapler` staples `.app`, `.dmg` and `.pkg` **only** — never a bare
  Mach-O. Hive ships a zipped `.app`, so this works, but the zip must be
  rebuilt after stapling.
- `xcrun notarytool submit --wait` blocks for roughly 2–15 minutes, adding that
  to every release run.
- `codesign --verify` alone accepts **any** valid Developer ID certificate. It
  is not an authentication of *Hive* unless paired with a requirement that pins
  the Team ID, e.g.
  `-R '=anchor apple generic and certificate leaf[subject.OU] = "<TEAMID>"'`.
  Without that pin the updater check is close to worthless against an attacker
  who holds any Apple developer account.
- No `.bats` or shell-test harness exists; `release.sh` has no test file.

### Prior lessons

`brain-search` returned no hits — nothing in the hive brain on codesigning,
notarization or release signing yet.

## Approach

Sign inside-out, notarize the zip, staple the `.app`, then re-zip — never
re-signing after stapling, because code-signing a bundle invalidates a ticket
already stapled to it. The sequence lives in one new script,
`scripts/sign-macos.sh`, called from `release.sh` after `build.sh` so the
checksum manifest is computed over the final shipped artifact.

**The Team ID is a committed source constant, not a build-time stamp.** It
lives in `internal/buildinfo` as a `var` with a real default, and every build —
release, local, CI, latest-channel — carries it.

An earlier draft stamped it via ldflags from `HIVE_SIGN_IDENTITY` so that
`release.sh` owned the value it gated on. That is wrong, and the failure is
quiet. `stageLatest` (`update_apply_darwin.go:313-360`) builds from a git
checkout by running `./build.sh` with no credentials, and any contributor or
second machine builds the same way. Under stamping, all of those binaries get an
*empty* pin — so they skip signature verification on every **release-channel**
update they later download. The user would have built themselves out of the
protection with no signal. A committed default means a credential-free build
still verifies what it downloads, which is the only version of this that is
safe by default.

`release.sh` then asserts that the Team ID parsed out of `HIVE_SIGN_IDENTITY`
(the `(TEAMID)` suffix of `Developer ID Application: Name (TEAMID)`) equals
`buildinfo.SigningTeamID()`, reading it back from the built
`hived --version --json`. Signing with one team while pinning another is still
unrepresentable, and it costs no ldflags plumbing.

**Only base-OS tools in the updater's critical path.** `xcrun stapler` ships
with the Xcode Command Line Tools, not with macOS, so calling it from
`verifyDeveloperIDSignature` would make `xcrun: unable to find utility` the
normal outcome on an end user's Mac — and with the fail-closed
`ok=false → os.RemoveAll` path (`update_apply_darwin.go:106-113`) that bricks
updates for almost everyone. The updater therefore uses `/usr/bin/codesign` for
the pinned signature check and `/usr/sbin/spctl --assess --type execute` for the
notarization check, both present on a stock system. `stapler` is used only in
`sign-macos.sh`, which runs on the maintainer's machine where Xcode CLT is
already a prerequisite (`build.sh:114-117` already requires `lipo`).

**The pin itself.** `codesign --verify --deep --strict -R '=anchor apple generic
and certificate leaf[subject.OU] = "<TEAMID>"'`. Bare `--verify` only proves
*some* valid Apple Developer ID signed the bundle, which an attacker with their
own $99 account satisfies; the requirement is what authenticates Hive rather
than Apple. `--deep` is correct here — Apple discourages it for *signing*, not
for verification.

**Where the check runs.** The spec asks for a check "before `ditto -x`", which
is not achievable: `codesign` and `spctl` operate on an unpacked bundle, not a
zip stream. The check runs on the extracted bundle in the temp staging
directory before `stageRelease` returns; the existing deferred
`os.RemoveAll(dir)` keeps it fail-closed with respect to the *installed* app,
which is the property that matters. Spec wording is amended to match.

**Fail before the release is half-made.** All signing pre-flight —
`HIVE_SIGN_IDENTITY` set, the identity present in the keychain, its OU parsed,
`HIVE_NOTARY_PROFILE` set, `stapler` available — happens next to the existing
clean-tree and tag checks at `scripts/release.sh:60-68`, *before* the commit and
tag at `:125-145`. Discovering a missing certificate or eating a 15-minute
`notarytool` failure after the tag exists leaves the maintainer with a local
release commit and tag and no documented unwind; the script only documents that
recovery for the main-advanced case (`:184`).

**Unconfigured builds stay buildable.** Unstamped, `buildinfo.SigningTeamID()`
is empty and verification is skipped, so `go build`, `wails dev` and CI keep
working. `release.sh`'s pre-flight refuses to publish without an identity, so an
unpinned build cannot reach users. The gate is what keeps "skip when empty"
honest rather than a hole.

**Local builds can be self-signed, credential-free.** `sign-macos.sh --adhoc`
signs with `-s -` and `--options runtime`, skipping notarization and stapling.
Verified: ad-hoc signing carries the hardened-runtime flag
(`flags=0x10002(adhoc,runtime)`), so this reproduces the runtime restrictions
locally *without a certificate*. That is what makes the spec's third open
question — whether hardened runtime breaks `hived` spawning PTY sessions —
answerable before the $99 account is spent, rather than discovered on the first
real release. It is also the right home for re-signing after `build.sh`'s
`lipo -create` steps (`:130-132`, `:150-152`), which can drop a signature.
It is a developer tool only: an ad-hoc signature has no Team ID, so it never
satisfies the updater's pin and cannot be mistaken for a release.

**No entitlements file.** Notarization requires hardened runtime
(`--options runtime`), not entitlements. Hive's cgo links system frameworks only
(`loginitem_darwin.go`, `notify_darwin.go`, `activity_darwin.go`), and PTY
sessions `exec` ordinary child processes rather than loading code into their own
address space — neither is restricted by hardened runtime. Ship without, and let
the smoke test say otherwise; adding a plist later is a smaller change than
justifying entitlements nobody proved were needed.

### Files to change

1. `build.sh` — package with `ditto -c -k --keepParent` instead of `zip -rq`
   (`:165-171`). That is the whole change: no signing env var, no ldflags
   splice, no allowlist. `build.sh` never needs credentials, so a
   latest-channel rebuild and a contributor's checkout keep working untouched.
2. `internal/buildinfo` — add `signingTeamID` (a `var`, not a `const`, so tests
   can override it) holding the committed Team ID, and a `SigningTeamID()`
   accessor beside `buildIDOverride` / `versionOverride`. `hived --version
   --json` reports it, so `release.sh` can prove the built binary pins the ID it
   is about to be signed with.
3. `scripts/release.sh` — signing pre-flight beside the existing checks
   (`:60-68`): require `HIVE_SIGN_IDENTITY` and `HIVE_NOTARY_PROFILE`, confirm
   the identity exists via `security find-identity -v -p codesigning`, parse the
   Team ID from it, and assert it equals `buildinfo.SigningTeamID()`. After the
   build (`:156-162`) and before checksums (`:171`), call
   `scripts/sign-macos.sh "release/Hive-${VERSION}-macos-universal.zip"`. Any
   non-zero exit fails the release.
4. `cmd/hivegui/update_apply_darwin.go` — delete the `:75-82` comment
   disclaiming supply-chain defense and replace it with an accurate one; add
   `verifySignatureFn` to the seam block (`:44-56`); call it in `stageRelease`
   after `extractZipFn` and before `verifyBundle` (`:142-146`); update the
   `dittoExtract` comment at `:578-580`, which cites the `zip -rq` that item 1
   replaces.
5. `cmd/hivegui/loginitem_darwin.go:30-32` — the comment says the call "is
   expected to fail on an unsigned build"; no longer true.
6. `build.sh:145-149` — comment says LoginItems is where the helper belongs "the
   day Hive is code-signed"; that day is this PR.
7. `AGENTS.md` "Releasing" (`:276-289`) — add signing prerequisites and a
   pointer to the new doc.
8. `README.md:75` / `CONTRIBUTING.md:24` — install text should say releases are
   signed and notarized, and drop any right-click-Open workaround.
9. `docs/product-specs/374-sign-and-notarize-macos-releases.md` — amend the
   "fail-closed before unpack" wording in `## Desired behavior` to the staging
   shape actually implemented; record the Team-ID pin.

### New files

- `scripts/sign-macos.sh <zip>` (or `--adhoc <app-dir>`) — signs, notarizes,
  staples and re-packages one release zip **in place**. With `--adhoc` it signs
  a locally built `.app` with `-s -` and `--options runtime` and stops there: no
  credentials, no notarization, no network, no Team-ID assertions. Everything
  below describes the release path. It takes the zip, not the `.app`: `release.sh:156`
  runs `build.sh --platform all`, and `build_windows`'s `wails build -clean`
  (`build.sh:180`) wipes `cmd/hivegui/build/bin` afterwards — the comment at
  `build.sh:203-206` says so outright — so by the time `release.sh` could call
  this, the `.app` directory is gone and only the zip remains. It therefore
  round-trips: `ditto -x -k` to a temp dir, sign, notarize, staple, re-package.
  Hard-fails on an empty or malformed Team ID, and asserts the
  identity's OU equals the committed pin before doing anything, so a build signed by team A can
  never pin team B. Writes to a temp path and `mv`s over `<zip>` only on full
  success, so an aborted run never leaves an unsigned artifact at the publish
  path. Before signing it also runs the extracted `hived --version --json` and
  asserts the reported Team ID equals the OU of `HIVE_SIGN_IDENTITY` — proof
  that the artifact about to be signed pins the key signing it.
  Signs inside-out — `hivebar.app`, `Contents/MacOS/hived`,
  `Contents/MacOS/hivegui`, then the outer `hivegui.app` — each with
  `--options runtime --timestamp --force`. Then `ditto -c -k --keepParent` to a
  temp zip, `notarytool submit --wait`, `stapler staple` the `.app`,
  `stapler validate`, a `codesign --verify --deep --strict -R <pin>` self-check,
  and a final `ditto -c -k --keepParent`. Any failure exits non-zero with the
  failing command's output **and prints the same unwind recipe
  `release.sh:184` gives for the main-advanced case** — a notarytool outage now
  aborts after `git tag` (`:145`), and the maintainer needs the way back.
- `cmd/hivegui/update_verify_darwin.go` — `teamIDRequirement(teamID string)
  string`, and `verifyDeveloperIDSignature(bundle string) error` running
  `/usr/bin/codesign` then `/usr/sbin/spctl` through a `runArgv` seam. Returns a
  signature-specific error ("this update is not signed by the Hive developer …")
  distinct from the checksum-mismatch text. Returns nil when
  `buildinfo.SigningTeamID()` is empty.
- `docs/releasing-signed-macos.md` — the setup walkthrough, written for an Apple
  Developer account that has no certificate yet: CSR via Keychain Access →
  Certificates page → download → install, finding the Team ID,
  `xcrun notarytool store-credentials`, the two env vars, the notarytool retry
  story (2–15 min, Apple-side outages, now on every release's critical path),
  and the key-rotation procedure the spec asks to be documented but not
  automated.
- `.changesets/374-signed-macos-releases.md` — required by
  `scripts/check-changeset.sh` and `.github/workflows/changesets.yml`, and this
  is user-visible (no more Gatekeeper prompt).

Not applicable: `scripts/check-daemon-contract.sh`. No `hived` protocol change,
so no `buildinfo.DaemonContract` bump is needed.

### Tests

Go, beside source, following the `httptest` + `stubBundle` table pattern in
`cmd/hivegui/update_apply_darwin_test.go`:

- `TestVerifyDeveloperIDSignaturePassesPinnedRequirement` — the load-bearing
  test. Stubs the `runArgv` seam, captures the argv actually handed to
  `codesign`, and asserts `-R` is present with the correctly quoted Team ID.
  `teamIDRequirement` returning the right string is worthless if the call site
  drops the flag, and that failure silently degrades the check to "signed by any
  Apple developer" while every other test still passes.
- `TestVerifyDeveloperIDSignatureUsesAbsoluteBaseOSPaths` — asserts
  `/usr/bin/codesign` and `/usr/sbin/spctl`, never `xcrun`. Guards the
  regression that would brick updates on Macs without Xcode CLT.
- `TestStageReleaseRejectsUnverifiedSignature` — `verifySignatureFn` stub
  errors; asserts `stageRelease` fails, the message names the signature rather
  than the checksum, and the staging directory was removed.
- `TestVerifyDeveloperIDSignatureSkipsWhenTeamIDUnset` — with the Team ID
  overridden to empty, returns nil without invoking the seam.
- `TestBuildWithoutCredentialsStillPinsTeamID` — asserts `SigningTeamID()` is
  non-empty in an ordinary `go build` with no signing environment, so a
  latest-channel or contributor build still verifies downloaded releases.
- `TestAdhocSignatureFailsTeamIDPin` — an ad-hoc signature has no OU, so
  `verifyDeveloperIDSignature` must reject a self-signed bundle. Proves the
  developer convenience cannot become a distribution hole.
- `TestTeamIDRequirement` — exact requirement string including quoting.
- `TestVerifyDeveloperIDSignatureTreatsDisabledSpctlAsPass` — stubbed `spctl`
  returns the "assessments are disabled" output; asserts the update is still
  accepted and that a `codesign` failure in the same run still refuses it.
- `TestVerifyDeveloperIDSignatureRespectsContextCancellation` — asserts the
  bounded timeout is plumbed through, so a hung `spctl` cannot wedge staging.

`signingTeamID` is a package `var`, not a `const`, so the skip-when-unset test
stays runnable after the real Team ID is committed.

`scripts/sign-macos.sh` gets no automated test — the repo has no shell-test
harness and a meaningful test needs real Apple credentials. `shellcheck` plus
the manual verification below cover it.

### Verification

```bash
scripts/test.sh go
GOTOOLCHAIN=$(sed -n 's/^go //p' go.mod) go test ./cmd/hivegui/... -count=1
for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os go vet ./...; done
shellcheck scripts/sign-macos.sh scripts/release.sh && bash -n scripts/sign-macos.sh
scripts/check-changeset.sh
```

Manual, gated on the certificate existing (see `docs/releasing-signed-macos.md`):

```bash
export HIVE_SIGN_IDENTITY="Developer ID Application: <Name> (<TEAMID>)"
export HIVE_NOTARY_PROFILE=hive-notary
./build.sh --zip --version 0.0.0-signtest --platform macos
scripts/sign-macos.sh cmd/hivegui/build/bin/hivegui.app release/Hive-0.0.0-signtest-macos-universal.zip

ditto -x -k release/Hive-0.0.0-signtest-macos-universal.zip /tmp/signtest
codesign --verify --deep --strict --verbose /tmp/signtest/hivegui.app
xcrun stapler validate /tmp/signtest/hivegui.app

# Gatekeeper proof. A locally-extracted bundle carries no com.apple.quarantine
# xattr, so spctl on it does NOT reproduce what a downloaded app faces — set the
# xattr, and pull the network, so a pass proves the ticket is stapled rather
# than fetched from Apple.
xattr -w com.apple.quarantine "0083;00000000;Safari;" /tmp/signtest/hivegui.app
networksetup -setairportpower en0 off
spctl --assess --type execute --verbose /tmp/signtest/hivegui.app
networksetup -setairportpower en0 on

# Tamper proof: the updater must refuse this bundle with a signature-specific error.
codesign --remove-signature /tmp/signtest/hivegui.app

# The staple must survive the swap. copyBundleFn is a plain `ditto src dest`
# (update_apply_darwin.go:588-590) and verification happens at stage time, never
# after swapBundle (:530-556) — so confirm the ticket is still there afterwards.
xcrun stapler validate "/Applications/Hive.app"
```

**Hardened-runtime smoke, runnable today without a certificate** — this is the
step that retires the spec's open question #3:

```bash
./build.sh --platform macos
scripts/sign-macos.sh --adhoc cmd/hivegui/build/bin/hivegui.app
codesign -dv --verbose cmd/hivegui/build/bin/hivegui.app   # expect flags=…(adhoc,runtime)
open cmd/hivegui/build/bin/hivegui.app
# then: start a session, confirm the PTY spawns a real shell and hived stays up
```

Plus the same smoke on the real signed build once the cert exists: launch it, open a PTY session
running a real shell, toggle the login item (which `loginitem_darwin.go` says
only works signed), and confirm no entitlement denial in Console or
`log stream --predicate 'subsystem == "com.apple.TCC"'`.

## Second opinion

Two rounds, both `revise`; all must-fix items applied.

**Round 1 — `revise`, confidence 7.** Caught that `xcrun stapler` ships with the
Xcode Command Line Tools rather than base macOS, so calling it from a
fail-closed updater check would have refused every update on a stock Mac; that
`release.sh` cannot read a Go `const`, so the Team-ID gate was hand-waved; that
nothing tied the signing identity to the pinned ID; that signing failed only
*after* `git tag`; that no changeset was planned; and that the planned tests
would all pass if the `-R` flag were dropped at the call site. Applied: base-OS
absolute paths, ldflags stamping derived from `HIVE_SIGN_IDENTITY`, an OU
assertion in `sign-macos.sh`, pre-flight ahead of commit/tag, a changeset, an
argv-capturing test, and a stale-comment sweep.

**Round 2 — `revise`, confidence 8.** Confirmed round 1's fixes held, then found
that the revision had introduced a seam that cannot exist: `release.sh:156` runs
`build.sh --platform all`, and `build_windows`'s `wails build -clean`
(`build.sh:180`) wipes `cmd/hivegui/build/bin` — `build.sh:203-206` says so in
its own comment — so the `.app` the script was to be handed is gone by then.
Verified directly and reverted `sign-macos.sh` to take the zip. Also applied:
`exec.CommandContext` timeouts (an uncontexted `spctl` can hang staging on a
captive-portal network), defined `spctl`-under-`--master-disable` semantics, a
stamp-matches-signature assertion, a `^[A-Z0-9]{10}$` allowlist on the value
spliced into `-ldflags`, a staple-survives-swap check, and the unwind recipe on
a post-tag signing failure.

Per the loop's one-retry rule the reviewer was not run a third time.

## Decision log

- **2026-09-06** — Trust root: Apple Developer ID + notarization. Why: settled
  in the spec's Notes; covers install and update paths with one mechanism.
- **2026-09-06** — Updater pins the Team ID via a `codesign -R` requirement.
  Why: bare `--verify` accepts any valid Developer ID, including an attacker's
  own, so it authenticates Apple rather than Hive.
- **2026-09-06** — Signature is checked on the extracted bundle in the staging
  directory, not on the zip. Why: `codesign`/`spctl`/`stapler` cannot read a zip
  stream; the existing `os.RemoveAll` cleanup keeps it fail-closed against the
  installed app. Spec wording amended to match.
- **2026-09-06** — Releases stay local; credentials come from a `notarytool`
  keychain profile. Why: operator decision — smallest change, no signing keys in
  GitHub secrets.
- **2026-09-06** — Team ID is read by the release scripts with a `sed` over the
  single declaration in `internal/buildinfo/signing.go`, and
  `TestSigningTeamIDMatchesSource` asserts that extraction agrees with what the
  compiler sees. Why: a shell script cannot call into Go, and the reviewer's
  objection that grepping source is brittle is answered by a test rather than by
  care. The alternative — adding the ID to `buildinfo.Identity` — would put it on
  the wire in `Welcome` and drag in the daemon contract for no benefit.
- **2026-09-06** — Added `sign-macos.sh --adhoc` for credential-free local
  signing. Why: operator asked for a self-sign path; verified that ad-hoc
  signing carries the hardened-runtime flag, so it answers the spec's PTY /
  hardened-runtime open question before the certificate exists. An ad-hoc
  signature has no Team ID, so it can never pass the updater's pin.
- **2026-09-06** — No entitlements plist in the first cut. Why: notarization
  requires hardened runtime, not entitlements; Hive's cgo links system
  frameworks only and PTY work is plain `exec`. Add one only if the smoke test
  demands it.
- **2026-09-06** — An empty Team ID (before the certificate exists) means skip
  verification, and `release.sh` refuses to publish without a signing identity. Why: keeps a certless
  checkout buildable without ever letting an unpinned build reach users.
- **2026-09-06** — Team ID is a committed `var` in `internal/buildinfo`, not an
  ldflags stamp. Why: operator flagged that the in-GUI latest-commit updater
  must build without signing credentials. Under stamping, every
  credential-free build (latest channel `stageLatest`, a second machine, a
  contributor) would carry an empty pin and silently skip verification of the
  release-channel updates it later downloads. A committed default verifies by
  default; `release.sh` asserts the signing identity matches it.
- **2026-09-06** — Updater uses `/usr/bin/codesign` + `/usr/sbin/spctl`, never
  `xcrun stapler`. Why: `stapler` ships with Xcode CLT, not macOS, so a
  fail-closed check calling it would refuse every update on a stock Mac.
- **2026-09-06** — Signing pre-flight moved ahead of the commit/tag step. Why:
  failing after `git tag` leaves a local release commit and tag with no
  documented unwind.

## Progress

- **2026-09-06** — Spec advanced BACKLOG → RESEARCH; research recorded.
- **2026-09-06** — Plan approved after two reviewer rounds and one operator
  revision round (Team ID committed rather than stamped; `--adhoc` added).
- **2026-09-06** — Implemented on `feature/374-sign-and-notarize-macos-releases`.
  Two pre-existing failures confirmed against a clean `origin/main` checkout and
  left alone: `TestTerminalQueriesAreNotWork` (internal/registry) and
  `stateLockPoll is unused` (GOOS=windows staticcheck, internal/daemon).
- **2026-09-06** — PR #378 opened.
- **2026-09-06** — Plan item 8 (README/CONTRIBUTING right-click-Open text)
  dropped: no such text exists in either file. It came from a reviewer's guess
  at the blast radius, not from the tree.

## Open questions

- **Decide at the plan stop:** under `spctl --master-disable` the notarization
  check does not evaluate. The plan treats that as pass and leans on the
  `codesign -R` Team-ID pin, on the reasoning that a user who globally disabled
  Gatekeeper has already made that call machine-wide. The stricter alternative
  is to refuse updates outright on a Gatekeeper-disabled machine.
- Not blocking. The Apple `Developer ID Application` certificate does not yet
  exist (`security find-identity -v -p codesigning` reports 0 valid identities),
  so the manual verification block and the `signingTeamID` value are deferred
  until `docs/releasing-signed-macos.md` has been followed.
