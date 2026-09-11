# Agent orchestration

How a session's agent is allowed to direct other sessions — message
them, watch them, start them — and why that ability is a per-session
grant rather than something every session has. This is the "later
layer" that [control-plane.md](control-plane.md) deferred; it builds on
the control plane and changes none of it.

Delivered in phases, each one a spec, each gated on evidence from the
one before:

| Phase | Spec | What a session gains | Gate to start the next |
|-------|------|----------------------|------------------------|
| 0 | [338 — session messaging](../product-specs/338-session-messaging.md) | nothing; *you* can message a session and be pinged when it idles | ships |
| 1 | [389 — orchestrator grant + `hived msg` from a session](../product-specs/389-orchestrator-grant-and-session-msg.md) | a **granted** session can `hived msg` a sibling in its project | ≥10 session-originated sends in real work over ≥2 weeks |
| 2 | [390 — observe: `hived session list` / `hived wait`](../product-specs/390-session-observe-list-and-wait.md) | any session can list its project's sessions and block until one reaches a state | `wait` shows up in real agent transcripts |
| 3 | [391 — agent-spawned sessions](../product-specs/391-agent-spawned-sessions.md) | a granted session can start a sibling and wait on it | one real task fanned out by an agent that you would repeat |
| 4 | — | results collection, headless workers, templates | **not designed** until phase 3 has evidence |

Phase 2 may fold into phase 1 if the phase-1 gate turns out to be
unreachable without `wait`; that is a decision-log entry, not a new
design.

## The question it answers

The control plane tells the user what every session is doing. The next
thing the user wants is to stop being the only one who can act on that
knowledge: "when the schema lands, tell the refactor session", "run
these three in parallel and tell me when they are all done". Today
every one of those is the user clicking into a session and typing.

## Where the risk actually is

Not OS privilege. The control socket is owned by the user and any
process running as them can dial it — `SECURITY.md` says so, and the
2026-09 narrowing of the session socket is defence in depth, not a
boundary.

The risk is **confused deputy**. An agent reads untrusted text — a web
page, a PR body, a README in a cloned repo — and acts on an instruction
hidden in it. Today the blast radius is that one session's permissions.
Give every session the ability to spawn siblings and prompt them and
the radius becomes: spawn N sessions, hand an instruction to a sibling
that happens to run in bypass mode, ping-pong two agents forever, and
launder an untrusted instruction into a peer as if the user typed it.

That is exactly why session creation was removed from `HIVE_SOCKET`.
Orchestration reverses that removal **only for sessions the user has
named**, and only within rules the daemon enforces.

## Trust model: per-session grant

- **Capability, not ambient.** An entry carries `Orchestrator bool`,
  set by the user in the launcher or on a running session's context
  menu, default off, persisted with the entry (it is user intent, like
  `Agent`). The daemon checks it on every frame that mutates another
  session. It is not an environment token: a child cannot copy it,
  and revoking it takes effect on the next frame.
- **Same project only.** A granted session can direct sessions in its
  own project and no other. Cross-project is a non-goal until someone
  needs it.
- **Provenance on every delivery.** A message from a session is
  delivered as *from that session*, never indistinguishable from the
  user typing. The daemon sets the sender from the connection it
  arrived on and ignores anything the client claims.
- **No permission inheritance.** A spawned session launches with the
  agent's defaults. It never inherits a bypass flag or a custom
  command from its spawner; the daemon overrides those fields, not the
  client.
- **Depth 1.** A spawned session is not an orchestrator unless the user
  grants it separately.
- **Visible and killable.** Every agent-spawned session shows who
  spawned it; closing an orchestrator offers to close its children.
- **Caps.** No message to self; a bounded number of live children per
  orchestrator. Numbers live in the specs and are constants, not
  settings, until someone needs to change one.
- **Read-only is free.** Listing a project's sessions and waiting on a
  state disclose nothing a same-user process cannot already get from
  the control socket, so phase 2 needs no grant.

## Why the daemon, again

The grant is checked in `hived`, on the `ModeSession` connection, for
the same reason the control plane derives state there: it is the only
Hive process every client and every hook can reach, and it already
resolves the caller's session and project once at handshake
(`serveControl`, `ownSessionID` / `ownProjectID`). Each phase adds
frames to `sessionModeFrames` and a check next to the existing
`restricted` branch — the shape the 2026-09 narrowing built is the
shape orchestration extends.

## What proves each phase

The user has a bounded amount of time and wants to build only what
gets used. Each phase therefore ships with its own evidence:

- Phase 1: the daemon logs every `SEND_TO_SESSION` with the caller
  kind (`control` for the GUI and shell, `session` for a session
  child). The gate is a count you can grep, not a feeling.
- Phase 2: the same log line for `WAIT`.
- Phase 3: the entry's `SpawnedBy` is on the wire and in the sidebar,
  so "did an agent actually fan out" is visible without a log.

A phase that fails its gate is left as shipped and the next one is not
built. That is the point.

## What is deliberately *not* here

- **Phase 4.** Collecting results, headless (`-p`) workers, task
  templates, a DAG. Every one of these is a guess about how phase 3
  will be used. Design it after phase 3 has been used.
- **Cross-project targets. Broadcast. Message history.** Non-goals
  carried over from 338.
- **An open model where every session may orchestrate.** It is what
  terminal multiplexers do and it is the confused-deputy case above. If
  the per-session grant proves too much friction, the fallback is a
  per-*project* grant — still user-named, larger radius — not "on for
  everyone".
- **Surviving a daemon restart.** An orchestration in flight dies with
  the daemon today; spawned children are ordinary entries and come
  back as such. [254](../exec-plans/active/254-resume-conversations-on-daemon-restart.md)
  is independent, but it becomes urgent once phase 3 ships.

## Alternatives considered

- **Per-project grant** instead of per-session. Fewer clicks; but one
  injected session then has the whole project as its radius, and the
  user cannot say "this one may, that one may not". Kept as the
  fallback if per-session friction is real.
- **A capability token in the child's environment** instead of a flag
  on the entry. Copyable, not revocable, and the daemon already knows
  which session a `ModeSession` connection belongs to. No.
- **Let agents use their own cross-session messaging** and build
  nothing. Only works agent-to-same-agent, and it is exactly the
  ambient model above with no provenance, no scope and no log.
