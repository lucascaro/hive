# Show a visual cue when sessions share a worktree

- **Spec:** [docs/product-specs/384-visual-cue-for-sessions-sharing-a-worktree.md](../../product-specs/384-visual-cue-for-sessions-sharing-a-worktree.md)
- **Issue:** #384
- **Status:** active
- **PR:** #385
- **Branch:** feature/384-shared-worktree-cue

## Summary

Sessions can be adopted onto an existing git worktree, so several sessions may
share one working directory. The GUI does not show this today. This plan adds a
color-coded link cue in the sidebar plus a count badge on the worktree glyph,
lists session occupancy in the worktrees modal, and makes shared sessions
auto-group adjacent so drag reordering moves the cluster as a unit.

## Research

### Relevant code

**Data / wire (no backend change needed)**
- `internal/wire/control.go:161-162` — `SessionInfo.worktree_path` **and** `worktree_branch` are both already on the wire (snake_case). Round-tripped in `internal/wire/wire_test.go:232-233,363`.
- `internal/registry/registry.go:1613-1614` — `Entry.Info()` copies both fields into every broadcast, and `cmd/hivegui/app_control.go:449-453` forwards the raw payload to the JS event bus. So "which sessions share a worktree" is already computable in the frontend.
- `internal/registry/registry.go:1296-1310,1378-1386` — the daemon's own `worktreeShared` is a *local bool inside `kill()`*, recomputed by scanning entries for a matching `WorktreePath`. It is never persisted and never leaves the daemon. The frontend groupBy is the same computation, so duplicating it as a wire field would buy nothing and add a sync burden on every create/kill broadcast.
- `internal/registry/create.go:340-352` — sharing arises from *sibling-cwd adoption*: a non-worktree create whose cwd matches another entry's `WorktreePath` adopts that path+branch. This is what CMD-P duplicate produces. `create.go:360-410` (`adoptDetachedWorktree`) is the other adoption path but always yields a solo session. Sharing is therefore incidental, never an explicit "attach to that session's worktree" action.

**Sidebar rendering**
- `cmd/hivegui/frontend/src/components/SessionRow.tsx:122-146` — row root `<li class="hv-session-row" data-sid data-pid data-state data-selected data-minimized>`. No worktree data attribute yet.
- `session-row.css:1-24` — explicit grid: `[state 14px][text 1fr][idea auto][worktree auto][meta+actions auto][swatch auto]`.
- `SessionRow.tsx:183-193` — the worktree `IconButton` (`.hv-session-row__worktree`), rendered only when a branch exists, deliberately outside the hover-swapped meta/actions cell. `IconButton` (`components/IconButton.tsx:7-24`) takes no children, so a count badge attaches as an absolutely-positioned sibling span.
- `SessionRow.tsx:87` reads `worktreeBranch ?? worktree_branch` — branch only. Grouping must key on **path**, not branch.
- `session-row.css:3,38-46` — `.hv-session-row` is already `position:relative`; `[data-selected]::before` is a 2px bar at `left:0` using `--accent`.
- `docs/design-docs/ui/patterns.md:5-12` — selection and attention "never share a position (bar at left edge vs icon in the state column)". The left edge is spoken for; the group bar goes on the right.
- `ProjectCard.tsx:203` — session rows live in one `<ul class="hv-project-card__body">` per project, so clustering never crosses cards. `ProjectCard.tsx:136-150` + `project-card.css:129-152` (`.hv-project-card__ideas`) is the existing numeric-count-pill precedent.

