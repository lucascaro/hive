# Archive a session now, unarchive it later

- **Spec:** [docs/product-specs/518-archive-a-session-now-unarchive-it-later.md](../../product-specs/518-archive-a-session-now-unarchive-it-later.md)
- **Issue:** #518
- **Status:** active

## Summary

Add an explicit Archive action that stops a session's process, keeps its worktree, and stores a durable record that is never pruned; a palette-opened modal lists archived sessions and Unarchive restores one via the same path #304 uses for closed sessions. This plan is the *how*; the *why* is in the spec.

## Research

### Relevant code (all under repo root)

- `internal/registry/closed.go` — tombstone store. `Tombstone` (:47), `RestoreResult` (:74), `ClosedDir` (:93), `writeTombstoneLocked` (:111), `readTombstone` (:160), `listTombstones` (:208), `pruneTombstones` (:253, only lists `closed/`, so a separate `archived/` dir is never pruned), `discardTombstone` (:269), `tombstoneClaimsWorktree` (:290, the orphan-reclaim exemption), `Restore` (:315, rebuilds the entry with the SAME id, re-adopts/recreates the worktree, then `Revive`), `resolveRestoreWorktree` (:425).
- `internal/registry/registry.go` — `kill()` (:1674-1856): dirty guard (:1741), tombstone write under `r.mu` (:1784), `sess.Close()` (:1834), `disposeWorktree` (:1837, deletes a *pristine* worktree even on plain Kill), `os.RemoveAll(sessions/<id>)` (:1840), `pruneTombstones` (:1854).
- `internal/registry/projects.go` — `ReclaimOrphanWorktrees` (:153), `worktreeClaimed` (:237) → `tombstoneClaimsWorktree`; `KillProject` (:330) kills live sessions only.
- `internal/wire/frame.go` frames 0x1c–0x1f (RestoreSession/ListClosed/Closed/SessionRestored), registration lists (:274, :297), names (:368); `TestEveryFrameClassified` (`plugins_test.go:51`). Payloads in `wire/control.go` (:424-476). Event names in `wire/client.go:128`.
- `internal/daemon/daemon.go` handlers: FrameKillSession (:1516), FrameListClosed (:1534), FrameRestoreSession (:1642, under `d.runOp`).
- `internal/plugin/limiter.go:34` (`FrameCost`) and `docs/plugins.md` (~:258, enforced by `internal/plugin/doc_test.go`) must list every new frame.
- Clients: `cmd/hivegui/app_calls.go` (:639/:667/:686), `cmd/hived-ws-bridge/main.go` (:351-380), `internal/wire/testclient/client.go` (:134-153); TS: `src/bridge.ts`, `test/e2e/wails-mock.ts`, `test/e2e-real/wails-bridge.ts`, dom-test bridge `vi.mock` stubs.
- GUI reopen flow to mirror: `src/app/commands.ts` (:178), `src/app/undo-close.ts`, `src/app/events.ts:961`. Modal pattern: `src/app/modals/message-session.ts` + `src/components/modals/MessageSession.tsx`, `IdeaInbox.tsx`, registered in `App.tsx`, `main.tsx`, `store.ts` (`ModalId`), `key-scopes.ts` (`modal(...)`, `KEY_SCOPES`). Close sites for an Archive item: `Sidebar.tsx:141-161`, `commands.ts:172`.
- `internal/buildinfo/contract.go:249` `DaemonContract = 28`.

### Constraints / risks

- Restore keeps the same session id (UUID); no collision risk. Unarchive must discard the archive record before `Revive`, as `Restore` does (:402).
- Codex/Copilot capture `AgentSessionID` up to 30s after spawn; archiving earlier yields `ConversationLost` (already reported by restore). Claude/Gemini/Pi pin the id at create.
- Plain `Kill` deletes a *pristine* worktree via `disposeWorktree`; archive must skip `disposeWorktree` entirely or criterion 4 fails for clean worktrees.
- The worktree browser's `RemoveWorktree` can delete a worktree an archive record points at; unarchive then recreates it from the branch (`WorktreeRecreated`/`WorktreeLost`, already reported).
- An old daemon receiving an unknown `KillSessionReq` flag would silently *close* instead of archive; this is why archive uses new frames, not a flag (see Decision log).
- Close and #304 must stay untouched: the existing `closed_test.go` suite is the regression guard.

