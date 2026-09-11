---
issue: null
pr: 377
shipped: 2026-09-07
title: "Idea inbox: capture ideas mid-session, start a session from one later"
type: enhancement
complexity: M
priority: P1
stage: DONE
---

# Idea inbox: capture ideas mid-session, start a session from one later

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/completed/337-idea-inbox.md](../exec-plans/completed/337-idea-inbox.md)
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
edited, marked done, or deleted. Editing covers all three of the things
capture asked for — the text, the kind, and which project the idea
belongs to. The project especially: the capture sheet pre-fills it from
whatever session happened to be focused, so filing into the wrong one
is an ordinary mistake, and delete-and-retype is not a correction.

**Start.** Every open idea has a **Start session** action: it opens the
existing agent launcher with the project fixed and an opening prompt
built from the idea, with the worktree checkbox honoured. The prompt is
an instruction rather than a label: the kind picks the verb (a bug asks
the agent to reproduce it and find the root cause before touching code;
an idea asks it to propose a plan first) and the note itself is the
subject. A bare "Idea: …" tells the agent what was noticed and nothing
about what to do with it. The prompt is editable in the launcher before
the agent is picked — the note was captured mid-task, and this is the
last moment to make it a brief; editing it there does not rewrite the
stored idea, which is the record of what was noticed. The new session is linked back to the idea,
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
- An idea's kind and project are editable after capture, from the
  inbox, without losing the note.
- Start session creates a session through the existing `CREATE_SESSION`
  path with `initial_prompt`; Claude and Pi receive it as their opening
  prompt argument; every other agent that can take one receives it
  **offered** to the user — Hive surfaces it on the session and the
  user pastes it into the agent's input box (unsubmitted) or dismisses
  it, when they can see the agent is ready.
  Amended twice on 2026-09-07, both times because a real `codex`
  startup was measured. First it said "followed by Enter"; In a fresh directory — which is
  every worktree this feature creates — codex opens on "Do you trust
  the contents of this directory? … Press enter to continue", with
  "Yes, continue" preselected, so an automatic Enter answered a
  security gate whose own text warns about prompt injection. Typing
  without Enter did not survive either: a probe found the note appeared
  ZERO times in the PTY stream afterwards, because the gate is a
  numbered menu that swallows arbitrary text. No signal available to
  the daemon distinguishes a prompt box from a startup gate, so the
  person looking at the terminal decides — hence paste/dismiss. Agents
  that cannot take a prompt at all (the plain shell, custom agents)
  receive nothing and leave the idea in the inbox.
- The idea's `status` becomes `started` and `session_id` is set when
  the prompt was actually handed over — for Claude and Pi (argv) that
  is at creation, and the sidebar row shows the idea glyph. Amended
  2026-09-07: on the typed path the idea is deliberately NOT claimed,
  because a successful PTY write does not prove the agent received it
  (measured: codex sits on its trust gate at the first idle edge, and
  that gate swallows arbitrary text and echoes nothing — a probe found
  zero occurrences of a unique marker afterwards). Claiming it there
  lost both halves at once: the note vanished into the gate and left
  the inbox. Unclaimed, the worst case is a session without its prompt
  and the note still where the user left it.
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
