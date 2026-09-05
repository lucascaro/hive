# Persist minimized projects across restarts

- **Spec:** [docs/product-specs/340-persist-minimized-projects-across-restarts.md](../../product-specs/340-persist-minimized-projects-across-restarts.md)
- **Issue:** #340
- **PR:** #342
- **Branch:** `feature/340-persist-minimized-projects-across-restarts`
- **Status:** active

## Summary

Minimized projects are already persisted to localStorage
(`hive.minimizedProjects`), yet a full daemon + GUI restart brings every
project back expanded. This plan finds the point in the boot path where the
persisted set is lost and fixes it there, with a regression test that would
have caught it.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/lib/collapsed.ts` — `MINIMIZED_PROJECTS_STORAGE_KEY`
  (`hive.minimizedProjects`), `loadCollapsed`, `serializeCollapsed`,
  `pruneCollapsed`. Pure, well covered by `test/unit/minimized-projects.test.ts`.
- `cmd/hivegui/frontend/src/store/store.ts:291` — `initialData()` seeds
  `minimizedProjects` from localStorage as an import side effect.
- `store.ts:392` `applyProjectList` — the **only** prune point; prunes the
  persisted set against the `project:list` id snapshot and re-persists when the
  set changed.
- `store.ts:683/689` `minimizeProject` / `restoreProject` — persist on every
  write. `view.ts:381/404` are the only callers; the legacy
  `hiveStateView.minimizedProjects` setter routes through `setMinimizedProjects`,
  which also persists. No unpersisted write path exists.
- `internal/registry/registry.go:717` `load()` — rebuilds `r.projects` from the
  persisted `ID` field in `projects/index.json` + `project.json`.
  `internal/registry/projects.go:39` `EnsureDefaultProject` only mints a UUID
  when the project list is genuinely empty.
- `internal/daemon/daemon.go:623` — the handshake sends `wire.FrameProjects`
  (`project:list`) from `reg.ListProjects()`, i.e. **every** known project, not
  only ones with live sessions.

### Constraints / findings

1. **Project IDs are stable across a daemon restart.** Verified on the
   operator's real state dir: `~/Library/Application Support/Hive/projects/`
   holds eight project directories dated May–Sep, and `index.json` lists those
   same eight UUIDs. Nothing is re-minted at boot.
2. **The browser round trip is correct.** A new e2e test
   (`test/e2e/minimize-project.spec.ts` › "a minimized project stays minimized
   across a reload") minimizes a project, asserts the localStorage write, reloads
   the page, and asserts the tray chip survives. It **passes** against the Wails
   mock.
3. **The real install's persisted set is stale.** Reading the actual WKWebView
   store
   (`~/Library/WebKit/com.wails.hivegui/WebsiteData/.../LocalStorage/localstorage.sqlite3`):
   - `hive.theme` = `dracula`, `hive.view` = `single`, `hive.fontSize` = `16`,
     `hive.sidebarWidth` = `328` — localStorage itself persists fine.
   - `hive.minimizedProjects` = `["7b50fc60-b5bc-425f-a879-88dd21d7c849"]`
   - `hive.collapsedProjects` = `["42b70ec6-…","63fd20ad-…", …]`

   **None of those UUIDs exist** in `projects/index.json`, in
   `sessions/`, in `closed/`, or anywhere in `hived.log` / `hivegui.log`.
   They are orphans from projects that no longer exist — which means
   `applyProjectList`'s prune has never successfully run in this install.
   That is the anomaly worth chasing: prune is the one code path that both
   cleans stale ids *and* is the only path that can silently narrow the set.

### Open lead

The set on disk is stale rather than empty, so the failure is not "the value is
wiped at quit". Either the `project:list` handler is not reaching
`applyProjectList` in the real app, or a second GUI window boots with a stale
in-memory set and clobbers the good one on its own write. Distinguishing them
needs a live before/after read of the sqlite store around a real
minimize + quit + relaunch.

### Prior lessons

Brain search returned no matching entries for this bug.

## Approach

Namespace the two persisted project-id keys by the **state directory of the
daemon this GUI is talking to**, so instances that own disjoint project
registries stop sharing (and pruning) one another's sets.

Why this over the obvious one-line alternative (delete the prune from
`applyProjectList`): dropping the prune would stop the data loss, but it leaves
the sets semantically wrong — minimizing a project in the main daemon would
still be recorded in the same bucket a worktree daemon reads — and it gives up
the "don't grow forever" property the prune was added for. Namespacing fixes
the cause; removing the prune only hides the symptom.

### The three real design problems

1. **The namespace is not known synchronously.** `initialData()`
   (`store.ts:291`) runs as an import side effect, before any Wails binding can
   be called. Hydration of the two sets therefore moves out of `initialData()`
   (which now starts them empty) into an explicit
   `hydratePersistedProjectSets(ns)` action, awaited inside the existing
   bootstrap IIFE in `main.tsx` **immediately before `await ConnectControl()`**.
   No frame can arrive before the connect call — the daemon sends its snapshot
   only after the handshake (`internal/daemon/daemon.go:623`) — so the
   synchronous top-level `wireDaemonEvents(...)` call does not move.

2. **The prune must not run before hydration.** If `project:list` arrived first
   it would prune an empty set and persist `[]`, reproducing the bug exactly.
   `AppData` (`store.ts:58` — *not* `AppState`, see below) gets
   `projectSetsHydrated: boolean`; `applyProjectList` still applies the project
   list but skips pruning until the flag is set.

3. **Failing safe must mean writing nothing.** If `StateDirID()` throws or
   returns empty, falling back to the bare key would re-create the shared-key
   bug *and* resurrect a legacy key a prior migration deleted, which another
   daemon's GUI would then adopt. So hydration failure disables persistence
   outright: a module-level `persistProjectSets` flag gates
   `persistCollapsed` / `persistMinimizedProjects`, and stays false until a
   non-empty namespace is installed. The sets remain usable in memory for the
   session; they are simply not written.

**Migration:** on first hydrate under a **non-empty** namespace, if the
namespaced key is absent and the legacy un-namespaced key exists, adopt the
legacy value and remove the legacy key. Migration is skipped entirely when the
namespace is empty — the two key names would be identical and "adopt then
delete" would destroy the value it just read.

`projectSetsHydrated` goes on `AppData` only. `AppState` (`app/state.ts:175`)
types the `hiveStateView` facade (`store.ts:958`), whose key list is frozen by
`test/unit/store.test.ts:383`; adding a field there would break that test and
force a getter/setter with no reader.

No wire frames change — `StateDirID` is a GUI-local Wails binding reading
`registry.StateDir()`, so no `buildinfo.DaemonContract` bump is required
(`scripts/check-daemon-contract.sh:36` watches `internal/{wire,daemon,session,registry}`
and `cmd/hived`; none are touched).

### Files to change

1. `cmd/hivegui/app_calls.go` — new binding `StateDirID() string`: first 8 hex
   chars of `sha256(registry.StateDir())`. Short, stable, and keeps a
   filesystem path out of web storage.
2. `cmd/hivegui/frontend/src/lib/collapsed.ts` — add `namespacedKey(base, ns)`;
   returns `base` for an empty `ns`, `` `${base}.${ns}` `` otherwise.
   `loadCollapsed` / `serializeCollapsed` / `pruneCollapsed` unchanged.
3. `cmd/hivegui/frontend/src/store/store.ts` —
   - module-level `storageNS` and `persistProjectSets`; `persistCollapsed` /
     `persistMinimizedProjects` resolve their key through `namespacedKey` and
     return early when `persistProjectSets` is false.
   - `initialData()` seeds both sets empty and `projectSetsHydrated: false`;
     `AppData` gains that field.
   - `loadSavedCollapsed` / `loadSavedMinimizedProjects` (`store.ts:212`) take
     an explicit namespace and become internal to the hydrate action — no
     un-namespaced loader stays on the public surface.
   - new `hydratePersistedProjectSets(ns)`: installs the namespace, runs the
     migration, loads both sets, enables persistence, flips the flag.
   - `applyProjectList`: prune only when `projectSetsHydrated`.
   - `resetStore()` (`store.ts:928`) clears `storageNS` / `persistProjectSets`
     so a namespace cannot leak between vitest files sharing a worker.
4. `cmd/hivegui/frontend/src/main.tsx` — bootstrap step 2.5 inside the existing
   IIFE, immediately before `await ConnectControl()`. Update the bootstrap-order
   comment at the top of the file, which currently says hydration is an import
   side effect.
5. `cmd/hivegui/frontend/src/bridge.ts` — re-export the new binding.
6. `cmd/hivegui/frontend/test/e2e/wails-mock.ts` — mock `StateDirID`.
7. `cmd/hivegui/frontend/test/unit/store.test.ts:264` — "hydrates every
   persisted field from storage" currently seeds both keys and asserts
   `resetStore()` yields `['p1','p2']` / `['p3']`. It becomes: `resetStore()`
   yields **empty** sets, with the old assertion moved to a new
   `hydratePersistedProjectSets` test.
8. `cmd/hivegui/frontend/test/dom/minimize-project.test.tsx:426` — "prunes ids
   missing from an authoritative project list" and "prunes against an empty
   authoritative list" boot via `resetStore()`, which now leaves
   `projectSetsHydrated: false` and would skip the prune. Both must hydrate
   first.
9. `.changesets/persist-minimized-projects-per-daemon.md` — `type: fixed`,
   `bump: patch`. Mention the key rename, since `CHANGELOG.md:601` documents
   `hive.collapsedProjects` by name.

### New files

- `cmd/hivegui/frontend/test/unit/storage-namespace.test.ts` — key derivation,
  the empty-namespace case, and legacy migration.

### Tests

**The regression gate** is the dom test
`test/dom/minimize-project.test.tsx` › "project:list before hydration does not
prune or persist". That is the assertion that fails on `main` and passes after
the fix. The e2e reload spec below already passes on `main` — it is coverage
against a future regression, not proof of this fix.

- `cmd/hivegui/app_calls_test.go` › `TestStateDirID` — 8 hex chars, stable for
  one dir, different for two.
- `test/unit/storage-namespace.test.ts` › `namespacedKey` returns the bare base
  for an empty namespace, a suffixed key otherwise; migration adopts and removes
  the legacy key under a real namespace, and is a no-op under an empty one.
- `test/unit/store.test.ts` › `resetStore()` leaves both sets empty;
  `hydratePersistedProjectSets('ns')` fills them from the namespaced keys;
  a failed hydrate (empty namespace) leaves persistence disabled so a subsequent
  `minimizeProject` writes nothing to storage.
- `test/dom/minimize-project.test.tsx` ›
  - **`project:list` before hydration does not prune or persist** (the gate).
  - after hydration under namespace A, a `project:list` from A prunes only A's
    dead ids.
  - a set stored under namespace B is untouched by a full boot under A.
  - restore-then-reboot leaves the project restored — the empty-set round trip,
    which otherwise looks identical to "hydration never ran" (spec success
    criterion 2).
  - the two existing prune tests, updated to hydrate first.

**Every existing test that asserts a persisted write must hydrate first and
read the namespaced key.** The `persistProjectSets` gate means a bare
`resetStore()` writes nothing, so these go from passing to
`JSON.parse(null)` or `expect(['p3'])` against `[]`:
`test/unit/store.test.ts:140,147` (toggleCollapsed round trip), `:157`
(`applyProjectList` prunes collapsed), `:192,198` (minimize/restore persist);
`test/dom/minimize-project.test.tsx:200,204` ("persists the minimized set"),
`:284` ("keeps the set intact across a render with no projects loaded yet"),
and `:415` ("drops the minimized id when the project is deleted" — which goes
vacuous rather than red, since both sides become `[]`; it routes through
`removeProject`, `store.ts:436`, a persist call site to hydrate for too).

**The gate test only fails on `main` if it seeds the bare keys.** It must write
`hive.collapsedProjects` / `hive.minimizedProjects` *un-namespaced* before
`resetStore()`, so that on `main` `initialData()` populates the sets and the
un-gated prune wipes them; then assert the sets survive and nothing was
written. Seeding the namespaced key instead makes it green on `main` — the
same vacuity the first review caught in the e2e spec.

`test/e2e/minimize-project.spec.ts:202` reads the literal
`hive.minimizedProjects`; its key expression must be derived from whatever
namespace `wails-mock.ts` returns, and the mock must return a non-empty one
(an empty namespace disables persistence, so nothing would be written at all).
- `test/e2e/minimize-project.spec.ts` › "a minimized project stays minimized
  across a reload" — already added; coverage.

### Verification

```
./scripts/ci-bootstrap.sh                 # regenerates wailsjs/ so tsc sees StateDirID
cd cmd/hivegui/frontend && npx tsc --noEmit && npx biome ci .
go test ./cmd/hivegui/...
scripts/test.sh unit dom
CI=1 npm run test:e2e -- test/e2e/minimize-project.spec.ts
```

`wailsjs/` is generated and untracked, so the bootstrap step must come first or
`tsc` fails on a missing binding for reasons unrelated to the diff.

### Risks / open questions

- **Two GUI windows on the same daemon** still last-writer-wins. Matches
  `window.json`'s documented behaviour; called out as a non-goal.
- **The namespace comes from the GUI process's own `registry.StateDir()`**, not
  from the daemon it actually connected to. Correct for the case in the spec,
  where `HIVE_SOCKET` and `HIVE_STATE_DIR` are set together, but wrong for a GUI
  pointed at a foreign socket. Deriving the id from the handshake would close
  that; out of scope here.
- **Changing `HIVE_STATE_DIR` orphans a namespace.** The old key lingers —
  a few dozen bytes, only for users who move their state dir. Not worth a GC pass.
- **Operator data restore.** The wiped ids (`7b50fc60-…` minimized;
  `42b70ec6-…`, `63fd20ad-…` collapsed, all belonging to
  `/tmp/hive-iso-azure-comet/state`) can be written back under that state dir's
  namespace once the key format exists. WebKit holds the sqlite open, so it
  needs every hivegui quit first: a manual post-merge step, explicitly **not**
  part of the diff.

## Second opinion

Reviewer verdict **revise**, confidence 8. It confirmed the approach and the
"no daemon-contract bump" claim, and found seven concrete gaps: two existing
tests the plan would have broken (`test/unit/store.test.ts:264`,
`test/dom/minimize-project.test.tsx:426`), the flag placed on the wrong type
(`AppState` rather than `AppData`, which would break the frozen facade-key test
at `test/unit/store.test.ts:383`), an unsafe fallback that would rewrite the
shared legacy key when `StateDirID()` fails, an undefined migration under an
empty namespace, a missing `wailsjs/` regeneration step before `tsc`, and a
vacuous headline assertion (the e2e reload spec already passes on `main`).

All seven applied above. A second pass confirmed the fixes landed and every
cited anchor is real, and returned `revise` again on one point: the
`persistProjectSets` gate breaks more existing tests than the two enumerated,
and the regression gate is only non-vacuous if it seeds the *bare* keys. Both
are now written into the Tests section as implementation scope. Per the
pipeline's one-retry rule there is no third review round. The reviewer also judged the pre-`ConnectControl()`
ordering safe — no frame can arrive before the handshake — so the synchronous
`wireDaemonEvents(...)` call stays where it is. Four of its nice-to-haves were
taken as well: resetting `storageNS` in `resetStore()`, namespacing the two
loaders, an explicit restore-then-reboot assertion for success criterion 2, and
a changeset note covering the documented key name.

## Decision log

- **2026-09-04** — Scope limited to projects; individually minimized sessions stay transient. Why: operator confirmed at clarifying round A; the session terminals are gone after a restart anyway.
- **2026-09-04** — Treated as a bug, not a new feature. Why: `store.minimizeProject`/`initialData()` already read and write `hive.minimizedProjects`; the machinery exists and is not taking effect.
- **2026-09-04** — Reproduction required before any patch. Why: operator's standing rule — guess-patches in this area have made things worse before.
- **2026-09-05** — Root cause confirmed empirically, not by inspection: read the real WKWebView localStorage sqlite and compared the persisted ids against every daemon's `projects/index.json`. Why: two static-analysis passes both concluded "no code-visible cause" — the cause was environmental (one shared web-storage origin, several daemons).
- **2026-09-05** — Namespace the keys rather than delete the prune. Why: operator's call at clarifying round B; the sets are per-daemon by nature, and dropping the prune trades one bug for unbounded growth.
- **2026-09-05** — Namespace = `sha256(registry.StateDir())[:8]`, not the raw path. Why: keeps the key short and avoids putting a filesystem path in web storage.
- **2026-09-05** — `test/e2e-real/wails-bridge.ts` also needed the `StateDirID` stub. Why: an existing test ("real harness defines every name bridge.ts re-exports") caught it — the e2e-real harness is a second binding surface the plan had not listed.
- **2026-09-05** — Fixed review iter 1's IMPORTANT: wrapped the `LogFrontend` call inside the `StateDirID` catch. Why: `LogFrontend` throws synchronously when the Wails binding is missing — the same bridge-absent condition that makes `StateDirID` fail — so the unwrapped call rejected the bootstrap IIFE and `ConnectControl()` never ran. Proved by reverting the wrap: the new e2e test hangs with no project cards.
- **2026-09-05** — Reviewer's MINOR "use the exported MOCK_STATE_DIR_ID in the spec" not applied as suggested. Why: `wails-mock.ts` imports the store and runs in the browser, so importing it from Playwright's node context dies on `import.meta`. Used a hand-synced constant with a comment saying why.
- **2026-09-05** — `storageNS` / `persistProjectSets` live as module state, not store state. Why: they are storage plumbing no component renders; putting them in `AppData` would widen the frozen `window.__hive_state` shape for no reader.

## Progress

- **2026-09-05** — Implemented on `feature/340-persist-minimized-projects-across-restarts`. Full local verification green: 1030 unit+dom tests, 271 e2e, `go test ./cmd/hivegui/...`, `biome ci .` (0 errors), `tsc --noEmit`.
- **2026-09-05** — Regression gate proved non-vacuous by simulating `main`'s behaviour (bare-key hydration + un-gated prune) in `store.ts` and re-running it: it failed with `expected '[]' to be '["foreign"]'` — the wipe itself. Reverted immediately.
- **2026-09-05** — Plan approved (no section feedback); stage IMPLEMENT.
- **2026-09-05** — Root cause confirmed; spec rewritten, complexity S→M, stage PLAN.
- **2026-09-04** — Spec + plan created, stage RESEARCH. Repro scenario from operator: quit and relaunch BOTH daemon and GUI; every minimized project returns expanded, every time.

## PR convergence ledger

Append-only. One line per `/hs-review-loop` iteration.

- **2026-09-05 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 0ae4ada1ce675449; threads_open: 0; action: fixed IMPORTANT + 2 MINOR, push, re-review; head_sha: 37f15e2.

## Open questions

None blocking.
