# vt snapshot: scrollback above visible viewport not preserved

- **Spec:** [docs/product-specs/143-vt-snapshot-scrollback-above-visible-viewport.md](../../product-specs/143-vt-snapshot-scrollback-above-visible-viewport.md)
- **Issue:** #143
- **Stage:** IMPLEMENT
- **Status:** active

## Summary

Restore the pre-#141 contract that GUI restart preserves scrollback. PR #141 swapped raw-byte replay for a vt10x snapshot of the *current visible screen only*; lines that had scrolled off the top of the viewport are gone. We need to capture rows as they're evicted from the vt10x grid and prepend them (as plain text-with-SGR, no cursor games) to the next `RenderSnapshot()` output.

## Research

### Relevant code

- `internal/session/vt.go:35-52` — `VT` wraps `vt10x.Terminal` with a mutex; constructed in `NewVT(cols, rows)`. The wrapper is the natural home for scrollback retention.
- `internal/session/vt.go:58-62` — `Write(p []byte)` is the single ingress point for PTY bytes into vt10x. Eviction must be detected here, around the underlying `term.Write`, while holding the mutex.
- `internal/session/vt.go:84-184` — `RenderSnapshot()` walks `cols × rows` of the visible grid and emits a self-contained ANSI repaint. It already handles alt-screen entry (`vt.term.Mode()&vt10x.ModeAltScreen`), SGR transitions per cell, padded rows via `\x1b[K`, and final cursor positioning. The fix prepends scrollback before this block but **only when the session is on the normal screen** — alt-screen apps (vim, htop, less, claude TUI) own their own redraw and should not get extra rows pushed at them.
- `internal/session/vt.go:165-181` — Final cursor `CUP` and `DECTCEM`. After prepending scrollback, the absolute row number for the visible viewport remains 1..rows because we'll emit `\x1b[2J\x1b[H` to home to the top, **then** print scrollback, **then** print the live viewport. The CUP at the end still targets viewport-relative `cur.Y+1`, which is still correct on xterm.js's own scrollback model: xterm.js treats the visible rows as the grid and any rows pushed above are scrolled-off history.
- `internal/session/vt.go:190-269` — `writeSGR`/`writeColor` are reusable for rendering the evicted rows; the same path that paints visible cells must paint history cells, otherwise SGR state can leak between the two regions.
- `internal/session/session.go:225-235` — `SubscribeAtomicSnapshot` calls `s.vt.RenderSnapshot()` under the session mutex. No changes here; the new behavior lives entirely inside `VT`.
- `internal/session/vt_test.go` — round-trip and SGR tests exist; the new feature needs new tests in the same style (write known sequence, force scroll, capture snapshot, assert evicted rows appear).
- `docs/native-rewrite/phase-1.md:157`, `docs/native-rewrite/phase-2.md:14-16,212-216` — document the broken contract: relaunch must show scrollback intact.

### vt10x scrollback model

- `vt10x.State.scrollUp(orig, n)` (`state.go:477-489`, in `$GOMODCACHE/github.com/hinshun/vt10x@v0.0.0-20220301184237-5011da428d02/state.go`) **does not retain** scrolled-off lines. It clears `lines[orig..orig+n-1]` and shifts the rest up — the row that was at the top is gone. `scrollDown` is symmetric.
- `vt10x.Terminal` exposes `Cell(x,y)`, `Cursor()`, `Size()`, `Mode()`, `CursorVisible()`. There is **no** evicted-line callback, no scrollback API, and no exported hook into `scrollUp`. A naive shadow-of-row-0 polling approach misses N>1 scrolls inside a single `Write` (e.g., a screen-full of output in one chunk).
- vt10x's only call sites for `scrollUp(t.top, 1)` are line-feed handling (`state.go:236`) and `csiHandle` for `IL`/`SU`. All go through the same `scrollUp(orig, n)` chokepoint.

### Constraints / dependencies