**Ordering**
- `SessionInfo.order` (`app/state.ts:28`) is a position in the daemon's single **flat, cross-project** `r.order` list (`lib/reorder.ts:5-16`). There is no per-project order.
- `Sidebar.tsx:496-498` is the *single* call site computing paint order per project: filter by project id, sort by `.order`. This is where render-side clustering goes.
- `Sidebar.tsx:176-184` (`reorderDroppedSession`) → `lib/reorder.ts:66-107` (`dropTargetIndex`) decides the committed `.order`. Cluster drag means moving a whole set, i.e. one `UpdateSession` per cluster member.
- `lib/reorder.ts:29-49` (`reorderTarget`) is the keyboard reorder path with the identical "index is the sibling's `.order`, not its array position" invariant — any cluster change must be mirrored there or the two paths disagree.
- **Constraint from spec 305**: the shipped invariant is "compute the drop slot against the sibling list with the dragged item already removed" (`lib/reorder.ts:79-89`). Cluster moves must do the same delete-before-insert bookkeeping for *every* member or reintroduce the off-by-one 305 fixed. Spec 305 explicitly non-goaled touching `moveInOrder`/`reindexLocked`.
- Daemon side: `internal/registry/persist.go:16,35-36` (`sessions/index.json` is the ordering authority), `registry.go:1567-1582` (`moveInOrder`), `1596-1634` (`reindexLocked`), `create.go:484-488` (`InsertAfterSessionID`).

**Theme**
- `src/theme/tokens.css:5-57` — flat token list per theme. There is **no** existing multi-color chrome palette; `--ansi-0..15` are terminal-tuned and `--session-color` is arbitrary user-picked hex (`SessionRow.tsx:112-114`).
- `theme.ts:1-72` + spec `305-add-ide-inspired-theme-presets.md:49-59,81-93` — 19 presets. The 6 default presets are WCAG-AA gated by `scripts/ui-contrast.mjs`; the 12 community ports are `--contrast-exempt`. New tokens must be defined in all 19 and pass contrast on the 6.

**Worktrees modal**
- `components/modals/Worktrees.tsx` `WorktreeRow` (~250-377) already shows a count via `statusLabel()` (`lib/worktrees.ts:179-196`) from `readSessionIds(w).length`.
- `WorktreeInfo.session_ids`/`sessionIds` (`lib/worktrees.ts:9-25,52-54`) already carries the occupying session ids. Resolving ids to names needs only `useAppStore((s) => s.sessions)` inside `WorktreeRow` — no new selector, no new bridge call.

**Blast radius**
- `components/TileChrome.tsx:90-177` — grid `TileHeader` has its own independent worktree `IconButton` (`.tile-worktree`, lines 162-177). Out of scope this round; noted as a deliberate skip.
- `components/MinimizedTray.tsx:27-46` / `Chip.tsx` — chips carry no worktree indicator at all today. Deliberate skip (adding one would be new surface, not parity).
- `test/e2e/theme.spec.ts-snapshots/worktrees-*.png` — visual baselines that any Worktrees-modal or row change will invalidate.
- Nothing else renders a session-row-like element.

### Constraints / dependencies

- New theme tokens are the expensive part: 6 values x 19 presets, contrast-gated on 6 of them.
- Cluster drag must preserve the spec-305 delete-before-insert invariant across every cluster member, in both the drag and keyboard reorder paths.
- Stored order and painted order can diverge under render-side clustering; the design accepts this and self-heals on the next drag.

### Prior lessons

- `brain-search "worktree shared session sidebar ordering reorder"` returned no qualifying hits — no prior lessons matched.

### Conventions card

```
module: github.com/lucascaro/hive
build:  ./build.sh          # macOS .app (GUI + daemon); README has Win/Linux
test:   scripts/test.sh     # all layers: go . unit . dom . e2e
bins:   hived (daemon) . hivegui (Wails GUI) . hived-ws-bridge (e2e-real only)
```

