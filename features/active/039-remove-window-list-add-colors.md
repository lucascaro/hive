# Feature: Remove window list from title bar and add more text colors

- **GitHub Issue:** #39
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** feature/39-window-list-colors-pane-title

## Description

Three UI improvements:

1. **Remove window list from title bar** — The title bar currently displays a window list that adds clutter. Remove it for a cleaner look.
2. **Add more colors to text** — Improve visual distinction by adding more color to text elements throughout the UI.
3. **Show the current terminal title** — Each agent (Claude, Codex, etc.) sets its terminal title via OSC 0/2 escapes. Surface that title:
   - **Always** in the attached full-screen view (tmux `status-left`).
   - **Conditionally** in the grid view tiles, only when the cell has enough room.
   - **Not** in the app's bottom statusbar.

## Research

### Part 1 — Window list in title bar

The "title bar" here is the **tmux status bar** that we install when attaching to a session. We override a handful of `status-*` options but never touch `window-status-format`, so tmux falls back to its default and renders the session's window list between `status-left` and `status-right`. With our purple status background it shows up as cluttered window names/indices alongside the session title.

### Part 2 — Text colors

Theme is centralized in `internal/tui/styles/theme.go`. There's already a solid base palette (accent purple, success green, warning amber, error red, muted gray) plus per-agent colors and a 10-color project palette. The opportunity is to apply these existing tokens to elements that currently render as plain `ColorText` or `ColorMuted` (breadcrumbs, hints, sidebar metadata, empty states).

### Part 3 — Pane title surfacing

**Big news: the data pipeline already exists.** PR #56 (two-tier session status detection) added `tmux.GetPaneTitles()` and wired it into the status poll. Every ~2s, `scheduleWatchStatuses` batch-fetches a `map[target]title` for every window in the hive tmux session via a single `list-windows -F '#{window_index}\t#{pane_title}'` call. Today those titles are passed into `escape.WatchStatuses()` purely for regex-based status detection (WaitTitleRe / RunTitleRe) and then discarded — they never land in the model.

So Part 3 is mostly **plumbing the existing data through to two render sites**, plus a small format-string change in the attach script:

- **Attach view (always):** Inject `#{pane_title}` into tmux's `status-left` format string so the bar shows `[live agent title]` next to our static session header. tmux interpolates the format on every redraw — no polling on our side needed for this surface.
- **Grid view (conditional):** Stash the latest titles map on the Model after each poll, hand it to `GridView` the same way `SetContents`/`SetProjectNames` do today, and conditionally render a one-line subtitle inside each cell when there's height to spare.

### Relevant Code

**Part 1 — Window list:**
- `internal/tui/views.go:226-235` — `statusBarOpts` slice listing the tmux options we save/restore on attach. Does **not** include `window-status-format` / `window-status-current-format` / `window-status-separator`, which is why tmux's default window list leaks through.
- `internal/tui/views.go:237-276` — `buildAttachScript()` builds the shell script that saves old options, applies our overrides (`status on`, `status-position top`, purple `status-style`, custom `status-left`/`status-right`), runs `tmux attach-session`, then restores. New options need to be added in three places: `statusBarOpts`, the `tmux set-option` block (lines 256-264), and they'll automatically get save/restore by virtue of being in `statusBarOpts`.
- `internal/tui/views.go:278-305` — `buildSessionHeader()` builds the title text rendered in `status-left` (status dot, agent type, session title, project, worktree branch). Useful context: this is the only thing the user actually wants to see in the bar.
- `internal/tui/app_test.go:881-910` — `TestBuildAttachScript_*` tests the script content. Will need a new assertion that `window-status-format` is set to an empty/hidden value.

