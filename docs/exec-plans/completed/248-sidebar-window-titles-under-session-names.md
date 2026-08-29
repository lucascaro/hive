# Show terminal window titles under session names in the sidebar

- **Spec:** [docs/product-specs/248-sidebar-window-titles-under-session-names.md](../../product-specs/248-sidebar-window-titles-under-session-names.md)
- **Issue:** —
- **PR:** #288
- **Branch:** feature/248-sidebar-window-titles
- **Stage:** DONE (merged 2026-08-29, shipped in v2.4.0)
- **Status:** completed

## Summary

Surface each session's OSC-set terminal window title in the sidebar, on a second line under the session name. The title is sourced daemon-side from the VT mirror every session already runs, carried on `wire.SessionInfo` as a new `title` field, and rendered by the sidebar as a dim secondary line. The daemon side is the whole point: `state.terms` in the GUI is populated lazily, so a frontend-only source would be blank for exactly the sessions the user has not opened.

## Research

### The title is already parsed — nobody reads it

- `internal/session/vt.go:71` — `VT` wraps `vt10x.Terminal`. `vt.go:118` (`Write`) feeds raw PTY bytes in; its doc comment says vt10x "silently swallows sequences it doesn't model (mouse, OSC titles, bracketed paste etc)". That comment is wrong about titles: `vt10x` **does** model them — `Terminal.Title() string` (vt.go:39 in the dep) backed by `State.setTitle` (state.go:739), driven from the OSC handler (str.go:70, str.go:143). The comment needs correcting as part of this work.
- `internal/session/session.go:205` — `deliver()` calls `s.vt.Write(p)` for **every** session, on the daemon's `readLoop` goroutine, with no dependency on whether a client is attached. That is what makes a daemon-side title free of the lazy-attach problem.
- `deliver` holds `s.mu` across the VT write *and* the sink fanout, deliberately (see its comment: it is what makes `SubscribeWithAtomicReplay` atomic). Anything this feature adds must not call into the registry while that lock is held — `registry` already calls into `session`, so a `session.mu → registry.mu` edge would be a new lock-ordering hazard. `readLoop` (`session.go:~178`) is the natural place to fire a notification, immediately after `deliver` returns and `s.mu` is free.

### The wire + broadcast path, and its exact precedent

`SessionInfo.Phase` is the same shape of problem as `Title` — daemon-owned, in-memory only, never persisted, pushed to clients on change — so it is the template to copy rather than invent against.

- `internal/wire/control.go:106` — `SessionInfo`. `Phase` at :122 is `json:"phase,omitempty"`, documented at :125-147 as in-memory only so "a daemon restart can never strand a session in a transient one". `Title` gets the same treatment.
- `internal/registry/registry.go:135` — `Entry.Info()` is the **single** place a `wire.SessionInfo` is built for real. Every other `wire.SessionInfo{...}` literal in the tree is a test fixture (`testclient/client_test.go`, `persist_logging_test.go`), so an additive `omitempty` field needs no changes in the other two wire clients (`cmd/hivegui/app.go`, `cmd/hived-ws-bridge/main.go`) — they pass `SessionInfo` through, they don't construct it. AGENTS.md's "update all three clients in lock-step" rule is about protocol semantics; this is a purely additive field.
- `internal/registry/registry.go:208` — `setPhase(id, phase)`: takes `r.mu`, **returns early if the value is unchanged**, writes the field, then `broadcastLocked(wire.SessionEventUpdated, info)`. The unchanged-guard is the pattern the title notifier needs (a spinner that re-sets the same title must not broadcast).
- `internal/registry/events.go:57` — `broadcast` builds `wire.SessionEvent{Kind, Session}` and pushes to every `Listener`; `internal/daemon/daemon.go:521` writes it out as `FrameSessionEvent`.
- `internal/registry/registry.go:97` — `Entry.Phase` is a stored field, which forces write-and-clear discipline at each of the four `e.sess = …` sites (`create.go:501`, `registry.go:552`, `registry.go:602`, `registry.go:668`). **The title does not need to be stored**: `Info()` can read through to `e.sess.Title()`, which makes "no session ⇒ no title" fall out for free and removes all four clear-on-death/restart sites from the diff.

### GUI side: the render path is the risk, not the read

