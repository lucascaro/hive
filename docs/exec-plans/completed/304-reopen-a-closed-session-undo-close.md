# Reopen a closed session (undo close)

- **Spec:** [docs/product-specs/304-reopen-a-closed-session-undo-close.md](../../product-specs/304-reopen-a-closed-session-undo-close.md)
- **Issue:** #304
- **PR:** #306
- **Branch:** feature/304-reopen-closed-session
- **Status:** completed

## Summary

Make a session close recoverable. `registry.kill()` writes a small tombstone to
`<stateDir>/closed/<id>.json` immediately before the teardown; a new
`RESTORE_SESSION` wire frame rebuilds the entry from it and revives the agent by
its pinned `agent_session_id`. The GUI offers an undo banner at the moment of
the close and `⌘Z` / File ▸ Reopen Closed Session for later recall. Restore
reports what it could not bring back instead of claiming a clean undo.

## Research

Authored via plan-first mode; findings below come from reading the close path
end to end during plan iteration.

**Relevant code**

- `internal/registry/registry.go:772` — `kill(id, force, removeWorktree)`. The
  ordering is deliberate and documented in place: dirty pre-flight → delete
  entry + reindex + persist index → `sess.Close()` (PTY dies) →
  `disposeWorktree()` → `os.RemoveAll(<stateDir>/sessions/<id>)` → broadcast
  `session:removed` plus refreshed survivors. The tombstone write goes in front
  of all of it.
- `internal/registry/registry.go:900` — `disposeWorktree()`. Every branch is a
  refusal except two: an explicit remove request, and a worktree holding
  nothing. Notably it refuses to delete anything not `worktree.IsManaged()`.
- `internal/registry/persist.go:11` — `MetaFile` is the whole persisted entry:
  id, name, color, order, created, agent, project_id, worktree_path,
  worktree_branch, `agent_session_id`. Small enough to copy verbatim into a
  tombstone.
- `internal/registry/registry.go:532` — `Revive()`. Re-resolves the agent
  command (so a moved binary is picked up), prefers the worktree path as cwd,
  self-heals a vanished worktree, and resumes by `agent_session_id`. **This is
  why the feature is feasible at all** — restore is mostly "rebuild the entry,
  then call Revive".
- `internal/worktree/worktree.go:193` — `Cleanup()` removes the directory and
  prunes git admin state but **never deletes the branch**. A pruned worktree is
  therefore recreatable via `git worktree add <path> <branch>`; committed work
  survives a close, uncommitted work does not.
- `internal/registry/projects.go:142` — `ReclaimOrphanWorktrees()` deletes
  pristine *unclaimed* worktrees at boot. A tombstoned worktree is unclaimed by
  definition; see the edge case below.
- `internal/registry/create.go:341` — `insertEntry()`. Restore reuses this
  shape rather than growing a parallel ordering/persist/broadcast path.
- `cmd/hivegui/frontend/src/ui/banner.ts` — the banner primitive already takes
  labelled actions and a dismiss; `src/app/banners.ts:425` owns the notice row.
- `cmd/hivegui/frontend/src/app/events.ts:660` — the three-way dirty-worktree
  dialog (Cancel / Close session / Close and delete worktree).

**Audit: does closing a dirty worktree already prompt?** Yes, for every live
session, across all five close call sites:

| Call site | force | Prompts? |
|---|---|---|
| `keyboard.ts:357` — ⌘W | false | yes |
| `keyboard.ts:666` — File ▸ Close Session | false | yes |
| `main.ts:134` — command palette | false | yes |
| `sidebar.ts:432` — sidebar kill, live branch | false | yes (after its own `Confirm`) |
| `sidebar.ts:424` / `session-term.ts:1623` — *dead* session | true | no prompt |

The two `force=true` sites are dead sessions only — no process left to guard.
They skip the dirty *check*, but still call `kill(…, removeWorktree=false)`, and
`disposeWorktree` independently refuses to delete a non-pristine worktree
(`registry.go:915`). So a dirty worktree survives even there and reappears in
the worktree browser. **No close path can destroy uncommitted work without the
user explicitly picking the danger choice.** That floor stays; this change does
not touch it.

