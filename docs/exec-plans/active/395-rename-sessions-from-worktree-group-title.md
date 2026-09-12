# Name a worktree group by double-clicking its title

- **Spec:** [docs/product-specs/395-rename-sessions-from-worktree-group-title.md](../../product-specs/395-rename-sessions-from-worktree-group-title.md)
- **Issue:** #395
- **PR:** #396
- **Branch:** feature/395-name-a-worktree-group
- **Status:** active

## Summary

The sidebar's worktree group header is inert while session rows and tile names already
rename on double-click. This plan wires the same `beginInlineRename` editor onto the
group title and commits to a **persisted, daemon-side worktree label** carried on the
owning project. Session names are never touched, and the `titleOnly` rule that hides a
member's branch-derived default name is unchanged.

## Research

### The shape the label should take

A label is a `(projectID, worktreePath) -> string`. `Project`
(`internal/registry/registry.go:150-157`) already holds exactly that class of state —
`Name`, `Color`, `Cwd` are all persisted, broadcast and GUI-consumed the same way — so
the label rides on `Project.WorktreeLabels map[string]string`.

The alternative, a `worktree_label` field on `SessionInfo`, was rejected after research:
`(*Entry).Info()` (`internal/registry/registry.go:219-242`) is a method on `*Entry` alone
with no reference back to `*Registry` or `*Project`, so it cannot reach a project-scoped
map without a signature change at roughly 20 call sites across `registry.go`,
`create.go`, `closed.go` and `projects.go` — for a value that is not per-session.

### Relevant code — Go

- `internal/registry/projects.go:420-463` — `UpdateProject`, the mutation pattern to copy:
  mutate under `r.mu`, `persistProjectLocked` under the same lock, snapshot `p.Info()`
  while still held, **unlock before broadcasting**, then `broadcastProject`.
- `internal/registry/projects.go:508-523` — `broadcastProject`; `internal/registry/events.go:13,485-523`
  — the project listener fanout.
- `internal/registry/persist.go:41-48` — `ProjectMetaFile`, written via
  `writeJSON` → `writeAtomic` (temp + rename, `persist.go:79-96`). A new map field needs
  **no** change to the persistence machinery.
- `internal/registry/registry.go:842-940` — `load()`; `:862`, `:867-870`, `:880`, `:884-887`
  are the two loader loops that must copy the new field. An old `project.json` without the
  key unmarshals to a `nil` map with no error, so backward compatibility is free — but a
  write path needs a nil-map guard.
- `internal/registry/worktrees.go:441-497` — `RenameWorktree`. Explicitly **not** the model:
  it shells out to git, touches no persisted state, sends no broadcast, and is refused
  outright when any session references the path (`:475-478`). A label must work precisely
  *while* sessions are running in the worktree.
- `internal/daemon/daemon.go:1275-1282` — `UpdateProject`'s dispatch: no `finishMutation`,
  no dedicated reply; the broadcast does the work. `:1371-1375` (`AddIdea`) is the
  precedent for running inline rather than via `runOp` — `runOp` exists for calls that
  shell out to git, and a label touches neither git nor `gitMu`.
- `internal/daemon/daemon.go:910-918, 940-952` — `sendWorktrees`/`finishMutation` reply to
  the **requesting connection only**. `internal/wire/frame.go:75-81` states there is
  deliberately no `WORKTREE_EVENT`. This is why the label rides a project event instead.
- `internal/wire/frame.go:28` — `PROTOCOL_VERSION` stays 1; new frames do not break HELLO.
  Next free frame byte is `0x2a` (after `FrameResolvePrompt = 0x29`).
- `internal/buildinfo/contract.go:86` — `DaemonContract = 8`, with a hand-maintained
  history comment at `:24-85`.

### Relevant code — frontend

- `cmd/hivegui/frontend/src/app/events.ts:426-442` — already handles `project:event` and
  calls `updateProject(ev.project)`, re-rendering on match. **No new event wiring.**
- `cmd/hivegui/frontend/src/components/Sidebar.tsx:375-410` — `renderRows()` has the owning
  `o.project` and `worktreeKey(head)` in scope exactly where the label is needed.
