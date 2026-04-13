# Feature: Fix empty preview when session has insufficient text

- **GitHub Issue:** #88
- **Stage:** DONE
- **Type:** bug
- **Complexity:** S
- **Priority:** P3
- **Branch:** —

## Description

When a terminal session does not have enough content to fill the preview area, the preview pane sometimes appears completely empty instead of showing the partial content that is available. This creates a confusing experience where active sessions look blank even when they have some output. The preview should render whatever text is present, regardless of whether it fills the full preview area.

## Research

### Relevant Code
- `internal/tui/components/preview.go:282-316` — `SetContent`: sets `p.hasContent = content != ""` against the **raw** tmux output, before sanitization. After `sanitizePreviewContent` strips all non-SGR ANSI sequences, the result for escape-heavy / no-text sessions is whitespace-only — but `hasContent` is already true, so no placeholder is shown and the viewport appears blank.
- `internal/tui/components/preview.go:47-67` — `sanitizePreviewContent`: strips all ANSI sequences except SGR color codes, plus control chars. Brand-new tmux panes commonly emit cursor-reset/clear-screen sequences (`\x1b[?1049h`, `\x1b[H`, `\x1b[J`) that are entirely stripped, leaving an empty string.
- `internal/tui/components/preview.go:326` — `View`: only shows "Waiting for output…" placeholder when `!p.hasContent`. The bug means this placeholder is suppressed even when nothing visible was rendered.
- `internal/tui/components/preview_test.go` — existing test suite; has `TestPreviewView_WithContent` and `TestPreviewView_EmptyContent_WithSession` but no test for escape-sequence-only content.
- `internal/tmux/capture.go:11-17` — `CapturePane`: runs `tmux capture-pane -p -e -J -t target -S -500`; returns raw ANSI output including cursor movement and screen-clear sequences.

### Constraints / Dependencies
- Fix must not regress any existing `TestPreviewView_ExactHeight` cases (short content must still fill full pane height)
- `strings.TrimSpace` is the right check — a tmux pane with only a shell prompt like `$ ` still has visible text that survives sanitization, so it correctly shows content

## Plan

Restructure `SetContent` so sanitization happens first and `hasContent` is derived from the trimmed sanitized result.

### Files to Change
1. `internal/tui/components/preview.go` — Refactor `SetContent` (lines 282–316): move `sanitizePreviewContent` call before the `hasContent` assignment; set `p.hasContent = strings.TrimSpace(sanitized) != ""`; skip truncation + viewport update when `!hasContent`.
2. `internal/tui/components/preview_test.go` — Add `TestPreviewView_EscapeOnlyContent_ShowsPlaceholder`: passes a string of non-SGR ANSI escape sequences (e.g. `\x1b[?1049h\x1b[H\x1b[J`) as content; asserts "Waiting for output…" is shown and "No active session" is not. Add `TestSetContent_EscapeOnlyContent_HasContentFalse`: calls `SetContent` with escape-only input and asserts `p.hasContent == false` directly.

### Test Strategy
- `TestPreviewView_EscapeOnlyContent_ShowsPlaceholder` — `preview_test.go` — verifies View() shows placeholder when raw content is all escape sequences
- `TestSetContent_EscapeOnlyContent_HasContentFalse` — `preview_test.go` — verifies `hasContent` is false after SetContent with escape-only input
- All existing tests must still pass (run `go test ./internal/tui/components/...`)

### Risks
- A pane with only whitespace (e.g. blank new terminal) will now show the placeholder — this is the desired behavior but is a subtle semantic change. Visually correct.
- No height-invariant regressions expected since the viewport path is unchanged when `hasContent` is true.

## Implementation Notes

- Restructured `SetContent` to call `sanitizePreviewContent` unconditionally at the top, then gate `hasContent` on `strings.TrimSpace(sanitized) != ""`. No deviation from plan.
- Two new tests added: `TestPreviewView_EscapeOnlyContent_ShowsPlaceholder` and `TestSetContent_EscapeOnlyContent_HasContentFalse`.
- All existing tests pass unchanged; no height-invariant regressions.

- **PR:** —
