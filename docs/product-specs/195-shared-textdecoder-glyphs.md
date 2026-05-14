# GUI: shared TextDecoder across sessions produces garbled glyphs

- **Issue:** #195
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/195-shared-textdecoder-glyphs.md](../exec-plans/active/195-shared-textdecoder-glyphs.md)

## Problem

Users see replacement characters and garbled multi-byte glyphs in session terminals, most visibly when running Claude (emoji, box-drawing, Powerline `✻`, spinner runes). The frontend uses one module-level streaming `TextDecoder` shared across every `SessionTerm`; partial multi-byte byte sequences buffered at the end of one session's chunk leak into the next session's decode, corrupting both. Claude amplifies the bug because its output is unusually dense in multi-byte UTF-8, so chunk boundaries land mid-rune frequently.

## Desired behavior

Each session's output is decoded independently. Multi-byte glyphs from Claude (and any other TUI) render correctly even when multiple sessions stream concurrently. No spontaneous U+FFFD characters appear in well-formed UTF-8 output.

## Success criteria

- A unit test interleaves PTY chunks from two sessions where each chunk splits a multi-byte rune at the boundary, and the decoded output contains no `U+FFFD` and matches the original strings exactly.
- Manual: running `claude` in two concurrent sessions for several minutes shows clean glyph rendering — no replacement characters, no drifting box-drawing.

## Non-goals

- Fixing CJK / wide-char column misalignment in the vt10x snapshot (tracked in #142). That is a separate column-width problem, not a decode problem.
- Switching the wire format from base64 to binary frames.

## Notes

- Bug surface: `cmd/hivegui/frontend/src/main.js` — module-level `decoder` and `SessionTerm.writeData`.
- Related: #190 (xterm renderer atlas corruption, fixed by PR #191) — same symptom ("garbled glyphs over time, worse with Claude") but a different mechanism. #190 was renderer/atlas; this is decoder-state contamination across sessions. After #191 shipped, users still reported glyphs — that residue is what this issue addresses.
- Related: #142 (wide-char snapshot column misalignment) — same neighborhood of code, different root cause.
