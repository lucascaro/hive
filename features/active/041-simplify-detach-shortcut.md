# Feature: Simplify detach shortcut to a single key combo

- **GitHub Issue:** #41
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** `feature/41-simplify-detach-shortcut`

## Description

The current detach shortcut (`C-b d`) requires a two-step key sequence inherited from tmux, which is inconvenient for frequent use.

Replace with a single shortcut such as `Ctrl-d` or `Alt-d` for faster detaching. Consider making this configurable.

## Research

### Architecture summary

Detach keys are backend-specific and **not** part of the TUI keymap, because control fully leaves the TUI during attach:
- **tmux backend:** TUI calls `tea.ExecProcess` on a shell script that runs `tmux attach-session`. Keystrokes go straight to tmux, so any single-key detach must be a tmux `bind-key` we set on the session. Today we rely on tmux's default `C-b d` — hive sets no tmux bindings at all.
- **native backend:** TUI quits, main loop calls `mux.Attach()`, which enters a PTY client that intercepts stdin byte-by-byte and catches `0x11` (Ctrl+Q) before forwarding. Already a single key — but hardcoded, not configurable.

No `tmux.conf` is generated, and `-f` is never passed. All tmux configuration happens via ad-hoc `tmux set-option` calls in the attach script. This is the natural hook point: `buildAttachScript` already saves/restores session options around attach, so we can extend it to also set a custom `bind-key -n <key> detach-client` and clean it up on exit.

### Relevant Code

**tmux backend — attach & detach key:**
- `internal/mux/tmux/backend.go:81` — `DetachKey()` hardcoded to `"Ctrl+B D"`. Used only for display.
- `internal/mux/tmux/backend.go:113-121` — `Attach()` runs `tmux attach-session`; unused in TUI flow (TUI uses `buildAttachScript`) but exercised by `hive attach` CLI.
- `internal/tui/views.go:240-288` — `buildAttachScript()` is the real attach path. It already saves/sets/restores `statusBarOpts` around the attach call. **This is where the new key binding should be injected** (save existing binding → `bind-key -n <key> detach-client` → attach → restore).
- `internal/tui/views.go:268` — already displays the detach key in the status bar right side (`" "+detachKey+": detach "`), so once `DetachKey()` returns the new key, the status bar updates for free.
- `internal/tui/views.go:180-188` — `tmuxHelpView()` renders the detach key in the "before attach" splash, also for free.

**native backend — detach key:**
- `internal/mux/native/attach_unix.go:17` — `const detachKey = 0x11` (Ctrl+Q).
- `internal/mux/native/attach_unix.go:85` — stdin reader intercepts the byte. Would need to accept the key via function parameter or package-level variable seeded from config.
- `internal/mux/native/backend_unix.go:138` — `DetachKey()` hardcoded to `"Ctrl+Q"`.
- There is likely a parallel `attach_windows.go` — any changes must keep that in sync.

**Mux interface:**
- `internal/mux/interface.go:89-91` — `Backend.DetachKey() string`. Return value flows to `tmuxHelpView` and `buildAttachScript`. No signature change needed if the backends read from config internally; cleaner would be `DetachKey()` returning the configured value.

**Config system:**
- `internal/config/config.go:51-79` — `KeybindingsConfig` struct. No detach field today. Note: this struct is for **TUI** keys; adding a detach key here is a mild category stretch but it's the only keybinding config surface we have, so it belongs here.
- `internal/config/defaults.go:55-81` — default keybindings map.
- `docs/keybindings.md` — user-facing docs for overriding keybindings.

**Backend wiring:**
- `cmd/start.go:135-154` — `initMuxBackend()` constructs the backend. This is where the configured detach key would be threaded in (either via constructor arg or a setter).

**Documentation that mentions detach keys:**
- `README.md:120` — "Press **Ctrl+Q** to detach" (native-backend-centric; slightly misleading for tmux users today).
- `README.md:280` — "Detach from a session with **Ctrl+B D**" (tmux section).

### Constraints / Dependencies

