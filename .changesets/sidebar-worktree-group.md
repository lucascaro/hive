---
type: changed
bump: minor
issue: null
pr: null
---

Sessions that share a git worktree now render as a group: a bordered panel
headed by the branch name and the number of sessions in it, collapsible from
the header. Because a worktree session is named after its branch, those rows
used to be identical to each other; inside the panel each row leads with its
window title instead, and the branch is stated once at the top. A session you
renamed yourself keeps its name. The colour bar belongs to the panel — the
whole group shares one colour — while the colour picker stays on each row.
