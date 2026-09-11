---
issue: null
title: "Observe from a session: hived session list and hived wait"
type: enhancement
complexity: S
priority: P3
stage: PLAN
---

# Observe from a session: `hived session list` and `hived wait`

- **Issue:** —
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Exec plan:** [docs/exec-plans/active/390-session-observe-list-and-wait.md](../exec-plans/active/390-session-observe-list-and-wait.md)
- **Design:** [docs/design-docs/agent-orchestration.md](../design-docs/agent-orchestration.md)
- **Depends on:** [389](389-orchestrator-grant-and-session-msg.md) shipped and its gate met — unless the 389 gate proves unreachable without `wait`, in which case this folds into 389 (record it in 389's decision log).

## Problem

A session that can message a sibling still cannot tell whether the
sibling is working, waiting, or gone, and cannot say "carry on once it
is idle". Without that, `hived msg` is fire-and-forget and the user is
still the one watching the sidebar.

## Desired behavior

**`hived session list`** from inside any session prints the sessions
in the caller's project: id, name, agent, state. No grant needed —
this discloses nothing a same-user process cannot already read from
the control socket.

**`hived wait <session> [--state idle,exited,error] [--timeout 10m]`**
blocks until the named session reaches one of the states, then prints
the state reached and exits 0. Timeout exits non-zero with a message.
A session that does not exist or is in another project fails
immediately. Default states: `idle`, `exited`, `error`.

**Evidence.** One log line per wait: `wait caller=session from=<id>
to=<id> reached=<state|timeout>`.

## Success criteria

- `LIST_SESSIONS` on a `ModeSession` connection returns the caller's
  project's sessions with exactly the fields above (not worktree,
  title, last prompt — see `ownSessionOnly` for the reasoning).
- `hived wait` returns on the first qualifying state transition and
  on timeout; daemon test drives the state machine with `AGENT_EVENT`
  frames and asserts both.
- `hived wait` inside a shell session on a sibling that is already
  idle returns immediately.

## Non-goals

- Reading a sibling's output or summary text (decide in phase 4 with
  evidence).
- Waiting on several sessions at once.
- Any GUI change.
