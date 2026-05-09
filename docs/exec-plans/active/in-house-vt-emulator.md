# In-house VT emulator (replace charmbracelet/x/vt)

- **Spec:** TBD (see Problem section below until a product-spec is filed)
- **Issue:** TBD
- **Stage:** RESEARCH
- **Status:** active

## Summary

The snapshot path on session reattach is built on `charmbracelet/x/vt`. Since the byte-replay → VT-snapshot switch in #141 we have shipped a steady stream of fixes against that backend: 24-bit RGB (#158), CJK wide chars (#142), scrollback preservation (#162), scrollback eviction heuristic (499bbc5), and three commits to work around `vt.SafeEmulator`'s response pipe deadlocking `vt.Write` (1723038, 04591df, 91553ac). The pattern suggests the library is not built for our use case. This plan scopes a leaner, in-house VT model that implements only what the snapshot path needs — sized to be cheaper to own than to keep patching.

## Problem (interim spec)

- **Who hurts:** users reattaching to sessions; agent TUIs (Claude, Codex) that emit terminal queries on startup.
- **Trigger:** snapshot generation on reattach; query responses written into an unbuffered `io.Pipe` block `vt.Write` and starve the live byte stream.
- **Why current is wrong:** we are accumulating workarounds (drainer goroutine, Close synchronization, eviction heuristics, per-feature snapshot fixes) against a library whose internals we do not control. Each new TUI feature (wide chars, RGB, hyperlinks, …) risks another fix cycle.
- **Desired:** a minimal terminal model we own, with a conformance corpus that prevents regressions on the real apps users run inside hive.

## Research

### Current VT integration surface

- `internal/session/vt.go` — sole importer of `charmbracelet/x/vt` and `ultraviolet`. Wraps `vt.SafeEmulator`.
- `internal/session/vt_test.go` — pins the cell/style/color/wide-char snapshot contract.
- `internal/session/session.go` — call sites at lines 180, 228, 248, 258 (`Write`, `Resize`, `RenderSnapshot`, `Close`).

### VT API actually used

- Construction: `vt.NewSafeEmulator(cols, rows)`, `SetCallbacks({CursorVisibility})`
- Lifecycle: `Write([]byte)`, `Resize(cols, rows)`, `Close()`
- Query: `Width()`, `Height()`, `IsAltScreen()`, `Scrollback()` (`Len`, `Lines`), `Render()`, `CursorPosition()`, `CellAt(x,y)`
- Cell shape (read in tests): `Content` (grapheme), `Width`, `Style.Attrs` (Bold/Italic/Underline/Blink/Reverse), `Style.Fg`/`Bg` (`color.Color`, RGBA via `.RGBA()`), `Style.Equal`
- `ultraviolet.Lines([]Glyph).Render()` for scrollback rendering

### Snapshot output contract (RenderSnapshot bytes)

1. Soft reset `\x1b[!p`, erase `\x1b[2J`, home `\x1b[H`
2. `\x1b[?1049h` if alt-screen
3. Scrollback (normal screen only, capped 500 rows) joined with `\x1b[m\r\n`
4. Visible grid via `Render()` with `\n`→`\r\n`
5. SGR reset `\x1b[m`
6. CUP `\x1b[{row};{col}H` using cursor in **display columns** (`cur.X + 1`)
7. DECTCEM on/off based on tracked `cursorVisible`

### Why the pain is library-shaped