**Part 2 — Text colors:**
- `internal/tui/styles/theme.go:12-34` — Base color tokens (`ColorAccent`, `ColorMuted`, `ColorSuccess`, `ColorWarning`, `ColorError`, `ColorText`, `ColorSubtext`) and per-agent `AgentColors` map. All new color usage should reuse these tokens; no new hex literals.
- `internal/tui/styles/theme.go:62-103` — Existing styles: `ProjectStyle`, `TeamStyle`, `SessionStyle`, `OrchestratorStyle`, `HelpKeyStyle`/`HelpDescStyle`, `MutedStyle`, `ErrorStyle`. New styles for colorized elements should live alongside these.
- `internal/tui/styles/theme.go:107-123` — `ProjectPalette` + `NextProjectColor`/`NextFreeColor`. Project rows in the sidebar already get cycled colors via this; the breadcrumb does not.
- `internal/tui/components/statusbar.go:60-174` — Breadcrumb and hint rendering. Breadcrumb path segments and `/` separators are plain text on `StatusBarStyle`. Hint keys use `HelpKeyStyle` (accent) but descriptions are uniformly `ColorMuted`.
- `internal/tui/components/sidebar.go:254-390` — Sidebar list. Empty-state messages (`286-287`), team rows (`356-362`), worktree badge (`374-376`) all use `MutedStyle`/`TeamStyle` and could carry semantic color (e.g., worktree badge in `ColorSuccess` or accent).
- `internal/tui/components/gridview.go` — Grid view styling; secondary surface for color polish.

**Part 3 — Pane title surfacing:**

*Data source (already implemented):*
- `internal/tmux/capture.go:42-63` — `GetPaneTitles(tmuxSession)` runs `tmux list-windows -t <s> -F '#{window_index}\t#{pane_title}'` and returns `map["session:windowIdx"]title`. One tmux call per session, batched across all windows.
- `internal/mux/interface.go:51-53,170-177` — `mux.GetPaneTitles()` public forwarder; returns `nil, nil` for the no-backend test path.
- `internal/mux/tmux/backend.go:62-63` — Backend adapter delegates to `tmux.GetPaneTitles`.

*Existing poll site:*
- `internal/tui/handle_preview.go:112-146` — `scheduleWatchStatuses()`. Lines 127-133 already call `mux.GetPaneTitles(mux.HiveSession)` every `PreviewRefreshMs*2` and pass the result into `escape.WatchStatuses(...)`. The titles map is currently consumed by status detection only. Two options for surfacing it to the model:
  1. Store directly on `m.paneTitles` here (simple, but mutates outside the message handler).
  2. Extend `escape.WatchStatuses` / `StatusesDetectedMsg` to round-trip the titles back to the main loop (cleaner; mirrors how `Contents` already flows back).
  Option 2 is the right shape since the rest of the per-poll state (statuses, contents) already moves through that message.

*Status message + handler:*
- `internal/escape/watcher.go:54-145` — `WatchStatuses()` returns `StatusesDetectedMsg`. Currently the msg has `Statuses` and `Contents` maps but no `Titles`. Add `Titles map[string]string`.
- `internal/tui/handle_session.go` (around the `handleStatusesDetected` handler) — Where statuses get applied to the model; same handler should copy `msg.Titles` into `m.paneTitles`. Also where `handleSessionKilled` lives — needs to `delete(m.paneTitles, target)` alongside the existing `contentSnapshots` cleanup.

*Model storage:*
- `internal/tui/app.go` `Model` struct — Add `paneTitles map[string]string` field, initialized in `New()`. Strictly transient; never persisted to disk (titles are ephemeral OSC state, not session metadata).

*Grid render site:*
- `internal/tui/components/gridview.go` (cell layout in `renderCell`, ~lines 216-321) — Current header line is one row: status dot + agent badge + session title + project + worktree badge, with `ansi.Truncate` handling overflow. `innerH = h - 3` (border rows + header). Add a `SetPaneTitles(map[string]string)` method (mirror of `SetContents`) and inject a one-line subtitle between the header and the content preview when (a) the title is non-empty and (b) the cell has spare height (e.g. `h >= 8`, leaving ≥5 lines of preview). Truncate to `innerW` with `ansi.Truncate`. Render in `MutedStyle` italic so it reads as metadata, not content.
- `internal/tui/viewstack.go` (`refreshGrid`) and `internal/tui/handle_preview.go` (`handleGridPreviewsUpdated`) — Both already call `gridView.SetContents(...)` / `SetProjectNames(...)`; add a parallel `SetPaneTitles(m.paneTitles)` call in both spots so the grid stays in sync after polls and view switches.

