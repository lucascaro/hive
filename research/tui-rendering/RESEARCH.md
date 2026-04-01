# TUI Rendering Issues: Research & Solutions

## Topic & Scope

Investigate why rendering artifacts still occur in Hive's TUI despite the existing sanitization layer, how
similar projects (claude-squad, etc.) approach the same problems, and what actionable improvements are
available without a full framework rewrite.

---

## Summary

Hive already has a solid rendering architecture: tmux `capture-pane -e -J` → SGR-only sanitization →
tab expansion → charmbracelet bubbles `viewport`. The sanitization layer is correct and comprehensive.
The **remaining artifacts** originate from two distinct problems:

1. **Frame height mismatches** — the rendered frame is occasionally taller or shorter than the terminal,
   causing Bubble Tea to scroll the terminal and leave ghost lines. This is logged (`FRAME_HEIGHT_MISMATCH`)
   but not corrected.

2. **No synchronized output** — Bubble Tea v1 redraws the full frame on every state change. Without
   DEC mode 2026, each redraw is a sequence of cursor movements visible to the terminal emulator and any
   wrapping multiplexer, causing transient flicker and tearing on fast machines or slow terminals.

---

## Current Architecture

### Rendering Pipeline

```
tmux capture-pane -p -e -J -S -500
         │  (raw: SGR + cursor movement + OSC + DCS + CR/VT/FF)
         ▼
sanitizePreviewContent()          internal/tui/components/preview.go:41-64
  • keep only SGR sequences  (ends with 'm', params are digits/;/:)
  • strip cursor movement, clear, OSC, DCS, mode changes
  • remove \r \v \f
         │
         ▼
expandTabs()                       preview.go:71-121
  • ANSI-aware 8-col tab stops
  • required because ansi.Cut() treats \t as zero-width
         │
         ▼
viewport.SetContent() / GotoBottom()
         │
         ▼
app.View()  →  lipgloss.JoinHorizontal / JoinVertical  →  Bubble Tea stdout
```

### Polling Intervals (all configurable via `PreviewRefreshMs`, default 500 ms)

| Watcher            | Interval  | Lines captured | Source                              |
|--------------------|-----------|----------------|-------------------------------------|
| Single preview     | 500 ms    | 500            | `app.go:schedulePollPreview`        |
| Grid overview      | 500 ms    | 200 / session  | `gridview.go:PollGridPreviews`      |
| Title watcher      | 1 000 ms  | all (raw)      | `app.go:scheduleWatchTitles`        |
| Status watcher     | 1 000 ms  | 50             | `app.go:scheduleWatchStatuses`      |

### Key Files

| File | Role |
|------|------|
| `internal/tmux/capture.go` | Thin wrapper for `tmux capture-pane` |
| `internal/escape/parser.go` | OSC-2 / null-byte title extraction |
| `internal/escape/watcher.go` | Title + status polling (uses RAW content) |
| `internal/tui/components/preview.go` | **Critical:** sanitise + viewport render |
| `internal/tui/components/gridview.go` | Grid overview, sanitises each cell |
| `internal/tui/app.go` | Main model; `View()` layout, height validation |
| `internal/config/defaults.go` | `PreviewRefreshMs` default (500) |

---

## Root Causes of Remaining Artifacts

### 1. Frame Height Mismatches (most likely culprit)

`app.View()` (lines 441-469) validates that the final rendered frame is exactly `TermHeight` lines.
When it is not, it logs `FRAME_HEIGHT_MISMATCH(off_by=N)` **but returns the wrong-height frame anyway**.
Bubble Tea then redraws using its cursor-position model, which assumes the previous frame filled the
terminal exactly. An off-by-N frame leaves N stale lines (ghost content) at the bottom or clips the UI.

Known contributing factors (from code comments and structure):
- `lipgloss.JoinVertical` can add/remove a newline at the boundary under some conditions.
- Viewport border frame accounting: `vp.Height` vs `borderHeight` vs actual rendered lines can diverge
  by ±1 when content is shorter than the viewport.
- `computeLayout` uses integer division; rounding can drift by 1 row under narrow terminals.
- `GotoBottom()` called inside `View()` every frame may cause the viewport to report a different
  line count than `Height`.

**To diagnose:** check `~/.cache/hive/hive.log` for `FRAME_HEIGHT_MISMATCH` entries during artifact
occurrence.

### 2. No Synchronized Output (DEC mode 2026)

Bubble Tea v1 does not wrap its renders in the "synchronized output" terminal feature (`\x1b[?2026h` …
`\x1b[?2026l`). Without this, each partial render step (cursor-up, write line, cursor-up …) is
individually visible to the terminal and to tmux's own renderer, producing flicker during rapid updates
(e.g., streaming agent output). Bubble Tea v2 supports this natively; v1 does not.

### 3. Escape Sequence Edge Cases (low risk, well mitigated)

The `allAnsiSeq` regex covers the full ECMA-48 CSI grammar. Rare sequences like 24-bit colour
(`\x1b[38:2:R:G:Bm`) use `:` as a sub-param separator and ARE covered by the regex's param character
class. This is unlikely to be the source of current artifacts.

---

## How Similar Projects Approach This

### claude-squad (smtg-ai/claude-squad)

Uses an almost identical capture strategy:
```go
// session/tmux/tmux.go:CapturePaneContent()
exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-t", t.sanitizedName)
```

Key differences:
- Strips ANSI codes **before content comparison** (for `HasUpdated()` hash), then re-renders raw to
  the preview. Does not use a structured sanitization stage like Hive — relies on the terminal emulator
  to interpret the full escape sequence stream. This means it can display richer content but is more
  vulnerable to cursor-movement corruption inside its TUI panels.