- `cmd/hivegui/frontend/src/app/sidebar.ts:279` — `renderSession()` builds the row: `dot`, `name`, optional worktree `glyph`, `swatch`, appended flat into a `li.session-item`.
- `cmd/hivegui/frontend/src/style.css:271` — `.session-item { display: flex; align-items: center; gap: 8px; … }`. A second line means wrapping `name` (and the new title) in a column-flex child; `dot`, `glyph` and `swatch` must stay vertically centered against the taller row.
- `style.css:365` — `.session-item .name` carries the project→session gradient via `background-clip: text; color: transparent`, plus `white-space: nowrap; overflow: hidden; text-overflow: ellipsis`. The wrapper must preserve `min-width: 0` down the chain or the ellipsis stops working and the row overflows.
- `style.css:619` — `.term-host .tile-term-title` is the existing visual vocabulary for this exact string in the grid header: `11px`, `rgba(255,255,255,0.55)`, nowrap + ellipsis, `::before` em-dash separator. The sidebar treatment should read as the same idea one level quieter, not as a new invention.
- `cmd/hivegui/frontend/src/app/session-term.ts:1061` — `_renderTermTitle()` already defines the suppression rule: hide the slot when the title is empty **or equals the session name** (avoids "foo — foo"). The sidebar must use the same rule, and it belongs in a shared helper rather than being written twice.
- **The risk:** `events.ts:391` calls a full `renderSidebar()` on every `session:event(updated)`, and `renderSidebar()` (`sidebar.ts:47`) does `projectsUL.innerHTML = ''` and rebuilds every row and listener. Title changes are frequent (an agent TUI rewrites its title as it works), so routing them through that path would thrash the sidebar and destroy DOM nodes mid-interaction — `sidebar.ts:56-61` already documents a dblclick bug caused by exactly that rebuild.
- The fix has a precedent in the same file: `updateSidebarSelection()` (`sidebar.ts:62`) patches classes on existing nodes instead of rebuilding, for exactly this reason. A title-only update should take the same in-place route.

### Constraints / dependencies

- Layer rule (`DESIGN.md`, restated in AGENTS.md): `wire → session/agent/worktree → registry → daemon`. Registry may import session; session must not import registry. The notification therefore has to be a callback or channel the registry installs on the session, not a registry call from inside `session`.
- Wire JSON is `snake_case` in Go tags; JS readers use `snake_case ?? camelCase` at the boundary (AGENTS.md, and the local memory note on this repo).
- AGENTS.md **Status Visibility**: "Status dots/badges must appear on every session row — never omit them to save space." The two-line layout must not push the dot, worktree glyph, or swatch off the row.
- Tests: Go tests beside source; frontend DOM tests under `cmd/hivegui/frontend/test/dom` (the sidebar tree is explicitly listed as covered there). `scripts/test.sh` runs `go · unit · dom · e2e`.

## Approach

The daemon already computes the title and throws it away. Add the read, a change
signal, and a wire field; then render it.

**Read-through, not a stored field.** The obvious alternative is an `Entry.Title`
field mirroring `Entry.Phase`. Rejected: `Phase` pays for that field with
write-and-clear discipline at four `e.sess = …` sites. `Entry.Info()` is the only
real construction site of a `wire.SessionInfo`, so it reads through to
`e.sess.Title()` instead — nil session yields an empty title, and death/restart
clear themselves with no extra code.

**Lock ordering.** `deliver()` holds `s.mu` across the VT write *and* the sink
fanout on purpose (its comment explains why: it is what makes
`SubscribeWithAtomicReplay` atomic). The title hook therefore fires from
`readLoop` *after* `deliver` returns, so the goroutine takes `r.mu` with no
session lock held. `registry` imports `session` and never the reverse, so there
is no cycle. This gets a comment at the call site.

**Throttle.** Some TUIs animate the title (`⠋ Working…`), and every change costs a
socket frame plus a Wails IPC hop plus a JSON parse per connected client.
`session` coalesces to at most one hook call per 500 ms with a trailing fire, so
the final title always lands. `setPhase`'s unchanged-guard is reused first: a
re-set of the identical string never fires at all.

**Render path.** `events.ts` currently calls a full `renderSidebar()` (an
`innerHTML` wipe and rebuild of every row and listener) on every
`session:event(updated)`. Title changes are frequent, so an update that differs
from the cached session *only* in `title` routes to a new in-place
`updateSidebarTitles()` instead — the same technique `updateSidebarSelection()`
already uses in that file, and for the same reason.

### Files to change

1. `internal/session/vt.go` — add `(*VT).Title()` under `v.mu`; fix the `Write`
   doc comment that wrongly claims vt10x swallows OSC titles.
2. `internal/session/session.go` — `title` field, `SetTitleHook(func(string))`,
   `Title()`. In `readLoop`, after `deliver`, diff `vt.Title()` against the cached
   value and fire the throttled hook outside `s.mu`.
3. `internal/wire/control.go` — `Title string `+'`'+`json:"title,omitempty"`+'`'+`` on
   `SessionInfo`, documented as daemon-owned and in-memory only, mirroring `Phase`.
4. `internal/registry/registry.go` — `Entry.Info()` reads through to
   `e.sess.Title()`, capped at 256 chars (untrusted input from the child process);
   add `noteTitleChange(id)` broadcasting `SessionEventUpdated`.