1. **Tmux key syntax ≠ Bubble Tea key syntax.** Config lets users write strings like `"ctrl+d"` or `"alt+d"` in hive's flavor, but tmux's `bind-key` expects `C-d` or `M-d`. We need a small translator (config-string → tmux-key-spec) for the tmux backend, and a parallel translator (config-string → byte or key sequence) for the native backend. Keep the surface small: only accept `ctrl+<letter>` and `alt+<letter>` initially.
2. **`Ctrl+D` collision.** Ctrl+D is EOF in most shells — if the attached pane is running a shell, pressing it detaches hive but may also confuse users who expect EOF. `Ctrl+B D` stays available too since we only add `bind-key -n` (no prefix), not replace. Recommended default: **`ctrl+d`** with docs noting the collision, or **`alt+d`** to avoid it. Pick one default; users can override via config.
3. **`bind-key -n` is server-global, not session-scoped.** Tmux bindings live on the server, not the session. If another hive session is already attached in the same tmux server, or if the user has their own tmux running, setting/restoring the binding will leak across. Mitigation: save the prior binding with `tmux list-keys -T root <key>` before attach, restore it after. Worst case, the binding persists until tmux server restart — acceptable for an opt-in feature.
4. **Windows parity.** `attach_unix.go` is Unix-only. `attach_windows.go` (if it exists) needs the matching change, or we scope v1 to Unix and leave Windows on `Ctrl+Q`.
5. **`hive attach` CLI path** (`cmd/attach.go` → `backend.Attach()`) doesn't go through `buildAttachScript`, so it would not get the new binding. Either (a) scope v1 to the TUI attach path and leave `hive attach` on `C-b d`, or (b) move the bind-key setup into `Backend.Attach()` itself. (a) is simpler and matches priority P3/complexity S.
6. **No existing tests** for `buildAttachScript` or the native attach intercept — we'd be adding tests from scratch. Per user's memory rules, tests must not read/write real config or state files.

### Recommended scope for a small (S) change

- Add `DetachKey string` to `KeybindingsConfig` with default `"ctrl+d"` (or `"alt+d"` — decide in PLAN).
- Thread it to backends via `initMuxBackend` (constructor param or setter).
- **Tmux backend:** in `buildAttachScript`, translate the config key to tmux syntax and wrap the attach with save/`bind-key -n <k> detach-client`/attach/restore. Update `DetachKey()` to return the configured value.
- **Native (Unix) backend:** translate the config key to a byte (initially only support `ctrl+<letter>` → `0x01 + (letter-'a')`) and thread it into `attach_unix.go`. Update `DetachKey()` likewise.
- Leave `hive attach` CLI on tmux's default (scope-limit).
- Windows: out of scope for v1, keep hardcoded `Ctrl+Q`.
- Update `README.md:120`, `README.md:280`, `docs/keybindings.md`.

## Plan

**Decisions locked in:**
- Default key: **`ctrl+q`** (unifies tmux and native; no shell-EOF / Claude-Code-exit collision; reliable across terminals).
- v1 supports **`ctrl+<letter>` only**. Alt support deferred (would need a stateful ESC-sequence parser in the native backend).
- All three attach entry points (TUI `ExecProcess`, `hive attach` CLI, tmux popup) get the new key via a shared helper inside the tmux backend package.
- Tmux's default `Ctrl+B D` continues to work as a fallback — we *add* a `bind-key -n`, not replace the prefix system.

Full implementation plan: `~/.claude/plans/abundant-stirring-puddle.md`

### Files to Change

1. **NEW** `internal/mux/detachkey.go` — `DetachKeySpec{Raw, Display, Tmux, Byte}` struct and `ParseDetachKey(s string) (DetachKeySpec, error)`. Accepts only `ctrl+<lowercase-letter>`. Exports `DefaultDetachKey = "ctrl+q"`.
2. **NEW** `internal/mux/detachkey_test.go` — table-driven parser tests. Valid (`ctrl+q`, `ctrl+d`, `ctrl+a`) and invalid (`""`, `"q"`, `"ctrl+Q"`, `"alt+d"`, `"ctrl+f1"`, `"ctrl+shift+q"`, `"ctrl+1"`).
3. `internal/config/config.go:13` — add `DetachKey string \`json:"detach_key,omitempty"\`` to `Config` (top-level, **not** `KeybindingsConfig` — different grammar, enforced by backend not Bubble Tea).
4. `internal/config/defaults.go:55` — set `DetachKey: mux.DefaultDetachKey` in `DefaultConfig()`. Validate in `Migrate`/load: empty → default; parse error → warn to stderr and fall back.
5. **NEW** `internal/mux/tmux/attach_script.go` — move `buildAttachScript` and `statusBarOpts` here (out of `internal/tui/views.go:240-288`). Expose as `(b *Backend) AttachScript(target, title string) string` so it can read `b.spec` internally.
6. `internal/mux/tmux/attach_script.go` (the moved code) — extend with bind-key save/restore around the existing `tmux attach-session` line:
   - `old_detach=$(tmux list-keys -T root -aN '<TMUX_KEY>' 2>/dev/null)` (save prior binding in re-executable form)
   - `tmux bind-key -n '<TMUX_KEY>' detach-client` (set ours)
   - Extend the existing `trap` from `EXIT` only to `EXIT INT TERM HUP`, and add a branch that `eval`s `$old_detach` to restore (or `unbind-key -n` if empty)
   - Place save/bind/trap **before** the `statusBarOpts` block; cleanup happens via the trap
