---
type: fixed
bump: patch
---

The update check survives an `update.json` saved with a UTF-8 byte order mark.
Windows PowerShell's `Set-Content -Encoding utf8` always writes one — 5.1 has no
BOM-less UTF-8 at all — and Notepad long did the same, so a settings file
produced by a setup script or corrected by hand made every update check fail
with `invalid character '\ufeff' looking for beginning of value`. The update
button then stayed dead with nothing in the app to explain it or undo it, since
the only way back was to find and re-encode the file. A BOM is an encoding
marker rather than content, so it is now stripped before the settings are
parsed. Settings that are genuinely malformed are still reported instead of
being silently replaced.