- `cmd/hivegui/frontend/src/components/Sidebar.tsx:293-301` / `:467-479` — the two existing
  `beginInlineRename` call sites (session, project). Both use a `useRef` span +
  `el.replaceWith(input)` and `refocusActiveTerm()` in `onDone`.
- `cmd/hivegui/frontend/src/components/Sidebar.tsx:480-491` — the project-card drag guard
  that `preventDefault`s on `.project-name-input`; the new input must join it.
- `cmd/hivegui/frontend/src/app/inline-rename.ts:73-136` — `onCommit` fires only on a
  non-empty, changed value. **Consequence: clearing a label cannot go through `onCommit`**;
  see Approach.
- `cmd/hivegui/frontend/src/components/WorktreeGroup.tsx:20-103` — props are
  `{branch, count, color, attention, children}`; `__branch` contains an `<Icon>` svg at
  `:70-76`, which is why the dblclick guard must use `closest()` rather than target identity.
- `cmd/hivegui/frontend/src/theme/components/sidebar.css:86-134` — `.hv-worktree-group`,
  `__header` (`grid-template-columns: 14px minmax(0, 1fr) auto`, sticky, opaque), `__branch`,
  `__count`.

### Constraints / dependencies

- **`DaemonContract` must be bumped 8 → 9.** The change touches `internal/wire`,
  `internal/daemon` and `internal/registry` — three of the trees
  `scripts/check-daemon-contract.sh:35-48` watches — and an old daemon receiving the new
  frame would log-and-ignore it, so the GUI would report a label saved that never was.
  That silent-wrong-answer mode is exactly what the contract exists to force a bump for
  (precedent: bump 5, the ideas frame set).
- **All three wire clients update in lock-step**, per `AGENTS.md`: `cmd/hivegui/app_calls.go`,
  `cmd/hived-ws-bridge/main.go`, and `internal/wire/testclient`.
- **Wire JSON is `snake_case`**; JS readers use `snake_case ?? camelCase` at the boundary.
- `internal/wire/wire_test.go` pins frame bytes, names, snake_case payloads and round-trips
  in four separate tables (`:250-341`, `:346-368`, `:375-395`, `:398-417`); each must gain
  an entry.
- **Correction to `AGENTS.md`:** it states `testclient.RequireIsolation` enforces e2e-real
  isolation Go-side. That symbol does not exist anywhere in the tree. Daemon tests isolate
  via `shortTempDir(t)` + per-test raw socket dials instead. Not load-bearing for this
  plan, but the doc is wrong and is noted here rather than silently relied upon.

### Prior lessons

`brain-search "worktree group sidebar rename inline session name" --rank --limit 8` —
no prior lessons matched.

### Conventions card

Verbatim from `AGENTS.md`:

```
build:  ./build.sh          # macOS .app (GUI + daemon); README has Win/Linux
test:   scripts/test.sh     # all layers: go · unit · dom · e2e
```

`AGENTS.md`'s "Build / test / lint commands" section is unfilled template placeholders
(literal `<command>` tokens); the concrete frontend commands are in
`cmd/hivegui/frontend/package.json`: `npm run typecheck` (`tsc --noEmit`), `npm run ci`
(`biome ci .` — the only form that checks formatting), `npm test` (`vitest run`),
`npm run test:e2e` (`playwright test`).

Conventions this feature touches:

- **TDD — tests come with every change**, and "boil the lake": golden path plus key edge
  cases in the same PR.
- **Test layers:** Go in `internal/…` beside source; pure `lib/` in `test/unit/`; jsdom in
  `test/dom/`; Playwright-vs-Wails-mock in `test/e2e/`.
- **Wire protocol change:** `json:"snake_case"` tags, then all three clients in lock-step.
- **Daemon contract:** bump `internal/buildinfo/contract.go` and add a history line.
- **Visual rules** in `docs/design-docs/ui/`; `scripts/ui-lint.sh --strict` enforces tokens.
- **Changelog:** add `.changesets/<slug>.md`; never edit `CHANGELOG.md`.

