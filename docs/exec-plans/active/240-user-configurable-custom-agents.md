# Add user-configurable custom agents

- **Spec:** [docs/product-specs/240-user-configurable-custom-agents.md](../../product-specs/240-user-configurable-custom-agents.md)
- **Issue:** —
- **PR:** #242
- **Branch:** feature/240-user-configurable-custom-agents
- **Stage:** REVIEW
- **Status:** active

## Summary

Let users define their own agents (e.g. a `claude-lite` running `claude --model haiku`) without editing Go. Definitions live in `agents.json` under the state dir and are edited through a new Settings modal opened with a native **⌘,** menu item. The design merges custom definitions at the `agent.Get()`/`agent.All()` chokepoint, which is what makes this cheap: every consumer — launcher, session create, revive, restart — already funnels through those two accessors, so listing and launching come for free and `launcher.js` needs no changes at all.

## Research

Authored via plan-first mode; the following was established by reading the code.

**The chokepoint.** `defsByID` (`internal/agent/agent.go:71`) is read only through `Get()` (`agent.go:160`) and `All()` (`agent.go:166`). Consumers: the GUI dropdown via `ListAgents` (`cmd/hivegui/app.go:355`), session launch via `registry.Create` (`internal/registry/registry.go:407`), and respawn via `Revive` (`registry.go:610`) / `Restart` (`registry.go:710`).

**Three constraints found by inspection:**

1. **Import cycle.** `internal/registry` imports `internal/agent`, so `agent` cannot call `registry.StateDir()` (`internal/registry/paths.go:18`). The config dir must be injected, not imported.
2. **Persistence stores only the ID.** `internal/registry/persist.go:18` persists `Agent string` — the command is re-resolved through `agent.Get` on every revive. This means (a) the daemon needs the config at revive time, and (b) an agent's ID must be stable across a rename or existing sessions break.
3. **Single window.** Wails v2.12 is one webview per process; this app's `OpenNewWindow` (`cmd/hivegui/app.go:513`) re-execs a whole new Hive process. A true second OS window would need a `--settings` process mode plus IPC.

**Existing patterns to reuse:** `project-editor.js` (form modal shape, focus-callback injection), `modals/registry.js:11` (`registerModal`), `menu_darwin.go:22` (native menu items emitting `menu:*` events, dispatched at `keyboard.js:333`). Note `menu_other.go:22` deliberately returns nil on Windows/Linux to avoid the documented accelerator double-fire.

**No shell-word splitter exists anywhere in the repo,** and `Def.Cmd` is already `[]string`.

## Approach

Merge custom definitions into the two accessors; everything downstream is unchanged.

Storage is `agents.json` in the state dir, read through a package-level cache keyed on mtime+size. This is chosen over a reload-notification wire frame because it needs no IPC at all — `hivegui` writes the file and `hived` picks it up on its next `agent.Get` — and it survives a daemon restart for free, which the persistence model (constraint 2 above) requires.

### Files to change

- `internal/agent/agent.go` — `Get()` checks built-ins first then customs; `All()` returns built-ins in `displayOrder` followed by customs in file order. `Available()` (`agent.go:62`) already handles customs correctly via `LookPath`.
- `cmd/hived/main.go` — `agent.SetCustomDir(stateDir)` beside the existing `registry.StateDir()` call at line 45.
- `cmd/hivegui/app.go` — `SetCustomDir` at startup; new bindings `ListCustomAgents()` and `SaveCustomAgents()` (atomic temp-file + rename). JSON tags camelCase to match `AgentInfo` (`app.go:347`).
- `cmd/hivegui/menu_darwin.go` — `Settings…` with `keys.CmdOrCtrl(",")` emitting `menu:settings`. Landed in the **File** menu, not the App menu — see the Decision log for why Wails v2 makes the App menu unreachable.
- `cmd/hivegui/frontend/index.html` — Settings modal markup, mirroring the `project-editor` block.
- `cmd/hivegui/frontend/src/app/keyboard.js` — `menu:settings` action entry plus the Cmd/Ctrl+, binding (the latter is the only path on Windows/Linux).
- `cmd/hivegui/frontend/src/bridge.js` — re-export the new bindings. Must stay a sibling of `main.js` (see its header comment) for the Playwright harness's vite substitution. Regenerate `wailsjs/`.
- `cmd/hivegui/frontend/test/wails-mock.js` — stub the new bindings.
- `CHANGELOG.md` / `.changesets/` — user-visible change.

