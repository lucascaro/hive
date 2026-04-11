# Feature: Terminal bell does not produce audible sound

- **GitHub Issue:** #34
- **Stage:** IMPLEMENT
- **Type:** bug
- **Complexity:** S
- **Priority:** P4
- **Branch:** —

## Description

The terminal bell (`\a` / BEL character) does not produce an audible sound when triggered from within hive sessions.

When a session emits a bell character (e.g., on tab completion failure, command error, or explicit `echo -e '\a'`), the user should hear an audible notification or see a visual bell, depending on their terminal configuration. Currently, no sound is produced.

## Research

### Root Cause

BEL characters emitted by sessions never reach the user's terminal. Two independent problems:

1. **`capture-pane` doesn't preserve BEL.** tmux's terminal emulator consumes the BEL when it arrives at the pane — `capture-pane` returns the visual buffer, which no longer contains `\x07`. Parsing captured content for BEL is therefore **unreliable/impossible**.

2. **Bubble Tea's alternate-screen renderer** would swallow any stray control characters anyway. Even if BEL survived capture, the full-screen redraw pipeline wouldn't emit it to the real terminal.

### The Right Approach: tmux `#{window_bell_flag}`

tmux natively tracks bells per-window. The format variable `#{window_bell_flag}` returns `1` when a bell has fired in that window since last checked. This is the authoritative signal — it works regardless of capture-pane limitations.

Verified working: `tmux list-windows -F "#{window_index}\t#{window_bell_flag}"` returns `0`/`1` per window.

### Relevant Code

**Bell detection hook point — `WatchStatuses` (all sessions):**
- `internal/escape/watcher.go:77-151` — `WatchStatuses()`: polls ALL non-dead sessions every ~500-1000ms. Already receives `titles map[string]string` from `GetPaneTitles()`. Bell flags can be queried alongside titles.
- `internal/escape/watcher.go:49-58` — `StatusesDetectedMsg`: carries statuses, contents, titles. Needs a new `Bells` field.

**Tmux query layer:**
- `internal/tmux/capture.go:42-63` — `GetPaneTitles()`: uses `tmux list-windows -F`. Can add `#{window_bell_flag}` to the same query or create a parallel `GetBellFlags()` function.
- `internal/mux/interface.go` — Backend interface; needs new method if adding a separate function.
- `internal/mux/tmux/backend.go` — Backend implementation.

**Bell forwarding to terminal:**
- `internal/tui/handle_session.go:121-164` — `handleStatusesDetected()`: processes `StatusesDetectedMsg`. This is where bell forwarding logic would go.
- The handler returns `tea.Cmd` — can emit `\a` to the real terminal via `tea.Printf("\a")` or direct `os.Stdout.Write([]byte("\a"))`.

**Scheduling:**
- `internal/tui/handle_preview.go:112-146` — `scheduleWatchStatuses()`: builds session targets and calls `WatchStatuses`. Already fetches pane titles here; bell flags would be fetched in parallel.

**Preview sanitization (minor cleanup):**
- `internal/tui/components/preview.go:63` — Should add `"\a", ""` to `strings.NewReplacer` as defensive cleanup, even though capture-pane likely won't contain BEL.

### Fix Approach

1. **Query bell flags from tmux:** Add `GetBellFlags(session) map[string]bool` in `tmux/capture.go`, or extend `GetPaneTitles` to also return bell state. Query `#{window_bell_flag}` via `list-windows -F`.
2. **Thread bell flags through `WatchStatuses`:** Add `Bells map[string]bool` to `StatusesDetectedMsg`. Pass bell flags from the scheduling layer.
3. **Forward bells in `handleStatusesDetected`:** When any session has bell=true, emit `\a` to the terminal. Debounce with a cooldown (e.g., 500ms minimum between bells) to avoid rapid-fire noise.
4. **Defensive cleanup:** Strip `\a` in `sanitizePreviewContent` at line 63.

This covers **all sessions** — active, background, unfocused — because `WatchStatuses` already polls every non-dead session.

### Constraints / Dependencies

- `#{window_bell_flag}` is reset by tmux after it's read (when `monitor-bell` is on, which is the default). Need to verify the flag auto-clears or if we need to clear it manually.
- Writing `\a` to stdout while Bubble Tea owns the terminal needs testing — should work since BEL doesn't affect cursor position, but must verify no rendering corruption.
- Need debounce to prevent the same bell from firing repeatedly across polling cycles.

## Plan

Fold bell flag detection into the existing `GetPaneTitles` tmux query (zero extra tmux execs), thread the flags through `WatchStatuses` → `StatusesDetectedMsg`, and forward `\a` to the real terminal with debounce.

### Files to Change