## Approach

Double-clicking a worktree group's title opens the shared inline rename editor. Committing
sets a **persisted, daemon-side label** on the owning project, keyed by worktree path. The
header then shows that name with the branch beside it. **No session is renamed**, and
`titleOnly` is untouched.

### Where the label lives

`Project.WorktreeLabels map[string]string`, persisted in `project.json`, delivered on
`ProjectInfo`, broadcast with `ProjectEventUpdated`. Three consequences worth stating:

- **No new event plumbing.** Worktree mutations reply only to the requesting connection
  (`daemon.go:910-918`) and there is deliberately no `WORKTREE_EVENT` (`frame.go:75-81`),
  but a label must repaint every open sidebar. `broadcastProject` already fans out to
  every control connection and `events.ts:426-442` already handles it. Only a **request**
  frame is new.
- **`RenameWorktree` is not the model.** It shells out to git, persists nothing,
  broadcasts nothing, and is refused while any session references the path
  (`worktrees.go:475-478`). Labels are deliberately **not** gated on occupancy — the
  feature exists to name a group of running sessions — and a test pins that.
- **Run inline, not via `runOp`.** `runOp` exists for calls that shell out to git; this
  touches neither git nor `gitMu`. Same reasoning `AddIdea` records at `daemon.go:1371-1375`.

### Clearing a name changes a shared module

`inline-rename.ts` fires `onCommit` only on a non-empty, changed value
(`inline-rename.ts:104-118`), so "commit empty to clear" is impossible without touching
the module all four rename affordances share. It gains an opt-in `allowEmpty?: boolean`
defaulting to `false`, so the three existing call sites are untouched by construction.

### Files to change — Go

1. `internal/registry/persist.go:41-48` — `ProjectMetaFile` gains
   `WorktreeLabels map[string]string \`json:"worktree_labels,omitempty"\``. No change to
   `writeJSON`/`writeAtomic`.
2. `internal/registry/registry.go` — `Project` gains the field; `Project.Info()`
   (`:159-169`) passes it; both loader loops (`:867-870`, `:884-887`) copy it. An old
   `project.json` unmarshals the missing key to `nil`; writes need a nil-map guard.
3. `internal/registry/projects.go` — new `SetWorktreeLabel(projectID, path, label string) error`,
   copied from `UpdateProject` (`:420-463`): mutate under `r.mu`, persist under the same
   lock, snapshot `p.Info()` while held, unlock **before** broadcasting. Empty label
   deletes the key. Key stored verbatim as the client spelled it, *not*
   normalised — the sidebar's lookup key must not drift from the key the
   daemon wrote. `remapWorktreeLabel` reconciles spellings by comparing
   `worktree.ResolvePath` forms instead.
4. `internal/wire/control.go` — `SetWorktreeLabelReq{ProjectID, Path, Label}` with
   `snake_case` tags; `ProjectInfo` (`:527-534`) gains `worktree_labels`.
5. `internal/wire/frame.go` — `FrameSetWorktreeLabel = 0x2a` plus its `String()` arm.
   `PROTOCOL_VERSION` stays 1.
6. `internal/daemon/daemon.go` — dispatch case modelled on `UpdateProject` (`:1275-1282`),
   run inline.
7. `internal/wire/testclient/client.go` — `SetWorktreeLabel` method.
8. `cmd/hivegui/app_calls.go` — `App.SetWorktreeLabel`.
9. `cmd/hived-ws-bridge/main.go` — `case "SetWorktreeLabel":`.
10. `internal/buildinfo/contract.go` — `DaemonContract` 8 → 9 plus a history entry.

### Files to change — frontend and docs

11. `src/bridge.ts` — export the generated `SetWorktreeLabel` binding.
12. `src/app/state.ts` — `ProjectInfo` gains `worktreeLabels?` / `worktree_labels?`.
13. `src/components/WorktreeGroup.tsx` — `label` and `onRenameTitle` props; `__title`
    wrapper span held by a `useRef`; `__label` before `__branch`; `closest()`-guarded
    `onDoubleClick`; `cancelInlineRenameFor` from an empty-dep `useEffect` cleanup.
