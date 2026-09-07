---
issue: 374
pr: null
type: security
bump: minor
---
- macOS releases are now signed with an Apple Developer ID certificate,
  notarized by Apple, and stapled. A fresh install no longer trips the
  "unidentified developer" warning, and no right-click-Open workaround
  is needed.
- The in-app updater now refuses any downloaded release that is not
  signed by Hive's Apple Developer team, and says so specifically
  rather than reporting a generic checksum error. Previously the only
  check was a SHA-256 manifest published alongside the download, which
  catches a corrupted transfer but not a tampered one. The installed
  app is left untouched when the check fails.