**Consequence for framing:** the accidental close — the case this feature
exists for — is by construction the non-destructive one, because deleting a
worktree takes a deliberate second click. The common undo is a clean undo.

**What is irreversible**

| Destroyed at close | Comes back? | Why |
|---|---|---|
| PTY child process | new one | Restore spawns a fresh process, not the same pid. |
| Terminal scrollback | **no** | In-memory ring plus the session dir are both gone; disk-backed scrollback does not exist. |
| Agent conversation | usually | Replayed by the agent's own resume when `agent_session_id` is set. A plain shell comes back blank. |
| In-flight agent state never written to its rollout file | **no** | It only lived in the killed process. |
| Worktree uncommitted changes, via *Close and delete worktree* | via patch | Was unrecoverable; the capped patch dump below changes that. Still requires the explicit danger choice. |
| Worktree unpushed commits | yes | The branch survives `Cleanup`; only the checkout dir went away. |
| Pristine worktree directory | yes | Recreatable from the branch. |
| Name / colour / project / agent binding | yes | All in the tombstone. |

## Approach

**Tombstone-on-kill, not deferred teardown.** The obvious cheap design is to
hold the teardown for ~15s behind a banner. Rejected: it keeps alive a process
the user asked to end, blocks worktree cleanup for the delay, has nothing to
offer after a daemon restart, and turns every close into a latency question.

Instead, immediately before the teardown, `kill()` writes:

```go
type Tombstone struct {
    Meta            MetaFile  `json:"meta"`
    ClosedAt        time.Time `json:"closed_at"`
    // The close ASKED to delete the worktree; disposeWorktree can
    // still refuse. Restore probes the filesystem rather than trusting
    // this, so it is a diagnostic, not a decision input.
    WorktreeRemoveRequested bool   `json:"worktree_remove_requested,omitempty"`
    WorktreeShared          bool   `json:"worktree_shared,omitempty"`
    PatchPath               string `json:"patch_path,omitempty"`
    PatchSkipped            bool   `json:"patch_skipped,omitempty"`
}
```

Written through the same atomic temp+rename helper as every other registry
write; the registry stays the only writer under `StateDir()`. Pruned on each
write by **both** bounds — last 20 records and 7 days, whichever prunes first.
Metadata only, under 1 KB each. It survives daemon restarts, costs the close
path one small file write, needs no timers, and gives "reopen the thing I closed
ten minutes ago" for free.

**Recovery patch.** On the destructive branch only (`removeWorktree=true`),
before `worktree.Cleanup` runs, dump the worktree's uncommitted state to
`<stateDir>/closed/<id>.patch`: `git diff HEAD` plus untracked files
(`git ls-files -o --exclude-standard` fed through `git diff --no-index /dev/null`).
Capped at 10 MiB; above the cap, skip and set `patch_skipped` so restore says so
rather than implying a patch exists. Best-effort — a failure logs and never
blocks the close the user asked for.

Deliberately **not** `git stash`: the stash stack is shared by every worktree of
the repo, and silently pushing onto a user's stack is a surprising side effect
that another tool (or another Hive session) can pop. A file under the state dir
is inert.

Restore does **not** auto-apply the patch. It recreates the worktree from the
branch and reports the patch path, leaving `git apply` to the user — auto-applying
into a checkout whose HEAD may have moved turns one bad close into a merge
conflict.

**Restore flow.** `registry.Restore(id, opts) (*Entry, RestoreResult, error)` — `opts` is the same `session.Options` the daemon hands `Revive` on boot. The
result carries what was degraded so the GUI can say so:

1. Read the tombstone. Missing → `ErrNotFound`.
2. Id already live → `ErrExists` (defensive; ids are unique).
3. **Project** gone → restore into the default project, flag `ProjectReassigned`.
4. **Worktree**, under `gitMu`:
   - path on disk and hive-managed → adopt as-is. Claimed by a sibling is fine
     and expected — duplicate sessions (⌘P) legitimately share one worktree.
   - path gone, branch exists → `git worktree add <path> <branch>`, flag
     `WorktreeRecreated`. Uncommitted work is *not* back.
   - path gone, branch gone → no worktree, cwd = project cwd, flag `WorktreeLost`.
   - path exists but is *not* hive-managed → adopt read-only as cwd, never
     recreate. Same paranoia as `disposeWorktree`.