- **No vt10x API for evicted rows.** Either (a) fork/patch vt10x to add a callback, (b) wrap vt10x and detect eviction by line-by-line comparison after each `Write`, or (c) parse PTY bytes ourselves to track newlines + cursor position.
- **Alt-screen.** When the session is on the alt screen, scrollback above the alt-screen viewport is meaningless to the user (vim/htop "own" the screen). We must continue to emit `\x1b[?1049h` first and **not** prepend history in that mode. The history buffer should still be maintained while on alt screen so re-exiting the alt screen presents prior normal-screen scrollback intact (vt10x's normal-screen state survives alt-screen entry; our buffer must too).
- **Cap.** The issue suggests ~500 lines. We need a fixed-size ring to bound memory; cap should be configurable in code (e.g., a const) but not user-tunable in the first cut.
- **SGR continuity.** Each evicted row's snapshot output must reset SGR before/after exactly like the visible-viewport loop already does, so that history rows don't bleed colors into live ones.
- **Wide-char interaction.** Out of scope here — #142 tracks the wide-char misalignment. Whatever we ship for scrollback inherits whatever wide-char behavior #142 produces.
- **RGB color interaction.** Out of scope here — #144 tracks the RGB-fallback issue. Same SGR rendering used for visible cells will be used for history cells, so #144's fix (when it lands) automatically covers history too.
- **Atomicity.** All eviction tracking must happen under `VT.mu` (held during `Write`), so a concurrent `RenderSnapshot()` sees a coherent buffer.

### Approach options surveyed

1. **Fork/vendor a patched vt10x** with a `WithScrollCallback(fn)` option. Cleanest but adds a maintenance burden (replace directive in `go.mod`, fork sync).
2. **Shadow detection in `VT.Write`.** Before `term.Write(p)`, snapshot row 0; after, compare. If row 0 changed and old-row-0 isn't visible elsewhere, it was evicted. **Loses rows** when one `Write` scrolls more than 1 row.
3. **Per-byte stepping.** Iterate PTY bytes, calling `term.Write([]byte{b})` and after each one check whether `cur.Y` jumped past `bottom`. Robust but expensive.
4. **Cursor-jump heuristic.** Capture cursor `Y` before/after `Write`; when post-cursor.Y < pre-cursor.Y while pre-cursor was at `bottom`, infer scroll count from the delta. Still misses screen-clear sequences and CSI scroll-region operations.
5. **Snapshot row 0 every time `cursor.Y == bottom`.** Combine with byte-by-byte stepping at the moment we'd otherwise lose rows.

The cleanest tradeoff is **(1) patched vt10x**, but a viable alternative without a fork is **(3) per-byte stepping with a shortcut**: walk bytes, but only step single-byte when cursor is on the bottom row; otherwise pass through chunks unchanged. This keeps the hot path fast for normal input and only pays the per-byte cost during scroll events.

A third option worth surfacing at plan time: **forget vt10x for history, keep a parallel raw-byte ring for *normal-screen-only* bytes** — and at snapshot time, replay the raw ring into a *fresh, taller* vt10x sized `cols × (rows + history)`, then render the top region as scrollback. This sidesteps the eviction-detection problem entirely at the cost of running a second vt10x instance per snapshot. PR #141's whole point was to stop doing raw replay for the visible region; doing it for *history only* (off-screen, where mid-CSI wraps don't visually matter because the snapshot is rendered fresh) might be a clean compromise.

## Approach

**(Revised 2026-05-08 — see Decision log: dual-vt10x discarded due to CUP corruption.)**

**Heuristic eviction detection + pre-rendered history ring.** In `VT.Write`, while the live mirror is on the normal screen, snapshot the top `rows` rows as `[][]vt10x.Glyph` before calling `term.Write`. After the write, find the largest `k` such that `preRows[k] == postRow[0]`; if `k >= 1`, rows `preRows[0..k-1]` were evicted by a scroll. Render each evicted row to a self-contained ANSI byte string (`\x1b[m...content...\x1b[K`) using `writeSGR`/`writeColor`, and push them onto a ring of capacity `historyRows = 500`. At `RenderSnapshot` time, prepend the ring (joined with `\r\n`) before the visible rows, only on normal screen.

### Why this beats the alternatives

- vs **dual-vt10x ("tall mirror")**: tall mirror breaks on every CUP sequence — `\x1b[1;1H` lands the second mirror at the top of its scrollback region (row 0 of 524) instead of the top of its viewport. Real apps (prompts, status lines, fzf, vim-on-normal-screen) use CUP constantly, so the tall mirror would scribble on its own scrollback.
- vs **forking vt10x for a scroll callback**: cleaner contract but adds a `replace` directive and a fork-sync burden. Heuristic is good enough for the common case (line-additive output that scrolls naturally).
- vs **per-byte stepping with cursor-jump detection**: equivalent semantically but pays the cost on every chunk, including chunks that don't scroll. The heuristic is O(cols × rows) per Write but only on Writes that touch the screen — and the post-write compare is short-circuited by the first match.

### Heuristic edge cases

- **CUP + overwrite** (e.g., prompt redraws row 0): post-row[0] differs from every pre-row[k]; no match; nothing captured. ✓
- **`\x1b[2J` clear**: post rows all blank; if pre-row[0] was non-blank, no match; nothing captured (scrollback before the clear is lost). This matches xterm.js's default behavior — `\x1b[2J` does not push to scrollback. `\x1b[3J` (which would also wipe xterm.js's scrollback) is also a no-capture.
- **Fully-blank evictions**: skip when pre-row[k] is fully blank, to avoid false positives where two identical blank rows make `pre[k] == post[0]` accidentally.
- **Scroll-region operations** (CSI `r` then LF inside): post-row[0] equals pre-row[0] (rows above the scroll region are untouched); k=0 (or no match >= 1); nothing captured. ✓ Region-internal eviction is invisible to user-visible scrollback anyway.
- **Multi-row scrolls** (a chunk containing `\n\n\n\n`): we look for the LARGEST k where `pre[k] == post[0]`, which catches scrolls of up to `rows-1` lines per Write. Larger scrolls (a chunk that pushes more than `rows` lines through) lose the surplus, but real-world chunks rarely write more than a screenful at once.
- **Alt-screen**: skipped entirely. The ring keeps its prior normal-screen content; on alt-screen exit the next normal-screen Write resumes capturing.