*Attach view render site:*
- `internal/tui/views.go:256-264` — `status-left` is currently set to `' <escaped session header> '`. To inject the live pane title, build a format string that combines the escaped static prefix with a literal `#{pane_title}` (which tmux will interpolate on each redraw). Critical: `buildSessionHeader` escapes `#` → `##` to suppress tmux interpolation, so the `#{pane_title}` token must be **appended after** the escaped portion, not built inside `buildSessionHeader`. Suggested format: `' <escaped header> · #{pane_title} '`. Bump `status-left-length` if needed (currently 200, probably fine).

### Constraints / Dependencies

- **Save/restore symmetry (Part 1):** Anything we add to `statusBarOpts` must round-trip cleanly through `show-option` / `set-option -u` so we don't leave stale overrides on the user's tmux session after detach. The existing loop already handles this — just keep new options in `statusBarOpts`.
- **Hiding vs. emptying window-status (Part 1):** tmux still allocates space for window-status even if `status-left-length` is large. The clean approach is `set window-status-format ''` and `window-status-current-format ''`; an alternative is `set window-status-separator ''` plus empty formats. Need to verify both formats are truly suppressed (especially the current window).
- **Dark-bg contrast (Part 2):** All foreground colors must remain readable on `ColorBg` (`#111827`) and on `StatusBarStyle`'s `#1F2937`. The existing palette is already vetted; reuse it and avoid introducing dim/saturated colors that fail WCAG.
- **Semantic consistency (Part 2):** Red/green/amber are already load-bearing (error/success/waiting). Don't repurpose them for decorative text or it'll dilute their meaning in status dots and badges.
- **Format-string escaping (Part 3, attach view):** `buildSessionHeader` doubles `#` to `##`. The literal `#{pane_title}` must be concatenated **after** that escape pass. Reordering this is the most likely place to introduce bugs — add a test that asserts the unescaped token appears in `status-left`.
- **Title content is untrusted (Part 3):** Pane titles come from whatever the agent emits via OSC 0/2. They can contain control chars, ANSI escapes, or be wildly long. tmux itself sanitizes for `status-left` rendering, but in the grid we render via lipgloss → must strip control chars and rely on `ansi.Truncate(title, innerW, "…")` to bound width. No regex parsing of the title content — display verbatim.
- **Grid space budget (Part 3):** Cell minimum height is 6 lines today; reserving a row for the title eats 1/6 of the cell. Gate the title row on `h >= 8` (or similar) so small grids stay information-dense. This is a render-time decision per cell, not a global toggle.
- **Polling cost (Part 3):** Already paid by PR #56's status poll — no new tmux calls. Just plumb the existing map through.
- **No persistence (Part 3):** Pane titles are ephemeral display state. They live on the in-memory `Model`, never in `state.Session` or any JSON file. Cleanup on session kill mirrors existing `contentSnapshots` cleanup.
- **Scope discipline:** This is an S-complexity ticket. Focus on cheap, high-signal additions (breadcrumb separators, hint variation, badge colorization, the pane-title plumbing which is already mostly built). Resist the urge to redesign the palette or restyle the preview pane (which already passes through agent ANSI colors). Part 3 is tractable *because* the data already flows; if we discover the plumbing is messier than expected, we can ship Parts 1 + 2 first and split Part 3 into its own issue.

## Plan

