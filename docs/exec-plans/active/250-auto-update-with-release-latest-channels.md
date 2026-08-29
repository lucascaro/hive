# Auto-update with release/latest channels

- **Spec:** [docs/product-specs/250-auto-update-with-release-latest-channels.md](../../product-specs/250-auto-update-with-release-latest-channels.md)
- **Issue:** —
- **PR:** #291
- **Branch:** feature/250-auto-update-with-release-latest-channels
- **Status:** active

## Summary

Adds the apply half of the update story: a persisted channel setting (`release` /
`latest`), a background staging step behind one button, and an in-place bundle swap that
reuses the existing daemon-restart + relaunch path. macOS only for the swap; other
platforms keep today's open-the-release-page banner.

## Research

Authored via plan-first mode; the code references below were identified during that pass.

- `cmd/hivegui/update.go` — existing `CheckForUpdate`, `startUpdateCheckLoop`,
  `compareSemver`, the `updateURLPrefix` allowlist, and the `Version() == "dev"` skip.
- `cmd/hivegui/frontend/src/app/banners.ts:129-260` — the update banner, its
  `update:available` subscription, dismiss-for-version localStorage key, and the
  Download button that opens the release page.
- `cmd/hivegui/window_state.go` — the GUI already writes its own JSON (`window.json`) into
  `registry.StateDir()` with temp+rename. Precedent for `update.json`; the "registry is the
  only writer" rule in `DESIGN.md` governs registry state, not GUI prefs.
- `cmd/hivegui/window_unix.go:27-48` — `spawnNewGUI` and `enclosingAppBundle`, which
  already resolve the running `.app` and relaunch it via `open -n`.
- `cmd/hivegui/app_control.go:155` — `RestartDaemon` already implements the whole teardown:
  `FrameShutdown` → `socketDead` probe → `killRunningHived` escalation → refuse-if-alive →
  `spawnNewGUI` → `wruntime.Quit`. The apply path delegates rather than reimplements.
- `cmd/hivegui/restart_unix.go` — the `looksLikeHivedFn` / `waitForExitFn` package-level
  seams are the house pattern for testing shell-out branches; the git and build runners
  copy it.
- `build.sh:95-135` — macOS bundle is `cmd/hivegui/build/bin/hivegui.app`; the release zip
  is produced with `zip -rq` into `release/Hive-<v>-macos-universal.zip`.
- `scripts/release.sh` — publishes exactly two artifacts today; no checksums.
- `cmd/hivegui/frontend/src/app/modals/settings.ts` — the modal is agents-only, with a
  draft/save cycle, a `loadFailed` guard, and `openToken` staleness invalidation.
- `cmd/hivegui/app_calls.go:63,275` — `ListCustomAgents`'s corrupt-file-is-an-error
  handling, and `PickDirectory` for the source-repo picker.

## Approach

Three new concerns in their own files, plus edits to the existing update path. Chosen over
the obvious alternative — bolting the staging logic into `update.go` — because the apply
step is macOS-only and needs `//go:build` splits, and because `staticcheck` runs per-GOOS
(AGENTS.md), so platform-specific code must live in platform-specific files or it reads as
dead on the other legs.

**Preferences.** `<stateDir>/update.json` holding `{channel, source_repo}`, written by the
GUI with temp+rename like `window.json`. Bound as `GetUpdateSettings` /
`SaveUpdateSettings`. Empty/unknown channel reads back as `release`; a corrupt file is an
error surfaced to the modal, never silently overwritten (mirrors `ListCustomAgents`).

**Source repo.** `resolveSourceRepo(configured)` takes the configured path when set,
otherwise walks up from `os.Executable()` for a `go.mod` declaring
`module github.com/lucascaro/hive`. Both paths validate `.git` + `build.sh` + that module
line. `SourceRepoStatus()` is bound so Settings shows resolved-or-not inline.

**Check.** `CheckForUpdate` dispatches on channel. `release` is today's code verbatim.
`latest` runs `git fetch` then compares `HEAD` against the tracked upstream; `Available`
when strictly behind, `Current` = `buildinfo.BuildID()`, `Latest` = upstream short SHA,
and `dev` is *not* skipped there. `UpdateInfo` gains `Channel`, `Stage`
(`idle|available|staging|ready|error`), and `Message`.

**Staging.** `StartUpdate()` runs one goroutine behind an `atomic.Bool`, emitting
`update:progress` events. Release: re-fetch the release JSON, take the
`Hive-<v>-macos-universal.zip` and `checksums.txt` assets (both URLs must pass
`updateURLPrefix`), download under a deadline and a byte cap into
`<stateDir>/updates/<v>/`, verify SHA-256, extract with `ditto -x -k`, assert
`hivegui.app/Contents/MacOS/{hivegui,hived}`. Latest: refuse on a dirty tree / detached
HEAD / no upstream, `git pull --ff-only`, run `./build.sh` streaming progress, stage
`cmd/hivegui/build/bin/hivegui.app`.

