---
type: added
bump: minor
issue: null
pr: 333
---

**Reload GUI** — pick up a new GUI build without restarting the daemon.
Every running shell and agent keeps going, with its scrollback intact.
Available from **File ▸ Reload GUI**, the command palette, and the
menu bar. Hive decides whether a restart is really needed by comparing
the two builds' *daemon contract*: only a change the daemon actually
exposes costs you a full restart now, so a frontend-only build no
longer kills your sessions. The stale-daemon banner follows the same
rule, and no longer nags about a daemon that is simply a different
build of the same behaviour.
