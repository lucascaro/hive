# Feature: Play bell sound on selection and add volume control in settings

- **GitHub Issue:** #94
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** —

## Description

When cycling through bell sound options in the settings panel, there is no audio feedback — users must exit settings and trigger a bell to hear the selected sound. This makes it difficult to choose the right sound without guessing.

Add two improvements: (1) play the selected bell sound immediately when the user cycles through bell sound options, so they can audition each choice in real time; (2) add a volume control setting for the bell, allowing users to adjust loudness independently of system volume.

## Research

### Relevant Code
- `internal/tui/components/settings.go:34-42` — `settingField` struct; `fieldSelect` cycle handler at lines 270-283 calls `f.set()` then `sv.dirty = true` — hook point for `onChange`
- `internal/tui/components/settings.go:832-842` — Bell Sound field definition using `audio.Bells` and `config.BellSound`
- `internal/audio/bell.go:52-58` — `Play(sound string)` dispatches async; `playSync` and `playWAV` hook are where volume must thread through
- `internal/audio/bell_unix.go:21-28` — `playWAVReal` probes `afplay` / `paplay` / `aplay`; `afplay` supports `-v <0.0-1.0>`, `paplay` supports `--volume=<0-65536>`
- `internal/audio/bell_windows.go:10-18` — PowerShell `SoundPlayer` has no volume API
- `internal/tui/bell_watcher.go:38` — `start(bellSound string, sessionTargets map[string]string)` calls `audio.Play(bellSound)` at line 92
- `internal/tui/views.go:263` — calls `watcher.start(m.cfg.BellSound, buildSessionTargets(...))`
- `internal/tui/handle_session.go:191` — calls `audio.Play(m.cfg.BellSound)`
- `internal/config/config.go:20` — `BellSound string`; `BellVolume` field to be added

### Constraints / Dependencies
- `playWAV` is a package-level function variable used as a test hook — its signature must change consistently
- `aplay` (ALSA) has no volume flag — skip and use system volume for that player
- Windows `SoundPlayer` has no volume API — pass volume through the signature but ignore it (document this)
- `BellVolume == 0` (zero value for existing configs) must behave as 100% volume to avoid breaking existing users

## Plan

### Files to Change
1. `internal/config/config.go` — Add `BellVolume int \`json:"bell_volume,omitempty"\`` after `BellSound`. Default 0 = 100% (handled in audio layer).
2. `internal/audio/bell.go` — Change `Play(sound string)` → `Play(sound string, volume int)`; thread volume into `playSync(sound, volume)` and through the `playWAV` hook (signature `func(string, int) error`). Add helper `effectiveVolume(v int) int` that maps 0 → 100.
3. `internal/audio/bell_unix.go` — Update `playWAVReal(path string, volume int) error`; build args: for `afplay` append `-v <float>` (volume/100.0), for `paplay` append `--volume=<int>` (65536*volume/100), for `aplay` ignore volume. Update `runCmd` hook to `func(string, ...string) error` (unchanged shape).
4. `internal/audio/bell_windows.go` — Update `playWAVReal(path string, volume int) error`; ignore volume, add `// Volume control not supported on Windows — SoundPlayer has no API for it.` comment.
5. `internal/tui/components/settings.go` — (a) Add `onChange func(newVal string)` to `settingField` struct. (b) In the `fieldSelect` cycle handler (line ~276), after `f.set(...)`, call `f.onChange(newValue)` if non-nil. (c) Wire Bell Sound `onChange` to `func(v string) { audio.Play(v, 100) }` to preview at full volume regardless of saved volume. (d) Add "Bell Volume" `fieldSelect` field immediately after Bell Sound, options `[]string{"10", "25", "50", "75", "100"}`, get/set `config.BellVolume` (converting int↔string).
6. `internal/tui/bell_watcher.go` — Change `start(bellSound string, sessionTargets map[string]string)` → `start(bellSound string, volume int, sessionTargets map[string]string)`; update `audio.Play(bellSound, volume)` call.
7. `internal/tui/views.go:263` — Update `watcher.start(m.cfg.BellSound, m.cfg.BellVolume, buildSessionTargets(&m.appState))`.
8. `internal/tui/handle_session.go:191` — Update `audio.Play(m.cfg.BellSound, m.cfg.BellVolume)`.

### Test Strategy
- `internal/audio/bell_test.go`:
  - Update `withHooks` — change `playWAV` hook to `func(path string, volume int) error`; expose `lastVolume *int` from `withHooks`
  - `TestPlay_VolumePassedThrough` — call `Play(BellBee, 75)`, assert `lastVolume == 75`
  - `TestPlay_ZeroVolumeDefaultsTo100` — call `Play(BellBee, 0)`, assert `lastVolume == 100`
  - Update all existing tests that call `playSync(sound)` → `playSync(sound, 100)`
- `internal/audio/bell_unix_test.go`:
  - `TestPlayWAVReal_AfplayVolumeFlag` — verify `-v 0.75` appended when volume=75
  - `TestPlayWAVReal_PaplayVolumeFlag` — verify `--volume=49152` appended when volume=75 (65536*75/100)
- `internal/tui/bell_watcher_test.go`:
  - Update all `w.start(audio.BellChime, makeTargetMap())` → `w.start(audio.BellChime, 100, makeTargetMap())`
  - `TestAttachBellWatcher_VolumePassedToPlay` — verify volume is forwarded to `audio.Play` hook

### Risks
- Changing `audio.Play` signature is a package-public API break — all callers must be updated atomically (there are exactly 3: `bell_watcher.go`, `handle_session.go`, `settings.go`)
- `playWAV` hook signature change means existing test doubles in `bell_test.go` and `bell_unix_test.go` must be updated — risk of missing one and getting a compile error
- `paplay --volume` uses an internal PulseAudio scale (0-65536 = "normal", values above 65536 amplify) — must cap at 65536

## Implementation Notes

- Added `onChange func(newVal string)` to `settingField` struct; triggered in the `fieldSelect` cycle handler. Only Bell Sound wires it (to `audio.Play(v, 100)` for a full-volume preview).
- `audio.Play` signature changed to `Play(sound string, volume int)`; `effectiveVolume()` maps 0→100 for backwards compatibility with existing configs.
- Unix: `afplay` gets `-v <float>`, `paplay` gets `--volume=<int>` (65536 scale); `aplay` ignores volume (no flag support).
- Windows: `playWAVReal` accepts volume param but ignores it; documented in a comment.
- Bell Volume stored as int 0–100 in config; zero-value treated as 100% by the audio layer.
- All existing tests updated; new tests added: `TestPlay_VolumePassedThrough`, `TestPlay_ZeroVolumeDefaultsTo100`, `TestEffectiveVolume`, `TestVolumeArgs_Afplay`, `TestVolumeArgs_Paplay`, `TestVolumeArgs_Aplay`, `TestVolumeArgs_AfplayFullVolume`.

- **PR:** —