- **Drainer + Close sync (1723038, 04591df, 91553ac):** SafeEmulator answers DA1/DA2/DECRQM/OSC color via an unbuffered `io.Pipe`. With no consumer, the first query blocks `Write`, which blocks `deliver()`, which starves clients. Workaround: `io.Copy(io.Discard, term)` goroutine + `WaitGroup` synchronization on `Close`. xterm.js already answers these queries client-side; we never need responses.
- **CJK fix (#142):** previous backend advanced cursor by 1 per rune, ignoring `Width`. Snapshot column offsets desynced from xterm.js.
- **24-bit RGB (#158):** legacy renderer dropped RGB SGR.
- **Scrollback eviction (499bbc5):** heuristic against the previous backend's behavior.

## Approach

Build a minimal terminal emulator behind the existing `internal/session/vt.go` interface. Keep public surface (`NewVT`, `Write`, `Resize`, `RenderSnapshot`, `Close`, `SetCursorVisibilityCallback`) byte-identical so the swap is one constructor change.

**Why this over staying on charmbracelet:** we have already paid the cost of 6+ fixes against the upstream library and own only workarounds, not the underlying behavior. A model we own with a conformance corpus is bounded work; staying is open-ended.

**Why this over swapping to another third-party VT (vt10x, hinoki, etc.):** the snapshot surface is genuinely small (no DCS, no mouse, no charsets). The cost of vetting and re-debugging another library is comparable to writing the subset we need, and we still wouldn't own the bugs.

**Critical de-risking step (do not skip):** build a conformance corpus *first*, before writing the new emulator. Capture real PTY byte streams from the apps users actually run, snapshot them with the current charmbracelet backend as the oracle, and replay against the new emulator in CI. This is the line between "in-house and stable" and "in-house and we own bugs forever."

### Phases

**Phase 0 — Conformance corpus (standalone, reusable).**
- Tool that records PTY output (stdout bytes + final size) into fixture files.
- Capture: Claude Code, Codex, vim, htop, less, tmux, fish, btop, plain bash with CJK / emoji / 24-bit color / hyperlinks (OSC 8).
- Golden snapshots = current `RenderSnapshot()` output.
- CI test: replay each fixture through the active emulator, diff against golden.
- This is valuable even if we keep charmbracelet — it pins regression behavior.

**Phase 1 — Parser + grid skeleton.**
- VT500 (Paul Williams) parser state machine, transcribed from the published diagram, not hand-rolled. ~400 lines.
- Grid: 2D cell array, cell = `{Content string, Width uint8, Fg, Bg color.Color, Attrs uint8}`.
- UTF-8 incremental decode (Write may split multi-byte sequences across calls).
- Grapheme + width via `github.com/rivo/uniseg` — do not hand-roll East Asian Width.

**Phase 2 — CSI/SGR/OSC dispatch.**
- CSI: CUU/CUD/CUF/CUB, CUP/HVP, ED, EL, IL/DL, ICH/DCH, SU/SD, DECSTBM (scroll regions — many TUIs depend on this), SGR (38;2;R;G;B and 38;5;N).
- DEC private modes: 1049 / 1047 / 47 (alt-screen), 25 (cursor visibility), 2004 (bracketed paste — passthrough), mouse modes (no-op).
- DECSC/DECRC (save/restore cursor including SGR state).
- OSC: 0/2 (title — track or drop), 8 (hyperlinks — preserve in cell metadata or drop on snapshot), 4/10/11 (color queries — drop, never answer).

**Phase 3 — Render + snapshot.**
- Cell-to-ANSI with SGR diffing.
- Reproduce exact `RenderSnapshot` byte format (preface, alt-screen, scrollback ≤ 500 lines, visible grid, SGR reset, CUP in display columns, DECTCEM).
- Scrollback ring with tail-drop.

**Phase 4 — Parallel run + cutover.**
- Behind a build flag or runtime flag, feed bytes to both emulators, diff snapshots on each `RenderSnapshot`, log mismatches with fixture-style capture.
- Run for one release in dev/canary.
- Cut over only when corpus diffs are zero and parallel-run mismatch rate is zero across N hours of real use.
- Delete charmbracelet imports + drainer goroutine + Close `WaitGroup` once cut over.

### Explicit non-goals (defend the scope in review)

- DCS sequences (Sixel, etc.) — swallow and discard
- Mouse tracking modes — passthrough/no-op
- Character sets / SCS / G0–G3 designations — assume UTF-8
- Tab stops beyond default-every-8 — fixed
- Double-height / double-width lines, status line — not used by target apps
- Printer controller mode — no
- Answering terminal queries (DA1/DA2/DECRQM/OSC color) — xterm.js handles client-side; we never write responses, so no response pipe and no drainer

### Files to change

- `internal/session/vt.go` — swap backend; keep public surface byte-identical
- `internal/session/vt_test.go` — keep existing assertions as a regression floor

### New files

- `internal/session/vtemu/parser.go` — VT500 state machine
- `internal/session/vtemu/grid.go` — cell grid + scrollback ring
- `internal/session/vtemu/sgr.go` — SGR parse + render diffing
- `internal/session/vtemu/csi.go` — CSI dispatch table
- `internal/session/vtemu/osc.go` — OSC dispatch (title, hyperlinks; ignore color queries)
- `internal/session/vtemu/render.go` — grid → ANSI snapshot
- `internal/session/vtemu/emu.go` — public `Emulator` type matching `vt.go`'s needs
- `internal/session/vtemu/conformance/` — corpus fixtures + replay test
- `cmd/vtcapture/` (or scripts/) — PTY capture tool that produces fixtures

### Tests

- Existing `vt_test.go` cases pass against new backend with no assertion changes
- `internal/session/vtemu/conformance_test.go` — replay every fixture, diff against golden
- Property-style tests: random SGR sequences round-trip; random CUP positions land at correct display column with mixed CJK/emoji
- UTF-8 split-write tests: byte-by-byte feed of multi-byte graphemes produces same grid as one-shot write
- Scroll region (DECSTBM) tests against vim/less fixtures specifically

### Sizing

~1500 LoC implementation + ~3000 LoC tests. One engineer, ~2 weeks for a solid first cut, with a tail of bug reports as new TUIs hit it. Tests are intentionally heavy — owning the bugs is the whole point, and that requires the corpus.

## Decision log

<Append-only. Empty until implementation starts.>

## Progress

- **2026-05-09** — Plan filed. Origin: discussion identifying a 6+ fix accumulation against `charmbracelet/x/vt` (drainer goroutine, Close sync, RGB, CJK, scrollback) as evidence the library is mismatched to our use case.

## Open questions

- Do we have appetite for the ~2 week implementation + parallel-run period, or do we only fund Phase 0 (conformance corpus) now and defer the rewrite? Phase 0 is valuable standalone.
- OSC 8 (hyperlinks): preserve through snapshot, or strip? Need to check whether xterm.js renders them on replay and whether any target TUI emits them.
- Where does the capture tool live — `cmd/vtcapture/` (shipped) or `scripts/` (dev-only)? Prefer the latter unless users need it.
- Cutover gate: define "mismatch rate is zero across N hours" concretely (which environment, which sessions).
