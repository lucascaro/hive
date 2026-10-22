---
type: added
bump: minor
---

A session's agent can now see and wait on its siblings. From inside any session in a project, `hived session list` prints that project's sessions (id, name, agent, state), and `hived wait <id-or-name>` blocks until the session is idle, exited or in error (or a state you choose with `--state`), then exits — failing after `--timeout` (default 10 minutes, at most 30). Run `hived wait --after-working` right after `hived msg`, since the target is still idle until its agent picks the note up. No grant is needed: it reveals nothing a process of yours could not already read.
