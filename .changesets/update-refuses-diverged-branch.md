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

The updater now counts the branch's local commits before it pulls, the same
place it already refuses a dirty tree, a detached HEAD, or a foreign remote,
and refuses in its own words: the branch, how many commits it carries that the
upstream does not, and the way back (`git checkout main`). Nothing has been
fetched or moved by the time it says so. The four checkout refusals now live
in one place shared by macOS and Windows rather than two copies that had to be
edited in step.