14. `src/components/Sidebar.tsx` — read the label from `o.project` by `worktreeKey(head)`
    in `renderRows()`; `onCommit` calls `SetWorktreeLabel` and makes **no** `UpdateSession`
    call; add `.group-name-input` to the drag guard at `:480-491`.
15. `src/app/inline-rename.ts` — opt-in `allowEmpty`.
16. `src/theme/components/sidebar.css` — `__title`, `__label`, `.group-name-input`;
    `__branch` shrinks first. Tokens only.
17. `test/e2e/wails-mock.ts`, `test/e2e-real/wails-bridge.ts` — mock the new binding.
18. `docs/design-docs/ui/components.md:50` — the heading signature gains two props; document
    the name and double-click-to-rename.
19. `.changesets/name-a-worktree-group.md`.

### Blast radius

- `internal/wire/wire_test.go` pins frame bytes, names, snake_case payloads and round-trips
  in four tables (`:250-341`, `:346-368`, `:375-395`, `:398-417`); `:398-417` asserts
  request frames have no event-name mapping, which is where the new frame belongs.
- `test/dom/sidebar-group.test.tsx`, `test/e2e/{sidebar-group-flatten,sidebar-sticky,sidebar-collapse-animation,shared-worktree-cue}.spec.ts`,
  `test/e2e/fixtures/seed-worktree-group.ts` — all select `.hv-worktree-group*`.
- `test/e2e/sidebar-density.spec.ts` is **not** at risk: session names are untouched, so
  `titleOnly` never flips and row heights cannot change.

### Tests