### New files

- `internal/agent/custom.go` — `customDef` JSON struct, `SetCustomDir`, mutex-guarded mtime cache, validation, merge helpers.
- `internal/agent/custom_test.go` — unit coverage.
- `cmd/hivegui/frontend/src/app/modals/settings.js` — the Settings modal.
- `cmd/hivegui/frontend/test/dom/settings.test.js` — modal coverage.

### Tests

Go (`internal/agent/custom_test.go`, `t.TempDir()` per AGENTS.md:132):
- `TestLoadCustomAgents` — valid file merges into `All()`; `Get()` resolves.
- `TestCustomAgentMalformedFileFallsBackToBuiltins` — invalid JSON yields built-ins, no panic.
- `TestCustomAgentMissingFileIsNotAnError`
- `TestCustomAgentCannotShadowBuiltin` — `id: "claude"` skipped; built-in keeps its `ResumeArgs`.
- `TestCustomAgentSkipsInvalidEntries` — empty id, empty cmd, duplicate id dropped; valid siblings survive.
- `TestCustomAgentReloadsOnFileChange`
- `TestCustomAgentConcurrentAccess` — parallel `Get`/`All` during file change; fails under `-race` if the mutex is missing.
- `TestSaveCustomAgentsRejectsInvalid` — save-side validation errors and leaves the file untouched.

Go functional (`internal/registry/`): swap `startSession` (the seam at `registry.go:35`); assert a session created with a custom agent ID spawns the configured argv, and that `Revive` re-resolves from an entry persisting only the ID.

Frontend (`cmd/hivegui/frontend/test/dom/settings.test.js`): open/close, add/edit/delete rows, exact `SaveCustomAgents` payload shape. Belongs in `test/dom/` because `test/unit/` mirrors `src/lib/` only.

## Decision log

- **2026-07-18** — Merge custom defs at `agent.Get`/`agent.All` rather than adding a parallel registry. Why: those two accessors are the sole read path for every consumer, so the launcher, create, revive, and restart all work with no changes downstream.
- **2026-07-18** — Inject the config dir via `SetCustomDir(dir)` instead of extracting `StateDir` into a leaf package. Why: `registry` imports `agent`, so importing back would cycle; injection is a far smaller diff than moving `StateDir` and updating its callers.
- **2026-07-18** — mtime-keyed cache instead of a reload wire frame. Why: no IPC needed, and it survives daemon restart, which the ID-only persistence model requires anyway.
- **2026-07-18** — Custom agent IDs are slugged at create time and never recomputed on rename. Why: `persist.go:18` stores only the ID, so a rename that changed the ID would silently break revive for every existing session of that agent.
- **2026-07-18** — Built-ins win ID collisions. Why: built-ins carry `ResumeArgs`/`CaptureSessionIDFn` Go funcs that JSON cannot express; shadowing one would silently break ⇧⌘R resume.
- **2026-07-18** — Validate at save time and return an error, not just skip-and-log at load. Why: a warning in `hived.log` is invisible to someone using the GUI; rejection must surface where the user is looking. Load-time skipping remains as the guard for hand-edited files.
- **2026-07-18** — In-window modal behind a native menu item, not a separate process in settings mode. Why: Wails v2 single-window means a real OS window costs a process mode plus IPC; ⌘, is genuinely native either way.
- **2026-07-18** — Command entry is one field split on whitespace. Why: no shell-word splitter exists in the repo and writing one is not worth it; `agents.json` stores a real array so spaces-in-args stay hand-editable. Ceiling marked with a `ponytail:` comment at the split site.
- **2026-07-18** — `slugify` keeps Unicode letters/digits rather than ASCII only. Why: a unit test showed the ASCII version mangled "Ünïcödé Tool" into "n-c-d-tool" and would reduce a name in a non-Latin script to nothing. An id is only a map key and a JSON string, so there is no reason to restrict it.
- **2026-07-18** — Escape/keyboard ownership for the settings modal lives in `keyboard.js`'s modal gate chain, not in the modal's own listener. Why: a Playwright test caught that the modal's listener never fires when focus is still on the terminal. Follows the same pattern as the help overlay, which also focuses an element inside the dialog on open. Tab is deliberately left alone (unlike the help overlay's trap) because settings is a form with many focusable inputs.
- **2026-07-18** — `SaveCustom` assigns ids in Go, not in the frontend. Why: keeps the "assign once, never recompute" rule in one place, and the modal never has to know the slug algorithm.