5. **Recovery patch**: if the tombstone names one, surface `PatchPath`, or flag
   `PatchSkipped` when it exceeded the cap. Never auto-applied.
6. **Agent** unresolvable (a custom agent since deleted) → `Revive` already
   falls back to a shell; flag `AgentFellBack`.
7. Insert the entry at the **end** of the order, reindex, persist index.
   Appending never surprises; restoring into the original slot was considered
   and dropped.
8. `Revive(id, opts)` — resumes by `agent_session_id` when present, flag
   `ConversationLost` when not.
9. Delete the tombstone, broadcast `session:added` plus refreshed survivors
   (same stale-snapshot reasoning as `kill`'s tail).

**UI.** Two affordances, no new modal.

*Undo banner* — a third `banner()` instance driven from a new
`src/app/undo-close.ts`. Shown on `session:removed` **only when this client
initiated the close**, which means tracking locally-issued close ids at call
time rather than reacting to every removal event. Auto-hides after 15s; the
tombstone outlives it. Copy is outcome-specific: `Closed "api-refactor". [Undo]`
for the ordinary path, `Closed "api-refactor" and deleted its worktree. [Reopen]
— changes recoverable from a saved patch` for the destructive one. After restore
returns, the text is replaced with what was actually degraded rather than a
silent success.

*Reopen last closed* — **`⌘Z`**. `⇧⌘T` was the first choice and is taken (New
Session in Worktree, `menu_darwin.go:38`). The keymap was audited: `⌘Z` is
unbound in both `keyboard.ts` and `menu_darwin.go`, Hive has no Edit menu so
there is no stock Undo item, and `⌘` is not a terminal modifier — xterm.js was
never going to deliver it to the agent, so nothing is taken away. An accidental
close is exactly the moment a hand reaches for `⌘Z`. Alternatives considered:
`⇧⌘Z` (conventionally redo), `⌘U` (weak mnemonic), `⌘R` (better saved for
Restart Session, which has no accelerator today), and no global key at all
(cheapest, but fails the "closed it ten minutes ago" case).

No "recently closed" browser UI in this change — the wire call returns a list,
so one can be added later without another protocol change.

**Edge cases**

1. **Daemon restart between close and undo.** Tombstones are on disk, so `⌘Z`
   still works; only the banner is lost. Main reason deferred kill was rejected.
2. **Boot orphan-reclaim races the undo window.** `ReclaimOrphanWorktrees`
   deletes pristine, unclaimed worktrees at startup, and a tombstoned worktree is
   exactly "unclaimed". Narrow (a pristine worktree is normally already removed
   by the kill that wrote the tombstone), but if a kill was interrupted between
   steps, boot would silently delete something the user could still undo into.
   Fix: `worktreeClaimed()` also consults live tombstones. Three lines, closes a
   silent-data-loss path.
3. **Double undo.** The tombstone is read-and-deleted under the lock; a second
   call gets `ErrNotFound` and the banner just hides.
4. **Restore while a sibling occupies the worktree.** Adopt, do not refuse —
   that is the existing duplicate-session semantic.
5. **Project close** (`KillProject` with `killSessions`) writes N tombstones for
   free. Undoing a whole project as one unit is a non-goal.
6. **Force kill from a dead tile** (`session-term.ts:1623`) tombstones like any
   other close.
7. **State-dir growth.** Bounded ring, sub-KB records. The patch dump is the only
   thing that could get large, hence its cap.
8. **e2e isolation.** `<stateDir>/closed/` is under `HIVE_STATE_DIR`, so the
   real-e2e harness is already isolated. No new escape hatch.
9. **Agent binary moved** (nvm switch). `Revive` re-resolves the agent command by
   design; nothing extra needed.

### Files to change

- `internal/registry/registry.go` — `kill()` writes the tombstone before the
  teardown; `disposeWorktree` reports whether it removed the dir and triggers
  the patch dump on the destructive branch.
- `internal/registry/paths.go` — `ClosedDir(stateDir)`.
- `internal/registry/projects.go` — `worktreeClaimed()` consults tombstones.
- `internal/worktree/worktree.go` — `DumpPatch(root, path, out string, cap int64)`,
  beside `Cleanup` and `Inspect` where the other git shell-outs live.
- `internal/wire/control.go` — `RestoreSessionReq`, `ListClosedReq`,
  `ClosedResp`, `ClosedSessionInfo`, all with `json:"snake_case"` tags.
- `internal/wire/frame.go` — `FrameRestoreSession`, `FrameListClosed`,
  `FrameClosed`.
- `internal/daemon/daemon.go` — dispatch both request frames alongside
  `FrameKillSession`.
- `cmd/hivegui/app_calls.go` — `RestoreSession(id)`, `ListClosedSessions()`.
- `cmd/hived-ws-bridge/main.go` and `internal/wire/testclient` — the lock-step
  clients the wire-change pattern in `AGENTS.md` requires.
- `cmd/hivegui/frontend/src/bridge.ts` — export the two calls.
- `cmd/hivegui/frontend/src/app/events.ts` — hand `session:removed` off to
  `undo-close.ts`; reword the "cannot be undone" note now that a patch is saved
  (accurately, not reassuringly).
- `cmd/hivegui/frontend/src/lib/keymap.ts`, `src/app/keyboard.ts`,
  `cmd/hivegui/menu_darwin.go` (File ▸ Reopen Closed Session, `⌘Z`), the help
  overlay and the status-bar hints — the five surfaces the Keybindings Policy
  names.
- `cmd/hivegui/frontend/src/main.ts` — command-palette entry.
- `CHANGELOG.md` — `[Unreleased]` entry (user-visible).
- `DESIGN.md` — one line: the registry also owns `closed/`.

JS readers of the new payload use `snake_case ?? camelCase` at the boundary, per
the hard rule.

### New files

- `internal/registry/closed.go` — `Tombstone`, write/read/list/prune, `Restore`,
  `RestoreResult`, and the capped recovery-patch dump.
- `internal/registry/closed_test.go`
- `cmd/hivegui/frontend/src/app/undo-close.ts` — banner + `⌘Z` handler.
- `cmd/hivegui/frontend/test/dom/undo-close.test.ts`

### Tests

TDD per `AGENTS.md` — every behaviour ships with its test.

**Go — `internal/registry/closed_test.go`**

- `TestKillWritesTombstone` — record exists, fields match the entry.
- `TestRestoreRebuildsEntry` — name, colour, agent, project, agent_session_id.
- `TestRestoreReattachesSurvivingWorktree`
- `TestRestoreRecreatesWorktreeFromBranch` — dir gone, branch present, commits
  back, uncommitted not.
- `TestRestoreWithoutWorktreeWhenBranchGone` — flags `WorktreeLost`, cwd falls
  back to project.
- `TestRestoreRefusesNonHiveManagedRecreate`
- `TestRestoreMissingTombstoneNotFound` / `TestRestoreTwiceSecondFails`
- `TestRestoreIntoDefaultProjectWhenProjectGone`
- `TestRestoreAppendsToEndOfOrder`
- `TestTombstonePruneKeepsLastNAndSevenDays` — both bounds, whichever first.
- `TestKillDumpsPatchBeforeWorktreeDelete` / `TestPatchSkippedAboveCap` /
  `TestPatchDumpFailureDoesNotBlockClose`
- `TestOrphanReclaimSkipsTombstonedWorktree` — the silent-data-loss guard.

**Go — daemon / GUI**

- `cmd/hivegui/menu_darwin_test.go`: assert `⌘Z` is bound once and to Reopen
  Closed Session (the file already walks accelerators for exactly this kind of
  collision check).
- `internal/daemon/control_frame_test.go`: `TestRestoreSessionFrame`,
  `TestRestoreSessionUnknownIDSendsError`, `TestListClosedFrame`.
- `cmd/hivegui/app_calls_test.go`: `TestRestoreSessionCall`,
  `TestListClosedSessionsCall`.

**DOM — `test/dom/undo-close.test.ts`**

- `shows an undo banner after a locally initiated close`
- `labels a close-and-delete-worktree undo as unrecoverable`
- `reports degradation returned by restore instead of a bare success`
- `does not show a banner for a close initiated elsewhere`
- `surfaces the recovery patch path when the worktree was deleted`

**e2e-real**

- Close a session, press `⌘Z`, assert the tile returns with the same name and a
  live PTY. Isolated via `HIVE_SOCKET` + `HIVE_STATE_DIR` like the rest of the
  suite.

## Decision log

- **2026-08-31** — Tombstone-on-kill over deferred teardown. Why: survives
  daemon restarts, keeps no process the user asked to end, no timers, and
  enables later recall for free.
- **2026-08-31** — Write a capped 10 MiB recovery patch before the destructive
  worktree delete. Why: it is the only genuinely unrecoverable case, and ~30
  lines converts it. Not `git stash` — the stash stack is shared across every
  worktree of the repo and polluting it is a surprising side effect.
- **2026-08-31** — Restore never auto-applies the patch. Why: applying into a
  checkout whose HEAD may have moved turns one bad close into a merge conflict.
- **2026-08-31** — Restore appends to the end of the session order rather than
  the original slot. Why: simpler, and never surprises.
- **2026-08-31** — Tombstone retention bounded by both count (20) and age (7
  days), whichever prunes first.
- **2026-08-31** — Undo banner only for closes this client initiated. Why: undo
  belongs to whoever pressed close.
- **2026-08-31** — Keybinding is `⌘Z`, not `⇧⌘T` (taken by New Session in
  Worktree). Why: unbound at both the app and terminal layers, and it is the
  reflex reached for after an accidental close.
- **2026-08-31** — `RESTORE_SESSION` with an empty id means "the most recently
  closed one", resolved daemon-side. Why: a client that lists and then restores
  can race the retention prune between the two calls.
- **2026-08-31** — The undo banner tracks locally-issued close ids in a new
  `closeActiveSession()` helper rather than reacting to `session:removed`. Why:
  a single entry point is the only way ⌘W, the File menu and the palette cannot
  drift into forgetting the note.
- **2026-08-31** — Dropped a speculative branch letting ⌘Z through the launcher
  guard. Why: it was written for a case that turned out not to occur (closing
  the last session does not auto-open the launcher), and unreachable code is
  not a safety net.

## Progress

- **2026-08-31** — Plan-first scaffold; stage = IMPLEMENT (set in spec
  frontmatter). Design approved via the plan-html review loop; irreversibility
  audit and all five open decisions resolved before scaffolding.
- **2026-08-31** — PR #306 opened; stage = REVIEW.
- **2026-08-31** — Implemented on `feature/304-reopen-closed-session`. Registry
  tombstones + `Restore`, `worktree.DumpPatch`, three new wire frames, daemon
  dispatch, GUI bindings, `undo-close.ts` with the banner and ⌘Z. Go suite,
  frontend unit/dom/e2e, tsc, biome and ui-lint all green.

## QA verdict

- **2026-08-31** — verdict: PASS; checks: 3 dimensions / 8 acceptance criteria / 5 non-goals passed, 0 failed, 0 followups; followups: none; one-line: undo-close delivers every success criterion, bleeds into no non-goal, and leaves every close confirmation byte-identical to main.
  - 2026-08-31 dimensions:
    - acceptance — PASS — all 8 criteria demonstrated by passing tests, not asserted from source; tombstone-before-teardown confirmed by ordering at registry.go:854.
    - non-goals — PASS — `kill()` dirty pre-flight and `disposeWorktree`'s Pristine()-gated branch byte-identical to main; no `git apply` exec anywhere; `KillProject` untouched.
    - doc accuracy — PASS — changeset, README keybinds row, help overlay, command palette and DESIGN.md persistence section all updated for ⌘Z and `closed/`.

## PR convergence ledger

- **2026-08-31 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: c6de5a00…; threads_open: 0; action: stop; head_sha: d8e652b.
- **2026-08-31 post-loop** — 3 IMPORTANT findings applied by hand rather than left for the gate (boil-the-lake): wire-id path guard, ErrExists + traversal coverage, restored-session naming.

## Open questions

None. All five open decisions were resolved during plan review.
