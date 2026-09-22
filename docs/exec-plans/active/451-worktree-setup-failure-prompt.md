# Ask the user when worktree setup fails instead of silently using a stale ref

- **Spec:** [docs/product-specs/451-worktree-setup-failure-prompt.md](../../product-specs/451-worktree-setup-failure-prompt.md)
- **Design:** —
- **Issue:** #451
- **Branch:** `feature/451-worktree-setup-failure-prompt`
- **PR:** [#452](https://github.com/lucascaro/hive/pull/452)
- **Status:** active

## Summary

Turn the two silent worktree-setup fallbacks into a user decision. The
create parks, the GUI raises a three-button dialog, and the create
resumes from the answer. Parking is *resumable state*, not a blocked
goroutine: `finishCreate` returns, `{spec, plan}` is stashed on the
registry, and the resolve frame runs the tail. That is what lets the wait
be indefinite without pinning `gitMu` or a context.

## Research

Verified against this tree, not assumed:

- `internal/worktree/worktree.go:161` `upstreamBaseRef` — `git remote
  get-url origin` (3s), `git fetch --quiet origin` (10s), `symbolic-ref
  refs/remotes/origin/HEAD` (3s). The fetch failure at `:176` is one
  `log.Printf`, then it proceeds with the cached ref; an unresolvable
  `origin/HEAD` returns `""` and the caller branches from local HEAD.
- `internal/worktree/worktree.go:64` `CreateWorktree` — owns both the
  fetch *and* the base-ref choice, which is why the split below is
  needed. Existing-branch fast path at `:76-83` never consults upstream.
- `internal/registry/create.go:785` `materializeWorktree` — takes
  `gitMu` at `:800` and holds it across the whole `git worktree add`.
  Parking inside this lock would block every other create and kill.
- `internal/registry/create.go:25` `createPlan` — all plain strings, no
  handles. Together with `wire.CreateSpec` it is fully serializable,
  which is what makes resume-from-state cheap.
- `internal/registry/create.go:114` `finishCreate` — the tail:
  `materializeWorktree` → `renameAfterWorktreeFailure` →
  `PhaseSpawning` → `spawn`. Resume re-enters here.
- `internal/daemon/daemon.go:1262-1275` — `FrameCreateSession` is
  decoded and run under `d.runOp` off the read loop; only errors are
  written back. `wire.CreateSpec` documents fire-and-forget at
  `internal/wire/control.go:102`.
- `internal/wire/control.go:166` / `internal/wire/frame.go:153` —
  `PendingPrompt` + `FrameResolvePrompt` 0x29: a question parked as
  session state, answered by a client frame, with the daemon never
  blocking. The precedent this plan follows.
- `cmd/hivegui/frontend/src/app/modals/choice-dialog.ts:81`
  `openChoiceDialog` — `{label, value, danger?}` choices, Escape and
  scrim resolve to the *first* choice. Cancel must therefore be first.
- `cmd/hivegui/frontend/src/app/events.ts:912` — `worktree_dirty`
  already does daemon-error → choice dialog → reissue. Shape to copy for
  the handler, though this feature is event-driven rather than
  error-driven.
- `internal/registry/registry.go:708` `MarkPendingRevive` — the boot
  pre-mark. The restart-while-parked rule hooks in around here.

### Prior lessons

- Hive wire payloads are `snake_case` on the wire; JS readers use
  `snake_case ?? camelCase` at the boundary. Applies directly to
  `pending_worktree_choice`.
- CSS/layout changes are not validated by vitest. This feature adds no
  new component (it reuses `openChoiceDialog`), so the dom test is
  behavioral, not visual.
- `npm run typecheck` needs the generated wailsjs bindings — run
  `./scripts/ci-bootstrap.sh` in a fresh worktree before believing tsc
  errors in files this change never touched.
- Verify under the CI toolchain (`GOTOOLCHAIN=$(sed -n 's/^go //p' go.mod)`)
  and run `staticcheck` per-GOOS — a symbol used only from a
  `*_darwin.go` file reads as dead on Linux.

### Conventions card

From `AGENTS.md`:

- **Build:** `./build.sh` (macOS `.app`). Go-only sanity: `go build ./...`
- **Lint:** `go vet ./...` and `staticcheck ./...`, each per-GOOS
  (`darwin`, `linux`, `windows`); `scripts/ui-lint.sh` for the frontend
  token/icon rules.
- **Tests:** `scripts/test.sh [go|unit|dom|e2e]`
- **Everything:** `scripts/test.sh`

Conventions this feature touches:

- Wire JSON is `snake_case` on the wire, `CamelCase` in Go with
  explicit `json:` tags; a new frame updates all three clients in
  lock-step (`cmd/hivegui`, `cmd/hived-ws-bridge`, `internal/wire/testclient`).
- Daemon-side behaviour change ⇒ bump `buildinfo.DaemonContract`;
  `scripts/check-daemon-contract.sh <base> <head>` is the local gate.
  Do **not** bump `wire.PROTOCOL_VERSION` for a new frame.
- The GUI never opens a PTY; every PTY operation goes through the wire.
- TDD: the test that would have caught the regression ships with the
  change.
- User-visible change ⇒ `.changesets/<slug>.md` (`type: fixed`,
  `bump: patch`) **and** a `site/features.json` entry with
  `since: "Unreleased"`.
- `DESIGN.md` is updated when the wire protocol changes.

## Approach

### 1. `internal/worktree` — separate the fetch from the add

- `PrepareBase(ctx, repoDir) (base string, err error)` — today's
  `upstreamBaseRef` body, but returning a typed `*FetchError` carrying
  git's stderr, the cached tip SHA and its commit time, instead of
  logging and swallowing. An absent `origin` remote is *not* an error
  (there is no upstream to be stale against): it returns `("", nil)`.
- `CreateWorktreeAt(ctx, repoDir, branch, path, base string) error` —
  the add, with the base supplied. Keeps the **TOCTOU** retry
  (`worktree.go:104-113` — same branch, harmless). **Drops the no-base
  retry** (`worktree.go:117-126`): re-running the add without a base
  branches from local HEAD, which is exactly the silent wrong-base
  outcome this spec forbids. That path now returns the error so the
  caller parks as `create_failed`.
- `CreateWorktree` stays, implemented over the two, for callers that do
  not want to ask (`CreateWorktreeForBranch`, tests). It **loses** the
  no-base retry along with everyone else — state this explicitly rather
  than leaving it to be inferred. Safe for both other callers:
  `internal/registry/closed.go:454` and `internal/registry/worktrees.go:361`
  pass branches that already exist and so take the existing-branch fast
  path (`worktree.go:76-83`), which never used the retry. Only
  `create.go:804` ever reached it.
- **Existing test to update:** `internal/worktree/worktree_test.go:285-300`
  `TestCreateWorktree_AllAttemptsFailJoinsErrors` asserts the joined
  error contains `"(no base)"`. Dropping the retry breaks it; budget
  the edit.

### 2. `internal/registry` — parked, resumable create

- `materializeWorktree` splits into steps that each take `gitMu` only
  for their own subprocess: `PrepareBase`, release, then
  `CreateWorktreeAt`.
- On failure, stash `{spec, plan, kind}` in `r.parked map[string]*parkedCreate`
  (guarded by its own mutex), set `PendingWorktreeChoice` on the entry,
  broadcast `SESSION_EVENT(updated)`, and **return** — no goroutine, no
  lock, no context retained.
- `ResolveWorktreeChoice(ctx, id, choice) error` — pops the parked
  state and re-enters the create tail under a fresh daemon op context:
  - `retry` → re-run the failed step.
  - `cached` / `project_dir` → proceed with today's fallback,
    explicitly chosen.
  - `cancel` → discard the worktree if partly made, remove the entry,
    broadcast removal. No session is left behind.
- **Third silent fallback, for completeness:** `worktree.Root(p.cwd)`
  failing at `create.go:792-796` also clears the worktree fields and
  proceeds. It is near-unreachable (`planWorktreeAndName` already gated
  on `worktree.IsGitRepo`, `create.go:449`), but the spec says no
  silent fallback remains on any path — so it logs and parks as
  `create_failed` like the others.
- On `cancel` **and on a kill while parked**, drop the parked state and
  `discardWorktree`. `r.kill` (`registry.go:1362`) knows nothing about
  the parked map, and a parked entry carries no `WorktreePath` yet, so
  its own worktree cleanup does not fire — without this the `{spec,
  plan}` leaks and a partly-made worktree is orphaned. `finishCreate`
  already handles this shape of race at `create.go:139` and `:167`.
- `PendingWorktreeChoice` is cleared on resolve and on kill. It rides
  on `SessionInfo`, so a stale value would make a reconnecting GUI
  re-raise a dialog for a decision already made.
- **No control client connected** → no park: fail the create,
  `FrameError`, remove the entry. Mechanism: a `hasControlClient
  func() bool` field the **daemon** sets on the registry, counting
  `wire.ModeControl` connections only. `r.listeners` is *not* usable —
  `serveControl` subscribes `ModeSession` connections too
  (`daemon.go:888-912`), so an agent's own events socket would count as
  something that can answer the dialog, and it cannot. When the field
  is nil (bare `Registry` in unit tests) the default is *assume a
  client*, so park tests work without wiring; the no-client test
  injects a stub returning false.
- **Daemon restart while parked.** `Phase` is in-memory and
  `MetaFile` (`internal/registry/persist.go:13-32`) has no field that
  tells a never-spawned parked entry from an ordinary worktree-less
  session, so boot would revive it as a plain session — silently. Add
  one marker to `MetaFile` (`awaiting_worktree_choice`, omitempty),
  written on park and cleared on resolve. Boot marks a marked entry
  dead with a reason naming the interrupted worktree setup instead of
  reviving it, around `MarkPendingRevive` (`registry.go:708-718`),
  which today treats every restored entry as ready. The resume state
  itself is still *not* persisted — only the bit.

### 3. `internal/wire` — one new frame, one new field

- `PendingWorktreeChoice` on `SessionInfo`, `json:"pending_worktree_choice,omitempty"`:
  `{kind, message, cached_tip, cached_tip_age_secs, branch}`. `kind` is
  `fetch_failed` or `create_failed`.
- `FrameResolveWorktreeChoice` (next free type after 0x29) with
  `ResolveWorktreeChoiceReq{SessionID, Choice}`. First answer wins;
  resolving an unparked session is a no-op, not an error — two GUI
  windows can race.
- New phase constant for the parked state, so the phase checklist has
  something to render.

### 4. Clients

- `cmd/hivegui/app_calls.go` — `ResolveWorktreeChoice(id, choice)`.
- `cmd/hived-ws-bridge/main.go` and `internal/wire/testclient` — same,
  in lock-step per the wire hard rule.
- `frontend/src/app/events.ts` — on a session event carrying the
  pending choice, raise `openChoiceDialog` with Cancel first, then
  Retry, then the fallback labelled with the cached tip's age; send the
  answer. Read with `snake_case ?? camelCase`.
- `frontend/src/lib/phase-steps.ts` — label for the parked phase.

### Files to change

- `internal/worktree/worktree.go`
- `internal/registry/create.go`, `internal/registry/registry.go`
- `internal/wire/control.go`, `internal/wire/frame.go`
- `internal/daemon/daemon.go`
- `internal/buildinfo/contract.go` (bump)
- `cmd/hivegui/app_calls.go`, `cmd/hived-ws-bridge/main.go`,
  `internal/wire/testclient`
- `AGENTS.md` — fix the broken `GOTOOLCHAIN=$(…)` form (missing `go`
  prefix) in "Project-specific verification"
- `cmd/hivegui/frontend/src/app/events.ts`,
  `cmd/hivegui/frontend/src/lib/session-state.ts`,
  `cmd/hivegui/frontend/src/lib/phase-steps.ts` — not just a label: add
  the phase to `PHASE` (`:27-37`) and to `isStarting` (`:56-66`) or a
  new predicate, and decide its place in `CREATE_ORDER` (`:95`). An
  unknown phase makes `createChecklist` return null (`:150`) and
  `sessionState` (`session-state.ts:126`) lumps it in with "starting" —
  the opposite of the distinct state the spec asks for,
  `cmd/hivegui/frontend/src/bridge.ts` (re-export), and the Wails
  **mock** bridge the `e2e` layer drives — it needs a
  `ResolveWorktreeChoice` stub or the e2e suite breaks on the new call
- `DESIGN.md` (new frame), `.changesets/<slug>.md`, `site/features.json`

### Tests

`internal/worktree/worktree_test.go`:

- `TestPrepareBaseReturnsFetchErrorWithCachedTip` — temp repo whose
  `origin` points at an unreachable URL, with a cached
  `refs/remotes/origin/main`. Asserts a `*FetchError` carrying that
  exact SHA and a non-zero age.
- `TestCreateWorktreeAtDoesNotFallBackToHead` — the add fails with a
  base ref supplied; asserts the error propagates and that no branch
  was created from local HEAD. This is the regression test for the
  dropped no-base retry.
- `TestPrepareBaseNoOriginIsNotAnError` — repo with no remote returns
  `("", nil)`, so a remoteless project never prompts.
- `TestCreateWorktreeAtUsesSuppliedBase` — the new branch points at the
  given base ref, not at HEAD.

`internal/registry/create_test.go` (or a new `create_park_test.go`):

- `TestCreateParksOnFetchFailure` — entry exists, phase is the parked
  phase, `PendingWorktreeChoice.Kind == "fetch_failed"`, and no
  worktree is on disk.
- `TestResolveWorktreeChoiceCachedProceeds` — branches from the cached
  tip and reaches `PhaseReady`.
- `TestResolveWorktreeChoiceRetrySucceeds` — the fetch is made to
  succeed between park and resolve; the branch lands on the fresh tip.
- `TestResolveWorktreeChoiceCancelRemovesEntry` — no entry, no
  worktree, no branch afterwards.
- `TestParkedCreateDoesNotBlockOtherOperations` — the one that proves
  the resumable design: with one create parked, a second create and a
  kill both complete. Fails if `gitMu` is held across the park.
- `TestCreateFailsWithNoControlClient` — returns an error, leaves no
  entry, and does not park.
- `TestResolveUnparkedSessionIsNoop` — two racing resolves; the second
  is not an error.
- `TestKillWhileParkedCleansUp` — killing a parked session drops the
  parked map entry and discards any partly-made worktree.
- `TestParkedEntryMarkedDeadOnRevive` — an entry persisted with
  `awaiting_worktree_choice` comes back dead with the reason set, not
  revived as a plain session.
- `TestResolveClearsAwaitingMarker` — the marker is gone from disk
  after any resolve, so a later restart does not resurrect the dialog.

`internal/wire/control_test.go`:

- `TestPendingWorktreeChoiceRoundTrip` — asserts the wire keys are
  `snake_case` (`pending_worktree_choice`, `cached_tip_age_secs`).

`cmd/hivegui/frontend/test/dom/`:

- `worktree-choice-dialog.test.ts` — a session event carrying the
  pending choice raises the dialog with Cancel first; choosing a value
  calls `ResolveWorktreeChoice` with it; the cached-tip button label
  contains the age.

### Verification

Each command fails if the change is wrong, not merely absent:

```sh
# Go layer, under the toolchain CI pins.
# NOTE the `go` prefix: GOTOOLCHAIN=1.27.1 is rejected outright
# ("invalid GOTOOLCHAIN"). AGENTS.md carries the same broken form and
# is fixed in this PR.
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./internal/worktree/... ./internal/registry/... ./internal/wire/...

# Per-GOOS static analysis (a darwin-only symbol reads as dead on linux)
for os in darwin linux windows; do GOOS=$os go vet ./... && GOOS=$os staticcheck ./...; done

# Frontend
scripts/test.sh unit dom

# The daemon-contract gate that CI will run on this PR
scripts/check-daemon-contract.sh origin/main HEAD

# Full harness before the PR
scripts/test.sh
```

Manual smoke, per `docs/verifying-the-gui-by-hand.md` (no human eyes
needed — `wails dev` + a throwaway Playwright script): break the fetch
by pointing `origin` at an unreachable host in a scratch repo, create a
worktree session, and confirm the dialog appears, Retry re-fetches
after the host is restored, and Cancel leaves nothing behind.

## Second opinion

**Verdict:** `revise`, confidence 8. Reviewed the drafted plan against
the tree before the operator saw it.

Confirmed sound: `createPlan` + `wire.CreateSpec` really do capture
what resume needs (`cmd` is re-derived by `resolveAgentCmd`;
cols/rows/env/shell come from the spec; `ideaID` and `pendingPrompt`
ride on the surviving `Entry`). Cancel-before-spawn strands no idea,
because `linkIdeaToSession` only fires after spawn (`create.go:200`).

Five must-fix items, four applied:

1. **No-base retry was a surviving silent fallback**
   (`worktree.go:117-126`) — re-runs the add from local HEAD. Dropped;
   it now parks as `create_failed`. Regression test added.
2. **`hasControlClient` mechanism was hand-waved.** `r.listeners`
   cannot answer it: `serveControl` subscribes `ModeSession`
   connections too (`daemon.go:888-912`). Specified as a daemon-set
   callback counting `ModeControl` only, with a stated default.
3. **Kill-while-parked leaked** the parked state and orphaned a
   partly-made worktree. Cleanup and a test added.
4. **`GOTOOLCHAIN=$(sed …)` is invalid** — `go.mod` says `1.27.1` and
   go rejects `GOTOOLCHAIN="1.27.1"`. Verified by running it. Needs the
   `go` prefix; `AGENTS.md` has the same bug and is fixed in this PR.
5. **Boot-dead criterion is unreachable** as specced — deferred to the
   operator, see Open questions. Not applied unilaterally because the
   fix collides with a spec non-goal.

Nice-to-haves applied: the vacuous "no branch was created" assertion
was replaced; `phase-steps.ts` scope corrected (`PHASE`, `isStarting`,
`CREATE_ORDER`) and `session-state.ts` added to Files to change; the
`PendingWorktreeChoice`-cleared rule is now stated.

Noted and deliberately out of scope: `internal/registry/closed.go:454`
is a third `CreateWorktree` caller (restore-from-closed) that falls
back silently, but it surfaces `WorktreeLost` to the user, so it is not
one of the two silent paths this spec targets.

No injection attempts found in the spec or plan.

## Decision log

- **2026-09-22** — Park on the session (the `PendingPrompt` shape)
  rather than pre-flighting from the GUI before `CreateSession`. Why:
  the decision is made at the exact moment of branching, with no window
  between probe and use. Cost accepted: a half-created session is
  visible while the user decides, which the user wants anyway.
- **2026-09-22** — Resumable state, not a blocked goroutine. Why: the
  wait is indefinite, and a parked goroutine would hold `gitMu` and a
  context, stalling every other create and kill.
- **2026-09-22** — No timeout. Why: any timeout fallback silently
  reintroduces the stale-base bug, just later.
- **2026-09-22** — Third choice is the *cached `origin/main`*, labelled
  with its age, not the local `main` branch. Why: it matches today's
  fallback exactly, so the explicit choice is behavior-preserving;
  local `main` may carry unpushed commits and is usually the worse base.
- **2026-09-22** — No control client → fail the create outright rather
  than fall back. Why: leaves no silent path anywhere.
- **2026-09-22** — The resume state (`{spec, plan}`) is not persisted;
  matches `pendingPrompt`'s in-memory rule (`create.go:92-97`) and
  avoids a persisted record needing versioning and GC for a window
  measured in seconds. **Amended after review:** one marker bit
  *is* persisted on `MetaFile`, because without it boot cannot tell a
  parked entry from an ordinary session and would revive it into a
  plain one — reintroducing the silent fallback. The spec's non-goal
  was narrowed to match.
- **2026-09-22** — The dialog auto-raises as soon as the session event
  lands, matching the dead-card precedent. Why: the whole point is that
  this stops being quiet. Accepted cost: launching several sessions
  that all fail queues several dialogs; revisit only if that is hit in
  practice.

## Progress

- **2026-09-22** — Plan written from a live trace of this tree.
- **2026-09-22** — Implemented; PR #452 open. All layers green (Go,
  955 unit/dom, 401 e2e), both CI gates pass locally. The dom test
  caught a real bug: the dialog hook sat after the `added` branch's
  early `return` in events.ts, so it would never have fired for a
  newly parked session.

## Open questions

- Whether the parked phase should also appear in the sidebar row, or
  only on the tile. Settle during implementation — `session-state.ts`
  has to be touched either way (see Files to change).
- **Client disconnects *after* a session parks.** The no-client rule is
  evaluated at failure time, so a GUI that quits while a session is
  parked leaves an entry nothing can answer. It is not a hang — no
  goroutine is held — but it is a stuck entry until a GUI reconnects
  (which does re-raise the dialog, since the state rides on
  `SessionInfo`) or the user kills it. Proposed: leave it parked and
  let the reconnect handle it; kill is always available. Confirm this
  is the wanted behavior during implementation.

  Related: `hivebar` handshakes as `wire.ModeControl`
  (`cmd/hivebar/client.go:106`) but cannot render the dialog, so a
  `ModeControl` count over-reports answerability when the GUI has quit
  and the menu-bar agent is still connected. `hivebar` never creates
  sessions, so it cannot cause a park by itself — it only widens this
  same window.
