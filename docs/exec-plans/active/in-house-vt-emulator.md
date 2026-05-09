# In-house VT emulator (replace hinshun/vt10x)

- **Spec:** TBD (see Problem section below until a product-spec is filed)
- **Issue:** TBD
- **Stage:** IMPLEMENT (Phase 0 only; Phases 1-3 deferred pending Phase 0 outcomes)
- **Status:** active

## Summary

The snapshot path on session reattach is built on `github.com/hinshun/vt10x`. It works, but its data model leaks into our code in fragile ways: we mirror unexported attribute constants, we undo two of its storage transforms (reverse-video FG/BG pre-swap, bold-bright FG+8) every render, and we maintain our own scrollback ring with a heuristic that reverse-engineers evictions from row diffs because vt10x has no scrollback API. The library has been unmaintained since March 2022 (`go.mod` pin: `v0.0.0-20220301184237-5011da428d02`). This plan scopes a narrower in-house terminal model, sized to what the snapshot path actually needs, and a conformance corpus that pins behavior on the apps users run.

## Problem (interim spec)

- **Who hurts:** users reattaching to sessions; agent TUIs (Claude, Codex, vim, htop, less) that paint with mixed SGR / wide chars / scrollback.
- **Trigger:** snapshot generation on reattach via `vt.RenderSnapshot()` against vt10x state.
- **Why current is wrong:**
  - **Upstream abandoned.** Last vt10x release 2022-03-01. No path to fix bugs except fork.
  - **We depend on private bit values.** `vt.go:20-29` mirrors `vtAttrReverse`/`Bold`/`Italic`/`Underline`/`Blink` directly from `vt10x/state.go` because the package keeps them unexported. Any upstream rebase would silently break.
  - **Storage quirks bleed into render.** `writeSGR` (`vt.go:418-461`) undoes two transforms vt10x applies during parse: pre-swapping FG/BG when reverse-video is set, and bumping FG by 8 for `bold && FG<8`. Cells where the user explicitly set bold+bright are indistinguishable from "bold over dim color" in storage; we pick one rendering and live with it.
  - **No scrollback API.** vt10x renders only the visible grid. Our scrollback ring (`history`) is fed by `captureEvictions` (`vt.go:121-149`), which compares pre/post top rows on every Write. The heuristic has needed two fixes already (`499bbc5` post-blank-screen guard, the in-flight #143 chunk-boundary fix) and is structurally a guess.
  - **Per-feature fix cadence.** Since #141 landed 5/5, against vt10x specifically: storage-transform undo (`337a5fe`), CJK column alignment (in-flight #142), 24-bit RGB (#158), scrollback preservation (#162), eviction hardening (`499bbc5`). Five fixes in four days, against an upstream we can't push to.
- **Desired:** a minimal terminal model we own, with a conformance corpus that prevents regressions on the real apps users run inside hive.

## Research

### Current VT integration surface

- `internal/session/vt.go` (505 LoC) — sole importer of `github.com/hinshun/vt10x`. Wraps `vt10x.Terminal` behind a `sync.Mutex`, plus our own `history [][]byte` scrollback ring.
- `internal/session/vt_test.go` (484 LoC) — pins the snapshot/round-trip/SGR/scrollback contract.
- `internal/session/session.go` — call sites at `:180` (Write), `:228` (Resize), `:248` (RenderSnapshot), `:258` (Close).

### vt10x API actually used

- Construction: `vt10x.New(vt10x.WithSize(cols, rows))`
- Lifecycle: `Write([]byte)`, `Resize(cols, rows)` (no error), `Close()` (implicit via GC; we don't call it)
- Query: `Size()`, `Mode() & ModeAltScreen`, `Cursor()` (`X`, `Y`), `CursorVisible()`, `Cell(x, y) Glyph`
- Cell shape: `Glyph{Char rune, Mode int16, FG, BG vt10x.Color}` — `Mode` is a bitfield with **unexported** constants we mirror; `Color` is an `int32` storing low-ANSI [0,16), 256-color [16,256), or 24-bit RGB packed `r<<16|g<<8|b`, with sentinels (`DefaultFG`, `DefaultBG`, `DefaultCursor`) at `>=1<<24`.
- No scrollback API. We reconstruct it.

### Snapshot output contract (RenderSnapshot bytes)

1. (alt-screen only) `\x1b[?1049h`
2. Soft reset `\x1b[!p` + erase scrollback `\x1b[3J` + erase visible `\x1b[2J` + home `\x1b[H`
3. (normal screen only) history rows joined by `\r\n`, then trailing `\r\n`
4. Visible grid via `renderRow`, lines separated by `\r\n`. Trailing blank cells elided with `\x1b[K`. SGR emitted as `\x1b[0;…m` from clean each transition.
5. Final SGR reset `\x1b[m` if needed
6. CUP `\x1b[{row};{col}H` using vt10x's 0-indexed cursor + 1
7. DECTCEM `\x1b[?25h` / `\x1b[?25l` per `term.CursorVisible()`

Tests (`vt_test.go`) pin: round-trip of plain text + bold; reverse-video no-double-apply; alt-screen prefix; RGB FG/BG; default-color sentinels emit nothing; scrollback above viewport; scrollback eviction cap (500 rows); alt-screen does not include normal scrollback; alt-screen round-trip preserves prior scrollback; SGR survives scrollback eviction; `\x1b[2J` does not push to history (single chunk and split chunk); blank rows preserved across evictions; resize over non-empty history.

### Why the pain is library-shaped

- **Unexported attr constants.** `vt.go:20-29` is a direct dependency on private bits. No upstream pressure-valve since 2022.
- **Storage transforms in the render path.** `writeSGR` re-swaps reverse-video FG/BG and demotes bold-bright FG by 8. These exist *only* because vt10x stores cells in a form that's lossy in one direction (bright-bold ambiguity).
- **Scrollback as a side channel.** `captureEvictions` compares pre/post-write top rows, finds the largest k where `preRows[k] == postRow[0]`, pushes `preRows[0..k-1]` onto our ring. Required carve-outs: bail when post is all-blank (else `\x1b[2J` leaks content into scrollback); accept blank match candidates inside legitimate scrolls (else paragraph spacing collapses). Two bug fixes against this heuristic, both visible in tests above.
- **CJK width handling.** vt10x advances the cursor based on its internal width judgment; we render via `renderRow` reading `Cell(x, y).Char` per column, which works as long as both sides agree on width. The in-flight #142 fix patches alignment by injecting blank cells where vt10x left a wide cell straddling columns.

## Approach

Build a minimal terminal emulator behind the existing `internal/session/vt.go` interface. Keep the public surface (`NewVT`, `Write`, `Resize`, `RenderSnapshot`) byte-identical so the swap is one constructor change.

**Why this over staying on vt10x:**
- vt10x is unmaintained upstream. Bugs we hit are bugs we fix locally forever.
- Our render path already owns vt10x's storage quirks via reverse-engineered transforms; cutting them out simplifies the read path (cell → SGR) instead of working around them.
- Scrollback is the load-bearing piece, and our current "infer from row diffs" approach has had two bugs already and is hard to extend (no styled scrollback search, no per-line metadata, no clear scrollback API for callers).

**Why this over swapping to another third-party VT (vt100 by Liam, asciinema's vt, charmbracelet/x/vt, etc.):**
- charmbracelet/x/vt: tried on a prior branch, hit unbuffered-pipe deadlocks needing a drainer goroutine + Close synchronization. Abandoned.
- The snapshot surface is genuinely small (no DCS, no mouse, no charsets, no responses). Vetting + re-debugging another library is comparable to writing the subset, and we still wouldn't own the data model.

**Critical de-risking step (do not skip):** build the conformance corpus *first*, before writing the new emulator. Capture real PTY byte streams from the apps users run, snapshot them with the current vt10x backend as the oracle, and replay against the new emulator in CI. This corpus is valuable even if we never finish the rewrite — it pins regression behavior against vt10x today.

### Phases

**Phase 0 — Conformance corpus (standalone, valuable independently).**
- Capture tool that records PTY output (stdout bytes + final size) into fixture files.
- Capture: Claude Code, Codex, vim, htop, less, fish, plain bash with CJK / emoji / 24-bit color / OSC 8 hyperlinks. Tmux and btop optional.
- Golden = current `RenderSnapshot()` output against vt10x.
- CI test: replay each fixture through the active emulator, diff against golden, fail on mismatch.
- **Ship Phase 0 standalone first.** It tightens the test floor against vt10x today, and becomes the migration safety net if Phase 1+ proceeds.

**Phase 1 — Parser + grid skeleton.**
- VT500 (Paul Williams) parser state machine, transcribed from the published diagram, not hand-rolled. ~400 LoC.
- Grid: 2D cell array, cell = `{Content string, Width uint8, FG, BG color, Attrs uint8}`. No reverse-video pre-swap. No bold-bright FG bump.
- UTF-8 incremental decode (Write may split multi-byte sequences across calls).
- Grapheme + width via `github.com/rivo/uniseg` — do not hand-roll East Asian Width.

**Phase 2 — CSI/SGR/OSC dispatch.**
- CSI: CUU/CUD/CUF/CUB, CUP/HVP, ED, EL, IL/DL, ICH/DCH, SU/SD, DECSTBM (scroll regions — vim/less depend), SGR (38;2;R;G;B and 38;5;N).
- DEC private modes: 1049 / 1047 / 47 (alt-screen), 25 (cursor visibility), 2004 (bracketed paste — passthrough), mouse modes (no-op).
- DECSC/DECRC (save/restore cursor including SGR).
- OSC: 0/2 (title — track or drop), 8 (hyperlinks — preserve in cell metadata or drop on snapshot — see open question), 4/10/11 (color queries — drop, never answer; xterm.js handles client-side).

**Phase 3 — Native scrollback + render.**
- First-class scrollback ring (no eviction heuristic). When SU / IL / scroll-region pushes a row off the top of the visible grid, append it to the ring directly.
- Cell-to-ANSI render with SGR diffing.
- Reproduce exact `RenderSnapshot` byte format (preface, alt-screen prefix, scrollback ≤ 500 rows, visible grid, SGR reset, CUP, DECTCEM).
- Existing `vt_test.go` cases pass with no assertion changes.

**Phase 4 — Parallel run + cutover.**
- Behind a runtime flag (`HIVE_VT_BACKEND=vtemu|vt10x`, default `vt10x`), feed bytes to both emulators, diff snapshots on each `RenderSnapshot`, log mismatches with fixture-style capture.
- Run in dev/canary for one release. Default flips to `vtemu` only when corpus diffs are zero AND parallel-run mismatch rate is zero across N hours of real use (define N concretely before flip — see open question).
- Delete vt10x dependency once cut over.

### Explicit non-goals (defend the scope in review)

- DCS sequences (Sixel, etc.) — swallow and discard
- Mouse tracking modes — passthrough/no-op
- Character sets / SCS / G0–G3 designations — assume UTF-8
- Tab stops beyond default-every-8 — fixed
- Double-height / double-width lines, status line — not used by target apps
- Printer controller mode — no
- Answering terminal queries (DA1/DA2/DECRQM/OSC color) — xterm.js handles client-side; we never write responses

### Files to change

- `internal/session/vt.go` — swap backend; keep public surface byte-identical
- `internal/session/vt_test.go` — keep existing assertions as a regression floor (drop the `vt10x.`-typed sanity assertions inside individual tests; rewrite to use the new public types or assert via snapshot bytes)
- `internal/session/session.go` — no changes expected; call sites at `:180`/`:228`/`:248`/`:258` use the public surface only
- `go.mod` / `go.sum` — drop `github.com/hinshun/vt10x` (Phase 4 cutover only)

### New files

- `internal/session/vtemu/parser.go` — VT500 state machine
- `internal/session/vtemu/grid.go` — cell grid + scrollback ring
- `internal/session/vtemu/sgr.go` — SGR parse + render diffing
- `internal/session/vtemu/csi.go` — CSI dispatch table
- `internal/session/vtemu/osc.go` — OSC dispatch (title, hyperlinks; ignore color queries)
- `internal/session/vtemu/render.go` — grid → ANSI snapshot
- `internal/session/vtemu/emu.go` — public `Emulator` type matching `vt.go`'s needs
- `internal/session/vtemu/conformance/` — corpus fixtures + replay test
- `scripts/vtcapture/` — PTY capture tool that produces fixtures (dev-only unless users need it; see open question)

### Tests

- Existing `vt_test.go` cases pass against new backend with no assertion changes (after the storage-typed sanity asserts are converted)
- `internal/session/vtemu/conformance_test.go` — replay every fixture, diff against golden
- Property-style tests: random SGR sequences round-trip; random CUP positions land at correct display column with mixed CJK/emoji
- UTF-8 split-write tests: byte-by-byte feed of multi-byte graphemes produces same grid as one-shot write
- DECSTBM tests against vim/less fixtures specifically
- Reverse-video and bold-bright tests that *don't* require storage-transform undo (regression: ensure new backend stores cells in the rendered form, not the parsed-and-mangled form)

### Sizing

~1500 LoC implementation + ~3000 LoC tests for Phases 1-3, plus ~300 LoC for Phase 0 corpus + capture tool. One engineer, ~2 weeks for a solid first cut, with a tail of bug reports as new TUIs hit it. Tests are intentionally heavy — owning the bugs is the whole point, and that requires the corpus.

**Phase 0 alone:** ~1-2 days. Producible as a standalone PR that ships value regardless of whether Phases 1-3 follow.

## Decision log

<Append-only. Empty until implementation starts.>

## Progress

- **2026-05-09** — Plan filed against the wrong backend (`charmbracelet/x/vt`). Rewritten same day against the actual current backend (`hinshun/vt10x`) after eng review caught the premise mismatch.
- **2026-05-09** — Scoped to Phase 0 only. Phases 1-3 deferred pending Phase 0 outcomes (corpus shape, fix cadence post-corpus, vtemu sizing reality check).

## Open questions

- Do we have appetite for the ~2 week implementation + parallel-run period, or do we ship Phase 0 (conformance corpus) only and defer the rewrite? Phase 0 is independently valuable.
- OSC 8 (hyperlinks): preserve through snapshot, or strip? Need to check whether xterm.js renders them on replay and whether any target TUI emits them.
- Where does the capture tool live — `cmd/vtcapture/` (shipped) or `scripts/vtcapture/` (dev-only)? Prefer the latter unless users need it.
- Cutover gate: define "mismatch rate is zero across N hours" concretely (which environment, which sessions, who watches the log).
- Storage representation: do we store cells in rendered form (no transforms) or accept a documented transform layer for memory savings? vt10x's pre-swap of reverse-video colors saves zero memory; bold-bright +8 saves nothing either. Default plan: rendered form, no transforms.
