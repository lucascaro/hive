# The control plane

Why Hive's daemon becomes the source of truth for what every agent
session is *doing*, not only for its PTY; how it learns that from
agents that cooperate and from agents that don't; and why the answer
lives in `hived`, not in the GUI.

Implemented across three specs, in this order:

1. [336 — session state model](../product-specs/336-session-state-model.md)
   (the core; everything else depends on it)
2. [337 — idea inbox](../product-specs/337-idea-inbox.md)
3. [338 — session messaging](../product-specs/338-session-messaging.md)

## The question it answers

A multiplexer answers "which PTYs exist and what bytes did they emit."
With a dozen agent sessions across projects that is not the question
the user has. The question is: *who is working, who is waiting on me,
who finished, what was each one asked, and how do I hand one of them
something.* Today Hive answers a sliver of that with the terminal bell
(`NeedsAttention`) and nothing else.

The landscape (Conductor, Nimbalyst, Claude Squad, kandev, Cyclops, …)
has converged on "worktrees + parallel agents + diff review + kanban".
That is table stakes and someone else's fight. What none of them do is
answer the question above **across agent CLIs** from a single daemon
that outlives the UI. That is the gap Hive fills.

## Three tiers of knowledge

Agents differ wildly in what they expose. Rather than pretend
otherwise, the control plane has three explicit tiers, and every
session carries which tier it is on (`state_source` on the wire):

| Tier | Source | Agents | What Hive learns |
|------|--------|--------|------------------|
| `hook` | Agent-native lifecycle hooks calling back into `hived` | Claude Code | exact turn boundaries, permission prompts, prompt/summary text, subagent activity |
| `extension` | A Hive-shipped extension loaded into the agent at spawn | Pi | exact turn boundaries (`agent_start` / `agent_settled`), prompt/summary text, a per-session inbox socket |
| `heuristic` | PTY output cadence + bell + OSC title + process exit | shell, Codex, Gemini, Copilot, Aider, custom | working / idle / exited, "waiting" only via bell |

The tiers are a **floor, not a fork**: every session gets the heuristic
state machine; hook/extension events override it while they flow and
the heuristic takes over again if they stop (agent upgraded, hook
broken, extension not loaded). A client never has to know which tier
produced a state — it only needs `state_source` to explain *why* one
session's data is richer than another's.

## Why the daemon, not the GUI

Same reason `NeedsAttention` moved onto the wire: more than one client
needs the answer (`hivegui` windows, `hivebar`, future CLIs) and only
one of them holds the attach stream. Deriving state per client means
several answers to one question. The daemon derives it once and
broadcasts.

Hooks and extensions reinforce this: a hook fires from inside the
agent's process tree and has to report *somewhere* that is always
running. The daemon is the only Hive process with that property.

## How events get in: the `event` hello mode

Hooks and the Pi extension report through a fourth wire mode next to
`control` / `attach` / `create`: **`event`**. An `event` connection
sends `HELLO{mode:"event"}`, then exactly one `AGENT_EVENT` frame, and
closes. No `WELCOME` listing, no subscriptions; it is a fire-and-forget
postcard, cheap enough to open per hook invocation.

Why a wire mode and not a file drop or a second socket:

- **One channel.** `DESIGN.md` forbids side-channel files; the wire is
  the IPC. A mode keeps `internal/wire/` free of I/O and lets the
  daemon's existing dispatch own the connection lifecycle.
- **Same binary.** The hook command is `hived hook` — the daemon binary
  in client mode, already on disk next to the GUI, already knows the
  socket path resolution (`daemon.SocketPath`). No shell script to
  install, no Node dependency for Claude's side.
- **Authentication by inheritance.** The daemon injects
  `HIVE_SESSION_ID` and `HIVE_SOCKET` into every session's environment
  (`session.Options.Env`). A hook inherits them from the agent process.
  The daemon accepts an `AGENT_EVENT` only for a session id it has
  alive; a stale or forged id is logged and dropped. The socket is
  already user-only; this is the same trust model as the attach mode.

## Correlation with the agent's own identity

Hive already pins Claude with `--session-id <hive-entry-id>`
(`agent.Def.SessionIDFlag`) and Pi with `--session-id`. So the agent's
own session id **is** the Hive session id. That makes two things free:

- A hook's `session_id` field matches `HIVE_SESSION_ID`; either one
  identifies the entry.
- Claude's local session registry (`~/.claude/sessions/<pid>.json`,
  fields `sessionId`, `status`, `messagingSocketPath`) can be looked up
  by Hive's own id to find the inbox socket for spec 338. Hive never
  reads the sibling `.key` files (messaging tokens); on macOS/Linux the
  auth line is optional and Hive does not send one.

## Surviving Claude Code's churn

Claude Code ships several times a week and renames things. The control
plane leans on it deliberately, so every Claude surface Hive touches is
classified once, here, and the code is shaped around that
classification.

