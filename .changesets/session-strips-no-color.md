---
type: fixed
bump: patch
---

Sessions no longer render monochrome when the GUI happened to be launched from
a shell with `NO_COLOR` set. Hive built every session's environment from the
daemon's own, overriding only `TERM`, so a `NO_COLOR` that reached `hivegui` by
inheritance was handed down to every agent and shell in every tile and stayed
there for the life of the daemon — with no setting anywhere to explain it. A
tile is a colour-capable xterm.js terminal whatever the GUI was started under,
so the variable is now dropped when a session's environment is built, for the
same reason `TERM` is already forced.