**Apply.** `ApplyUpdateAndRestart()` resolves the install target via
`enclosingAppBundle`, `ditto`s the staged bundle to a *sibling* of it (staging dir and
`/Applications` can be on different volumes, so `os.Rename` would fail `EXDEV`), then does
two same-directory renames with rollback on the second, and finally calls `RestartDaemon`.
Swap first, restart second: a failed swap must leave a working window.

**Frontend.** One shared reducer (`lib/update-state.ts`) drives the button label
(`Update` → `Updating…` → `Restart`) from `UpdateInfo.stage` + `update:progress`, so the
banner and the modal cannot disagree. The button acts immediately rather than joining the
modal's draft/save cycle, so Cancel never discards staging.

**Release script.** `scripts/release.sh` emits `release/checksums.txt` and attaches it.
Without it the release channel has nothing to verify against, so it ships here.

### Files to change

- `cmd/hivegui/update.go` — channel dispatch, `Channel`/`Stage`/`Message` on
  `UpdateInfo`, stage constants; the release check split out as `checkRelease`.
- `cmd/hivegui/app.go` — `updateState` field on `App`.
- `cmd/hivegui/frontend/index.html` — `#settings-updates` section; banner action button.
- `cmd/hivegui/frontend/src/app/modals/settings.ts` — channel select, source-repo row,
  update button; `SaveUpdateSettings` runs before `SaveCustomAgents`.
- `cmd/hivegui/frontend/src/app/banners.ts` — action button, `update:progress`
  subscription, skipped-reason text now comes from Go.
- `cmd/hivegui/frontend/src/bridge.ts` + `test/e2e/wails-mock.ts` +
  `test/e2e-real/wails-bridge.ts` — the six new bindings.
- `cmd/hivegui/frontend/src/style.css` — updates section, disabled-button state.
- `scripts/release.sh` — publish `checksums.txt`.
- `CHANGELOG.md`, `README.md` (rewritten Updating section), `DESIGN.md` (the
  registry-only-writer rule now names the three GUI-owned files).

### New files

- `cmd/hivegui/update_prefs.go` — `UpdateSettings` load/save + the two bindings.
- `cmd/hivegui/source_repo.go` — `resolveSourceRepo`, validation, `SourceRepoStatusFor`.
- `cmd/hivegui/update_latest.go` — the git side of the latest channel (`checkLatest`,
  `runGitFn` seam). Cross-platform: only *applying* is macOS-only.
- `cmd/hivegui/update_action.go` — the `StartUpdate` / `UpdateStatus` /
  `ApplyUpdateAndRestart` state machine, shared by both surfaces.
- `cmd/hivegui/update_apply_darwin.go`, `cmd/hivegui/update_apply_other.go` — staging
  (download + verify + `ditto`, or pull + `build.sh`) and the bundle swap.
- `cmd/hivegui/frontend/src/lib/update-state.ts` — the shared button reducer.

### Tests

Go — `update_prefs_test.go`, `source_repo_test.go`, `update_latest_test.go`,
`update_action_test.go`, `update_apply_darwin_test.go`, plus two additions to
`update_test.go`. The ones that carry the risk:

- `TestUpdateSettingsCorruptFileIsError` — a bad `update.json` is surfaced, not
  overwritten with defaults.
- `TestSaveUpdateSettingsRefusesLatestWithoutSourceRepo` — the channel cannot be
  saved into a state with nothing to check.
- `TestStageReleaseRejectsChecksumMismatch` / `…RejectsOffPrefixAssetURL` /
  `…RequiresChecksumManifest` — nothing unverified is unpacked, and the staging
  dir is removed so a later run can't mistake it for verified.
- `TestStageLatestRefusesDirtyWorktree` — asserts no `git pull` and no build ran.
- `TestSwapBundleRollsBackOnFailure` / `TestApplyRefusesOutsideAppBundle`.
- `TestStartUpdateIsSingleFlight` / `TestRememberCheckDropsStaleStaging` — the button
  can't double-stage, and a newer release invalidates a staged bundle.
- `TestCheckForUpdate_LatestChannelSkipsReleasesAPI` — the channel actually routes.
- `stubReleases` now isolates `HIVE_STATE_DIR`, so the existing update tests can no
  longer read the developer's real channel setting.

Frontend — `test/unit/update-state.test.ts`, `test/dom/settings-updates.test.ts`,
`test/dom/update-banner.test.ts`, plus two Playwright cases in
`test/e2e/settings.spec.ts` that check the new section is actually hittable under a
long agent list (jsdom is blind to that) and that the channel reveals the repo row.

## Manual verification

Everything below needs a real bundle on disk; no automated layer covers the
install-and-restart. Run from a clean tree.

