# Windows in-app update

- **Spec:** this file (design agreed in-session; no separate product spec)
- **Issue:** —
- **PR:**
- **Stage:** IMPLEMENT
- **Status:** active
- **Depends on:** the `internal/proc` console-window fix (branch
  `fix/windows-console-popups`). This plan branches off it because
  `proc.Command` and `TestNoDirectExecOnWindows` do not exist on `main`
  yet, and every child process added here is subject to that rule.

## Summary

In-app update is macOS-only: `stageUpdate` and `applyStagedBundle` are
stubs off darwin (`update_apply_other.go`), and the frontend hides the
button behind `isMac`. On Windows the user is told to "download it
manually on this platform" — advice that is wrong twice over on the
latest channel, which tracks a git checkout and has no release artifact
to download at all.

This adds a real Windows implementation of both channels with the same
UX: the same Update → Updating… → Restart button, the same progress
lines, the same reload-vs-restart distinction.

## Research

The update pipeline is already almost entirely cross-platform. Only two
functions are darwin-gated:

- `cmd/hivegui/update_apply_darwin.go` — `stageUpdate`,
  `applyStagedBundle`, plus ~200 lines of *portable* helpers that merely
  live in a darwin-tagged file (`download`, `checksumFor`, `sha256File`,
  `stagingDir`, `updatesRoot`, `assetURL`, `fetchReleaseAssets`,
  `plainProgressLine`, `verifyUpstreamRemote`, `isDownloadedStaging`).
- `cmd/hivegui/update_apply_other.go` — `//go:build !darwin` stubs
  returning `errUnsupported`.

Everything else already works on Windows: `update.go` (release check),
`update_latest.go` (git-based latest check), `update_action.go` (the
staging state machine), `update_prefs.go`, `restart_windows.go`
(kill + relaunch hived), `window_windows.go` (detached respawn).

Two portability bugs found while reading:

- `update_restart_kind.go` is **untagged** but hardcodes
  `Contents/MacOS/hived`, so the staged-daemon contract probe cannot
  work off darwin.
- `shell_env_darwin.go`'s `executableIn` tests `st.Mode()&0o111`, which
  is always 0 on Windows — a port that reused it would report every
  build tool missing.

### Empirical basis for the swap strategy

`update_apply_other.go` asserts that replacing a running `.exe`
"would need a detached helper process". Measured on Windows 11
(probe in session, a running Go binary as the target):

| Operation on a running `.exe` | Result |
|---|---|
| Overwrite in place | FAIL — sharing violation |
| Delete | FAIL — access denied |
| **Rename (`MoveFile`)** | **OK** |
| Create a new file at the freed path | OK |
| Process survives all of the above | yes |

So the rename-aside then install then rollback shape that `swapBundle`
already uses on macOS works unchanged on Windows, with **no helper
process and no elevation**. The only residue is that the renamed-aside
image cannot be deleted while it is still mapped, so it is pruned at
next startup.

## Approach

Three structures were considered:

- **Duplicate** a standalone `update_apply_windows.go`. Zero risk to
  macOS, but copies ~200 lines of download/checksum/git logic.
- **Full extraction** — one cross-platform `stageUpdate` driven by
  platform hook vars. Best on paper, but it rewrites working darwin
  orchestration that cannot be verified locally: darwin cross-compile
  fails on cgo in `internal/notify` and `internal/activity`, so the
  macOS leg is CI-only.
- **Mechanical move + separate skeletons** — chosen.

The portable helpers move **byte-identical** out of the darwin file into
an untagged `update_apply_common.go`. A pure file move is the safest
possible edit to code that cannot be run locally. The orchestration
skeletons (`stageRelease`, `stageLatest`, `applyStagedBundle`) stay
per-platform, because their step sequences genuinely differ — ditto +
codesign + `.app` bundle versus stdlib zip + two loose `.exe`s — and two
explicit platform stories read better than one function behind eight
hook variables.

Moving the portable *tests* to an untagged file is a side benefit:
`download`, `checksumFor`, `stagingDir` and `plainProgressLine` are
currently exercised on macOS only, and start running on all three CI
legs.

### Windows specifics

