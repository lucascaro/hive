---
item: "`disposeWorktree`'s non-git fallback deletes without the managed check"
surfaced_in: "2026-08-24 improvements"
severity: med
owner: "`unowned`"
notes: "`internal/registry/registry.go`: when `worktree.Root(projectCwd)` errors (project cwd is no longer a git repo), teardown falls back to `os.RemoveAll(wtPath)` — skipping the `IsManaged` guard the very next branch exists to enforce. Same function, two contradictory trust levels. Deciding either way is a behaviour change: refusing leaves orphan directories when a repo is moved or de-inited, so it needs a call, not a drive-by fix."
---
