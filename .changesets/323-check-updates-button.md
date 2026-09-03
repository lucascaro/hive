---
issue: 323
pr: null
type: added
bump: minor
---
- Added a "Check for updates" button to the sidebar header, next to the
  "New project" (+) button. It runs the same check the macOS app menu's
  "Check for Updates…" item ran, and reports the result in the usual update
  banner — up to date, update available, or check failed. Until now that check
  had no in-window trigger at all, and none whatsoever outside macOS.

- Changed the "New project" (+) button to the app's standard icon-button
  styling so it matches its new neighbour: it is flat and picks up its
  background on hover, rather than carrying one at rest.
