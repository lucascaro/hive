---
type: fixed
bump: patch
---

The JSON files Hive expects people to hand-edit survive a UTF-8 byte order mark.

Windows PowerShell's `Set-Content -Encoding utf8` always writes one — 5.1 has no
BOM-less UTF-8 at all — and Notepad long did the same, so a file written by a
setup script or corrected by hand is likely to carry one. `encoding/json` does
not skip a BOM, so the read failed on the very first byte.

In `update.json` that killed the update check with `invalid character '\ufeff'
looking for beginning of value`, and the update button then stayed dead with
nothing in the app to undo it: the only way back was to find the file on disk and
re-encode it. In `agents.json` it was quieter and worse — a parse failure there
is answered by disabling every custom agent and logging the reason to the daemon
log, so custom agents simply disappeared from the launcher with nothing on screen
to explain it.

Both now drop a leading BOM before parsing. A BOM is an encoding marker rather
than content, so this does not make either file more forgiving of real mistakes:
settings that are genuinely malformed are still reported instead of being
silently replaced, and a file in an encoding Hive cannot read at all — UTF-16,
which is what PowerShell's `Out-File` and `>` write by default — still fails,
which is the honest answer for a file that really is unreadable.
