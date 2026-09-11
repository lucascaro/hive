---
issue: null
title: "Orchestrator grant: a session you name can message its siblings"
type: enhancement
complexity: M
priority: P2
stage: PLAN
---

# Orchestrator grant: a session you name can message its siblings

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/389-orchestrator-grant-and-session-msg.md](../exec-plans/active/389-orchestrator-grant-and-session-msg.md)
- **Design:** [docs/design-docs/agent-orchestration.md](../design-docs/agent-orchestration.md)
- **Depends on:** [338 — session messaging](338-session-messaging.md)

## Problem

After 338 the user can hand a session a message. The agent in a
session cannot: `HIVE_SOCKET` refuses `SEND_TO_SESSION`, by design,
because letting every session prompt every other session is the
confused-deputy case the design doc describes. Yet "when you are done,
tell the refactor session to rebase" is the most common thing the user
wants an agent to do for them, and today it is the user who relays it.

## Desired behavior

**Grant.** The launcher has a checkbox, off by default: *May direct
other sessions*. A running session's sidebar context menu has the same
item as a toggle. A granted session shows a small glyph on its row.
The grant survives daemon restart.

**`hived msg` from inside a granted session.** `hived msg <session>
<text…>` — `<session>` is a session id or the exact name of a session
in the caller's project — delivers the text with 338's semantics
(Claude inbox / Pi inbox / typed-on-idle, always queued). It prints
the delivery kind. From a session without the grant it fails with a
one-line explanation naming the launcher checkbox. Outside any session
it behaves as 338 specified (control socket, unrestricted).

**Provenance.** The target sees the message as coming from the sending
session by name — `Message from Hive session "<name>": …` — on every
delivery path. The sender is set by the daemon from the connection;
nothing the client sends can change it.

**Scope.** Same project only; a target in another project is reported
as not found. A session cannot message itself.

**Evidence.** The daemon logs one line per `SEND_TO_SESSION`:
`send_to_session caller=<control|session> from=<id> to=<id>
delivery=<kind>`. This is the phase gate.

## Success criteria

- `CreateSpec.orchestrator`, `UpdateSessionReq.orchestrator`,
  `SessionInfo.orchestrator` on the wire; persisted in `MetaFile`.
- `hived msg` on a `ModeSession` connection: granted ⇒ delivered
  with provenance; not granted ⇒ `ERROR{code: not_orchestrator}`;
  other project ⇒ `ERROR{code: not_found}`; self ⇒ `ERROR{code:
  invalid}` (or the existing nearest code).
- Daemon test: revoking the grant with `UpdateSession` makes the very
  next `SEND_TO_SESSION` on an already-open session connection fail.
- Registry test: every delivery path receives the provenance prefix;
  a client-supplied sender field, if the wire gains one, is ignored.
- GUI: launcher checkbox and context-menu toggle round-trip through
  the daemon; glyph reflects `orchestrator` from the wire.
- Log line present and greppable for both caller kinds.

## Non-goals

- Listing or waiting on other sessions (phase 2, spec 390).
- Starting sessions from a session (phase 3, spec 391).
- Cross-project targets, broadcast, rate limits beyond "not self".
- Remembering the checkbox across launches.
