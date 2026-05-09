# vt snapshot: CJK / wide-char column misalignment

- **Issue:** #142
- **Type:** bug
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/142-vt-snapshot-cjk-wide-char-alignment.md](../exec-plans/completed/142-vt-snapshot-cjk-wide-char-alignment.md)

## Problem

PR #141 introduced a vt10x-based snapshot for session reattach. vt10x advances `cur.X` by exactly 1 for every rune and has no double-width-cell concept, but xterm.js renders CJK characters and wide emoji as 2 columns. On reattach, any line in the live session containing CJK / wide emoji is misaligned in the snapshot: cells shift right by N columns where N is the count of wide chars on that row. The shift is purely visual on the snapshot frame — the next live byte from the PTY restores correctness.

## Desired behavior

Reattaching to a session that contains CJK characters or wide emoji renders the snapshot frame with the same column layout xterm.js will use once live bytes flow. No visual shift on reattach; what the user saw before quitting matches what they see when reopening.

## Success criteria

- Reattach to a session running `echo こんにちは世界` (or similar wide-char content) shows the line correctly aligned with no shift.
- Reattach with wide emoji (e.g. flags, family emoji) renders aligned with subsequent narrow content on the same row.
- No regression for ASCII-only sessions.

## Non-goals

- Combining characters / grapheme clusters beyond what `go-runewidth` covers.
- Bidi / RTL handling.
- Re-architecting the snapshot path beyond what's needed to fix width.

## Notes

- Found in adversarial review of #141.
- Possible approaches: fork vt10x to add wide-cell handling via `go-runewidth`; skip vt10x for snapshot and track widths ourselves; switch to a maintained emulator that already handles widths (e.g. charmbracelet's `vt`).
- vt10x parser reference: https://github.com/hinshun/vt10x/blob/master/parse.go#L17-L28
