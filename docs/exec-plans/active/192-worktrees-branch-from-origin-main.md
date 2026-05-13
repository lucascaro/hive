# Worktrees should branch from origin/main, not local HEAD

- **Spec:** [docs/product-specs/192-worktrees-branch-from-origin-main.md](../../product-specs/192-worktrees-branch-from-origin-main.md)
- **Issue:** #192
- **Stage:** REVIEW
- **Status:** active
- **PR:** #193
- **Branch:** feature/192-worktrees-branch-from-origin-main

## Summary

`CreateWorktree` in `internal/worktree/worktree.go` runs `git worktree add -b <branch> <path>`, which branches from the current HEAD of the main checkout. When the local default branch is stale, every new worktree inherits stale code. Fix: fetch `origin` and base the new branch on the detected upstream default ref (typically `origin/main`), falling back to HEAD when the remote is unavailable.

## Research

- `internal/worktree/worktree.go:57` — `CreateWorktree(repoDir, branch, worktreePath)`. The `git worktree add -b <branch> <path>` invocation creates the branch from HEAD. No base ref is supplied.
- `internal/registry/registry.go:431` — sole production caller. Treats worktree-create failure as non-fatal (falls back to plain cwd session), so any added remote-fetch failure can safely degrade rather than block session creation.
- `internal/worktree/worktree_test.go` — uses an isolated `git init` repo with no remote. New behavior must not require a remote; tests run in a no-network sandbox.
- `git symbolic-ref refs/remotes/origin/HEAD` resolves to the upstream default (e.g. `refs/remotes/origin/main`). Falls back to first-time fetch otherwise.

## Approach

Add a private helper `detectUpstreamBase(repoDir string) string` that:

1. Resolves `refs/remotes/origin/HEAD` via `git symbolic-ref --short`. If present, returns its value (e.g. `origin/main`).
2. On failure, returns `""`.

Modify `CreateWorktree` to:

1. Best-effort `git fetch origin --quiet` (5s timeout). Errors logged, not returned.
2. Call `detectUpstreamBase`. If non-empty, run `git worktree add -b <branch> <path> <baseRef>`.
3. If detection returns `""` or the upstream-based add fails for any reason other than "branch already exists", fall back to the existing `git worktree add -b <branch> <path>` (HEAD-based).
4. Keep the existing "branch already exists" fallback unchanged.

Chosen over the obvious alternative (always pass a base ref from the caller) because callers don't know the repo's upstream layout and the worktree package already owns git-level details. The caller signature stays the same.

### Files to change

- `internal/worktree/worktree.go` — add `detectUpstreamBase` + `fetchOrigin` helpers; rework `CreateWorktree` to prefer upstream base ref. Update package comment.
- `internal/worktree/worktree_test.go` — add `TestCreateWorktree_PrefersUpstreamBase` (repo with an origin remote whose default ref is ahead of local main). Add `TestCreateWorktree_NoRemoteFallsBackToHEAD` (repo with no remote — existing behavior preserved).
- `CHANGELOG.md` — add a `Fixed` entry under `[Unreleased]`.

### New files

None.

### Tests

- `TestCreateWorktree_PrefersUpstreamBase` — initialize a bare repo, clone it as `repo`, advance the bare repo's `main` ahead of `repo`'s local main without pulling, call `CreateWorktree(repo, "feature", path)`, assert the new worktree's HEAD matches `origin/main`, not local main.
- `TestCreateWorktree_NoRemoteFallsBackToHEAD` — extends current `TestCreateAndRemoveWorktree`: repo with no remote → `CreateWorktree` succeeds; new worktree HEAD == local HEAD.
- Existing `TestCreateAndRemoveWorktree`, `TestCreateWorktree_BranchAlreadyExists` continue to pass unchanged.

## Decision log

- **2026-05-13** — Detect upstream via `origin/HEAD` rather than hard-coding `origin/main`. Why: works for repos whose default is `master` or any other name; matches what `git clone` set up.
- **2026-05-13** — Fetch is best-effort, errors logged not returned. Why: offline / sandboxed environments must still succeed (registry treats worktree failure as non-fatal, but degrading gracefully is better than spurious warnings).

## Progress

- **2026-05-13** — Spec + plan written; advanced through TRIAGE → RESEARCH → PLAN → IMPLEMENT.

## Open questions

None.

## PR convergence ledger

- **2026-05-13 iter 1** — verdict: APPROVE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 2af6099.