- No explicit height-mismatch detection or correction.

### Claude Code / Ink (Anthropics)

Identified the same flicker issue as a major bug (github.com/anthropics/claude-code/issues/37076).
Resolution path: migrate to a **differential / double-buffered renderer** and add DEC mode 2026
support. This took months and is incomplete for some terminal emulators.

### Community consensus

1. **DEC mode 2026 + tmux 3.4+** is the long-term flicker fix.
2. **Differential rendering** (cell-diff, not full-frame redraw) eliminates ghost lines.
3. **Bubbletea v2** delivers both natively (new import `charm.land/bubbletea/v2`).
4. Short-term workaround: emit `\x1b[?2026h` / `\x1b[?2026l` manually around each render cycle.

---

## Actionable Improvements (Prioritised)

### Short-Term (no framework upgrade)

**A. Fix the height-mismatch at the source** *(highest value)*
- Trace exactly where the off-by-N happens. Likely candidates:
  - `computeLayout` rounding — ensure `ch = TermHeight - statusBarHeight` with no off-by-one.
  - Viewport frame accounting — use `vp.Height` (inner) not `borderHeight` (outer).
  - `lipgloss.Height(result)` before return; if it ≠ `TermHeight`, pad or trim to exact height.
- Padding approach: wrap final output in a `lipgloss.NewStyle().MaxHeight(TermHeight)` or append
  `strings.Repeat("\n", deficit)` when frame is short.
- Reference: `app.go:441-469`, `layout.go:computeLayout`, `preview.go:237-285`.

**B. Manually emit synchronized output markers** *(reduces flicker immediately)*
- Wrap Bubble Tea's output writer to inject `\x1b[?2026h` before and `\x1b[?2026l` after each
  rendered frame. Can be done by wrapping `os.Stdout` before calling `tea.NewProgram`.
- Only effective when terminal + tmux support it (tmux ≥ 3.4 with
  `set -as terminal-features ',xterm*:sync'`). Safely ignored otherwise.
- Requires no API change.

**C. Cache sanitized content by hash**
- `sanitizePreviewContent()` runs the full regex + tab expansion on every `View()` call (every
  Bubble Tea frame). Memoising by SHA-256 of raw input avoids redundant work.
- Store `(inputHash, sanitizedOutput)` pair in `Preview`; only re-sanitise when hash changes.

**D. Move `GotoBottom()` out of `View()`**
- Currently `Preview.View()` calls `p.vp.GotoBottom()` on every frame. This mutates the viewport
  model inside a view function, which violates the Elm architecture and can cause Bubble Tea to
  detect spurious diffs. Move it to `SetContent()` and `Resize()` only.

### Medium-Term

**E. Upgrade to Bubbletea v2**
- New import path: `charm.land/bubbletea/v2`
- Breaking changes: `View()` returns `tea.View` (not `string`), `tea.KeyMsg` → `tea.KeyPressMsg`,
  `tea.Sequentially` → `tea.Sequence`, resize event changed, alt-screen is a `View` field.
- Gain: built-in synchronized output, differential renderer ("Cursed Renderer"), better colour
  downsampling, improved SSH/remote session support.
- Risk: all component `View()` signatures and key-handling code must change.
- Reference: https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md

**F. Differential content updates for grid view**
- `PollGridPreviews` re-captures and re-sanitises every session every 500 ms. Add a per-session
  content hash: only update the viewport for sessions whose hash changed since last poll.

---

## Constraints & Assumptions

- The project targets tmux as the primary backend; native PTY is secondary.
- Go 1.25 + bubbletea v1.3.10 + lipgloss v1.1.0 + bubbles v1.0.0 (all stable releases).
- Bubbletea v2 is released but uses a different module path; upgrading is opt-in and non-trivial.
- tmux 3.4 is needed for DEC mode 2026 passthrough; older versions silently ignore the sequence.
- The existing SGR-only sanitization strategy is correct and should be preserved.

---

## Open Questions

1. **Exact height mismatch trigger:** Under what terminal size / content length does the mismatch
   first appear? Check log for `off_by` values and correlate with pane height changes.
2. **Lipgloss JoinVertical newline behaviour:** Does `JoinVertical` add a trailing newline? Does
   it behave differently when one component is empty?
3. **Bubble Tea v2 compatibility with bubbles/lipgloss:** Are `charmbracelet/bubbles v1` and
   `charmbracelet/lipgloss v1` compatible with the `charm.land/bubbletea/v2` import? (They likely
   need to track the `charm.land/` vanity path too.)
4. **User-visible symptoms:** Are artifacts always ghost lines at the bottom, or also missing lines /
   duplicated content? This distinguishes under-height from over-height mismatches.

---

## Follow-Up Sources

- Bubble Tea v2 upgrade guide: `https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md`
- Synchronized output discussion: `https://github.com/charmbracelet/bubbletea/discussions/1320`
- Claude Code flicker issue: `https://github.com/anthropics/claude-code/issues/37076`
- claude-squad tmux layer: `https://github.com/smtg-ai/claude-squad/blob/main/session/tmux/tmux.go`
- HN discussion on synchronized output in TUIs: `https://news.ycombinator.com/item?id=46701013`
- Hive debug log: `~/.cache/hive/hive.log` (look for `FRAME_HEIGHT_MISMATCH`)
- Key local files: `internal/tui/app.go:375-472`, `internal/tui/components/preview.go:41-285`,
  `internal/tui/layout.go:computeLayout`