5. `internal/registry/create.go:501`, `internal/registry/registry.go:552` —
   install the hook at the two `e.sess = sess` sites.
6. `cmd/hivegui/frontend/src/app/state.ts` — `title?: string` on `SessionInfo`.
7. `cmd/hivegui/frontend/src/app/sidebar.ts` — `renderSession` wraps `name` plus a
   new `.session-title` in a column `.session-text` div; export
   `updateSidebarTitles()` patching text in place.
8. `cmd/hivegui/frontend/src/app/events.ts` — route title-only updates to
   `updateSidebarTitles()` instead of `renderSidebar()`.
9. `cmd/hivegui/frontend/src/app/session-term.ts` — `_renderTermTitle` switches to
   the shared helper so the suppression rule lives in one place.
10. `cmd/hivegui/frontend/src/style.css` — `.session-text` column, `.session-title`
    at 11px / `rgba(255,255,255,0.42)` / nowrap-ellipsis, `min-width: 0` threaded
    through so `.name`'s gradient and ellipsis survive; `.session-item` keeps
    `align-items: center` so the dot, worktree glyph and swatch stay centered on
    the taller row; `.session-item.dead .session-title { display: none }`.
11. `.changesets/248-sidebar-window-titles.md` — user-visible change.

### New files

- `cmd/hivegui/frontend/src/lib/term-title.ts` — `displayTitle(title, name)`
  returns `''` when the title is empty or equals the session name. That rule
  exists once today (`session-term.ts:1061`) and is about to be needed twice.

### Tests

- `internal/session/vt_test.go` — `TestVTTitleFromOSC`: `\x1b]0;hello\x07` and the
  `\x1b]2;…\x1b\\` ST form both land in `Title()`; no OSC yields `""`.
- `internal/session/session_test.go` — `TestSessionTitleHookFiresOnChange`: hook
  receives the new title; re-setting the identical string does not fire again.
- `internal/registry/registry_test.go` — `TestInfoCarriesSessionTitle`,
  `TestInfoTitleEmptyWithoutSession`, `TestInfoTitleTruncated` (>256 chars).
- `internal/registry/` — `TestTitleChangeBroadcastsUpdated`: a title change reaches
  a `Subscribe` listener as `SessionEventUpdated`.
- `cmd/hivegui/frontend/test/unit/term-title.test.ts` — the three `displayTitle`
  branches.
- `cmd/hivegui/frontend/test/dom/sidebar-title.test.ts` — renders the title under
  the name; omits the line when absent; omits it when it equals the name; a
  title-only update **preserves the `li` node identity** (the regression guard for
  the render-thrash risk).

## Decision log

