---
type: fixed
bump: patch
issue: 528
---

Plugins started from the Finder or Dock now find Node.js on your login shell's PATH, and a plugin that needs a newer Node than yours shows "requires Node.js X or newer" in Settings → Plugins instead of failing silently.
