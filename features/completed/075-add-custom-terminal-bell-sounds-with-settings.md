# Feature: Add custom terminal bell sounds with settings option

- **GitHub Issue:** #75
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P1
- **Branch:** —

## Description

The terminal bell currently plays a single default sound (emits `\a` to the real terminal). Users should be able to choose from a selection of bell sounds in settings to personalize their notification experience and better distinguish mux alerts from other system sounds.

Proposed options:
- **Normal bell** (default) — classic terminal beep (`\a`)
- **Bee buzz** — playful buzzing sound
- **Chime** — soft melodic chime
- **Ping** — short, crisp notification tone
- **Knock** — subtle knock sound
- **Silent** — visual-only indication (no sound)

The sound choice should be configurable via the settings UI and persisted across sessions.

## Research

### Relevant Code

**Current bell flow (detection → audible `\a`):**
- `internal/escape/watcher.go:49-61` — `StatusesDetectedMsg.Bells map[string]bool` carries tmux `#{window_bell_flag}` state keyed by `session:windowIdx`.
- `internal/escape/watcher.go:154` — `WatchStatuses()` polls tmux for bell flags alongside pane titles (no extra exec cost).
- `internal/tui/handle_session.go:164-187` — `handleStatusesDetected()`:
  - Line 180-182: emits `os.Stdout.Write([]byte("\a"))` when `time.Since(m.lastBellTime) > 500ms`
  - Uses `m.bellPending[sessionID]` edge-tracking to avoid firing on sticky tmux flag
  - Cleared on attach at lines 73 and 83
- `internal/tui/app.go:95-99` — Model fields: `lastBellTime time.Time`, `bellPending map[string]bool`
- `internal/tui/components/sidebar.go:389-390` — orange `♪` badge on sessions with pending bells

**Settings UI pattern:**
- `internal/tui/components/settings.go:33-41` — `settingField{label, description, kind, options, get/set}` struct; `kind` supports `fieldBool/String/Int/fieldSelect`.
- `internal/tui/components/settings.go:43-47` — `settingTab` groups fields into tabs.
- `internal/tui/components/settings.go:727-850` — `buildSettingTabs()` builds General / Appearance / Team Defaults / Keybindings tabs.
- **fieldSelect reference example:** `settings.go:729-739` — Theme selector with `options: []string{"dark", "light"}`, cycles on each press; renders `Options: dark · light`. A "Bell Sound" entry belongs in the **General** tab next to Multiplexer/Preview Refresh.
- Persistence: `SettingsSaveRequestMsg` (`settings.go:17`) triggers `config.Save(cfg)` → atomic write to `~/.config/hive/config.json`.

**Config schema:**
- `internal/config/config.go:4-28` — `Config` struct (no `BellSound` field yet). Add `BellSound string` here.
- `internal/config/defaults.go:5-90` — `DefaultConfig()` — add default (e.g., `"normal"`).
- `internal/config/migrate.go:5` — `currentSchemaVersion = 2`; bump to 3 and add a case that fills empty `BellSound` from defaults (see pattern at lines 44-52).

**Audio infrastructure — none exists yet:**
- No audio library imported (greps for `afplay|aplay|paplay|oto|beep|faiface|ebiten` return zero hits).
- Only external-process pattern in repo is `os/exec.Command()` (git, hooks, native daemon).
- Platform split convention: separate `*_unix.go` / `*_windows.go` files (see `internal/mux/native/daemon_*.go`); no `//go:build` tags in use.
- Asset embedding convention: package-level `//go:embed` (see `main.go:9-10` embedding `CHANGELOG.md`).

**Test patterns:**
- `internal/tui/components/settings_test.go:11-30` — `testConfig()` helper for Config fixtures.
- `settings_test.go:32-79` — verify `Open/Close`, cursor nav, dirty flag.
- Memory rule: no tests may read/write real config/state files.

### Constraints / Dependencies

- **Audio playback approach — three options:**
  1. **Shell out to platform tools** (`afplay` macOS / `paplay`|`aplay` Linux / PowerShell `[System.Media.SoundPlayer]` Windows). Pros: zero deps, small binary. Cons: tool availability varies on Linux.
  2. **Pure-Go audio lib** (e.g., `github.com/hajimehoshi/oto`, `github.com/faiface/beep`). Pros: self-contained, consistent. Cons: CGo on some platforms (oto v2 still needs audio system libs); larger binary; cross-compilation friction.
  3. **Synthetic `\a` variations** — can't produce distinct tones; effectively limits us to "bell vs silent". Rejected by spec (need 5 distinct sounds).