| Surface | Used by | Stability | Isolation | Fallback |
|---------|---------|-----------|-----------|----------|
| `--session-id <uuid>` | today | public CLI flag, documented | `agent/claude.go` | none needed; already shipped |
| `--settings '<json>'` + `hooks` events (`Stop`, `UserPromptSubmit`, `Notification`, `PermissionRequest`, `PostToolUse`, `SessionStart/End`, `StopFailure`) | 336 | public, documented, versioned in the changelog; event *names* have been stable, payload fields grow additively | `agent/claude.go` (builder), `cmd/hived/hook.go` (parser) | session stays on the **heuristic** tier |
| positional initial prompt (`claude "…"`) | 337 | public, documented | `agent/claude.go` `PromptArgs` | typed-on-idle path |
| `~/.claude/sessions/<pid>.json` (`sessionId`, `messagingSocketPath`, `pid`) | 338 | **internal**; documented only as "registers itself in files on disk" | `agent/claude_inbox.go` | typed-on-idle path |
| inbox socket message line | 338 | **internal**; only the auth line and the 30 s timeout are documented | `agent/claude_inbox.go` + one version-pinned fixture | typed-on-idle path |

Rules that follow:

1. **Public surfaces only for state; internal ones only where no
   public one exists, and never for anything load-bearing.** 336 uses
   nothing internal. 338 uses two internal surfaces for one
   nice-to-have (proper message provenance) and degrades to typing.
2. **Every adapter is optional at runtime.** An adapter that fails to
   attach, parse, or dial logs once and the session behaves as if the
   adapter did not exist. No error surfaces to the user for a Claude
   upgrade; the visible symptom is `state_source: heuristic` on a
   session that used to say `hook`, which is exactly the signal to go
   look.
3. **Tolerant parsing.** `hived hook` ignores unknown fields, maps
   unknown `hook_event_name` / `notification_type` values to `ping`
   (keeps the tier alive, changes no state), and never exits non-zero.
   A renamed event costs one state transition, not the feature.
4. **Version gate per surface.** At spawn the Claude adapter runs
   `claude --version` once per daemon lifetime (cached) and compares
   against a per-surface minimum in `agent/claude.go`
   (`minHooksVersion`, `minInboxVersion`). Below the minimum the
   surface is skipped, not attempted. Above an *observed-broken*
   maximum (a constant bumped by hand when drift is found) it is also
   skipped, so a known-bad release does not spam logs.
5. **Drift probe, not drift hope.** `scripts/probe-claude.sh` launches
   `claude -p` with Hive's hook settings against a trivial prompt and
   asserts that the expected `AGENT_EVENT`s reach a scratch `hived`;
   for 338 it also checks that `~/.claude/sessions/*.json` still carries
   the three fields. It runs in CI when `claude` is on PATH (the macOS
   leg) and is part of the release checklist. Recorded hook payloads
   live under `testdata/claude-hooks/` with the Claude version in the
   filename, so a diff against a fresh capture is the review.
6. **One file per surface.** Each row above maps to one Go file; a
   Claude change is a one-file diff plus a fixture refresh. Nothing in
   `internal/registry/` or `internal/daemon/` knows a Claude field name.

Pi is the mirror image: its extension API is documented and versioned
with the package, and Hive controls both ends (the extension is
embedded in `hived`). The same rules apply — inert without env, silent
on failure, heuristic fallback — but the risk is lower.

## What is deliberately *not* here

- **Task DAGs, fan-out/fan-in, cross-agent dependencies.** Orchestration
  is a later layer. It needs everything here first and it competes
  head-on with Claude agent teams, Cyclops, kandev. Build the control
  plane, watch how it gets used, then decide.
- **A custom harness.** Pi's SDK would let Hive own the agent loop
  entirely. Pi-only, small audience, and everything it would give us is
  reachable through the extension tier. Revisit if Pi becomes the
  primary agent.
- **Hook parity for Codex/Gemini/Copilot.** They have no comparable
  surface today. They stay on the heuristic tier; the tier model exists
  so promoting one later is an adapter, not a redesign.
- **Persisting state across daemon restart.** `state`, `last_prompt`,
  `last_summary` are in-memory like `Phase` and `Title`. A restart
  starts every session `idle`/`heuristic`; the first hook event
  re-promotes it. Ideas (spec 337) *are* persisted — they are user data,
  not derived state.

## Alternatives considered

- **Parse agent transcripts on disk** (`~/.claude/projects/**/*.jsonl`,
  Pi session files) instead of hooks. Rejected: format is unversioned
  and private, polling is racy, and it only works for agents whose
  transcript we reverse-engineer. Hooks are the supported surface.
- **Run Claude/Pi headless (`-p`, `--mode rpc`) so Hive owns the
  stream.** Rejected for the default path: the user wants the agent's
  own TUI in the tile. Headless workers are a fine *addition* for a
  future orchestration layer, not a replacement for the terminal.
- **Heuristics only, no adapters.** Simplest, agnostic, and it cannot
  tell "waiting for permission" from "thinking hard" — the single most
  useful distinction. The floor stays; it is not enough on its own.
