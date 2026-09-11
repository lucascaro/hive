---
type: fixed
bump: patch
pr: 387
---

`Ctrl+Shift+V` pastes once instead of twice. The terminal read the clipboard and
wrote it to the session itself, but never cancelled the keypress, so the webview
also ran its own paste on top — the same text arrived twice on every use. The
paste now also goes through xterm's paste path, so multi-line clipboard content
keeps its newline normalisation and bracketed-paste framing instead of being
written raw, which had agents submitting once per pasted line.