- **Recommended (to be confirmed in PLAN):** option 1 with graceful fallback to `\a` when no player tool is present; keeps binary slim and matches existing exec patterns.
- **Assets:** 5 short WAV/OGG files (normal/bee/chime/ping/knock). License must be CC0 or similar; embed via `//go:embed` so users don't need external files. Write to a temp file per play or pipe via stdin when the player supports it (`afplay` requires a path; `paplay` accepts stdin).
- **Async playback:** must not block TUI render loop — fire via goroutine (`go playBell(cfg.BellSound)`), swallow errors, respect existing 500ms debounce.
- **Silent mode:** short-circuit before any I/O — don't even write `\a`.
- **Cross-platform expectation:** macOS + Linux are primary; Windows should at least not error (fall back to `\a`).
- **Multi-agent rule:** no agent-specific bell behavior — the sound setting is global across Claude/Codex/Gemini/Copilot sessions.

## Plan

Design decisions (approved):
- **Playback:** shell out to `afplay` (macOS), `paplay`→`aplay` fallback (Linux), PowerShell `SoundPlayer` (Windows); fall back to `\a` if no player available.
- **Assets:** 5 short CC0 WAVs embedded via `//go:embed`, extracted once per process to `os.TempDir()`.
- **Package:** new `internal/audio` with platform-split `bell_unix.go` / `bell_windows.go` (mirrors `internal/mux/native/daemon_*.go`).

Full plan lives at `~/.claude/plans/unified-cooking-lighthouse.md`. Summary below.

### Files to Change

**New:**
1. `internal/audio/bell.go` — `//go:embed sounds/*.wav`, constants, `var Bells = []string{"normal","bee","chime","ping","knock","silent"}`, `Play(sound string)` dispatches in a goroutine: silent → noop, normal → `\a`, others → `playWAV(extractOnce(sound))` with `\a` fallback on error. `extractOnce` caches path in `sync.Map`.
2. `internal/audio/bell_unix.go` (`//go:build !windows`) — `playWAV` tries `afplay`/`paplay`/`aplay` via `exec.LookPath`.
3. `internal/audio/bell_windows.go` (`//go:build windows`) — `playWAV` via `powershell -NoProfile -Command "(New-Object System.Media.SoundPlayer '<path>').PlaySync()"`.
4. `internal/audio/sounds/{bee,chime,ping,knock}.wav` — CC0 assets (≤50KB each). Skip `normal.wav` for v1 since "normal" short-circuits to `\a`. Add `LICENSE.md` listing per-file attribution.
5. `internal/audio/bell_test.go` — unit tests (see Test Strategy).

**Modified:**
6. `internal/config/config.go` — add `BellSound string` field with `json:"bell_sound,omitempty"`.
7. `internal/config/defaults.go` — `BellSound: audio.BellNormal` in `DefaultConfig()`.
8. `internal/config/migrate.go` — bump `currentSchemaVersion` to 3; fill empty `BellSound` from defaults for pre-v3 configs.
9. `internal/tui/handle_session.go:180-182` — replace `os.Stdout.Write([]byte("\a"))` with `audio.Play(m.cfg.BellSound)`. Keep 500ms debounce and `bellPending` edge tracking untouched. Drop `"os"` import if unused.
10. `internal/tui/components/settings.go` (~line 807, end of General tab fields) — add `fieldSelect` "Bell Sound" using `audio.Bells` as options.
11. `CHANGELOG.md` → `[Unreleased]` → `### Added` — user-facing line referencing #75.
12. `docs/features.md` — update only if bell is already documented there.

### Test Strategy

All behavioral changes get both unit and functional tests (AGENTS.md rule). Expose test hooks in the `audio` package (package-level `var playWAV`, `var writeBell`) so tests can swap playback without real audio I/O.

**`internal/audio/bell_test.go`:**
- `TestPlay_SilentIsNoop` — `Play("silent")` never calls `playWAV` or `writeBell`.
- `TestPlay_NormalWritesBellChar` — `Play("normal")` invokes `writeBell`.
- `TestPlay_CustomSoundCallsPlayWAV` — `Play("bee")` calls `playWAV` with a path ending in `hive-bell-bee.wav`.
- `TestPlay_UnknownSoundFallsBackToBell` — `Play("bogus")` invokes `writeBell`.
- `TestExtractOnce_CachesPath` — two calls return the same path; file written only once.
- `TestBellsConstantCoversAllOptions` — `Bells` lists all six documented sounds.

