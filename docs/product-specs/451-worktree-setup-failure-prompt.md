---
issue: 451
title: "Ask the user when worktree setup fails instead of silently using a stale ref"
type: bug
complexity: M
priority: P1
pr: 452
stage: REVIEW
---

# Ask the user when worktree setup fails instead of silently using a stale ref

- **Issue:** #451
- **Type:** bug
- **Complexity:** M
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/451-worktree-setup-failure-prompt.md](../exec-plans/active/451-worktree-setup-failure-prompt.md)

## Problem

Creating a worktree session fetches `origin` before branching so the new
branch starts on the current upstream tip
(`internal/worktree/worktree.go:161`). When that fetch fails, the failure
is a single `log.Printf` at `worktree.go:176` and creation continues from
whatever `origin/HEAD` was last cached. The session starts on stale code
and nothing in the GUI says so.

Both failure modes are real, from one user's `hived.log`:

- `ssh: Could not resolve hostname <host>` — the remote only resolves on
  VPN. Four occurrences, 2026-06-03 through 2026-08-17.
- `signal: killed` — the 10s timeout firing, because `git fetch origin`
  pulls every branch on a large remote. Four occurrences, most recently
  2026-09-17.

`git worktree add` failing has the same shape
(`internal/registry/create.go:785-789`): `materializeWorktree` clears the
plan's worktree fields, the session falls back to the project directory,
and `renameAfterWorktreeFailure` relabels it. The user gets a session that
looks like a worktree session and is not one.

## Desired behavior

A worktree setup failure parks the session and asks. The parked session is
visible in the sidebar in a distinct state, and waits indefinitely.

- Fetch failed → **Cancel** / **Retry** / **Use cached origin/main (N days old)**
- `worktree add` failed → **Cancel** / **Retry** / **Use project directory**

The dialog names the failure in git's own words and how old the cached tip
is. The cached ref is used only when the user picks it — no code path
falls back silently. Cancel abandons the create and leaves no session
behind. Retry re-runs the step that failed.

## Success criteria

- A create whose fetch fails parks the session and raises the dialog
  instead of branching; nothing is created until the user answers.
- Picking **Use cached origin/main** produces today's behavior, and the
  button states the cached tip's age.
- Picking **Retry** re-runs the fetch; a fetch that now succeeds branches
  from the fresh tip.
- Picking **Cancel** leaves no session and no worktree.
- A `git worktree add` failure raises the same dialog with the
  project-directory choice; no silent fallback to the project directory
  remains on any path.
- A parked create holds no goroutine and no `gitMu`: other sessions can be
  created and killed while one sits on the dialog.
- With no control client connected, a failing create fails outright and
  reports an error. It does not park and does not fall back.
- A daemon restart while a session is parked marks that entry dead with a
  reason naming the interrupted worktree setup.

## Non-goals

- Changing the fetch itself (narrowing the refspec, raising the 10s
  timeout). Worth doing, but separate — this spec makes the failure
  visible, it does not make it rarer.
- Rebasing or refreshing a worktree after creation. Being behind `main`
  because time passed is not this bug.
- Persisting the parked *position* across a daemon restart — the
  `{spec, plan}` needed to resume stays in memory. A single marker bit
  on the persisted record is in scope, purely so boot can tell a
  never-spawned parked entry from an ordinary session and mark it dead
  rather than reviving it into a plain one.
- Any prompt for the existing-branch path: checking out a branch that
  already exists never consults upstream and is out of scope.

## Notes

Prior art in this repo, all reused rather than reinvented:

- `SessionInfo.PendingPrompt` + `FrameResolvePrompt` (`internal/wire/control.go:166`,
  `internal/wire/frame.go:153`) — a daemon-side question parked as state on
  the session and answered by a client-initiated frame. This is the model.
- `openChoiceDialog` (`cmd/hivegui/frontend/src/app/modals/choice-dialog.ts:81`)
  — the three-button dialog primitive, already used for `worktree_dirty`
  (`app/events.ts:912`). No new component needed.
- Create phases (`internal/wire/control.go:326-334`) and the phase
  checklist (`frontend/src/lib/phase-steps.ts:95`) already render create
  progress, which is where the parked state belongs.
