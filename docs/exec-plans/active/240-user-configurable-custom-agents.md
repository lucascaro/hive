# Add user-configurable custom agents

- **Spec:** [docs/product-specs/240-user-configurable-custom-agents.md](../../product-specs/240-user-configurable-custom-agents.md)
- **Issue:** —
- **Stage:** IMPLEMENT
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
- `cmd/hivegui/menu_darwin.go` — `Settings…` with `keys.CmdOrCtrl(",")` in the App menu section, emitting `menu:settings`.
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

## Open questions

None. Resume support for custom agents is deliberately out of scope (see spec Non-goals).

**Not verified by automation:** the native macOS ⌘, menu item was confirmed to compile and the `menu:settings` event path is covered in Playwright, but the actual menu bar rendering was not visually checked — macOS blocked AppleScript/screencapture in this environment.