**1. Build and install a fake "old" release.**

```sh
./build.sh --version 0.0.1
cp -R cmd/hivegui/build/bin/hivegui.app /Applications/
open /Applications/hivegui.app
```

The sidebar footer should read `hive 0.0.1 (<sha>)`. Anything else means the
`--version` stamp did not reach `buildinfo`.

**2. Release channel — check.** Settings (⌘,) → Updates → channel `Release`,
Save. Then **File → Check for Updates…**. The banner should offer the real
latest release with an **Update** button next to Download. (Version 0.0.1 is
below every published tag, so this is guaranteed to report available.)

**3. Release channel — apply.** Click **Update**. Watch the banner text walk
`Looking up release… → Downloading checksums… → Downloading Hive-…zip… →
Verifying download… → Unpacking…`, then the button becomes **Restart**. Staging
lands in `~/Library/Application Support/Hive/updates/<version>/`; confirm the
`.zip` is gone and `app/hivegui.app` is there.

Click **Restart**. Expect: the window closes and reopens, the footer now shows
the released version, and any sessions that were open come back. Confirm
`/Applications/hivegui.app` changed (`ls -la` mtime) and that no `.hivegui.app.new`
or `.hivegui.app.old` is left in `/Applications`.

> **Note:** this only works once the first release carrying `checksums.txt` is
> published — `stageRelease` refuses a release without the manifest. Until then,
> test step 3 against a local stub by pointing `updateReleasesAPI` at a file
> server, or verify it after cutting the next release.

**4. Latest channel — auto-detect.** Run a build from the checkout itself
(`./build.sh && open cmd/hivegui/build/bin/hivegui.app`). Settings → channel
`Latest`. The source-repo row should appear already saying **Detected
/path/to/hive** without you typing anything. Save.

**5. Latest channel — apply.** Move the checkout one commit behind:
`git reset --hard HEAD~1`. Check for updates → banner reports `commit <sha> is
available`. Click **Update**; the banner should stream `build.sh` output. When
it says Restart, click it and confirm the footer's build id advances to the
upstream sha.

**6. Latest channel — dirty-tree refusal.** With an uncommitted change in the
checkout, click Update. Expect an immediate error naming the uncommitted
changes, and `git log` unchanged — no pull, no build.

**7. Negative — corrupt download.** Point `updateReleasesAPI` at a local stub
serving a zip whose bytes do not match its `checksums.txt` entry (the shape
`TestStageReleaseRejectsChecksumMismatch` builds). Expect a `checksum mismatch`
banner, an untouched `/Applications/hivegui.app`, and no leftover staging
directory.

**8. Cleanup.** `rm -rf /Applications/hivegui.app` if you installed a throwaway
build, and reset the channel to `Release`.

## Decision log

- **2026-08-29** — macOS-only self-update. Why: Windows needs a detached helper to replace a
  running `.exe` and Linux ships no release artifact; both already have a working
  open-the-release-page fallback.
- **2026-08-29** — `latest` resolves the checkout by walking up from `os.Executable()`, with
  an explicit path fallback. Why: zero config for local builds, still usable from an
  installed `.app` where auto-detect cannot work.
- **2026-08-29** — Check automatically, stage and restart only on click. Why: Hive hosts
  live PTY sessions and a `latest`-channel stage runs a multi-minute `wails build`; neither
  should happen unasked.
- **2026-08-29** — Verify release downloads against a SHA-256 manifest published by
  `release.sh`. Why: a truncated download would otherwise become a broken `hivegui.app`
  with no in-app way back. Not a supply-chain defense — signing/notarization is out of scope.
- **2026-08-29** — Apply delegates its teardown to the existing `RestartDaemon` rather than
  reimplementing the kill/relaunch dance. Why: that function already handles the zombie-hived
  case and refuses to quit into a dead window.
- **2026-08-29** — Prefs live in a GUI-owned `update.json`, not the registry. Why:
  `window.json` sets the precedent, and routing a GUI preference through the wire protocol
  would touch all three wire clients for no benefit.

## Progress

- **2026-08-29** — Plan-first scaffold; Stage = IMPLEMENT (set in the spec).
- **2026-08-29** — Implemented on `feature/250-auto-update-with-release-latest-channels`.
  `scripts/test.sh` green; `staticcheck` + `go vet` clean for darwin/linux/windows;
  `biome ci` and `tsc --noEmit` clean.

## Open questions

- If the bundle is ever signed/notarized, the in-place swap needs revisiting. Tracked as a
  `ponytail:` comment in the apply path rather than a blocker.

## PR convergence ledger

<Append-only. One line per `/hs-review-loop` iteration.>

- **2026-08-29 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 284e0007…81e6; threads_open: 0; action: escalated:risky fix needs human decision; head_sha: b977ca9.