## Progress

- **2026-07-18** — Plan-first scaffold; Stage = IMPLEMENT.
- **2026-07-18** — Backend complete: `internal/agent/custom.go` (mutex-guarded mtime cache, validation, atomic save), merged into `Get`/`All`, wired into `cmd/hived/main.go` and `NewApp`.
- **2026-07-18** — Frontend complete: settings modal, native ⌘, menu item, keyboard gate, command-palette entry, help-overlay entry via `lib/shortcuts.js`, CSS.
- **2026-07-18** — Tests: 13 Go unit (`internal/agent`), 4 functional (`internal/registry`), 3 binding-layer (`cmd/hivegui`), 2 daemon e2e (`cmd/hived`, `-tags=e2e`), 12 jsdom (`test/dom/settings.test.js`), 8 Playwright (`test/e2e/settings.spec.js`). All pass under `-race`.
- **2026-07-18** — Verified: `./build.sh` produces a universal .app; `go vet` clean; `go test -race ./...` and `-tags=e2e` green; 195 vitest + 66 Playwright pass. Daemon e2e confirms a custom agent's command actually runs in the PTY and that a corrupt `agents.json` leaves the daemon usable.

- **2026-07-18** — "Settings…" lives in the **File** menu, not the macOS app menu. Why: Wails v2 builds the app menu entirely in Objective-C from a role enum (`WailsMenu.m` `appendRole`); `processMenuItem` (`darwin/menu.go:116`) returns as soon as it sees `Role != 0`, so an appended item is never traversed — verified in the vendored source, not assumed. Hand-building the app menu would forfeit Hide / Hide Others / Show All, which need selectors Go cannot invoke. Operator chose File over a one-item top-level menu.
- **2026-07-18** — Tab wraps at the dialog boundaries rather than pinning to one control. Why: `aria-modal="true"` promises focus stays inside, but settings is a form — the help overlay's single-element pin would make the fields unreachable. Without the wrap, Tab past Save lands on a terminal behind the backdrop and keystrokes leak into it.
- **2026-07-18** — Blank/unsluggable names report a *name* error, not "missing id". Why: the id is assigned by Go and never shown in the form, so blaming it sends the user hunting for a field that does not exist.

## PR convergence ledger

Append-only. One line per `/hs-review-loop` iteration.

- **2026-07-18 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 995dded5; threads_open: 0 (was 7); action: autofix+push; head_sha: 7e703c4. BLOCKING: a malformed `agents.json` rendered as an empty list, so Save wrote `[]` back over it — silent, total data loss on a file the feature invites users to hand-edit. All four reviewer dimensions converged on it independently.
- **2026-07-18 iter 2** — verdict: REQUEST_CHANGES (rubric said COMMENT; coerced by `mergeable: CONFLICTING`); findings_hash: 255eafb5; threads_open: 0; action: merge+autofix+push; head_sha: c93aa03. No BLOCKING — iter 1's data-loss fix holds and the hash moved, confirming convergence. Merged `main` (CHANGELOG union; `shortcuts.js` kept the Settings row and took main's new `? or /` label). Fixed a comment in `keyboard.js` that asserted the opposite of the App-menu constraint the feature is built around, plus `:disabled` and `:focus-visible` affordances for the settings buttons. Deferred as risky: `openSettings()` has no already-open guard, so **File ▸ Settings…** while the modal is open wipes the in-progress draft (`menu:settings` bypasses the keydown gate).
- **2026-07-18 iter 2 — orchestrator corrections.** Two claims in the iter-2 escalation did not survive checking, recorded here so the ledger is not a misleading record:
  1. **"CI failure is pre-existing on main, inherited via the merge."** Not the whole story. `e5e1cb6` already contained that merge (and therefore `df19ee3`) and passed CI on Linux, macOS **and** Windows. `d70092f` fails — and its entire diff over `e5e1cb6` is one line of this markdown file. Identical code, different result: these are **flakes**, not an inherited breakage. (`scroll-codex.spec.js:203` does also fail on main at `df19ee3`, so that one is genuinely fragile upstream — but it is not what made this branch red.)
  2. **The macOS failure was misattributed.** macOS did not fail on `scroll-codex`; it failed on **our own** `settings.spec.js` "⌘, opens settings, Esc closes it" in the mock-Wails suite. Root cause found and fixed: the test typed immediately after `toBeHidden()`, but `closeSettings` restores focus through `setFocusedTile`, which defers the real `focus()` into a `requestAnimationFrame` retry chain (`src/app/focus.js:120`). The CI page snapshot showed focus *had* landed by failure time — the keys were simply sent into the gap. `ux-polish.spec.js:23` already carries a guard for this exact race with a comment naming the same cause, which is independent confirmation; our test just lacked it.
