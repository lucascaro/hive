---
type: changed
bump: minor
---

The **Latest** update channel no longer pulls and builds your own checkout.
It fetches `main` from this repository's remote and builds it in a linked
worktree of its own, `latest-src` under Hive's state directory, so you can
press Update from any branch — dirty, detached or diverged — and your
checkout is left exactly as it was. The first update runs `npm ci` in that
tree once; later ones reuse it. On Windows, running Hive straight out of the
checkout's `cmd/hivegui/build/bin` is no longer refused, since the build no
longer erases that directory. The Settings option now reads "Latest — tip of
main", which is what the channel built from a checkout on `main` before.
