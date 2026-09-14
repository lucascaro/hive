---
type: fixed
bump: patch
---

The latest-channel updater now says why it cannot update a checkout that has
wandered off its upstream, instead of relaying git.

Leave an integration or feature branch checked out with its upstream still set
to `main` and the periodic check keeps finding commits the running build lacks,
so the banner keeps offering an update. Pressing it ran `git pull --ff-only`,
which has nothing to fast-forward, and the banner showed exactly what git said:
`Not possible to fast-forward, aborting.` — with the command line pasted in
front of it, and no word about which branch, how far off it was, or what to do.

The updater now compares the branch against its upstream before it pulls, the
same place it already refuses a dirty tree, a detached HEAD, or a foreign
remote, and refuses only when the branch has truly diverged — commits ahead
*and* behind at once, which `pull --ff-only` cannot reconcile. A branch that
is only ahead (or only behind) still updates normally, exactly as before this
change. When it does refuse, it does so in its own words: the branch, how far
ahead and behind it is, and the way back — `git checkout main` for a
differently-named branch, or to push or move the local commits when the
diverged branch is the one upstream itself tracks (e.g. `main`). The
comparison is made against a freshly fetched upstream, not the one the last
periodic check saw hours ago, so the answer is the one the pull would have
found; nothing in the checkout has moved by the time it says so. The four
checkout refusals now live in one place shared by macOS and Windows rather
than two copies that had to be edited in step.