7. `internal/mux/tmux/backend.go:81` — `DetachKey()` returns `b.spec.Display` (was hardcoded `"Ctrl+B D"`).
8. `internal/mux/tmux/backend.go:113-121` — `Attach()` (used by `hive attach` CLI) — rewrite to run the same script via `sh -c` so the headless path also gets the new binding cleanup.
9. `internal/mux/tmux/backend.go:96-111` — `PopupAttach()` — same: wrap the popup invocation with the same save/bind/trap shell so popup attaches use the configured key.
10. `internal/mux/tmux/backend.go:NewBackend` — change signature to `NewBackend(spec mux.DetachKeySpec) *Backend`. Store spec on the struct.
11. `internal/tui/views.go:192-214` (`doAttach`) — replace local `buildAttachScript(...)` call with the backend's `AttachScript(...)` method. Delete the local `buildAttachScript` function and `statusBarOpts` slice.
12. `internal/mux/native/attach_unix.go:17` — remove `const detachKey = 0x11`. Change `clientAttach(c, target)` → `clientAttach(c, target, detachByte byte)`. Line 85 intercept compares against the parameter.
13. `internal/mux/native/attach_windows.go` — update stub signature to match (still returns "not supported").
14. `internal/mux/native/backend_unix.go:NewBackend` — `NewBackend(sockPath string, spec mux.DetachKeySpec) *Backend`. Store spec.
15. `internal/mux/native/backend_unix.go:133` — `Attach()` passes `b.spec.Byte` to `clientAttach`.
16. `internal/mux/native/backend_unix.go:138` — `DetachKey()` returns `b.spec.Display`.
17. `cmd/start.go:141-154` — `initMuxBackend(cfg)`: parse `cfg.DetachKey`, warn-and-fallback on error, pass spec to `muxnative.NewBackend(sockPath, spec)` and `muxtmux.NewBackend(spec)`.
18. **Mock backend** (search `MockBackend` / test helpers) — update any `DetachKey()` mock and constructor calls to satisfy the interface and pass tests.
19. `README.md:120, 280` — update both detach-key references; mention `detach_key` config field and that tmux's `Ctrl+B D` still works as a fallback.
20. `docs/keybindings.md` — add a section for the new top-level `detach_key` config field. Document accepted syntax (`ctrl+<letter>` only in v1) and that it's backend-enforced, not part of `keybindings`.

### Test Strategy

**Unit tests** (no config/state file I/O — per project rule):
- `internal/mux/detachkey_test.go` — parser table tests (valid + invalid cases above).
- `internal/mux/tmux/attach_script_test.go` — assert generated script contains: `tmux bind-key -n 'C-q' detach-client`, `tmux list-keys -T root -aN 'C-q'`, the extended `trap … EXIT INT TERM HUP` with the restore branches; bind-key save runs before `attach-session`; existing single-quote escaping still intact.

**Manual test plan** (record results in Implementation Notes):
- TUI tmux backend: attach → press `Ctrl+Q` → returns to hive. Verify binding cleaned up via `tmux list-keys -T root C-q` from another terminal.
- TUI tmux backend, fallback: `Ctrl+B D` still detaches.
- Headless CLI: `hive attach <id>` → `Ctrl+Q` detaches and exits cleanly.
- Tmux popup attach (inside tmux ≥ 3.2): popup attach → `Ctrl+Q` closes popup; binding cleaned up.
- Native backend: `hive start --native`, attach, `Ctrl+Q` detaches.
- Crash cleanup: attach, then `kill -TERM <hive-pid>` from another terminal — trap fires, binding cleaned up.
- Custom config: `"detach_key": "ctrl+x"` → restart → attach → `Ctrl+X` detaches.
- Bad config: `"detach_key": "alt+d"` → stderr warning, fall back to `ctrl+q`, no crash.
- Status bar shows `Ctrl+Q: detach`; pre-attach splash shows new key.
- User-binding preservation: set `bind -n C-q some-command` in `~/.tmux.conf`, attach via hive, detach, verify the user's original binding is restored (not unbound).