- Lint: `scripts/ui-lint.sh` is the named lint tool (token/icon rules, plus `--contrast`). AGENTS.md's later lint placeholders are unfilled.
- Read `docs/design-docs/ui/README.md` before touching `src/theme/`, `src/components/`, or markup (AGENTS.md:154).
- Test layers via `scripts/test.sh [layer...]`: `unit` = pure `lib/` functions, `dom` = Vitest jsdom (AGENTS.md:131-132 names "sidebar tree" as the dom target), `e2e` = Playwright vs the Wails mock bridge.
- TDD: every change ships with the test that would have caught the regression (AGENTS.md:119-125).
- User-visible change requires a `.changesets/<slug>.md` entry via `/hs-changelog-update`; never hand-edit `CHANGELOG.md` (AGENTS.md:195-228).
- Wire JSON is snake_case on the wire, CamelCase in Go; JS reads `snake_case ?? camelCase` (AGENTS.md:64-96). No wire change is needed here.

## Approach

Group entirely on the client. `worktree_path` is already on every broadcast
`SessionInfo` (`internal/wire/control.go:161-162`), and the daemon's own
`worktreeShared` is a transient bool inside `kill()` — not session identity. A
`groupBy` over the session array the sidebar already holds is the same
computation, so the alternative (a `shared_count` field on `SessionInfo`) would
add a value to keep in sync on every create/kill/rename broadcast for zero
behavioural gain.

Two channels carry the cue, because colour alone is not an accessible signal:

1. **A 3px bar on the right edge** of each row in a shared group, coloured from
   a new `--worktree-accent-1..6` token family. The left edge is unavailable —
   `[data-selected]::before` owns it and `docs/design-docs/ui/patterns.md:5-12`
   forbids two indicators sharing a position.
2. **A count on the worktree glyph** plus the sharing stated in that button's
   accessible label, so the group is legible without colour.

Adjacency is enforced where the rows are painted, not in the daemon. The
sidebar's one per-project sort (`Sidebar.tsx:496-498`) clusters shared sessions,
anchoring each cluster at its lowest-order member. That makes grouping robust
without touching `moveInOrder`/`reindexLocked`, which spec 305 explicitly
non-goaled. It also makes a cluster-aware *drag* necessary rather than
optional: with render-side clustering, dragging one member of a group produces
no visible movement unless the whole group moves, so the drop path commits one
`UpdateSession` per member and lands them contiguous. The keyboard reorder path
(`reorderTarget`) is deliberately left alone — render-side clustering already
hides any cluster it breaks, and mirroring cluster logic into a second path is
where spec 305's off-by-one class of bug lives.

The group colour is the **session colour**, not a new token family. Session
colours are never unset — `create.go:321-324` auto-assigns one via
`pickColor(r.lastSessionColor, projectColor)` — and the adoption branch at
`create.go:340-352` already holds the sibling entry it is joining, so a session
that adopts a worktree inherits that sibling's colour instead of picking a
fresh one. The right-edge bar then reads `var(--session-color)`, which the row
already sets inline (`SessionRow.tsx:112-114`). No new tokens, no `themes.css`
edit, no `ui-contrast.mjs` change.

Three costs, accepted knowingly:

