# Worktrees should branch from origin/main, not local HEAD

- **Issue:** #192
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/192-worktrees-branch-from-origin-main.md](../exec-plans/completed/192-worktrees-branch-from-origin-main.md)

## Problem

When Hive creates a worktree-backed session, `internal/worktree/worktree.go:65` runs `git worktree add -b <branch> <path>`, which branches from the current HEAD of the main checkout. If the user's local `main` is stale relative to `origin/main` (common in long-lived checkouts), every new worktree starts on outdated code. Agents then operate against old code, producing diffs that conflict with upstream and may rediscover bugs already fixed.

## Desired behavior

New worktrees are created from the latest `origin/<default-branch>` (typically `origin/main`). Hive performs a `git fetch origin` before `worktree add` and bases the new branch on the remote ref. If the remote is unreachable, fall back to local HEAD with a visible warning rather than failing.

## Success criteria

- Creating a worktree when local `main` is behind `origin/main` results in a worktree whose HEAD matches `origin/main`, not the stale local ref.
- When the remote is unreachable, worktree creation still succeeds (falls back to local HEAD) and surfaces a warning to the user / logs.
- Existing test `internal/worktree/worktree_test.go` is updated or extended to cover the origin-based branching behavior.

## Non-goals

- Auto-rebasing existing worktrees onto a newer `origin/main`.
- Configurable per-project base branch beyond detecting the repo's default branch (can be a follow-up).
- Pulling on the main checkout itself — only the new worktree gets the upstream tip.

## Notes

Discovered while running `/hs-feature-loop` from a worktree several days behind `origin/main`.