- **2026-07-18 iter 2 — CI verdict settled empirically.** Re-ran both failed jobs on the *unchanged* commit `d70092f`. macOS flipped failure → **success**; Linux failed again but on a **different** set of tests (`wheel-scroll.spec.js:215` + `:228`, where the first run had `scroll-codex.spec.js:203` + `wheel-scroll:228`). A failing set that changes across runs of identical code is the definition of flake. Conclusion: the `e2e-real` suite (real daemon, timing-sensitive) is unstable on Linux independently of this PR — `scroll-codex.spec.js:203` also fails on `main` at `df19ee3` — and none of it is caused by this branch. Worth a separate issue; it will intermittently redden any PR.
- **2026-07-18 iter 2 follow-up** — the deferred `openSettings()` guard was **fixed**, not left open: on macOS the native accelerator consumes ⌘, before the webview (the same precedence `menu_darwin.go` documents for the dead `?` branch), so ⌘, with the modal open arrives as `menu:settings` and hit `draft = []`. That makes it a reachable data-loss path on the primary platform, the same class as the iter-1 BLOCKING bug — not a taste call. Chose the non-destructive early-return over a `toggleSettings` (the `toggleHelpOverlay` precedent) because the help overlay has no unsaved state to lose and this form does. Regression test `re-opening settings does not wipe an in-progress draft` was confirmed to fail with the guard removed.

- **2026-07-18 iter 3** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 28f40bd9 (matches neither prior hash — iter 1 and iter 2 fixes both landed); threads_open: 0; action: stop (rubric), then orchestrator fixed both IMPORTANTs; head_sha: 92ded50. No BLOCKING. Two IMPORTANT findings, both verified against source before fixing rather than taken on faith:
  1. **CSS injection via custom agent color.** `validateCustom` accepted any string as `Color`; `launcher.js` sets it as `--agent-color`, consumed by `background: var(…)`. Reproduced in a real browser — `"color": "url(http://host/x)"` fired an outbound GET just from opening the ⌘T launcher. Fixed in Go (`safeColor`, covering the hand-edited-file load path as well as Save) plus a `background-color` sink so a future caller that skips validation is still safe. Substitutes the default rather than rejecting, so a bad color never drops an otherwise valid agent.
  2. **Backdrop click discarded the draft mid-edit.** A text-selection drag starting in a field and released outside dispatches its click on the backdrop; now both `mousedown` and `click` must land there. Third instance of the same data-loss class (after the iter-1 `LoadCustom` bug and the `openSettings` re-entry guard) — worth noting as a pattern in this modal: every path that can discard user input needs an explicit guard.

  Both regression tests were confirmed to fail with their fix reverted.

## Open questions

None. Resume support for custom agents is deliberately out of scope (see spec Non-goals).

**Outstanding for QA — rename stability.** A manual smoke test against an isolated `HIVE_STATE_DIR` confirmed the modal writes a valid `agents.json`, but the rename-then-relaunch check was not signed off. That is the specific failure this design's ID-immutability rule exists to prevent, so it should be exercised before the QA verdict: create an agent, start a session on it, rename the agent, relaunch, and confirm the session revives with its configured command rather than dropping to a bare shell.

**Needs a human visual pass (QA):** macOS blocked AppleScript/screencapture in the implementation environment, so the real Wails webview and native menu bar were never seen. Playwright covers the modal and the `menu:settings` event path, but against the mock bridge in Chromium — not the native shell. Before QA sign-off, run the app and confirm: File ▸ Settings… renders and ⌘, opens the modal; add an agent, save, and see it in the ⌘T dropdown; quit and relaunch to confirm a session on that agent revives with its command.