- It reinterprets `create.go:318` ("Color is reserved for project/session
  identity") to mean worktree identity for adopted sessions.
- Colour no longer distinguishes siblings inside a group; the count badge and
  the accessible label are the per-row channel.
- Adopted sessions bypass `pickColor`'s "differ from the last one" rule, so two
  *different* groups in one project can collide on a colour by chance.
  Mitigation: still record the inherited colour as `lastSessionColor` so the
  next fresh session steers away from it.

A user can re-colour one member and break the link by hand; the count badge is
what keeps that non-fatal.

**Risk — the cluster drop is N round-trips, not one.** `UpdateSession` is one
call per member and each one triggers a daemon `moveInOrder` plus a re-broadcast,
so the ops must be computed up front against a simulated list and issued in a
fixed sequence; recomputing from incoming state between calls would race the
broadcast. `clusterDropOps` therefore returns the whole op list, and the caller
awaits them in order and stops on the first failure rather than fanning out.
Clusters are small (a duplicated session or two), so the round-trips are
bounded by group size, but this is the one place the feature could visibly
half-apply.

### Files to change

1. `cmd/hivegui/frontend/src/components/Sidebar.tsx` — wrap the per-project
   sort at 496-498 in `clusterSessions()`; compute the project's worktree
   groups once and pass `groupSlot`/`groupSize` down through `ProjectItem` to
   each row; switch `reorderDroppedSession` (176-184) from `dropTargetIndex` to
   `clusterDropOps`, issuing its ops sequentially.
2. `cmd/hivegui/frontend/src/components/SessionRow.tsx` — accept
   `groupSlot: number | null` and `groupSize: number`; set
   `data-wt-group={groupSlot}` on the `<li>`; wrap the existing worktree
   `IconButton` (183-193) in a `.hv-session-row__worktree-slot` span alongside a
   `.hv-session-row__worktree-count`; extend the button label to state the
   sharing.
3. `cmd/hivegui/frontend/src/theme/components/session-row.css` — add
   `.hv-session-row[data-wt-group]::after` (right edge, 3px) filled with
   `var(--session-color)`, the wrapper's `grid-column: 4`, and the count pill
   (modelled on `.hv-project-card__ideas`, `project-card.css:129-152`).
4. `internal/registry/create.go` — in the adoption branch (340-352), inherit
   the adopted sibling's colour rather than calling `pickColor`, and still
   record it as `lastSessionColor`.
5. `internal/registry/create_test.go` — cover both halves of that.
6. (removed — the `--worktree-accent-*` token family, its `themes.css` values
   and the `ui-contrast.mjs` change are all dropped; the session colour
   replaces them.)
7. `cmd/hivegui/frontend/src/components/modals/Worktrees.tsx` — in
   `WorktreeRow`, resolve `session_ids` to names from the store and render them.
8. `cmd/hivegui/frontend/src/lib/worktrees.ts` — add a pure
   `sessionNames(w, sessions)` helper so the mapping is unit-testable.
9. `cmd/hivegui/frontend/test/dom/ui-session-row.test.tsx` — its `base` props
   object (lines 11-25) constructs `SessionRowProps` directly, so the two new
   fields must be added there (or given defaults in `SessionRow`) or
   `npm run typecheck` fails on this plan's own verification step. Sweep for any
   other direct `SessionRowProps` construction site and do the same.
10. `docs/design-docs/ui/components.md` and `patterns.md` — document the right
   edge as the worktree-group channel, next to the existing selection/attention
   rule.

### New files

- `cmd/hivegui/frontend/src/lib/worktree-groups.ts` — the whole grouping and
  cluster-drag calculus as pure functions: `worktreeKey`, `worktreeGroups`
  (project sessions to key to ids, groups of two or more only),
  `clusterSessions` (display order), `clusterDropOps` (drag to a list of
  `{id, order}` moves; delegates to `dropTargetIndex` when the dragged session
  is not in a group; returns `[]` when the drop target is inside the dragged
  cluster).
- `cmd/hivegui/frontend/test/unit/worktree-groups.test.ts`
- `cmd/hivegui/frontend/test/dom/shared-worktree-cue.test.tsx`
- `.changesets/<slug>.md` via `/hs-changelog-update` (user-visible change).

### Tests

- `test/unit/worktree-groups.test.ts`
  - `worktreeGroups` ignores empty paths, ignores solo occupants, groups two or
    more, and never groups across projects.
  - `clusterSessions` clusters at the lowest-order member and leaves ungrouped
    sessions in `.order` position.
  - `clusterDropOps` — table tests replayed against an inline `moveInOrder`
    replica, exactly the pattern in `test/unit/reorder.test.ts:22-30`. Asserts
    the final array has the cluster contiguous at the intended slot, for a drag
    up, a drag down, a drop at the end of a project, and a single-session drag
    (must equal `dropTargetIndex`'s answer). This is the test that would fail if
    the spec-305 delete-before-insert bookkeeping is wrong for any member.
- `test/dom/shared-worktree-cue.test.tsx` (shape of
  `test/dom/ui-session-row.test.tsx:1-70`)
  - two rows sharing a path both carry `data-wt-group` with the same value and
    a count of 2; a solo row carries neither; a row with no worktree renders no
    glyph at all; the worktree button's `aria-label` states the sharing.
- `test/dom/sidebar-reorder.test.tsx` — new case: dragging one member of a
  two-session cluster captures two `UpdateSession` calls whose resulting orders
  are contiguous.
- `test/dom/sidebar-reorder.test.tsx` — a second new case: when the first
  `UpdateSession` of a cluster drop rejects, the remaining ops are not issued.
  This is the one path the plan calls out as able to half-apply, so it gets the
  test that would catch it.
- `test/unit/worktrees.test.ts` — `sessionNames` resolves ids to names and drops
  ids with no live session.
- `internal/registry/create_test.go` — a session adopting a sibling's worktree
  inherits that session's colour; a plain create still gets a fresh colour from
  `pickColor`.
- `test/dom/` worktrees-modal case — a worktree with two occupants lists both
  names.

### Verification

```
scripts/ci-bootstrap.sh                 # generated wailsjs bindings
scripts/test.sh go unit dom
scripts/ui-lint.sh
scripts/ui-lint.sh --contrast
cd cmd/hivegui/frontend && npm run typecheck
CI=1 npx playwright test theme.spec.ts --update-snapshots   # baselines move
```

Each is non-vacuous: `--contrast` still guards the tokens the bar
sits against; the unit replay fails if any cluster op is
off by one; the dom tests fail if the bar, the count, or the label text is
missing. The Playwright theme snapshots
(`test/e2e/theme.spec.ts-snapshots/worktrees-*.png`) will need regenerating —
run Playwright with `CI=1` so it does not reuse a stale dev server.

## Decision log

- **2026-09-08** — Reorder slots enumerate painted BLOCKS, not rows. Why:
  review round 2 found a slot inside a group is not a position the list can
  hold — the next paint undoes it — so a move resolving there emitted zero ops
  and the gesture died silently. ⇧⌘↑ on a row beside a group was a permanently
  dead press; a drop into a group's interior now snaps past the block.
- **2026-09-08** — One reorder sequence at a time (`app/reorder-runner.ts`);
  concurrent presses are DROPPED, not queued. Why: a reorder is now a list of
  moves computed against a simulated list, so key auto-repeat would interleave
  two sequences into an order neither press asked for. A queue would replay a
  move computed against an order the user can no longer see.
- **2026-09-08** — `grid-layout.ts`'s `grid-project` scope routes through
  `clusterSessions` too. Why: `grid-all` reaches it via `orderedSessions()`,
  so leaving the project grid on raw `.order` would have this PR create the
  exact divergence its own "One order" rule forbids.

- **2026-09-08** — Review round 1 found the design flaw and the operator chose
  the root fix: `clusterSessions()` is now THE order. `orderedSessions()`
  clusters, so ⌘1-9, ⌘↑/⌘↓, the tray and the palette all follow the painted
  order; the operator independently hit this in the running app ("cmd up/down
  iterate not in the order I see"). Why: two orders was the bug — the drop slot
  was resolved in `.order` space while the user dragged in painted space.
- **2026-09-08** — Reorder targets are computed as a painted ARRAY and turned
  into daemon moves by `opsToReach`, which fixes the first disagreeing slot and
  simulates forward. Why: it removes hand-derived indices in the daemon's
  space, which is where the spec-305 off-by-one lived. Consequence: the emitted
  moves may name a sibling rather than the dragged session — same resulting
  order, sometimes one fewer round-trip.
- **2026-09-08** — Reordering *within* a group is supported on operator
  request, and the gesture picks which: dropping on a fellow member (or a
  reorder key with room left inside the group) moves the member; dropping
  outside the group (or a reorder key at the group's edge) moves the block.
  Why: dragging one member elsewhere while its group stayed put would be
  undone by the next paint, and edge-escalation keeps the key from ever being
  a dead press.
- **2026-09-08** — `lib/reorder.ts` (`dropTargetIndex`, `reorderTarget`) is
  deleted. Why: production-dead once both paths route through
  `worktree-groups.ts`, and keeping it would leave the slot math in two places.
- **2026-09-08** — CI `daemon-contract` is satisfied with the
  `daemon-contract-override` label rather than a `DaemonContract` bump. Why:
  operator call — the change alters a value inside an existing field, not the
  protocol, and a bump costs every user their running sessions.

- **2026-09-08** — The daemon's `pickColor` "differ from the last one" rule is
  preserved for adopted sessions by recording the inherited colour as
  `lastSessionColor`. Why: without it the next freshly-picked session could
  land on the group's colour and read as a member.
- **2026-09-08** — The keyboard reorder path (`reorderTarget`) is left
  cluster-unaware. Why: render-side clustering repaints any cluster it breaks,
  and a second copy of the cluster math is where spec 305's off-by-one would
  come back.
- **2026-09-08** — `clusterDropOps` returns an op list the caller issues
  sequentially, stopping on the first failure. Why: each `UpdateSession`
  re-broadcasts, so indices computed against a simulated list must be applied
  in order; a failed op leaves the group split rather than scattered.
- **2026-09-08** — The e2e Wails mock now mirrors the daemon's colour
  inheritance on worktree adoption. Why: a mock that kept picking its own
  colour would make the group link untestable in the browser layer.

- **2026-09-08** — Visual cue is a color-coded left accent bar per shared worktree plus a count badge on the existing worktree glyph. Why: operator choice at clarifying round A; works regardless of row adjacency and needs no layout change, unlike a nested group header.
- **2026-09-08** — Shared sessions are auto-grouped adjacent within their project and drag moves the whole cluster. Why: operator choice; predictable grouping was preferred over preserving arbitrary manual interleaving.
- **2026-09-08** — Surfaces: sidebar rows and the worktrees modal only. Grid tiles and minimized project chips are out of scope. Why: operator choice at clarifying round A.
- **2026-09-08** — "Shared" means two or more sessions with the same non-empty worktree path. Sessions sharing a plain project cwd are not marked. Why: operator choice; matches the daemon's existing `WorktreeShared` notion and avoids marking the common all-sessions-in-one-project case.

- **2026-09-08** — The shared-worktree bar goes on the **right** edge of the row. Why: `[data-selected]::before` owns the left edge and `patterns.md:5-12` forbids sharing a position between indicators.
- **2026-09-08** — Group colors come from new `--worktree-accent-1..6` theme tokens defined across all 19 presets. Why: operator chose correctness in every theme over cycling terminal-tuned `--ansi-*` slots; the cost is 19x6 token values plus contrast gating on the 6 default presets.
- **2026-09-08** — Adjacency is render-side clustering in `Sidebar.tsx:496-498` plus a cluster-aware drag that commits one `UpdateSession` per member. Why: no daemon change, and spec 305 non-goaled touching `moveInOrder`/`reindexLocked`; stored-vs-painted divergence is accepted and self-heals on the next drag.
- **2026-09-08** — No new wire field for "shared". Why: `worktree_path` is already broadcast, and the daemon's own `worktreeShared` is a transient kill-time bool, not session identity.

## Progress

- **2026-09-08** — Spec created, triaged M/P2, research dispatched.
- **2026-09-08** — Plan approved (round 2), colour source changed to the
  session colour on operator review.
- **2026-09-08** — Implemented on `feature/384-shared-worktree-cue`. Go, unit,
  dom and e2e layers green; `ui-lint` and `--contrast` clean; typecheck clean.
- **2026-09-08** — Review iteration 3: APPROVE, no BLOCKING or IMPORTANT
  findings, zero threads. Its two MINORs (a `title` on the truncated occupant
  list, a reset hook for the runner's module-level in-flight flag) were fixed
  after the approving review.
- **2026-09-08** — Review iteration 2 escalated on a BLOCKING defect in the
  round-1 rewrite (row-shaped slot space) plus 6 IMPORTANT. Fixed: block-shaped
  slots, in-flight guard, grid-project clustering, and the three missing test
  classes — the branchless glyph, the lowest-Order colour tiebreak (verified
  red against a map-order pick), and interleaved-project fixtures. The last was
  the reason the BLOCKING survived round 1: every fixture was
  project-contiguous, so no test placed a non-member beside a group.
- **2026-09-08** — Review iteration 1 escalated (5 IMPORTANT, all from the
  two-orders flaw plus two independent ones). Reworked: one order, cluster-aware
  keyboard reorder, within-group reordering, branchless-worktree cue,
  deterministic inherited colour, `reorder.ts` retired.
- **2026-09-08** — Open question 2 (right-edge crowding) resolved in a real
  browser, not by reasoning: `test/e2e/shared-worktree-cue.spec.ts` asserts the
  `::after` computes to 3px of the session colour, that both members resolve to
  the same fill, and that a hit test at the row's right edge still lands inside
  the row. jsdom cannot see any of that.

## PR convergence ledger

_Append-only. One line per `/hs-review-loop` iteration._

- **2026-09-08 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: be491758.
- **2026-09-08 iter 2** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: daa044659e4b5aa9a12f386bec767443031f773454607c84792c992a3f4fc1bc; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: dabc4108.
- **2026-09-08 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 9c0cd8fd72897015973cce62cd79f3a7be61862f458e2c511b346df4d116ead7; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: 2b87a834.

## Open questions

**Resolved — the colour source.** The operator's review note replaced the
planned `--worktree-accent-1..6` token family with the existing per-session
colour, inherited on worktree adoption. That deletes the token definitions, the
per-preset values, the `ui-contrast.mjs` `var()`-resolution change and its
`PAIRS` entries, the slot-assignment rule and its tests, and spec success
criterion 7, which existed only to gate those tokens. Costs are recorded in
Approach.

**Resolved — right-edge crowding.** Confirmed in a real engine rather than by
reasoning about CSS: `test/e2e/shared-worktree-cue.spec.ts` reads the computed
`::after` (3px, session colour, same fill on both members) and hit-tests the
row's right edge. The bar is not clipped, and `::after` paints above the row's
in-flow children, so the swatch that shares that end cannot cover it.

## Second opinion

- **Round 1 — `revise`, confidence 7.** Approach maps onto every success
  criterion and the verification commands are real; the gap was a typecheck
  break the plan's own verification would have hit.
  - Applied: `test/dom/ui-session-row.test.tsx` builds `SessionRowProps`
    directly (lines 11-25), so the two new props must be added there or
    defaulted — now item 9 in Files to change.
  - Applied (nice-to-have): dropped the wrong preset-block count in favour of
    "every `[data-theme=...]` block"; added the cluster-drop partial-failure
    test to the Tests section.
- **Round 2 — `revise`, confidence 7.** Confirmed round 1's fix landed and that
  every verification command and flag is real (`--contrast` at
  `scripts/ui-lint.sh:52-53`). Its one must-fix was that Open question 1 was
  still unresolved, and the ANSI-derived option as written would leave the new
  tokens ungated, failing success criterion 7.
  - Applied: resolved the question with a third option — derive the tokens from
    ANSI once, teach `ui-contrast.mjs` one level of `var()` resolution so they
    are genuinely gated, and hex-override only the presets that fail 3:1.
  - Applied (nice-to-have): the Playwright snapshot regeneration is now a line
    in the Verification block, not prose under it.
  - Not applied: Open question 2 stays open by design — it is a look-at-it-in-
    the-app check during implementation, not a plan-time decision.
- **Operator review, round 2 — approved.** The operator's one note ("maybe this
  uses the session color, and each session in a group shares the color")
  replaced the entire token workstream with a few lines in the adoption branch.
  Adopted, with its three costs stated in Approach.
