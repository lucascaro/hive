# Tech debt tracker

Known shortcuts, deferrals, and rough edges. One row per item. Keep entries short — link to the exec plan that introduced or will resolve the debt.

| Item | Surfaced in | Severity | Owner | Notes |
|------|-------------|----------|-------|-------|
| `disposeWorktree`'s non-git fallback deletes without the managed check | 2026-08-24 improvements | med | `unowned` | `internal/registry/registry.go`: when `worktree.Root(projectCwd)` errors (project cwd is no longer a git repo), teardown falls back to `os.RemoveAll(wtPath)` — skipping the `IsManaged` guard the very next branch exists to enforce. Same function, two contradictory trust levels. Deciding either way is a behaviour change: refusing leaves orphan directories when a repo is moved or de-inited, so it needs a call, not a drive-by fix. |

## Conventions

- Add a row whenever an exec plan accepts a known shortcut. Linking back from the plan is required.
- `gc-sweep` may add rows automatically when it detects a deviation it can't safely auto-refactor.
- Resolve a row by deleting it in the same PR that pays the debt; the commit message is the audit trail.