### Prior lessons

None matched in the hive brain.

### Conventions card (from AGENTS.md)

- Build `./build.sh`; tests `scripts/test.sh [go|unit|dom|e2e]`; Go checks under `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`; `staticcheck` + `go vet` for each of darwin/linux/windows; `scripts/ui-lint.sh`; `biome ci .`; `npm run typecheck` (needs `./scripts/ci-bootstrap.sh` in a fresh worktree). Verify by exit code.
- Wire JSON `snake_case`; JS readers `snake_case ?? camelCase`. New frame → update all 3 clients + testclient; do NOT bump `wire.PROTOCOL_VERSION`; DO bump `buildinfo.DaemonContract` (+ history entry; `scripts/check-daemon-contract.sh <base> <head>`).
- No hard-coded key labels; palette-only commands need no binding. New key scope needs a fixture in `every-shortcut.spec.ts` `FIXTURES`.
- Shell out only via `internal/proc`. GUI never opens a PTY. Registry is the only writer of state (atomic temp+rename).
- Docs: `DESIGN.md` persistence paragraph (:51), `README.md`, `docs/plugins.md`, `site/features.json` (`since: "Unreleased"`), `.changesets/518-*.md` (`type: added`, `bump: minor`); never edit `CHANGELOG.md`.
- Tests ship with the change (TDD); validate CSS/layout in a real browser (Playwright, `CI=1`).

## Approach

**Separate durable store, same record shape, new frames.** Archived records live in `<stateDir>/archived/<id>.json` using the existing `Tombstone` shape (plus `ArchivedAt`), written under `r.mu` in the same slot as the tombstone, before any teardown. `pruneTombstones` never sees the dir, so records are permanent (criterion 3) and Close/⌘Z are byte-identical (criterion 11). Unarchive is `Restore` parameterized by source dir, so name/colour/project/agent/worktree/conversation and the degraded-report behaviour (criteria 7-8) are inherited, not reimplemented.

Chosen over: (a) an `Archive` flag on `KillSessionReq` — an older daemon would ignore it and silently *close*; new frames fail loudly instead; (b) extending tombstone retention — conflates ephemeral undo with durable keep and changes Close behaviour (violates a non-goal).

Archive path in `registry`: new `Archive(id)` calling `kill` with an `archive` mode that (1) skips the dirty guard and `PhaseChecking` (`kill(id, force=true, removeWorktree=false)` already skips the guard at :1741, so the mode differs only in the items below), (2) writes the archive record instead of a tombstone, (3) skips `disposeWorktree`, (4) skips `pruneTombstones`.

**Write-failure is fatal for archive.** `writeTombstoneLocked` only logs on failure and `kill` continues; archive must NOT inherit that. `Archive` first refuses a parked or still-creating session (`kill` runs `dropParked`/`discardWorktree` and clears `pendingChoice` at registry.go:1680-1698 *before* the record slot, so abort would not be side-effect free). The archive record is then written *before* any teardown by a new `writeArchiveLocked` that returns its error and uses a new durable writer: write temp file, `Sync` the file, rename, `Sync` the directory (existing `writeAtomic`, persist.go:99-108, does not fsync, so it is not reused). A write error returns from `Archive` with the session still running and registered (no entry delete, no index persist, no process kill).

**Project name snapshot.** The record and `ArchivedSessionInfo` carry `project_name` (snapshotted at archive time) alongside `project_id`, because `KillProject` deletes projects without touching archived records; the modal groups by the snapshotted name. Unarchive into a deleted project falls back to the default project (`ProjectReassigned`, inherited from `Restore`).