- **2026-08-28** — Source the title daemon-side rather than from the GUI's existing `SessionTerm.termTitle`. Why: `state.terms` is populated lazily (`view.ts:82`, `renderGrid`), so in single view it is empty for precisely the unopened sessions the spec cares about, and it resets on every GUI restart.
- **2026-08-28** — Accept that a program cannot clear its title back to empty. vt10x drops empty OSC titles (`if title != ""` in its handleSTR), so the last non-empty value sticks for the life of the PTY. Why: the alternative is forking the OSC parser to gain a rare edge case; a finished session is instead detected by its process being gone, which the read-through already handles. Documented on `VT.Title` and asserted in `TestVTTitleFromOSC`.
- **2026-08-28** — Cover the two-line layout with a Playwright spec (`test/e2e/sidebar-window-title.spec.ts`), not just jsdom. Why: every claim this feature makes is a CSS claim, and jsdom computes no styles — the DOM tests pass whether or not the title actually renders below the name.
- **2026-08-28** — Give title changes their own wire kind, `SessionEventTitle`, instead of riding `SessionEventUpdated`. Why: `updated` means "the daemon's view of the session changed" — a rename, reorder, phase transition or death — and every consumer treats it as authoritative state worth a full re-render. A title change is neither authoritative nor rare; it is the child process redrawing at its own rate. Sharing the kind forced three separate workarounds (two Go tests that assert on the event stream, plus a `titleOnlyChange` object-differ in the frontend) and left the stream nondeterministic for any future consumer. The dedicated kind reverts all three and is additive: a client that does not know it ignores it and shows no titles. SUPERSEDES the two test-patching entries below, which were symptom fixes for this.
- ~~**2026-08-28** — Assert order-invariance, not event-absence, in `TestCreateAppendEmitsNoShiftedUpdates`.~~ Why: a session now broadcasts whenever the program on its PTY re-titles itself, and a shell does that from its prompt — so "a plain append emits no events for siblings" is no longer a true invariant, while "a plain append shifts nobody's order" (the test's actual name and intent) still is. Caught by CI on Linux, where bash titles itself and macOS's did not.
- ~~**2026-08-28** — Run `cat`, not `/bin/bash`, in `TestTitleChangeBroadcastsUpdated`.~~ (Kept in spirit: the end-to-end test still runs `cat` and now re-sends the OSC on a loop, because `Cmd` is spawned under a login shell and a single early write can lose the startup race.) Why: a shell re-titles itself after every command, so two writers race into one 500 ms coalesce window and the prompt's title can legitimately swallow the test's — correct throttle behavior, but it made the test nondeterministic (it passed on macOS, timed out on Linux).
- **2026-08-28** — Throttle title-change broadcasts to 500 ms (trailing) in `session`. Why: a TUI that animates its title would otherwise emit a socket frame + Wails IPC hop + JSON parse per frame, per connected client.
- **2026-08-28** — Read through to `e.sess.Title()` in `Entry.Info()` instead of storing `Entry.Title`. Why: nil-session and restart cases clear themselves, removing four write/clear sites that the `Phase` field needs.

## Progress

- **2026-08-28** — Spec + exec plan created; triage approved (enhancement / M / P2). Stage = RESEARCH.
- **2026-08-28** — Research approved; approach drafted and approved. Stage = IMPLEMENT.
- **2026-08-28** — Confirmed `TestKill_DirtyWorktree_FrameError` fails identically on `origin/main` (3/3) under machine load: its 3s budget for `git worktree add` is environment-sensitive. Pre-existing, not from this branch.
- **2026-08-28** — Implemented on `feature/248-sidebar-window-titles`. All AGENTS.md checks pass: `gofmt`, `go vet`, `go test ./internal/...`, `biome ci`, `tsc --noEmit`, and `scripts/test.sh` (go · unit · dom · e2e — 187 e2e passed, 1 skipped).
- **2026-08-28** — Filter title events inside `phaseLog.add` (the shared helper behind every phase test) rather than in each assertion. Why: title events carry a Phase like any other event but arrive asynchronously and unpredictably, so one filter at the recording site keeps every phase test reading as a pure lifecycle sequence. The `TestCreateAppendEmitsNoShiftedUpdates` skip is the same idea in the one place that does not use the helper. This is what the dedicated event kind buys: consumers that care about lifecycle can now say so.
- **2026-08-28** — Reproduce the Linux-only failures locally with `PROMPT_COMMAND='printf "\033]0;%s\007" "$PWD"'`. Why: the difference between the two CI legs is simply that the Linux runner's bash titles itself from its prompt and this macOS shell does not — with that env var set, macOS reproduces the failures exactly, which turns a push-and-see loop into a local one. `noteTitleChange` is driven directly (deterministic, asserts the kind and the dead-entry silence) and only one test goes through a real PTY. Why: asserting on a title that a shell is simultaneously overwriting is inherently racy, and `internal/session` already covers OSC bytes reaching `Title()`.
- **2026-08-28** — CI on Linux caught two failures macOS did not: `TestCreateAppendEmitsNoShiftedUpdates` (a real consequence of the new broadcast traffic) and `TestTitleChangeBroadcastsUpdated` (nondeterministic against a self-titling shell). Both fixed at the root; re-verified with `-count=2` on the registry/daemon/session packages and `-race` on session/registry.
- **2026-08-29** — PR #288 merged (`4e76d3b`); shipped in v2.4.0. Plan moved to `completed/`.

## Notes

- To exercise this branch's event traffic the way CI's Linux runner does, run the Go tests with a self-titling prompt:
  `PROMPT_COMMAND='printf "\033]0;%s\007" "$PWD"' go test ./internal/...`
  Without it, macOS's shell sets no title and the title-broadcast paths stay dormant.

## PR convergence ledger

- **2026-08-28 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; threads_open: 0; action: fix+push (Linux CI: TestCreateAppendEmitsNoShiftedUpdates, TestTitleChangeBroadcastsUpdated); head_sha: 761cdec.
- **2026-08-28 iter 2** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; threads_open: 0; action: redesign+push (title moved to its own SESSION_EVENT kind rather than patching more consumers); head_sha: fa6efde.
- **2026-08-28 iter 3** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; threads_open: 0; action: fix+push (filter title events in phaseLog; Linux condition reproduced locally via PROMPT_COMMAND); head_sha: 71bf3b7.
- **2026-08-28 iter 4** — verdict: APPROVE; mergeable: MERGEABLE; threads_open: 0; action: stop (all checks green on Linux, macOS and Windows; Greptile pass; no unresolved threads); head_sha: 71bf3b7.

## Open questions

- 500 ms is a guess at "imperceptible" for the throttle. If it reads laggy in the real app, drop to 250 ms — it is one constant (`titleThrottle` in `internal/session/session.go`).
- Noticed while validating, out of scope here: `#projects` already overflows horizontally by ~28 px at rest, with no titles involved. The new spec asserts the title adds nothing to that figure rather than asserting it is zero. Worth its own issue.