### Storage format

Each evicted row is rendered to a `[]byte` immediately at capture time, starting with `\x1b[m` (full reset) and ending with `\x1b[K`. This keeps each entry self-contained (no SGR bleed at boundaries) and dense (~50–200 bytes per row, ~50 KiB max for the full ring).

### Snapshot output ordering (normal screen)

```
\x1b[!p\x1b[3J\x1b[2J\x1b[H              # soft reset + erase scrollback + erase visible + home
<each ring entry, joined by \r\n, terminated by \r\n>   # NEW
<rows rows from live mirror>                             # existing logic, unchanged
\x1b[<cur.Y+1>;<cur.X+1>H                                # CUP — viewport-relative, no offset needed
\x1b[?25h | \x1b[?25l
```

`\x1b[3J` is added so a fresh reattach replaces (rather than augments) any scrollback already present in the receiving terminal. xterm.js's CUP is viewport-relative, so the existing `cur.Y+1` offset is correct after history scrolls into its scrollback automatically.

Alt-screen path unchanged: existing `\x1b[?1049h` prefix, no history prepend, history ring left untouched for re-emergence to normal screen.

Why this beats the alternatives:

- vs **forking vt10x for a scroll callback**: no `replace` directive in `go.mod`, no fork to maintain across upstream bumps. Implementation lives entirely in our package.
- vs **detect evictions in `VT.Write` (cursor-jump heuristic / per-byte stepping)**: vt10x's `scrollUp` is unobservable from outside, and a chunked write that scrolls N>1 rows at once cannot be reverse-engineered without parsing the byte stream ourselves — at which point we *are* re-implementing vt10x. Letting a real vt10x do the parsing is correct by construction.
- vs **raw-byte ring replayed at snapshot time**: equivalent in spirit, but parsing the entire ring on every snapshot is O(history) per snapshot; running an always-on tall mirror is amortized O(byte) per byte and zero-cost at snapshot time except for the render walk.

