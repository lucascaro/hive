# GUI: session terminal text gets replaced by garbled glyphs over time; resize fixes it

- **Issue:** #190
- **Type:** bug
- **Complexity:** M
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/190-gui-text-replaced-with-garbled-glyphs.md](../exec-plans/active/190-gui-text-replaced-with-garbled-glyphs.md)

## Problem

After working in a session for some time (observed with Claude at least), the rendered terminal text starts being replaced by weird/garbled glyphs. Sometimes only a few glyphs are affected, other times nearly the whole visible buffer is corrupted. The underlying text is intact — resizing the window forces a re-render and the text comes back correctly.

Likely a renderer / canvas / atlas issue in xterm.js (stale glyph atlas, font texture corruption, or DPR/zoom desync). Reproduces in long-lived sessions; resize is the known workaround.

## Desired behavior

Session text remains correctly rendered for the lifetime of the session. No manual resize needed to recover from corrupted glyphs.

## Success criteria

- Long-lived sessions (multi-hour, with sustained output like a Claude conversation) render text correctly without visible glyph corruption.
- No regression in rendering performance or memory.

## Non-goals

- Changes to non-GUI surfaces (CLI, headless).
- Switching the renderer engine away from xterm.js.

## Notes

- Reproduced with Claude sessions; likely renderer-agnostic but worth testing both webgl and canvas addons.
- Resize workaround suggests an atlas/cache issue (full re-layout flushes it).
