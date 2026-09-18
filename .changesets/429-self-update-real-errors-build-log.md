---
pr: 433
type: fixed
bump: patch
---
- **Show the real reason a self-update fails, with the full build log one click away.** A git failure (for example, an unaccepted Xcode license) was reported as "checkout has no upstream branch to track", and a failed build quoted Wails' sponsorship link instead of the error. The update banner now shows git's own message and the last meaningful build line, plus a **View log** button that opens the complete build output in a scrollable modal. Hive launched from Spotlight or Finder now also runs the same `git` as your terminal.