Cost: ~2× vt10x parse work per byte and a second grid in memory. vt10x is fast and PTY throughput is human-typing-bounded; cap `historyRows` at 500 (≈80×500×8B per cell ≈ ~300 KiB worst case per session).

### Snapshot output ordering (normal screen)

```
\x1b[!p\x1b[2J\x1b[H              # soft reset + clear + home
<historyRows rows from tall mirror, top region>   # NEW
<rows rows from live mirror>                       # existing logic, unchanged
\x1b[<cur.Y+1+historyRows>;<cur.X+1>H              # CUP shifted to account for prepended rows
\x1b[?25h | \x1b[?25l
```

xterm.js will treat any rows pushed above the visible grid as scrollback automatically; it does not need to know which rows are "history" vs "live". The CUP at the end positions the cursor inside the visible region; row offset must add `historyRows` so it lands on the correct screen-line in the receiving terminal.

### Alt-screen behavior (unchanged + 1 new rule)

- If live mirror is on alt screen at snapshot time: existing path runs (`\x1b[?1049h` first, no history prepended). The tall mirror's normal-screen state survives untouched and is available again on next normal-screen snapshot.
- New rule in `Write`: if pre-write mode == post-write mode == normal, also write to tall mirror. Otherwise (mode toggled mid-chunk, or both are alt), drop the bytes for the tall mirror. This loses the toggle-byte chunk from history, which is fine — those bytes are control sequences, not user content.

### Files to change

1. `internal/session/vt.go`:
   - Add `history [][]byte` (ring of pre-rendered ANSI rows) and `const historyRows = 500` to `VT`.
   - Extract the per-row paint loop into a helper `renderRow(buf *bytes.Buffer, term vt10x.Terminal, y, cols int, …)` so capture-time and snapshot-time share it. Refactor `RenderSnapshot` to call it; behavior unchanged for the visible loop.
   - Add `captureRowANSI(term, y, cols) []byte`: renders one row to a self-contained `\x1b[m...\x1b[K` byte string. Wraps the helper with fresh SGR state.
   - Add `pushHistory(b []byte)` that appends to `history` and trims from the front when len > `historyRows`.
   - In `Write`: hold the mutex; capture `preMode = term.Mode()&vt10x.ModeAltScreen`; if normal, copy pre-rows as `[][]vt10x.Glyph`. Do `term.Write(p)`. If pre and post mode are both normal, run the eviction heuristic: find largest `k` in `[1, rows)` where `rowsEqual(preRows[k], term, 0, cols)` and pre-row[k] is non-blank; for `y in [0, k)` skip blank pre-rows, then `pushHistory(captureRowANSI(preRowAsTerm))`. Since pre-rows are glyphs not a terminal, render directly from glyphs — add a `renderRowFromGlyphs` mirror of `renderRow` that takes `[]vt10x.Glyph` instead of a `Terminal`.
   - In `RenderSnapshot`: change the prefix to `\x1b[!p\x1b[3J\x1b[2J\x1b[H`. On normal screen, after the prefix and before the visible loop, write the joined `history` ring with `\r\n` separators and a trailing `\r\n`. The visible-rows loop and final CUP/cursor logic are unchanged (no row offset; CUP is viewport-relative).
   - Helpers: `rowsEqual(glyphs []vt10x.Glyph, term vt10x.Terminal, y, cols int) bool` and `glyphRowBlank(glyphs []vt10x.Glyph) bool`.
2. `internal/session/vt_test.go`: new tests (see below).

### New files

None.

### Tests

All in `internal/session/vt_test.go`, following the existing `TestVTSnapshotRoundTrip` style (write known sequence, render snapshot, optionally feed into a fresh `dst` VT and read back).