1. **`internal/tmux/capture.go`** — Extend `GetPaneTitles` to also return bell flags.
   - Change the `list-windows -F` format string from `"#{window_index}\t#{pane_title}"` to `"#{window_index}\t#{pane_title}\t#{window_bell_flag}"`.
   - Return a second value: `bells map[string]bool` (keyed by `"session:windowIdx"`, true when flag is `"1"`).
   - Signature becomes: `GetPaneTitles(session string) (titles map[string]string, bells map[string]bool, err error)`.

2. **`internal/mux/interface.go`** — Update `GetPaneTitles` in the `Backend` interface to return the new signature: `GetPaneTitles(session string) (map[string]string, map[string]bool, error)`.
   - Update the package-level forwarding function `GetPaneTitles()` to match.

3. **`internal/mux/tmux/backend.go`** — Update `Backend.GetPaneTitles` to forward the new return values from `tmux.GetPaneTitles`.

4. **`internal/escape/watcher.go`** — Add `Bells map[string]bool` field to `StatusesDetectedMsg`. In `WatchStatuses`, accept a new `bells map[string]bool` parameter (the tmux bell flags, keyed by target), and translate target keys → sessionID keys before putting them in the msg.

5. **`internal/tui/handle_preview.go`** — In `scheduleWatchStatuses()`:
   - Update the `mux.GetPaneTitles()` call to unpack the new `bells` return value.
   - Pass `bells` to `escape.WatchStatuses()`.

6. **`internal/tui/handle_session.go`** — In `handleStatusesDetected()`:
   - Check `msg.Bells` for any `true` values.
   - If any bell detected AND `time.Since(m.lastBellTime) > 500ms`, write `\a` to stdout via `os.Stdout.Write([]byte("\a"))` and update `m.lastBellTime`.
   - The 500ms debounce prevents rapid-fire bells from multiple sessions or repeated polling.

7. **`internal/tui/app.go`** — Add `lastBellTime time.Time` field to `Model` struct (near `contentSnapshots`/`stableCounts`). No initialization needed (zero value is fine).

8. **`internal/tui/components/preview.go:63`** — Defensive: add `"\a", ""` to `strings.NewReplacer` in `sanitizePreviewContent`.

### Test Strategy

- **Unit test for `GetPaneTitles`:** Existing tests (if any) need updating for the new return signature. Add a test case that parses a line with bell flag `1`.
- **Unit test for `handleStatusesDetected`:** Verify that when `Bells` has a `true` entry, the handler writes BEL (mock stdout or check `lastBellTime` update). Verify debounce: second call within 500ms should not write BEL again.
- **Unit test for `sanitizePreviewContent`:** Add test case with `\a` in input, verify it's stripped.
- **Manual test:** Run hive, open a session, run `echo -e '\a'` — should hear a bell. Switch to a different session, trigger bell in background session — should still hear it.

### Risks

- **`#{window_bell_flag}` is sticky.** Since hive never attaches a tmux client to windows (it uses `capture-pane`), the flag stays set indefinitely after a bell fires. This means the first `WatchStatuses` poll after a bell will detect it, but subsequent polls will also see it as `1`. **Mitigation:** After detecting a bell, track "already-seen bell" per session (e.g., `bellSeen map[string]bool` on Model) and only fire when the flag transitions from `0` → `1`. Clear the seen state when the flag returns to `0` (which happens when the user attaches to that session via tmux, or we could manually reset it — but let's start with edge tracking).
- **stdout write during Bubble Tea render.** Writing `\a` to stdout while Bubble Tea owns the terminal could theoretically interleave with a frame. BEL is a single byte that doesn't affect cursor state, so this should be safe, but needs manual verification.
- **Bell storm.** A runaway process spamming BEL could set the flag on every poll cycle. The 500ms debounce + edge-tracking limits this to at most one bell per 500ms per new bell event.

## Implementation Notes

Implemented as planned with no deviations. Key decisions:

- **Edge-triggered bell detection:** `bellSeen map[string]bool` on Model tracks which targets had bell=1 on the previous poll. Only fires `\a` on 0→1 transitions per target. The `bellSeen` map is rebuilt from scratch each poll (not merged) so targets whose flag clears naturally drop out.
- **SplitN increased to 3:** The `GetPaneTitles` parser now splits on 3 fields (`index\ttitle\tbell_flag`). Lines with only 2 fields (older tmux versions) still parse correctly for titles — bell flag defaults to false.
- **Defensive BEL strip in preview:** Added `\a` to `sanitizePreviewContent`'s `strings.NewReplacer` even though `capture-pane` shouldn't contain BEL.
- **All backends updated:** tmux, native (unix + windows), and mock backends all updated to new `GetPaneTitles` 3-return signature.

- **PR:** —