This ticket is delivered as **three independent slices** that can land in a single PR. Each slice is internally cohesive and could in principle be reverted on its own. Implement in order: Part 1 → Part 2 → Part 3 (Part 3 is the largest; if anything turns sour we ship 1+2 and split 3).

### Architectural decisions

- **Part 3 — title plumbing:** Use the existing `escape.WatchStatuses` round-trip rather than mutating Model from `scheduleWatchStatuses`. We add a `Titles` field to `StatusesDetectedMsg` and populate it as a pass-through (`WatchStatuses` already receives `titles` as a parameter — it just stops dropping them on the floor). This keeps Model mutation confined to `Update()`/handler paths, matches AGENTS.md's "only mutate state from Update()" rule, and requires zero changes to `escape/watcher_test.go` test fixtures (they already pass `titles` in).
- **Part 3 — title staleness:** The `titles` map in `scheduleWatchStatuses` is captured ~2s before the tick fires, so by the time it reaches `handleStatusesDetected` it's ~2-4s old. That matches the cadence the user already sees for status detection — acceptable for "FYI" display. We do **not** move `GetPaneTitles` inside the tick callback because that would force `escape/watcher_test.go` to mock `mux.GetPaneTitles` (currently passes the map in directly).
- **Part 3 — Model field key shape:** `m.paneTitles` is `map[string]string` keyed by `target` (`"hive:0"`), not by sessionID. This matches `mux.GetPaneTitles`' return shape and the grid render lookup (`gv.paneTitles[mux.Target(sess.TmuxSession, sess.TmuxWindow)]`). Replace the whole map every poll (don't merge) so dead session entries naturally fall out — no explicit cleanup needed in `handleSessionKilled`.
- **Part 3 — attach view:** `#{pane_title}` is appended to the literal `status-left` shell argument **after** `buildSessionHeader`'s `#`→`##` escape pass. The format becomes `' <escaped header> · #{pane_title} '`. tmux interpolates `#{pane_title}` on every status redraw — no additional polling on our side.
- **Part 3 — grid render:** Add a one-line italic muted subtitle row between the existing header and the content preview. Gate on `h >= 8` (current minimum is 6, so we still render the existing header + 5+ content rows when title row is omitted) **and** non-empty title. Use the same `ansi.Truncate(title, innerW, "…")` pattern as the existing header. **Pre-strip control characters** (titles are untrusted OSC payload).
- **Part 2 — color scope:** Conservative, additive changes only — no new hex literals, no palette redesign. Reuse `ColorAccent`, `ColorMuted`, `ColorSuccess`, `ColorSubtext`. Three concrete additions: (a) cycle hint description colors using the project palette per item (visual variation, no semantic meaning), (b) colorize breadcrumb `/` separators with `ColorAccent`, (c) brighten the worktree `⎇` badge from `MutedStyle` to a subtler accent.

### Files to Change

**Part 1 — Remove window list (smallest, lowest risk):**

1. `internal/tui/views.go:226-235` — Append three entries to `statusBarOpts`:
   ```go
   "window-status-format",
   "window-status-current-format",
   "window-status-separator",
   ```
   The existing save/restore loop will pick them up automatically. No other plumbing needed for round-trip safety.

2. `internal/tui/views.go:256-264` — Inside the `tmux set-option` block in `buildAttachScript`, after the existing `status-right-length` line, add:
   ```go
   "tmux set-option -t "+s+" window-status-format ''",
   "tmux set-option -t "+s+" window-status-current-format ''",
   "tmux set-option -t "+s+" window-status-separator ''",
   ```
   Empty format strings collapse the window list entirely (verified against tmux 3.x behavior). The separator override is belt-and-suspenders — without it tmux still draws single-character separators between empty formats in some versions.