1. **`TestVTSnapshotPreservesScrollback`** — Construct `NewVT(20, 5)`. Write `rows*3 = 15` distinct lines like `"line-NN\r\n"`. `RenderSnapshot()`. Feed the snapshot into `dst := NewVT(20, 5+500)` (tall enough to capture history). Assert lines `00..09` are visible in the top region and `10..14` are in the bottom 5 rows.
2. **`TestVTSnapshotScrollbackCappedAtHistoryRows`** — Write `rows + historyRows + 50` distinct lines. After snapshot+round-trip, assert the oldest 50 lines are gone and lines `[50, 50+historyRows+rows)` are present.
3. **`TestVTSnapshotScrollbackSkippedOnAltScreen`** — Write 10 normal-screen lines, then `\x1b[?1049h`, then 10 alt-screen lines. Snapshot. Assert: snapshot starts with `\x1b[?1049h`, contains the alt-screen lines, and **does not** contain the normal-screen lines (those are scrollback that won't be visible while alt mode is up).
4. **`TestVTSnapshotScrollbackSurvivesAltScreenRoundTrip`** — Write 10 normal-screen lines, enter alt, do stuff, exit alt (`\x1b[?1049l`), snapshot. Assert all 10 normal-screen lines are still present in the resulting scrollback.
5. **`TestVTSnapshotScrollbackResize`** — Write 10 lines, `Resize(40, 10)`, write 5 more, snapshot. Assert no panic and that the more-recent 5 lines appear in the visible viewport on the resized snapshot. Don't over-specify which old lines survive — vt10x reflows on resize.
6. **`TestVTSnapshotScrollbackPreservesSGR`** — Write a colored line, scroll it off with plain lines, snapshot. Round-trip into a `dst` VT. Assert the colored cell still has its FG color set when read via `dst.term.Cell(x, y)`.

### Rendering / SGR

Reuse `writeSGR`/`writeColor` unchanged. SGR state must reset between the history block and the visible block: emit `\x1b[m` after the last history row's `\x1b[K`, then start the visible block with attrs reset to defaults. The existing visible-loop already starts with `curMode=0, curFG=DefaultFG, curBG=DefaultBG, atDefault=true` — we keep the same invariant by passing fresh state into the visible loop.

## Decision log

- **2026-05-08** — Set scope: normal-screen scrollback only; alt-screen sessions get the existing snapshot path unchanged. Why: alt-screen apps own their own redraw and never had a "preserved scrollback" contract.
- **2026-05-08** — Picked dual-vt10x ("tall mirror") over fork/heuristic/raw-ring. Why: no fork to maintain; vt10x does the parsing correctly by construction; amortized O(byte) cost beats per-snapshot O(history) reparse.
- **2026-05-08** — `historyRows = 500` constant; not user-configurable in v1. Why: matches the issue's suggestion; ~300 KiB ceiling per session is acceptable.
- **2026-05-08** — Discarded dual-vt10x ("tall mirror") approach. Why: any CUP sequence (`\x1b[r;cH`) lands the second mirror at the wrong row of its grid (top of scrollback vs top of viewport), so prompts and status lines would scribble on scrollback. Replaced with heuristic eviction detection: snapshot top rows of live before each Write, find largest k where `pre[k] == post[0]`, capture `pre[0..k-1]` as evicted, render to ANSI and push to a ring. Loses some edge cases (multi-row scrolls past `rows-1` per chunk; CUP-then-overwrite-then-scroll) but correct on the common path and simple to reason about.

## Progress

- **2026-05-08** — Plan created at RESEARCH stage.
- **2026-05-08** — Advanced to PLAN; approach selected.
- **2026-05-08** — Pivoted approach during IMPLEMENT (CUP corruption in tall mirror); revised plan committed.
- **2026-05-08** — Implementation complete in `internal/session/vt.go`; 7 new unit tests in `vt_test.go` all pass alongside existing 4. Full Go suite passes; `cmd/hivegui` build failure (frontend/dist) is pre-existing and unrelated.

## Open questions

- None blocking. Future: promote `historyRows` to config if users hit the cap.