**Staged payload.** `updateState.bundle` already carries a path string;
on Windows it is a *directory* holding `hivegui.exe` + `hived.exe`, so
no types change. Release channel stages into
`<stateDir>/updates/<ver>/app/`; latest channel points at
`cmd/hivegui/build/bin`, mirroring darwin.

**Extraction.** stdlib `archive/zip`, not `ditto`. Only the exact names
`hivegui.exe` and `hived.exe` are extracted and every other entry is
ignored, which makes zip-slip structurally impossible rather than merely
guarded against.

**Trust.** Checksums only. macOS pins an Apple Developer Team ID
(`buildinfo.SigningTeamID`), but Windows release binaries are not signed
at all — there is no `signtool` in `build.sh` or
`scripts/release-artifacts.sh` — so there is no publisher to pin.
`verifySignature` is a Windows no-op, reusing the shape darwin already
has for builds with an empty pin. `checksums.txt` still catches a
corrupt or truncated download, which is the failure users actually hit.
**This is a deliberate and documented gap**: the Windows release channel
has integrity checking but no supply-chain defense, and closing it
requires either code-signing certificates or a detached signature over
`checksums.txt` — both upstream release-process decisions, out of scope
here.

**The swap.** `applyStagedBundle` copies each staged exe to
`installDir\.<name>.new` (same directory, so the final rename is
same-volume and effectively atomic), then per file renames `<name>` to
`.<name>.old` and `.new` to `<name>`, rolling back every rename already
made if any step fails. `.old` files are pruned at next GUI startup,
since they cannot be removed while still mapped.

**Refusals, both pre-flighted before any work starts.**

- *Unwritable install directory* (e.g. `C:\Program Files\Hive`): probed
  by creating and removing a temp file. Refused with a message naming
  the directory. No UAC, no elevated child process — deliberately, since
  an elevated process copying from a user-writable staging directory is
  a privilege-escalation vector, and Hive on Windows ships as a zip with
  no installer anyway.
- *Install directory is the build output directory.* On the latest
  channel `wails build -clean` wipes `cmd/hivegui/build/bin`, and
  deleting a running image fails — so a user running Hive straight out
  of the checkout would break the build before it started. Detected in
  `stageLatest` and refused within a second of the click rather than
  five minutes into a build, with a message naming the fix (run Hive
  from outside the checkout).

**`canApply` stops being a browser guess.** `UpdateInfo` gains
`CanApply bool` + `CanApplyReason string`, carried by the existing
event/status plumbing, so no new Wails binding. The frontend drops
`isMac` from the update path and renders the backend's reason verbatim.
darwin returns `true, ""` unconditionally — preserving today's macOS
behaviour exactly rather than changing UX that cannot be tested locally.

### Files to change

- `cmd/hivegui/update_apply_darwin.go` — portable helpers move out; the
  darwin skeletons stay. No behaviour change.
- `cmd/hivegui/update_apply_other.go` — build tag becomes
  `!darwin && !windows`; its duplicate `pruneStagingDirs` is dropped in
  favour of the shared one; gains the Linux `updateCapability`,
  `stagedDaemonPath` and `pruneRenamedAside` stubs.
- `cmd/hivegui/app.go` — `pruneRenamedAside()` on startup.
- `cmd/hivegui/update_apply_darwin_test.go` — portable tests move out.
- `cmd/hivegui/update.go` — `UpdateInfo.CanApply` + `.CanApplyReason`,
  populated on every check.
- `cmd/hivegui/update_restart_kind.go` — staged-hived path becomes a
  platform hook instead of a hardcoded `Contents/MacOS/hived`.
- `cmd/hivegui/frontend/src/lib/update-state.ts` — render
  `canApplyReason`; stop appending "Open releases page manually." on the
  latest channel, which has no release page.
- `cmd/hivegui/frontend/src/app/banners.ts`,
  `cmd/hivegui/frontend/src/components/modals/Settings.tsx` — pass
  `info.canApply` instead of `isMac`.
- `README.md` — the two lines stating in-app update is macOS-only.

### New files

- `cmd/hivegui/update_apply_common.go` — the moved portable helpers.
- `cmd/hivegui/update_apply_common_test.go` — the moved portable tests,
  now running on every platform.
- `cmd/hivegui/update_apply_windows.go` — `stageUpdate`,
  `applyStagedBundle`, zip extraction, the exe swap, refusal
  pre-flights, startup pruning.
