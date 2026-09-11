---
type: changed
bump: minor
issue: null
pr: null
---

Sidebar rows no longer print the agent twice. An auto-generated session name
ends in the agent's own id ("rising-shore claude"), which the row then repeated
as a two-letter code; the name now drops that trailing id at display time — the
stored name is untouched, so rename, search and the `hive` CLI are unaffected,
and a name you chose yourself is never altered. The two-letter code now carries
the agent's own colour as a tint, so the agent is visible at a glance without
reading the row. Agents that declare no colour render the code plain.
