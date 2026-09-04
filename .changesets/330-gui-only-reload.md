---
type: added
bump: minor
issue: null
---

**Reload GUI** — pick up a new GUI build without restarting the daemon.
Every running shell and agent keeps going, with its scrollback intact.
Available from **File ▸ Reload GUI**, the command palette, and the
stale-build banner. Hive decides which you need by comparing the two
builds' *daemon contract*: only a change the daemon actually exposes
costs you a full restart now, so a frontend-only build no longer kills
your sessions.
