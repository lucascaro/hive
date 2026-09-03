---
issue: 323
pr: 325
type: added
bump: minor
---
- Added a "Check for updates" button to the sidebar header, next to the
  "New project" (+) button. It runs the same check the macOS app menu's
  "Check for Updates…" item ran, and reports the result in the usual update
  banner — up to date, update available, or check failed. Until now that check
  had no in-window trigger at all, and none whatsoever outside macOS.

- Changed the "New project" (+) button to the app's standard icon-button
  styling so it matches its new neighbour. At rest it is now flat and its
  glyph is dimmed, the same as every other icon button in the app; hovering
  restores the full-strength glyph and brings the background back.