**Conversation-expired detection.** After an archive lasting weeks the agent CLI may have pruned the transcript. This check lives in `Unarchive` only, *not* the shared `Restore` path, so ⌘Z's `ConversationLost` reporting is unchanged (criterion 11). `Unarchive` sets `ConversationLost` when the agent defines `TranscriptPaths` (agent.go:77; claude.go:107, pi.go:187) and it yields no existing file for the stored id, resolved against the cwd `Revive` will use (worktree if present, else project cwd; registry.go:1550-1555, since Claude keys transcripts by cwd). It goes through the existing `takeoverTranscriptExists` seam (acp.go:874) so tests never touch `$HOME`. A missing transcript is safe: `ResumeArgs` already falls back to a fresh session. Agents without `TranscriptPaths` keep today's behaviour.

**Lock order:** `archiveMu` > `gitMu` > `r.mu`; `archiveMu` is never taken under `r.mu`, and is released before `Revive` spawns the agent.

**Unarchive/Delete serialization.** A registry `archiveMu` is held across Unarchive's (read record → claim check → entry insert → discard record) and DeleteArchived's (read record → claim check → optional worktree removal → discard record). `d.runOp` only spawns a goroutine (daemon.go:180) and serializes nothing, so the lock lives in the registry. Whichever op takes the lock second finds the record gone (Unarchive → `no_such_archived_session`) or the session live (Delete's claim check keeps the worktree).

**Empty id.** Archive/unarchive/delete handlers do NOT inherit ⌘Z's "empty id means latest" resolution (undo-close.ts:150-156, resolved daemon-side before `Restore`); `validSessionID("")` rejects and the handlers return `bad_request`.

**Boot consistency.** `ListArchived` skips records whose id is a live session (crash between record write and index persist, or between unarchive's persist and discard), and boot discards such stale records.

**Banner/wording.** `onSessionRestored` (undo-close.ts:164-211) takes a subject parameter ("reopened" vs "unarchived") and unarchive uses its own banner id so a pending ⌘Z Undo button from a recent close is not hidden.

**Send state.** `kill` drops #338 send state (`dropSendStateLocked`); archiving a session with queued messages drops them, same as Close. Documented in Notes below; acceptable for v1.

**Worktree browser.** `RemoveWorktree` consults `worktreeClaimed` (now archive-aware) and refuses with a clear "held by archived session <name>" error instead of relying on unarchive recreating it. Worktree reclaim: `worktreeClaimed` also consults archived records. Delete-archived (v1, criterion 10): discards the record. Delete dialog options, each with its key shown: **Keep worktree** (`k`, default; record only), **Remove worktree** (`r`; only if managed and unclaimed by live sessions/other records; refused when dirty), **Remove worktree even if dirty** (`d`, danger styling; requires `force`; no recovery patch — it would be orphaned, since `patchPath` lives under `closed/` and nothing surfaces it — the danger styling and explicit key are the protection, same as #304's "Close and delete worktree" confirm copy naming the uncommitted file count), **Cancel** (`esc`).

GUI: palette commands **Archive Session** (acts on the active session; also an Archive item beside Close in the sidebar session menu) and **Archived Sessions…** (opens the modal). No default keybinding (keeps keymap policy surface minimal; user can rebind later). Modal: searchable list (name/project filter) grouped by project, rows show name, agent, project, archive date; actions Unarchive (Enter) and Delete (confirm overlay with the three-way worktree choice, showing keys). Unarchive reuses the `session:restored` handler and its degraded-report text.

### Files to change

1. `internal/wire/frame.go` — frames `ARCHIVE_SESSION`, `LIST_ARCHIVED`, `ARCHIVED`, `UNARCHIVE_SESSION`, `DELETE_ARCHIVED`; register in request/event lists and names. `internal/wire/control.go` — `ArchiveSessionReq`, `ListArchivedReq`, `ArchivedResp`, `ArchivedSessionInfo`, `UnarchiveSessionReq`, `DeleteArchivedReq`. `internal/wire/client.go` — event name `archived:list`.
2. `internal/registry/closed.go` — parameterize tombstone helpers by dir; add `archivedDir`, `ArchivedSession` writes/reads/list/discard; `Restore` gains a source; `tombstoneClaimsWorktree` (or sibling) also scans `archived/`. Likely split archive-specific code into new `internal/registry/archive.go`.
3. `internal/registry/registry.go` — `kill` archive mode; `Archive`, `ListArchived`, `Unarchive`, `DeleteArchived`.
4. `internal/registry/projects.go` — `worktreeClaimed` includes archived claims.
5. `internal/daemon/daemon.go` — handlers (archive under `runOp`; list answers in place; unarchive replies `FrameSessionRestored`; delete replies the refreshed `ARCHIVED` list).
6. `internal/plugin/limiter.go`, `docs/plugins.md` — frame costs / parity table.
7. `internal/buildinfo/contract.go` — bump `DaemonContract` 28→29 with history entry.
8. `cmd/hivegui/app_calls.go`, `cmd/hived-ws-bridge/main.go`, `internal/wire/testclient/client.go` — `ArchiveSession`, `ListArchivedSessions`, `UnarchiveSession`, `DeleteArchivedSession`.
9. Frontend: `src/bridge.ts`, `src/app/commands.ts` (+ palette titles; `commands.test.ts` id list), `src/app/archive.ts` (new logic), `src/app/modals/archived-sessions.ts`, `src/components/modals/ArchivedSessions.tsx`, `src/components/App.tsx`, `src/main.tsx`, `index.html` host div, `src/store/store.ts` (`ModalId`, archived slice), `src/app/key-scopes.ts` (modal scope), `src/app/events.ts` (`archived:list`, reuse `session:restored`), `src/components/Sidebar.tsx` (Archive item), `src/lib/shortcuts.ts` only if a help row is warranted.
10. Test mocks: `test/e2e/wails-mock.ts`, `test/e2e-real/wails-bridge.ts`, dom `vi.mock` bridge stubs.
11. Docs: `DESIGN.md:51`, `README.md`, `site/features.json` (entry, `since: "Unreleased"`), `.changesets/518-archive-sessions.md`.

### New files

- `internal/registry/archive.go`, `internal/registry/archive_test.go` — archive store + registry ops + tests.
- `cmd/hivegui/frontend/src/app/archive.ts`, `src/app/modals/archived-sessions.ts`, `src/components/modals/ArchivedSessions.tsx` — GUI.
- `cmd/hivegui/frontend/test/dom/archived-sessions.test.tsx`, `test/e2e/archive.spec.ts`, `test/e2e-real/archive.spec.ts`.

### Tests

Registry (`internal/registry/archive_test.go`):
- `TestArchiveWritesRecordBeforeTeardown` (record exists and survives reopening the registry; criterion 1)
- `TestArchiveRecordWriteFailureLeavesSessionRunning` (make `archived/` unwritable → `Archive` errors; session still registered, process alive, worktree untouched), `TestArchiveRefusesParkedSession` (parked/creating session: refused with no state change), `TestWriteArchiveRecordSyncsFileAndDir` (injected sync seam observed in order: file, rename, dir)
- `TestArchiveSnapshotsProjectName`, `TestUnarchiveAfterProjectDeletedFallsBackToDefault`
- `TestArchiveRemovesSessionAndKillsProcess` (criterion 2)
- `TestArchiveNeverPruned` (21 records / aged >7d all survive `pruneTombstones`, and a Close made *after* archiving (which runs prune) leaves `archived/` untouched; criterion 3)
- `TestArchiveKeepsPristineWorktree` (a clean managed worktree still exists after archive; fails if archive is wired through plain `Kill`, which disposes pristine worktrees), `TestArchiveKeepsDirtyWorktreeWithoutGuard` (`Archive` returns nil, not `ErrWorktreeDirty`; the dirty file's bytes are unchanged; criteria 4-5)
- `TestOrphanReclaimSkipsArchivedWorktree` (mirrors `TestOrphanReclaimSkipsTombstonedWorktree`)
- `TestUnarchiveRebuildsEntry`, `TestUnarchiveRevivesConversation`, `TestUnarchiveDropsRecord`, `TestUnarchiveTwiceSecondFails` (criteria 7, 9)
- `TestUnarchiveWorktreeGoneReportsDegraded`, `TestUnarchiveConversationLostReported` (empty id), `TestUnarchiveConversationFileGoneReportsLost` (stored id but transcript pruned; criterion 8)
- `TestUnarchiveDeleteArchivedRace` (`-race`; concurrent Unarchive + DeleteArchived with `remove_worktree`: the worktree of a live session is never deleted)
- `TestListArchivedSkipsLiveIds`, `TestRemoveWorktreeRefusedWhenArchivedRecordHoldsIt`
- `TestListArchivedReturnsAllNewestFirst` (criterion 6, server side)
- `TestDeleteArchivedDiscardsRecord`, `TestDeleteArchivedRemovesManagedWorktreeWhenAsked`, `TestDeleteArchivedRefusesDirtyWorktreeWithoutForce`, `TestDeleteArchivedKeepsSharedWorktree` (criterion 10)
- `TestArchivedRestoreRejectsTraversalIds`
- Regression: existing `closed_test.go` runs unmodified (criterion 11); add `TestKillStillWritesTombstoneNotArchive`.

Wire/daemon: `TestEveryFrameClassified` (existing), `TestControlFrameArchiveBadPayload`, `TestControlFrameUnarchiveEmptyIdRejected` (no "latest" resolution), `TestControlFrameListArchivedAnswersInPlace`, plugin `doc_test.go` parity, `check-daemon-contract.sh`, testclient round-trip `TestArchiveUnarchiveOverWire` in `internal/daemon`.

Bridge: `cmd/hivegui/app_calls_test.go` method-table rows + `TestListArchivedSessionsCall`.

Frontend (criterion 5 GUI half): e2e asserts the Archive action opens no confirm overlay even for a session with a dirty worktree. Dom `archived-sessions.test.tsx` (filter by name/project, group by project, Unarchive/Delete, empty state, degraded report); `commands.test.ts` ids; `key-scopes.test.ts` scope id; e2e `archive.spec.ts` (mock bridge: archive from sidebar + palette, list shows all, filter, unarchive restores tile, delete confirm shows keys); `every-shortcut.spec.ts` FIXTURES entry for the new scope; e2e-real `archive.spec.ts` (real hived: archive → process gone → worktree on disk → unarchive), isolated via `HIVE_SOCKET`/`HIVE_STATE_DIR`.

## Verification

```
export GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)
go test -race ./internal/agent/... ./internal/registry/... ./internal/daemon/... ./internal/wire/... ./internal/plugin/... ./cmd/hivegui/... >/dev/null && echo GO-OK
for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os go vet ./... && echo LINT-$os-OK || exit 1; done
scripts/check-daemon-contract.sh origin/main HEAD >/dev/null && echo CONTRACT-OK
scripts/check-changeset.sh >/dev/null && echo CHANGESET-OK
scripts/test.sh unit dom >/dev/null && echo FE-OK
CI=1 scripts/test.sh e2e >/dev/null && echo E2E-OK
scripts/check-test-hooks-stripped.sh >/dev/null && echo HOOKS-OK
(cd cmd/hivegui/frontend && npm run typecheck >/dev/null && echo TSC-OK)
(cd cmd/hivegui/frontend && npx biome ci . >/dev/null && echo BIOME-OK)
(cd cmd/hivegui/frontend && CI=1 npm run test:e2e:real -- archive >/dev/null && echo REAL-OK)
scripts/ui-lint.sh >/dev/null && echo UI-OK
```
Each line must print its own `*-OK`; a missing line is a failure.

Plus a throwaway Playwright pass against `wails dev` (`docs/verifying-the-gui-by-hand.md`) to see the modal in a real browser and `elementFromPoint`-check it is clickable (z-index/layout).

## Decision log

- **2026-10-15** — Separate `archived/` dir + new frames, not a flag or longer tombstone retention. Why: an old daemon would silently close on an unknown flag; retention changes conflate undo with keep and alter Close (spec non-goal).
- **2026-10-15** — Archive skips `disposeWorktree` entirely. Why: plain Kill deletes pristine worktrees; spec requires the worktree always stays.
- **2026-10-15** — No default keybinding; palette + sidebar item only. Why: keeps keymap-policy surface (menu accel, README table, help rows, RESERVED_CHORDS) out of v1; rebindable later.
- **2026-10-15** — Delete-archived ships in v1 (spec criterion 10, lean from the brainstorm). Why: it is the only way to clear the list.
- **2026-10-15** — Second-opinion round 1 (revise, 7/10): archive write failure aborts before teardown; project name snapshotted; conversation-expiry detected via `TranscriptPaths`; `archiveMu` serializes unarchive/delete; empty id rejected; verification made fail-able. Why: data-loss and race findings were correctness issues.
- **2026-10-15** — Archive has no confirm dialog. Why: non-destructive; worktree kept; unarchive is one step.

- **2026-10-15** — Archive surface is the command palette only; no sidebar item. Why: the session row has no context menu and an inline icon needs a new sprite icon plus hover-row layout work; the palette satisfies the spec's criteria. Revisit if users ask for a row action.
- **2026-10-15** — Delete uses the existing choice dialog (Cancel / Delete keep worktree / Delete and remove worktree), then a second dialog on `archived_worktree_dirty` before forcing; not the k/r/d keys drafted in the plan. Why: reuses the shipped confirm primitive that already shows confirm/cancel keys. The dirty refusal has its own error code because `worktree_dirty` triggers the close flow in events.ts.
- **2026-10-15** — `ListArchived` filters live ids; no boot-time discard of stale records. Why: the filter alone prevents double listing and a discard-on-boot adds a writer for no extra safety.
- **2026-10-15** — Unarchive reports on the status line (not the `undo-close` banner slot). Why: no new banner slot needed, and a pending ⌘Z Undo button is never hidden.

## Progress

- **2026-10-15** — Research complete; plan drafted.
- **2026-10-15** — Implemented: registry archive store + `Archive`/`Unarchive`/`DeleteArchived`, wire frames 0x48-0x4c, daemon handlers, GUI/ws-bridge/testclient calls, SDK frame table, `DaemonContract` 29, palette commands + modal, mocks, tests, docs, changeset.

## Open questions

- None blocking.

## Notes

- Archiving a session with queued #338 messages drops them, same as Close.

## Second opinion

Round 1 (independent reviewer): **revise**, confidence 7. Must-fix: (1) write failure data loss, (2) expired-conversation detection, (3) unarchive/delete race, (4) project-name loss, (5) criterion-5 test asserted the wrong thing, (6) vacuous verification loop, (7) empty-id handling. All applied. Nice-to-haves applied: stale-record boot filter, banner wording/id, RemoveWorktree claim check, send-state note, prune-after-close assertion, delete-dialog options. Round 2: **revise**, confidence 7 — must-fix: abort side effects before record slot (parked sessions), false fsync claim, orphaned forced-delete patch; all applied (no third round per loop rules). Nice-to-haves applied: Unarchive-only transcript check, lock order, e2e no-confirm assertion, `./internal/agent/...` in test run.