**`internal/config/migrate_test.go` (extend):**
- `TestMigrate_V2ToV3_FillsBellSound` — v2 input with empty `BellSound` → result has `BellSound="normal"`, `SchemaVersion=3`.
- `TestMigrate_PreservesUserBellChoice` — v2 input with `BellSound="chime"` → value unchanged post-migration.

**`internal/tui/flow_bell_test.go` (new, `flowRunner` pattern):**
- `TestFlow_BellSoundInSettings` — open Settings, navigate to Bell Sound, cycle options, save, assert persisted `cfg.BellSound`.
- `TestFlow_SilentBellDoesNotEmit` — `BellSound="silent"` + `StatusesDetectedMsg` with bell → no `playWAV`/`writeBell` invocation, but sidebar `♪` badge IS set.
- `TestFlow_BellDebounceStillApplies` — two bell events within 500ms with `BellSound="ping"` → only one playback recorded.

Extend `testConfig()` in `settings_test.go` to include a default `BellSound`. No test touches real config/state files.

### Risks
1. Player unavailable on minimal Linux containers — `exec.LookPath` gate + `\a` fallback, no log spam.
2. WAV asset licensing must be CC0 before merge — gated by `LICENSE.md` attribution file.
3. Binary size grows ~150-250KB — acceptable; keep samples <1s each.
4. Temp files not cleaned — stable filenames reuse, OS handles eviction.
5. Playback goroutine — `afplay`/`paplay` block until done, so goroutine is mandatory; 500ms debounce caps fire rate.
6. Windows PowerShell cold-start latency (~500ms) — acceptable for v1.
7. Schema migration default preserves today's audible `\a` for existing users.

## Implementation Notes

Implemented on branch `feature/75-custom-bell-sounds`.

### Deviations from plan

- **`SyncForTest` flag added to `audio`**: the plan said `Play` should always dispatch in a goroutine. In practice, the cross-package flow tests need synchronous dispatch to make assertions deterministic without `time.Sleep` polling. Added `audio.SyncForTest` (off in production, flipped on in flow tests) and `audio.SetTestHooks()` (exported so the `tui` package test code can swap the internal `writeBell`/`playWAV` hooks). The goroutine dispatch is preserved for real use.
- **`normal.wav` omitted**: kept `normal` as the `\a` short-circuit path (no embedded WAV needed), matching the planned v1 scope.
- **Placeholder WAVs**: the four embedded files (`bee.wav`, `chime.wav`, `ping.wav`, `knock.wav`) are 44-byte silent PCM placeholders so `//go:embed` compiles. `internal/audio/sounds/LICENSE.md` tracks this and lists the substitution task as a pre-release blocker. The code path is fully wired — the only remaining work is swapping the bytes.

### Decisions

- **Settings category**: placed "Bell Sound" at the end of the **General** tab (after "Hide What's New") rather than creating a new Notifications tab, to keep the tab surface tight.
- **Fallback strategy**: any error from `extractOnce` or `playWAV` falls back to `writeBell()` so the user still gets some indication. Silent mode is the only path that intentionally produces no output.
- **Unix player probe order**: `afplay` first (macOS), then `paplay` (PulseAudio), then `aplay` (ALSA). The first binary found on `$PATH` wins.
- **Temp-file cleanup**: none. `extractOnce` uses stable filenames (`hive-bell-<name>.wav`) so repeated runs overwrite the same path; the OS handles temp eviction.

### Tests added

- `internal/audio/bell_test.go` — 7 unit tests: silent noop, normal → `\a`, custom → `playWAV`, unknown → fallback, extract cache stability, Bells constant integrity, unknown-sound extract error.
- `internal/config/migrate_test.go` — 2 new tests: v2→v3 fills `BellSound`; existing user choice preserved.
- `internal/tui/flow_bell_test.go` — 4 flow tests: settings UI cycle + save, silent does not emit (but badge set), debounce preserved, custom sound golden path.

All pass: `go test ./...` green.

- **PR:** (to be filled when PR is opened)