3. `internal/tui/app_test.go:883-911` — Extend `TestBuildAttachScript` with three new assertions:
   ```go
   if !strings.Contains(script, "window-status-format ''") {
       t.Error("script should hide window list via empty window-status-format")
   }
   if !strings.Contains(script, "window-status-current-format ''") {
       t.Error("script should hide active window via empty window-status-current-format")
   }
   if !strings.Contains(script, `had_window_status_format" = 1`) {
       t.Error("script should save/restore window-status-format via had_* flag")
   }
   ```
   The third assertion verifies the new option got picked up by the save/restore loop (which derives variable names by replacing `-` with `_`).

**Part 2 — Text colors (additive, theme-only):**

4. `internal/tui/styles/theme.go:97-103` — Add a small helper that returns a project-palette color for an integer index, and a new `BreadcrumbSeparatorStyle`:
   ```go
   BreadcrumbSeparatorStyle = lipgloss.NewStyle().Foreground(ColorAccent)
   WorktreeBadgeStyle       = lipgloss.NewStyle().Foreground(ColorSubtext) // brighter than MutedStyle, dimmer than ColorText
   ```
   Reuse existing tokens — no new hex literals.

5. `internal/tui/components/statusbar.go:120` — In `buildBreadcrumb`, replace `strings.Join(parts, " / ")` with a manual join that wraps the `/` separator in `BreadcrumbSeparatorStyle`:
   ```go
   sep := " " + styles.BreadcrumbSeparatorStyle.Render("/") + " "
   return strings.Join(parts, sep)
   ```
   Also update the two error/installing branches at lines 115 and 118 to use the same join helper. Audit `ansi.StringWidth` math in `View()` (line 75 logs `ansi.StringWidth(rawBreadcrumb)`) — `ansi.StringWidth` already accounts for ANSI colors, so the truncate-to-`innerW` logic at line 61 stays correct.

6. `internal/tui/components/statusbar.go:168-173` — In `buildHints`, vary hint description colors per index using `styles.NextProjectColor(i)`. Each hint description gets a different palette color so the hint row reads as a colored sequence rather than monochrome muted text:
   ```go
   for i, h := range hints {
       descColor := lipgloss.Color(styles.NextProjectColor(i))
       descStyle := lipgloss.NewStyle().Foreground(descColor)
       parts = append(parts, styles.HelpKeyStyle.Render(h.key)+":"+descStyle.Render(h.desc))
   }
   ```
   Keys remain `ColorAccent` (purple) for visual anchor; descriptions cycle through the 10-color palette for differentiation. The status legend stays muted.

7. `internal/tui/components/sidebar.go:374-376` — Replace `MutedStyle` with `WorktreeBadgeStyle` for the worktree badge so worktree sessions get a subtle visual lift:
   ```go
   worktreeBadge = " " + styles.WorktreeBadgeStyle.Render("⎇ "+item.WorktreeBranch)
   ```
   And the empty-branch fallback two lines below.

**Part 3 — Pane title surfacing (largest, depends on Parts 1 + 2 patterns):**

8. `internal/escape/watcher.go:49-52` — Add a `Titles` field to `StatusesDetectedMsg`:
   ```go
   type StatusesDetectedMsg struct {
       Statuses map[string]state.SessionStatus // sessionID → detected status
       Contents map[string]string              // sessionID → captured pane content (for next diff)
       Titles   map[string]string              // target ("session:windowIdx") → pane title; pass-through from input
   }
   ```
   At line 143, return `StatusesDetectedMsg{Statuses: statuses, Contents: contents, Titles: titles}`. The `titles` parameter is already in scope (it was previously consumed for status detection only). No other changes to `WatchStatuses`. **No test changes needed** — existing tests assert on `Statuses`/`Contents` and ignore extra fields.

9. `internal/tui/app.go:80-83` — Add a `paneTitles` field to `Model` immediately after `contentSnapshots`:
   ```go
   // paneTitles holds the most recent pane title (set by agents via OSC 0/2)
   // for each tmux target ("hive:N"). Refreshed every status poll, never persisted.
   paneTitles map[string]string
   ```

10. `internal/tui/app.go:132-134` — Initialize in `New()`:
    ```go
    paneTitles:       make(map[string]string),
    ```

11. `internal/tui/handle_session.go:121-155` — In `handleStatusesDetected`, after the content/stableCounts update loop and before the status-change loop, replace the model's title map wholesale:
    ```go
    if msg.Titles != nil {
        m.paneTitles = msg.Titles
        if m.HasView(ViewGrid) {
            m.gridView.SetPaneTitles(m.paneTitles)
        }
    }
    ```
    Wholesale replacement (not merge) handles dead-session cleanup automatically.

12. `internal/tui/components/gridview.go:46-56` — Add a `paneTitles` field to `GridView`:
    ```go
    paneTitles    map[string]string // target → pane title
    ```
    Add a setter mirroring `SetContents` after line 77:
    ```go
    // SetPaneTitles updates the per-target pane title map used in cell subtitles.
    func (gv *GridView) SetPaneTitles(titles map[string]string) {
        gv.paneTitles = titles
    }
    ```

13. `internal/tui/components/gridview.go:216-321` — In `renderCell`, after the `headerLine` is built (line 290) and before the content preview block, conditionally build a subtitle row:
    ```go
    // Optional pane-title subtitle: only render when the cell is tall enough
    // to spare a row without crushing the content preview.
    var subtitleLine string
    showSubtitle := false
    if h >= 8 {
        if t := gv.paneTitles[mux.Target(sess.TmuxSession, sess.TmuxWindow)]; t != "" {
            t = sanitizePaneTitle(t)
            if t != "" {
                trunc := ansi.Truncate(t, innerW, "…")
                subtitleLine = lipgloss.NewStyle().
                    Foreground(styles.ColorMuted).
                    Italic(true).
                    Width(innerW).MaxWidth(innerW).
                    Render(trunc)
                showSubtitle = true
                innerH-- // give the row back from the content area
                if innerH < 1 {
                    innerH = 1
                }
            }
        }
    }
    ```
    Then change the `JoinVertical` at line 316 from:
    ```go
    inner := lipgloss.JoinVertical(lipgloss.Left, headerLine, contentStr)
    ```
    to:
    ```go
    parts := []string{headerLine}
    if showSubtitle {
        parts = append(parts, subtitleLine)
    }
    parts = append(parts, contentStr)
    inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
    ```
    Add a `sanitizePaneTitle` helper at the bottom of the file that strips ANSI escapes and control characters (mirror `escape.stripANSI`'s regex but also strip `\x00-\x1f` and `\x7f`). Pane titles are untrusted OSC payload; tmux sanitizes for its own status bar but lipgloss does not.

14. `internal/tui/viewstack.go:154-168` — In `refreshGrid`, add `m.gridView.SetPaneTitles(m.paneTitles)` after `SetProjectColors`. Same in `openGrid` (line 170-178).

15. `internal/tui/app.go:184-186` — In `restoreGrid`, add `m.gridView.SetPaneTitles(m.paneTitles)` after `SetProjectColors`. (At restore time `m.paneTitles` may be empty if no poll has run yet — that's fine, the grid will just not render subtitles until the first poll fills it.)

16. `internal/tui/views.go:260` — Inject `#{pane_title}` into the `status-left` format string. The current line is:
    ```go
    "tmux set-option -t "+s+" status-left "+sq(" "+title+" "),
    ```
    Change to:
    ```go
    "tmux set-option -t "+s+" status-left "+sq(" "+title+" · #{pane_title} "),
    ```
    Critical: the `#{pane_title}` literal is **outside** the `title` parameter (which has been through `buildSessionHeader`'s `#`→`##` escape). The `sq` shell-quoting wrapper does not transform `#`, so the token survives intact through to tmux, which interpolates it on each redraw. No change to `status-left-length` needed (currently 200, plenty of headroom).

17. `internal/tui/app_test.go:883-911` — Extend `TestBuildAttachScript` with assertions for the pane-title injection:
    ```go
    if !strings.Contains(script, "#{pane_title}") {
        t.Error("script should inject literal #{pane_title} into status-left for tmux interpolation")
    }
    // The escaped session header should be present once and not have the pane_title token doubled
    if strings.Count(script, "#{pane_title}") != 1 {
        t.Error("expected exactly one #{pane_title} token in script")
    }
    ```

### Test Strategy

**Automated:**
- `go test ./...` — full suite must pass.
- `internal/tui/app_test.go` — extended `TestBuildAttachScript` covers Parts 1 + 3-attach (window-status options hidden, save/restore symmetry, `#{pane_title}` injected exactly once and not double-escaped).
- `internal/escape/watcher_test.go` — existing tests must pass without modification (additive `Titles` field is invisible to existing assertions).
- No new tests for the grid render — `gridview.go` has no existing test file and the conditional layout is straightforward enough that visual verification is sufficient for an S-complexity ticket.

**Manual smoke (in this order):**

1. **Part 1 — attach view, no window list:**
   - `go run . start`, create a session, attach.
   - Verify: top status bar shows our title on the left, detach key on the right, **no window names/indices** in between.
   - Detach. Verify the host tmux session (if any) is unaffected — `tmux show -gv window-status-format` should match its pre-attach value.

2. **Part 2 — colors:**
   - Open the main view. Verify: breadcrumb `/` separators are purple. Verify: hint row at the bottom shows colored descriptions (cycling through palette). Verify: a worktree session in the sidebar shows the `⎇` badge in a brighter shade than before.
   - Open settings overlay → close. Verify nothing else regressed visually.

3. **Part 3 — pane title in attach view:**
   - Attach to a Claude session. Verify the top status bar shows `[session header] · [live agent title]` and the title updates as the agent works (Claude updates its terminal title during long operations).
   - Detach. Re-attach. Verify still working.

4. **Part 3 — pane title in grid view:**
   - Open grid view (`g`) with a single session — large cell, `h >> 8`. Verify a one-line italic muted subtitle appears between the header and the preview.
   - Spawn 4-9 sessions. Verify subtitles appear in the larger grid layouts (3×2, 3×3) but disappear gracefully when the cell collapses below `h=8` (try resizing the terminal narrower).
   - Spawn ~12 sessions to force the smallest cells; verify the subtitle row vanishes and the content preview reclaims the row.
   - Kill a session from the grid; verify no stale title leaks into the next render (wholesale replace via `msg.Titles` should handle this — but worth eyeballing).

5. **Part 3 — untrusted title content:**
   - In a custom session, run `printf '\033]2;EVIL\x1b[31mRED\x1b[0m\007'` to inject ANSI into the title. Verify the grid subtitle renders the literal text without color bleeding into adjacent cells.
   - Run `printf '\033]2;%s\007' "$(printf 'a%.0s' {1..500})"`. Verify the long title is truncated to cell width with `…` and doesn't break layout.

### Risks

- **R1 — `window-status-separator ''` may not be honored on older tmux:** The man page says it should work, but in practice some tmux 2.x builds still draw a default separator. Mitigation: also setting `window-status-format ''` and `window-status-current-format ''` should give us empty rendered windows regardless. If a stray separator still leaks through, we have headroom in `status-left-length` (200) to mask it. **Test on tmux 3.0+** (the project's stated minimum) before merging.
- **R2 — `#{pane_title}` interpolation can leak ANSI into tmux's status bar:** tmux generally sanitizes pane titles for status display, but if an agent emits raw escapes via OSC 2 we may see odd colors in the bar. Mitigation: this is tmux's job, and if it's a real problem we wrap as `#{=40:pane_title}` or `#{T:pane_title}` (the latter may interpret formats — verify it doesn't). For initial implementation, plain `#{pane_title}` is correct and matches how every tmux user does this.
- **R3 — `paneTitles` map can be replaced mid-render:** The Bubble Tea event loop is single-threaded, so `handleStatusesDetected` (which writes the map) cannot interleave with `gridView.View()` (which reads it). Safe by construction. The snapshot copy in `scheduleWatchStatuses` (lines 134-143) is for the tick *goroutine* — not relevant to `paneTitles` since we hand the map straight from the message into the model on the main goroutine.
- **R4 — Title staleness during burst activity:** When an agent flips its title rapidly (e.g. spinner frames), the displayed title in the grid lags by ~2-4s. This matches existing behavior for status detection and is acceptable for FYI display. If this becomes a UX issue we can add a faster dedicated title poll later — explicitly out of scope for this S ticket.
- **R5 — Hint row width regression (Part 2):** Cycling colors via `lipgloss.NewStyle().Foreground(...)` allocates one style per hint per render. Negligible perf cost (~12 hints × cheap allocation), but `ansi.StringWidth` correctly handles ANSI escapes so the truncation math at `statusbar.go:66` stays valid. Verified by inspection of the existing logging at line 75.
- **R6 — `BreadcrumbSeparatorStyle` interaction with `ansi.Truncate`:** The new colored separators add ANSI escapes but no display width. `ansi.Truncate(rawBreadcrumb, innerW, "")` at line 61 handles this correctly (it's already used for content with embedded ANSI). Worth eyeballing the `statusLog` output during manual testing for `HEIGHT_MISMATCH` warnings.
- **R7 — Grid subtitle math at the `h=8` boundary:** Decrementing `innerH` by 1 when adding the subtitle row means cells at exactly `h=8` go from 5 lines of content to 4. That's still above the `if innerH < 1` floor and visually fine, but if user feedback says it's cramped we can bump the threshold to `h >= 9`.

## Implementation Notes

Implemented all three slices in a single PR following the plan exactly.  No deviations from the file-by-file mapping.

**Decisions made during coding:**

- **Sanitization regex (Part 3, gridview):** The plan said "strip ANSI escapes and control characters" without specifying how.  Used a single `regexp.MustCompile` covering CSI sequences, OSC sequences (BEL- *or* ST-terminated), and `[\x00-\x1f\x7f]` in one pass:
  ```go
  var paneTitleSanitizeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|[\x00-\x1f\x7f]`)
  ```
  Mirrored the existing `escape.stripANSI` regex shape but added ST termination for OSC and broadened the control-char strip.  Importing `regexp` into `gridview.go` was preferred over duplicating the unexported `stripANSI` from package `escape` because (a) `regexp` is already used in many TUI files and (b) cross-package coupling for one helper would have been messier than a 1-line import.

- **Combined Part 1 + Part 3 attach edits:** Both modify the same `tmux set-option` block in `buildAttachScript`.  Edited them in a single Edit call rather than two separate ones to keep the diff clean.  The new `status-left` value `' <title> · #{pane_title} '` and the three `window-status-*` overrides land in the same chunk.

- **Wholesale title-map replacement:** Plan called for `m.paneTitles = msg.Titles` instead of merging.  Implemented exactly that — guarded by `if msg.Titles != nil` so a synthetic msg from a test wouldn't accidentally clear the map.

- **Test extension:** Added the new assertions to the existing `TestBuildAttachScript` rather than creating a separate test function.  All six new assertions (3 for Part 1, 3 for Part 3) live alongside the existing alt-screen / detach-key checks since they all verify properties of the same generated script.

**Checks:**
- `go build ./...` — clean
- `go vet ./...` — clean
- `go test ./...` — all packages pass (escape, tui, components, styles, state, config, git, hooks, mux/native, tmux)
- CHANGELOG.md updated under `[Unreleased]` with three Added entries and one Changed entry per Documentation Maintenance rules
- No README/docs/ updates needed: changes are visual (colors, hidden window list, subtitle row) and don't introduce new keybindings, CLI flags, config options, or hook events

- **PR:** #58