**`internal/registry/worktree_labels_test.go`** (new), via `freshRegistryWithProject(t)`:
persists and round-trips across a reload; clearing deletes the key rather than storing
`""`; **succeeds with a live session in the worktree** (the contrast with
`RenameWorktree`'s `ErrWorktreeInUse`); unknown project returns `ErrProjectNotFound`;
an old `project.json` without the key loads without erroring; two path spellings resolve
to one key.

**`internal/daemon/worktree_labels_test.go`** (new): dial **two** connections, set the
label on A, assert **B** receives a `PROJECT_EVENT` carrying it. No existing worktree test
covers a fan-out beyond the caller.

**`test/dom/sidebar-group-rename.test.tsx`** (new), mocking `SetWorktreeLabel` *and*
`UpdateSession`: editor seeds with the branch when unnamed and the name once set; Enter
calls `SetWorktreeLabel` with project id, path and name; **zero `UpdateSession` calls and
every member name byte-identical**; empty commit clears; Escape sends nothing; header shows
name + branch, unnamed shows branch only; members still render `titleOnly` after naming;
the branch icon opens the editor but the chevron and count do not; closing a member
mid-edit cancels instead of committing.

**`test/dom/inline-rename.test.ts`**: `allowEmpty` defaults off.

**`test/e2e/sidebar-group-flatten.spec.ts`**: a named group at the 220px floor asserting a
**measurement** (bounding-box width, no header overflow), not `toBeVisible()` — which
passes on a branch ellipsed to nothing.

### Verification

```bash
scripts/test.sh go
cd cmd/hivegui/frontend && npm run typecheck && npm run ci
cd ../../.. && scripts/test.sh unit dom
scripts/check-daemon-contract.sh
scripts/ui-lint.sh --strict
cd cmd/hivegui/frontend && CI=1 npx playwright test test/e2e/
```

`--strict` is load-bearing (`ui-lint.sh:32`: "Exit 0 in warn mode"); `CI=1` stops Playwright
reusing a stale vite dev server.

## Second opinion

Two reviewer rounds ran against the **superseded** fan-out design; both returned `revise`
(round 1 confidence 8, 8 must-fix; round 2 confidence 7, 5 must-fix). Round 1 caught two
defects that would have shipped: the feature would have worked exactly once (after a rename
no member carries the default name), and the fan-out dropped the `!!defaultName` guard so a
detached-HEAD group would have renamed every unnamed member. It also caught that
`scripts/ui-lint.sh` exits 0 without `--strict`, that the e2e check used `toBeVisible()`
(which passes on a branch ellipsed to nothing), and four more e2e specs selecting this
header. Round 2 then showed round 1's own fix was wrong — the "only distinct candidate"
label clause promoted a private name to a group label and fed it back into the fan-out —
and flagged a stale closure in `onCommit` and the project-card drag guard.

Disposition: round 1's items were applied and re-reviewed; round 2's were applied without a
third round, per the loop's single-retry rule. **The operator then redirected the design at
plan review round 1**, replacing the fan-out with a persisted group label. That removed the
subject matter of most findings — there is no `groupLabel` heuristic, no fan-out and no
`UpdateSession` call left to get wrong. Three findings survived the redesign and are carried
into this plan: `ui-lint.sh --strict`, the measured e2e assertion, and the
`.group-name-input` drag guard. The current design has had **no** independent review.

## Decision log

- **2026-09-11** — Header shows the group name with the branch beside it. Why: operator choice, clarifying round A and reaffirmed at plan review round 1.
- **2026-09-11** — Session names are never modified; `titleOnly` keeps hiding branch-derived defaults. Why: operator direction at plan review round 1 ("don't change the session names, just keep hiding the default ones"), superseding the earlier fan-out design.
- **2026-09-11** — The label is persisted daemon-side and delivered over the wire, not held in `localStorage`. Why: operator choice at plan review round 1; the name must be visible to any client, which per-webview storage cannot do. This is what makes the feature `L`.
- **2026-09-11** — The label lives on `Project.WorktreeLabels`, not on `SessionInfo`. Why: `(*Entry).Info()` cannot reach project-scoped state without changing ~20 call sites, and the label is not per-session.
- **2026-09-11** — The change reuses `ProjectEventUpdated` instead of adding a reply or event frame. Why: worktree mutations reply only to the requesting connection (`daemon.go:910-918`) and there is deliberately no `WORKTREE_EVENT` (`frame.go:75-81`), while a label must repaint every open sidebar — which `project:event` already does (`events.ts:426-442`).
- **2026-09-11** — Labels are not gated on worktree occupancy, unlike `RenameWorktree`. Why: the feature exists to name a group of *running* sessions; a refusal while sessions are live would make it useless.
- **2026-09-11** — Committing an empty value clears the label and deletes the map key. Why: avoids empty-string entries accumulating in `project.json`.

## PR convergence ledger

Append-only, one line per `/hs-review-loop` iteration.

- **2026-09-11 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: b610c0cad6dff9f6955cc906314544fa7fcbc052e642060de638846f52e3df13; threads_open: 4; action: escalated:risky-fix-needs-human-decision; head_sha: c804659a.
- **2026-09-11 iter 2** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: unrecorded (worker died before returning its envelope); threads_open: 1; action: autofix+push; head_sha: 76fe723e.
- **2026-09-11 iter 2 (envelope, late)** — verdict: REQUEST_CHANGES; findings_hash: cfd1873efc19e15462f1ad8dfb1aa882cecebe6f6a238acd1e4c2830abc24448; threads_open: 1; action: escalated:risky-fix-needs-human-decision (unbounded label length); head_sha: 76fe723e.
- **2026-09-11 iter 2b** — operator-directed fix applied: label bounded at wire.MaxWorktreeLabel, branch min-width floor, thread replied and resolved; head_sha: 3e7a7f6a.

## Progress

- **2026-09-11** — Spec created, triaged M/P2. Research complete (frontend + brain: no prior lessons). Plan drafted; two reviewer rounds, both `revise`.
- **2026-09-11** — Implemented on feature/395-name-a-worktree-group; PR #396 opened. Go + frontend + tests green (one pre-existing `internal/registry` failure, `TestTerminalQueriesAreNotWork`, confirmed on origin/main).
- **2026-09-11** — Plan review round 1 returned operator feedback redirecting the design: name the group, do not rename sessions. Spec rewritten, complexity raised M → L, backend research completed, plan re-drafted.

## Open questions

- None. The design fork (storage) was settled at plan review round 1.
