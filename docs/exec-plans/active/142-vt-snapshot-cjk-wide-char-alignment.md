# vt snapshot: CJK / wide-char column misalignment

- **Spec:** [docs/product-specs/142-vt-snapshot-cjk-wide-char-alignment.md](../../product-specs/142-vt-snapshot-cjk-wide-char-alignment.md)
- **Issue:** #142
- **Stage:** IMPLEMENT
- **Status:** active

## Summary

vt10x advances `cur.X` by 1 per rune regardless of East-Asian width, so its cell grid is in *runes* while xterm.js renders in *display columns*. On reattach the daemon paints the emulator's grid back to the client, and any line containing wide runes ends up with a different column layout than the live stream produced. Live output self-heals on the next byte; the snapshot frame is the only thing that's wrong.

## Research

### Surface area

- `internal/session/vt.go` (270 lines) — `VT` wrapper around `vt10x.Terminal`. Owns `Write`, `Resize`, `RenderSnapshot`. RenderSnapshot iterates `term.Cell(x, y)` for `x := 0..cols-1`, emits each cell's `Char`, then does final CUP using `term.Cursor()`'s `X`/`Y` directly as 0-indexed → 1-indexed columns.
- `internal/session/vt_test.go` (152 lines) — round-trip, reverse-video, alt-screen, resize tests. No wide-char coverage.
- `internal/session/session.go:139,180,228,248` — sole call sites: `NewVT` at construction, `vt.Write` per PTY chunk, `vt.RenderSnapshot` on reattach, `vt.Resize` on size change.

### Where the column math goes wrong

1. **Cursor positioning at end of snapshot** (`vt.go:165-175`). `col := cur.X + 1` treats vt10x's cell index as a 1-indexed display column. For a row with N wide runes preceding the cursor, the live xterm.js cursor sits at `cur.X + N` columns — the snapshot under-shoots by N. This is the smallest, most reliably reproducible piece of the bug.

2. **`lastNonBlank` in cell index** (`vt.go:117-129`). Iterating right-to-left in cell space is correct *for vt10x's storage*, but the trailing `\x1b[K` then runs from the wrong xterm column. In practice `\x1b[K` clearing to EOL at a too-early visual column is harmless when the row is already blank past that point — and it normally is.

3. **Wrap-point divergence**. If the live PTY wrote enough wide runes to fill the xterm.js row (40 wide runes at cols=80), xterm.js wraps to the next row while vt10x continues filling the current row up to 80 cells. After that point the two grids diverge by a whole row, not just by columns. This is the hardest case and is not solvable without a width-aware emulator.

4. **Absolute-CUP overwrites**. A program that uses `\x1b[r;cH` to re-target xterm columns (status bars, progress bars, `tput cup`) addresses xterm's wide-aware columns, but vt10x interprets `c` as cell index. After a wide rune was placed earlier in the row, a CUP targeting "col 10" in xterm space lands at cell-X=10 in vt10x, which is a different visual column. The snapshot then emits cells in vt10x order, which can shift overlay text relative to the wide content. This is the case the issue reporter is most likely seeing.

### Dependency state

- `github.com/hinshun/vt10x v0.0.0-20220301184237-5011da428d02` — pinned to a 2022 commit, upstream effectively dormant. No wide-cell branch.
- `go-runewidth` is **not** currently a dependency.
- Candidate replacements: `github.com/charmbracelet/x/exp/teatest`-adjacent vt packages; `github.com/leaanthony/go-ansi-parser`; building a thin width-aware emulator on top of `mattn/go-runewidth` ourselves.

### Constraints

- `RenderSnapshot` is called on every reattach and must remain cheap (currently O(rows × cols)).
- The snapshot's goal is "what the user saw" — visual fidelity is more important than vt10x-grid fidelity.
- Tests in `vt_test.go` use `term.Cell(x,y)` directly to validate state; any new emulator path must keep that introspection or the tests need rewriting.
- v2 daemon is the only consumer (`internal/session/session.go`).

### Approach options (to lock in during PLAN phase)

