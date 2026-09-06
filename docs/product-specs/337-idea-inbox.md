---
issue: null
pr: 352
title: "Idea inbox: capture ideas mid-session, start a session from one later"
type: enhancement
complexity: M
priority: P1
stage: IMPLEMENT
---

# Idea inbox: capture ideas mid-session, start a session from one later

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/337-idea-inbox.md](../exec-plans/active/337-idea-inbox.md)
- **Design:** [docs/design-docs/control-plane.md](../design-docs/control-plane.md)

## Problem

While working in one session the user constantly notices things that
belong in *another* session: a bug in a corner the agent just touched,
a refactor idea, feedback on the last output. Today those either
interrupt the current session ("also, could you…" — derailing its
context), get typed into a notes app that has no link back to the
project, or get lost. There is no one-keystroke way to say "remember
this for *this project*" and no way to later turn that note into a
session with the right cwd, worktree and opening prompt.

## Desired behavior

**Capture.** From anywhere in Hive, ⌘I opens a small sheet: a text
field, a kind picker (idea / bug / feedback), and the project
pre-filled from the focused session. Enter saves, Escape cancels; the
sheet never steals focus for longer than typing takes. The same thing
is reachable from inside any Hive session's shell:

```sh
hived idea add "the grid loses focus after ⌘G twice"          # kind defaults to idea
hived idea add -k bug "sidebar drag handle is 1px off"
```

which resolves the project from the session's environment, so an agent
can file one too (a Claude session that notices an unrelated bug can
run it from Bash rather than fixing the wrong thing).

**Browse.** Each project in the sidebar shows an inbox count when it
has open ideas. Clicking it (or ⇧⌘I on the focused project) opens the
project's inbox panel: a list of ideas, newest first, each with kind,
text, age, and the session it came from when known. Ideas can be
edited, marked done, or deleted.

**Start.** Every open idea has a **Start session** action: it opens the
existing agent launcher with the project fixed and the idea text as
the opening prompt (prefixed with the kind: "Bug report: …"), with the
worktree checkbox honoured. The new session is linked back to the idea,
the idea flips to `started`, and the inbox shows the link. Closing the
session leaves the idea `started`; marking it done is an inbox action.
Nothing is lost on session close — the idea outlives it. What must not
happen silently is losing ideas when a **project** is deleted: that is
guarded, at the same level as a dirty worktree, by a confirm.

Ideas persist across GUI and daemon restarts. They are per project,
not per session; a session that filed one can be closed without losing
it.

## Success criteria

- `ideas/<id>.json` files written only by the registry, atomically;
  survive daemon restart; listed via `LIST_IDEAS`, streamed via
  `IDEA_EVENT(added|updated|removed)`.
- ⌘I from a focused session pre-fills that session's project; Enter
  saves and returns focus to the terminal within one frame.
- `hived idea add` inside a Hive session files against the right
  project with `source_session_id` set; outside Hive it prints a clear
  error and exits 2.
- Sidebar shows the open count per project; the inbox panel lists,
  edits, completes, deletes.
- Start session creates a session through the existing `CREATE_SESSION`
  path with `initial_prompt`; Claude and Pi receive it as their opening
  prompt argument; every other agent receives it typed into the PTY
  once the session reaches `idle` (spec 336) followed by Enter.
- The idea's `status` becomes `started` and `session_id` is set on
  creation; the sidebar row of the session shows the idea glyph.
- Playwright mock e2e: capture → count → start → prompt visible in the
  fake PTY. Go tests: registry persistence, wire round-trip, CLI.

## Non-goals

- Syncing with GitHub issues (later; the record shape leaves room:
  `external_ref`).
- Prioritisation, ordering, tags beyond `kind`, kanban columns.
- Capturing from outside Hive (browser extension, mobile).
- Attaching files or screenshots.
- Ideas without a project (a "default" project exists already; use it).
- Ideas outliving their project. Deleting a project deletes its
  ideas, after a confirm when any are still open.
- A full idea CLI. `hived idea` ships `add` and `list`; editing,
  completing and deleting are GUI actions.

## Notes

- Depends on spec 336 only for the "type prompt once idle" path on
  heuristic agents; Claude/Pi paths work without it. Sequence 337
  after 336 phase 1 lands.
- Claude accepts a positional initial prompt in interactive mode
  (`claude "…"`); Pi accepts positional messages too (`pi [messages...]`).
