---
issue: null
pr: 291
type: added
bump: minor
---
- In-app updates. Settings now carries an update channel: **Release**
  follows tagged versions, **Latest** follows the tip of your source
  checkout (auto-detected when Hive is running from one, or pointed at a
  directory you pick). Hive still only *checks* in the background —
  nothing is downloaded or built until you press **Update**. The button
  reports progress while it works and turns into **Restart** when the new
  build is ready; pressing it swaps the app in place and relaunches on
  the new version. Release downloads are verified against a SHA-256
  manifest now published with every release, and a mismatch is discarded
  rather than installed. The in-place swap is macOS-only for now —
  Windows and Linux keep the existing Download link.