1. **Width-aware translation layer in `vt.go`** — keep vt10x storage, add `go-runewidth` to translate cell-index → visual column at emit time. Fixes (1) cleanly. Mitigates (2). Does not fix (3) or (4) — those require true emulator change.
2. **Fork vt10x** — add wide-cell handling at parse time so the grid itself is xterm-compatible (wide rune at X, continuation sentinel at X+1). Fixes all four cases. Highest cost; carries a forked dependency forever.
3. **Replace vt10x** — adopt a maintained emulator that already handles widths. Fixes everything but pulls a larger swap into a recently-shipped path (#141).

## Approach

**Replace `github.com/hinshun/vt10x` with `github.com/charmbracelet/x/vt`.** The Charm package is actively maintained (Charm uses it under their own TUI products), models cells in display columns natively (uses `github.com/charmbracelet/x/wcwidth`), supports alt-screen, and exposes `Render() string` that already produces a self-contained ANSI byte stream of the current screen state. That single method replaces our hand-rolled snapshot loop, the `writeSGR` / `writeColor` reverse-video gymnastics, and the `lastNonBlank` / `\x1b[K` logic.

Why this beats the alternatives:
- vs translation layer over vt10x: vt10x is dormant and the runewidth-only fix doesn't address wrap-point divergence or absolute-CUP overlay shift (cases 3 and 4 in Research). A maintained replacement fixes the whole class.
- vs forking vt10x: maintaining a fork adds permanent cost. Charm already did the equivalent work and ships it under MIT.

### Files to change

1. `go.mod` / `go.sum` — drop `github.com/hinshun/vt10x`, add `github.com/charmbracelet/x/vt` (and any transitively required Charm modules pinned). Run `go mod tidy`.
2. `internal/session/vt.go` — replace `vt10x.Terminal` with `vt.SafeEmulator`. Reduce `RenderSnapshot` to:
   - if `emu.IsAltScreen()` → emit `\x1b[?1049h`
   - emit `\x1b[!p\x1b[2J\x1b[H` (soft reset + clear + home, same preface as today)
   - emit `emu.Render()` (the full grid as ANSI; encodes SGR + wide cells correctly)
   - emit final CUP using `emu.CursorPosition()` (already in display columns)
   - emit `\x1b[?25h` / `\x1b[?25l` based on cursor visibility (check Charm API for the equivalent of vt10x's `CursorVisible`; if not exposed, default to visible — live PTY corrects on next byte).
   - Delete the entire `writeSGR` / `writeColor` block and the `vtAttr*` constants.
3. `internal/session/vt_test.go` — rewrite to the new API:
   - `TestVTSnapshotRoundTrip` — replace `src.term.Cell(x, y)` with `src.emu.CellAt(x, y)`; bold attribute check uses the Charm cell's style API. Add coverage for a wide-char round-trip on the same test.
   - `TestVTReverseVideoNoDoubleApply` — keep the intent (reverse video must round-trip without double-apply); rewrite against Charm's cell colour model. The vt10x-specific pre-swap commentary becomes obsolete and should be removed.
   - `TestVTAltScreenSnapshot` — substitute `vt10x.ModeAltScreen` check with `emu.IsAltScreen()`.
   - `TestVTResize` — minor renames only.
4. `internal/session/session.go` — call sites at lines 139, 180, 228, 248 are unchanged in shape (`NewVT`, `vt.Write`, `vt.RenderSnapshot`, `vt.Resize`); only the internal `s.vt` field type changes through `vt.go`. Verify no leaking of vt10x types.
5. `CHANGELOG.md` — add `[Unreleased]` entry: `Fix: GUI reattach now renders CJK / wide-emoji rows with correct column alignment.`

### New files

None.

### Tests

All in `internal/session/vt_test.go`:

- `TestVTSnapshotRoundTrip` — existing, retargeted to Charm API. Verifies hello / bold-world round-trip with cell-grid equality.
- `TestVTSnapshotWideCharRoundTrip` — **new**. Writes `こんにちは世界\r\nhello` into the source emulator, renders the snapshot, replays it into a fresh emulator, asserts the wide row's cell grid matches, asserts cursor lands at the same display column on both, asserts a follow-up narrow-char row stays aligned.
- `TestVTSnapshotWideCharCursorPosition` — **new**. Writes `世界abc` (cursor lands at display col 7 on xterm.js semantics), renders snapshot, parses the trailing `\x1b[r;cH` from the snapshot bytes, asserts `c == 7`. Guards specifically against the under-shoot bug.
- `TestVTSnapshotWideCharOverlay` — **new**. Writes wide content, then issues an absolute CUP that lands inside the wide region, then writes narrow text. After round-trip, asserts the narrow text appears at the same display column as the live emulator paints it. Exercises failure mode #4 from Research.
- `TestVTReverseVideoNoDoubleApply` — existing, retargeted. The double-apply guard remains valuable even with the new emulator: if Charm's `Render()` re-emits stored colours plus `;7`, the same regression could resurface.
- `TestVTAltScreenSnapshot` — existing, retargeted to `IsAltScreen()`.
- `TestVTResize` — existing, retargeted.

All tests live alongside source per the `AGENTS.md` convention. No flow-test addition needed since this layer is below the TUI.

## Decision log

- **2026-05-08** — triaged as bug / L / P2.
- **2026-05-08** — Approach: replace vt10x with charmbracelet/x/vt rather than patching width handling on top of vt10x. Why: vt10x is dormant, the patched fix only covers the cursor case, and Charm already ships a maintained width-aware emulator with a `Render()` helper that subsumes our hand-rolled snapshot loop.

## Decision log

- **2026-05-08** — triaged as bug / L / P2.

## Progress

- **2026-05-08** — Spec ingested from #142, exec plan created at RESEARCH stage.
- **2026-05-08** — Approach approved (replace vt10x with charmbracelet/x/vt).
- **2026-05-08** — Implementation landed: `vt.go` rewritten on `vt.SafeEmulator`; `vt_test.go` retargeted; 3 new wide-char regression tests (`TestVTSnapshotWideCharRoundTrip`, `TestVTSnapshotWideCharCursorPosition`, `TestVTSnapshotWideCharOverlay`). All `internal/...` tests pass. CHANGELOG entry added under [Unreleased].

## Open questions

- ~~Does `charmbracelet/x/vt` expose a cursor-visibility flag?~~ Resolved: no direct accessor, but `Callbacks.CursorVisibility` fires on DECTCEM transitions. Tracked in a `cursorVisible` field on `VT`, mutated under `v.mu` (callback fires synchronously inside `Write`).
- ~~Does `Render()` include trailing CUP / EL?~~ Resolved: `Render()` emits only the styled cell grid joined by `\n`. We still emit the soft-reset preface, transform `\n` → `\r\n`, and append our own final CUP + DECTCEM.
