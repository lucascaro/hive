# vt snapshot: RGB / 24-bit colors fall back to default

- **Spec:** [docs/product-specs/144-vt-snapshot-rgb-24-bit-colors.md](../../product-specs/144-vt-snapshot-rgb-24-bit-colors.md)
- **Issue:** #144
- **Stage:** PLAN
- **Status:** active

## Summary

`internal/session/vt.go` `writeColor` drops 24-bit RGB colors to default in snapshot output, so GUI reattach loses RGB styling from modern prompts and TUIs until the app repaints. Decode the RGB-encoded `vt10x.Color` and emit `38;2;R;G;B` / `48;2;R;G;B` SGR sequences.

## Research

### Relevant code

- `internal/session/vt.go:235-270` — `writeColor`. Switch routes `c<8`, `c<16`, `c<256`, default (drops). Default arm is the bug.
- `internal/session/vt.go:186-232` — `writeSGR` callers; emits `\x1b[…m` SGR for adoption.
- `internal/session/vt.go:130-145` — `RenderSnapshot` cell loop calls `writeSGR` per cell when attrs change.
- `internal/session/vt_test.go:15-…` — existing `TestVTSnapshotRoundTrip` uses bold to verify SGR replay; pattern to follow.

### vt10x encoding (verified by reading vendored code)

- `state.go:651, 672` — RGB stored as `Color(r<<16 | g<<8 | b)`, range `[0, 1<<24)`. **No `1<<24+2` offset** (issue body's example is wrong; the real encoding is just `r<<16|g<<8|b`).
- `color.go:27-30` — Sentinels live at `1<<24` (`DefaultFG`), `1<<24+1` (`DefaultBG`), `1<<24+2` (`DefaultCursor`). Always represent "default", never a real color.
- `state.go:637-655, 658-676` — Parser only writes RGB on receipt of `38;2;r;g;b` / `48;2;r;g;b` SGR sequences.
- 256-palette values (0..255) are stored as the bare index — this overlaps RGB encoding for small (r,g,b). vt10x discards the palette/RGB distinction at parse time, so on replay there is no way to tell `Color(5)` apart from RGB `(0,0,5)`. The current `c<8` / `c<16` / `c<256` routing is therefore unchanged.

### Constraints

- Must not change live render path; only snapshot replay.
- Must keep sentinel handling (≥ `1<<24`) → fall through (no SGR), as today.
- Output goes through a `bytes.Buffer` in semicolon-prefixed SGR-fragment style (`;38;2;R;G;B`) — caller wraps in `\x1b[…m`.

## Approach

Insert a new switch arm above `default` that catches `c < 1<<24`: this is the RGB range. Decode `r=(c>>16)&0xff`, `g=(c>>8)&0xff`, `b=c&0xff` and emit `;38;2;R;G;B` (FG) or `;48;2;R;G;B` (BG). Leave the existing default arm to silently drop sentinels.

Why this beats the alternative of decoding only `c >= 1<<24+2`: the issue body's encoding is wrong. There is no `+2` offset; that range is `DefaultCursor`, not RGB. Following the issue body verbatim would mis-decode sentinels as RGB and emit garbage.

### Files to change

1. `internal/session/vt.go` — extend `writeColor` switch to handle the RGB range `[256, 1<<24)`. Update the comment block that currently rationalizes dropping the color.
2. `internal/session/vt_test.go` — add RGB coverage.

### New files

None.

### Tests

- `internal/session/vt_test.go::TestWriteColorRGBForeground` — call `writeColor(buf, vt10x.Color(0xFF8040), true)`, assert buffer contains `;38;2;255;128;64`.
- `internal/session/vt_test.go::TestWriteColorRGBBackground` — same with `isFG=false`, assert `;48;2;255;128;64`.
- `internal/session/vt_test.go::TestWriteColorSentinelsNoOutput` — sanity: `Color(1<<24+2)` (DefaultCursor) emits nothing (regression guard against the issue-body's wrong encoding).
- `internal/session/vt_test.go::TestVTSnapshotRoundTripRGB` — write `"\x1b[38;2;200;100;50mhi\x1b[m"` to a source VT, snapshot, replay into a fresh VT, assert dst cell (0,0).FG equals src cell (0,0).FG (both should be the encoded RGB color).

### Open questions / risks

- Risk: the c<8 / c<16 / c<256 arms still mis-classify low-value RGB encodings (e.g., `Color(5)` as ANSI red). Out of scope — vt10x discards the distinction at parse time; only the modern `38;2;…` path is fixable here. This matches the spec's success criterion (modern TUIs that use `38;2`).

## Decision log

- **2026-05-08** — Reject issue-body's `c >= 1<<24+2` decoding. Why: vt10x stores RGB as `r<<16|g<<8|b` with no offset; `1<<24+2` is `DefaultCursor`. Verified in `state.go:651`.

## Progress

- **2026-05-08** — Spec ingested, triaged S/P2/bug, research complete, plan written.

## Open questions

None.