- `cmd/hivegui/update_apply_windows_test.go` — Windows coverage.
- `cmd/hivegui/shell_env_windows.go` — `envWithLoginPATH`,
  `missingBuildTools`, `pathOf`, `pathSourceDescription` counterparts,
  using `exec.LookPath` (PATHEXT-aware) rather than the darwin mode-bit
  check.
- `cmd/hivegui/update_capability_darwin.go`,
  `cmd/hivegui/update_capability_windows.go` — the `canApply` probe and
  the staged-hived path. The Linux third lives in
  `update_apply_other.go` rather than a file of its own, since it is
  three constant-returning stubs.
- `.changesets/windows-in-app-update.md`.

### Tests

Windows (`update_apply_windows_test.go`):

- `TestSwapExesReplacesInstalled` — both exes replaced, `.old` left behind.
- `TestSwapExesRollsBackOnFailure` — a failure on the second exe restores the first.
- `TestSwapSurvivesRunningImage` — swap against a *running* child process, the empirical premise pinned as a regression test.
- `TestApplyRefusesUnwritableInstallDir`
- `TestStageLatestRefusesBuildDirInstall` — the collision refusal.
- `TestExtractZipTakesOnlyExpectedNames` — zip-slip / stray entries ignored.
- `TestVerifyPayloadRequiresBothExes`
- `TestPruneRenamedAsideOnStartup`
- `TestMissingBuildToolsUsesLookPath` — the `0o111` trap pinned.

Shared (`update_apply_common_test.go`): the moved
`TestChecksumForAcceptsBinaryMarker` and `TestStagingDirSanitizesVersion`,
now running on all three legs.

Frontend (`test/unit/update-state.test.ts`): `canApply` sourced from the
backend; `canApplyReason` rendered; no release-page sentence on the
latest channel.

### Discovered during implementation

- Extracting zip entries by *base* name let a nested `docs/hivegui.exe`
  shadow the real one, non-deterministically — Go map iteration order
  decided which won, so the test failed about one run in two. Entries
  are now matched only at the top level of the archive, which is where
  build.sh puts them.
- `rememberCheck` now calls `updateCapability()`, which reads
  `update.json` and probes the install directory. That is read-only with
  respect to hive state, but it is new I/O on a path tests drive.

## Decision log

- **2026-09-12** — Rename-aside instead of a detached helper. Why:
  measured that renaming a running `.exe` succeeds on Windows, which the
  existing comment assumed impossible; removes an entire helper process
  and its elevation surface.
- **2026-09-12** — Checksums-only trust on Windows. Why: no Windows
  release binary is signed, so there is no publisher to pin; documented
  as a gap rather than papered over.
- **2026-09-12** — Refuse rather than elevate on an unwritable install
  dir. Why: an elevated process copying from user-writable staging is a
  privilege-escalation vector, and Windows Hive ships as a zip with no
  installer.
- **2026-09-12** — Refuse rather than work around the
  install-dir-is-build-dir collision. Why: the workaround moves the
  running images out of the install directory for the duration of a
  multi-minute build, leaving the install broken if the app dies
  mid-build.
- **2026-09-12** — darwin `canApply` stays unconditionally true. Why:
  macOS cannot be compiled or tested locally; a real probe there is a
  behaviour change that belongs in its own PR.

## Progress

- **2026-09-12** — Plan written; branch `feat/windows-in-app-update`.
- **2026-09-12** — Implemented. Gates on this Windows machine: `go build
  ./...` and `go vet ./...` clean; `go test ./...` passes apart from two
  pre-existing environmental failures that also fail on a clean tree
  (`TestPackageIsIsolatedFromRealHiveState` — this machine's TEMP sits
  under the user's home; `registry.TestManagedPath` — symlink creation
  needs a privilege this account lacks). Frontend: `tsc --noEmit` clean,
  biome clean on every touched file, 1243 vitest tests and 314
  Playwright mock tests pass.
- **2026-09-12** — The macOS leg is unverified locally and must be
  green in CI before this lands: darwin cross-compile fails on cgo in
  `internal/notify` and `internal/activity`, so nothing here could
  typecheck the darwin files. The darwin changes are a verbatim block
  move plus an import-list trim, and both darwin files parse under
  `gofmt -e`.

## Open questions

None.