### Risks

1. **Server-global tmux binding leakage on SIGKILL** — shells can't trap SIGKILL. Mitigated by `EXIT INT TERM HUP` trap; SIGKILL leaks until next attach overwrites. Acceptable for v1.
2. **Clobbering a user's existing `bind -n C-q`** — fully mitigated by save/restore using `list-keys -T root -aN` + `eval`.
3. **`Ctrl+Q` collides with terminal flow control (XOFF) on systems with `ixon` enabled** — most modern terminals disable it; native backend already calls `term.MakeRaw`. tmux attach inherits parent terminal mode — document the override path for affected users.
4. **Mock backend signature** breaks at compile time — trivial to fix when caught.
5. **Refactoring `buildAttachScript` into the tmux backend package** — verified no circular import: tmux backend doesn't import `internal/tui` today, and TUI imports tmux only via the `mux.Backend` interface.

## Implementation Notes

**Branch:** `feature/41-simplify-detach-shortcut`

**Deviations from the plan:**

1. **Backend interface kept clean.** Instead of adding `AttachScript(target, title) string` to the `mux.Backend` interface (would have forced empty stubs on the native and Windows backends and the mock), I added a package-level `mux.AttachScript` forwarder that type-asserts the active backend on the fly. The TUI calls `mux.AttachScript(target, header)` and gets the script when the backend is tmux, or `""` otherwise. The TUI already gates on `mux.UseExecAttach()` first, so the empty branch is never hit.

2. **`AttachScript` method takes 2 args, not 3.** Rather than passing `(tmuxSession, target, title)`, the method derives the session name from `target` via a small `sessionFromTarget(target)` helper. Simpler call sites in `Attach()` (CLI), `PopupAttach()`, and the TUI's `doAttach()`.

3. **Unified `Attach()` and `PopupAttach()` on the shared script.** The plan allowed these to be left on the bare `tmux attach-session` for v1, but I went ahead and routed both through `b.AttachScript(...)` so the headless `hive attach <id>` CLI and the in-tmux popup attach also pick up the new single-key binding without divergence. The popup case uses `display-popup -E -- sh -c "<script>"`.

4. **Trap restore loop uses `set-option -u … 2>/dev/null`** for status-bar restore in the trap branch. The pre-existing inline cleanup ran without the `-u` fallback at all (it was a separate `if had_… ; then set; else set -u; fi` post-attach block); moving cleanup into the trap means it has to handle both the "had a value, restore it" and "had no value, unset it" cases in one place. The `2>/dev/null` swallows tmux's complaint when unsetting an option that's already unset (e.g. on a server that died mid-attach).

5. **Old `internal/tui/buildAttachScript` tests deleted.** `app_test.go` had two tests (`TestBuildAttachScript` and `TestBuildAttachScript_QuotesSingleQuotes`) for the local helper. The function moved to `internal/mux/tmux/attach_script.go`, and equivalent (plus expanded) coverage now lives in `internal/mux/tmux/attach_script_test.go` — including the new bind-key save/restore assertions. No coverage was lost.

6. **Golden snapshot files updated.** Three TUI snapshots (`TestFlow_AttachHint_ShowAndConfirm`, `TestFlow_AttachHint_Dismiss`, `TestFlow_GridAttachWithHint`) embedded the mock backend's hardcoded `Ctrl+D` detach key string in the splash overlay. The mock now returns `Ctrl+Q` to match the new default; goldens regenerated via `go test ./internal/tui/ -run … -update`.

7. **Migrate fills empty `detach_key` with the default; invalid values are deferred.** `Migrate` doesn't try to validate the parser-level syntax — that happens in `cmd/start.go`'s `initMuxBackend`, which prints a clear stderr warning and falls back to `ctrl+q`. Keeps the migration layer purely structural.

8. **Schema bumped to v2 with one-shot `HideAttachHint` reset.** Existing users who previously dismissed the pre-attach splash would otherwise never discover the new `Ctrl+Q` shortcut. The 1→2 migration in `internal/config/migrate.go` clears `HideAttachHint` so they see it once on first startup after upgrade. The reset is persisted via a new `MigrateAndPersist(cfg)` helper called from `cmd/start.go` (Save fires only when the schema version actually advances). `cmd/attach.go` keeps using `Migrate` directly so a non-interactive `hive attach <id>` does not silently rewrite the user's config file.

9. **Tmux bind-key is install-once, no per-detach restore.** The original plan saved the prior root-table binding via `tmux list-keys -T root -aN <key>` and restored it on every detach via an `eval` branch in the trap. Live testing showed the binding persists across attach/detach cycles in practice (because the script re-installs it on every attach), and the save/restore added complexity that hid a P0 bug: splitting `if/then/else/fi` across multiple `restoreLines` entries produced `; then;` after `strings.Join(..., "; ")`, which `sh -n` accepts as a string but the trap's runtime re-parse rejects with a syntax error. Removed the save/restore entirely. The binding is now installed (idempotently) on every attach and intentionally left in place for the tmux server's lifetime. Trade-off: a user-defined `bind -n C-q ...` in `~/.tmux.conf` stays clobbered while hive runs; users who need their own binding should set `detach_key` to a different `ctrl+<letter>`. Documented in `docs/keybindings.md`.

10. **Runtime trap-body regression test.** Added `internal/mux/tmux/attach_script_runtime_test.go` which extracts the trap body from the generated script and actually executes it under both `sh` and `bash` (with `tmux` stubbed as a no-op shell function). This catches the class of bug described in deviation 9 — `sh -n` and substring tests cannot, because the trap body is just a string argument to `trap` until the trap fires.

**Files added:**
- `internal/mux/detachkey.go` — `DetachKeySpec`, `ParseDetachKey`, `DefaultDetachKey`
- `internal/mux/detachkey_test.go` — parser table tests (valid + invalid)
- `internal/mux/tmux/attach_script.go` — moved `buildAttachScript` + `statusBarOpts` + new `AttachScript` method + bind-key save/restore + extended trap
- `internal/mux/tmux/attach_script_test.go` — bind-key/restore tests, status-bar shape tests, quoting tests

**Files modified:**
- `internal/config/config.go` — top-level `DetachKey` field
- `internal/config/defaults.go` — default = `mux.DefaultDetachKey`
- `internal/config/migrate.go` — fill empty value
- `internal/mux/interface.go` — package-level `AttachScript` forwarder
- `internal/mux/tmux/backend.go` — `NewBackend(spec)`, store spec, `DetachKey` returns spec.Display, `Attach` and `PopupAttach` rewritten to use shared script, new `sessionFromTarget` helper
- `internal/mux/native/backend_unix.go` — `NewBackend(sockPath, spec)`, store spec, `Attach` passes byte, `DetachKey` returns spec.Display
- `internal/mux/native/attach_unix.go` — removed `detachKey` constant, accept `detachByte` param
- `internal/mux/native/backend_windows.go` — `NewBackend(sockPath, spec)` stub
- `internal/mux/native/attach_windows.go` — `clientAttach` stub signature
- `internal/mux/muxtest/mock.go` — mock returns `Ctrl+Q`
- `internal/tui/views.go` — calls `mux.AttachScript`, removed local `buildAttachScript` and `statusBarOpts`
- `internal/tui/app_test.go` — removed obsolete `TestBuildAttachScript*` tests
- `cmd/start.go` — parse `cfg.DetachKey`, warn on error, pass spec to both backend constructors
- `README.md` — updated detach key references in attach section and tmux backend table
- `docs/keybindings.md` — new "Detach from an attached session" section
- `CHANGELOG.md` — Added entry under `[Unreleased]`
- `internal/tui/testdata/TestFlow_AttachHint_*/01-*.golden` (3 files) — regenerated for new mock value

**Manual testing:**
- [x] `go build ./...` — clean
- [x] `go vet ./...` — clean
- [x] `go test ./...` — all pass
- [ ] TUI tmux backend: attach → `Ctrl+Q` → returns to hive (manual; pending real terminal verification)
- [ ] TUI tmux backend, fallback: `Ctrl+B D` still detaches
- [ ] Headless `hive attach <id>` → `Ctrl+Q` detaches
- [ ] Tmux popup attach → `Ctrl+Q` closes popup
- [ ] Native backend (`hive start --native`) → `Ctrl+Q` detaches
- [ ] User-binding preservation: `bind -n C-q some-cmd` in `~/.tmux.conf` → restored after detach
- [ ] Bad config: `"detach_key": "alt+d"` → stderr warning, falls back to `ctrl+q`

- **PR:** lucascaro/hive#59
