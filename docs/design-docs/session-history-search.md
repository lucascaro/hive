# Searching session history beyond the terminal buffer

Research captured during the `/hs-feature-loop` run on spec [430](../product-specs/430-find-text-in-a-session-with-cmd-f.md), which was **held at PLAN** because in-terminal search cannot reach agent-session history. This document exists so the follow-up brainstorm does not repeat the investigation.

Status: **resolved into spec [431](../product-specs/431-search-an-agent-session-s-transcript-history.md)** — source chosen (agent transcripts, route C below), UX chosen by spike. This document remains the record of the routes considered and why the others were not taken.

## The problem that stopped 430

Agent sessions (Claude Code, Codex, pi) run on the terminal's **alternate screen buffer**, which has no scrollback by definition. Measured with a PoC against `@xterm/xterm@5.5.0` + `@xterm/addon-search@0.16.0`:

- On the normal buffer, search covers the full 5000-line scrollback (needle on line 5 found with the viewport at line 291).
- On the alternate buffer the corpus is **exactly `rows` lines** — buffer length was 10 with 10 rows, and the 100 normal-buffer lines were unreachable.

So a browser-side find box searches one screenful on exactly the sessions most worth searching. The operator's judgement: that makes the feature unusable on its own.

## The history is not lost

`internal/session/vt.go` keeps a per-session ring of **raw PTY bytes**:

- `ringCap = 8 << 20` — 8 MiB (`vt.go:57`); circular after first overflow, trimmed to a safe replay boundary so it never starts mid-escape (`vt.go:369-460`).
- `Write()` calls `appendRing(p)` **unconditionally**, before any alt-screen branch (`vt.go:166-171`). Alt-screen output is captured exactly like normal-screen output.
- Asserted by `TestInitialReplayBytesAltScreen` (`vt_test.go:139-168`).
- In-memory only. `reviveAll` forks a fresh PTY per entry after a daemon restart (`internal/daemon/daemon.go:384-420`), so the ring starts empty.
- Reads are deliberately package-private: `ringBytes()` is "unexported on purpose" (`vt.go:616-621`); the only exported reader is `ReplayBytes()` (`vt.go:656-668`), gated behind `FrameRequestReplay` (`internal/wire/frame.go:66-72`).

## Three routes, and what each costs

### A. Query the daemon ring

The bytes are already there, for every session type including plain shells. Needs a new wire frame pair — an additive, well-worn pattern (`internal/wire/frame.go:39-160`, `internal/wire/control.go:338-343` for the `ListSessionsReq`/`SessionsResp` shape) — plus ANSI stripping before matching.

Costs and caveats:

- The ring is a **raw byte stream, not lines**. A cursor-addressing TUI writes CUP/erase sequences and repaints the same region repeatedly, so the ring records every repaint in order. Stripping escapes yields duplicated frames and text split across sequences. Usable for "find this error string", noisy for anything structural.
- 8 MiB cap; long sessions lose their earliest bytes.
- Lost on daemon restart.
- **Matches cannot be highlighted in xterm** — they are not in the browser's buffer. The UI is a results list, not in-place highlight.

### B. Record alt-screen scrollback in the VT

A line-based eviction buffer already exists — `history [][]byte`, `historyRows = 500` (`vt.go:48,86`), which shipped for spec 143 (normal-screen scrollback above the viewport).

It is **deliberately skipped on the alternate screen**, on both the write side (`captureEvictions` gated `!postAlt`, `vt.go:234`) and the read side (`RenderSnapshot` prepends history only when `!onAlt`, `vt.go:722`), with a test pinning the behavior (`TestVTSnapshotScrollbackSkippedOnAltScreen`, `vt_test.go:383-406`).

Un-skipping it would preserve in-place highlighting, which is the one thing routes A and C give up. But alt-screen apps repaint whole frames, so evicted rows would be largely repaint churn and the 500-row buffer would fill with duplicates. **Viability is unmeasured** and hinges on whether a given agent scrolls within the alt screen rather than repainting it — worth measuring per agent before ruling it in or out.

### C. Read the agent's own transcript

Hive already derives the Claude Code transcript path — `~/.claude/projects/<encoded-cwd>/<sessionID>.jsonl` (`internal/agent/claude.go:18-31`) — and knows, per session, which agent is running (`registry.Entry.Agent`, `internal/registry/registry.go:90`) and the agent's own conversation id (`Entry.AgentSessionID`, `:94-101`).

But it never reads content. Every existing read is an existence check or a single-line id probe:

- `claudeSessionExists` does `os.Stat` only (`internal/agent/claude.go:42-53`).
- `codexCaptureSessionID` reads **only the first line** of `~/.codex/sessions/.../rollout-*.jsonl` to parse `session_meta` (`internal/agent/codex.go:59-152`, the `ReadSlice('\n')` at `:139`).
- Pi reports over the wire via a Hive-authored embedded extension (`internal/agent/pi.go`, `//go:embed pi/hive.ts`), not a file read.
- `internal/agentstate/activity.go` holds tool/plan shape in a 200-entry in-memory ring, explicitly "dies with the daemon… there is no disk format" (`:24-30`) — not turn text.

Costs: a parser per agent CLI (Claude JSONL, Codex rollout JSONL, pi, and gemini/copilot/aider unknown); nothing at all for shell sessions. And it searches the **conversation**, not the **terminal** — arguably a better product, but a different one.

## What has no precedent today

- No `hive logs` / `hive tail` / dump / search subcommand anywhere in `cmd/` (`cmd/hived/main.go:34-51` dispatches only `hook`, `idea`, `-version`).
- The only session→text transforms are `RenderSnapshot` (current screen, ANSI) and `ReplayBytes`/ring (raw bytes) — both built to repaint a terminal, not to produce searchable text.

## Outcome

**Source: route C, agent transcripts.** Chosen because the operator's case is "find an error from earlier in a session running right now", and transcripts give clean text with no ANSI or repaint churn. Route A (the daemon ring) was not taken — it is the only route that covers plain shell sessions, so it stays on the table for that, but its raw-byte corpus makes it the noisier source for the case at hand. Route B (un-skipping alt-screen eviction in the VT) was not taken and remains unmeasured.

Scope landed at Claude and pi only.

**The pi mapping is resolved, and it is exact — not the heuristic this doc first assumed.** pi writes `~/.pi/agent/sessions/<encoded-cwd>/<timestamp>_<session-id>.jsonl`, and that suffix is the `--session-id` it was handed, echoed verbatim. `internal/registry/create.go:587-590` passes Hive's own entry id there, so a Hive session resolves to its transcript with `glob(<encoded-cwd>/*_<hiveSessionID>.jsonl)` — one file, no recency ranking, no cwd confirmation, no sibling-session ambiguity.

The evidence, gathered on a real machine: a Hive probe transcript named `..._hive-probe-1788745415.jsonl` (not a uuid at all, which is what proves the echo); and across all 91 pi transcripts, 46 with a UUIDv4 suffix (Hive spawns — `uuid.NewString()` is v4), 44 with UUIDv7 (pi's own generation), every id unique.

One trap the encoding hides: pi's directory encoding **preserves dots**, so `/Users/u/repo/.worktrees/x` becomes `--Users-u-repo-.worktrees-x--`. Claude's `encodeClaudeProjectDir` folds `.` to `-`. Reusing Claude's encoder for pi resolves to a directory that does not exist, and the failure is silent — the session just looks like it has no history.

**UX: the transcript takes over the terminal.** A throwaway spike built three structurally different variants against a real 3000-line transcript and drove them in a real browser:

| Variant | Model | Verdict |
|---|---|---|
| **A — transcript takeover** | Transcript replaces the terminal while searching; matches highlighted in place in the surrounding conversation | **Chosen.** Best for reading around a hit. Cost: the session is hidden while you look. |
| B — side results panel | Terminal stays visible; 390px panel of match rows plus a context strip | Never takes the session away, but the context strip is cramped. |
| C — centered palette | Command-palette modal over a dimmed app, match list plus wide preview | Most readable context; heavy for a per-session lookup. |

This resolves the tension named below: by showing a different view rather than highlighting the terminal, route A of the UX sidesteps the "in-place highlighting only works for text the browser holds" problem entirely.

**The finding the spike produced that nobody asked for.** Searching `error` over the real transcript returned 41 matches, nearly all from a single `npm ci` failure block — because 2570 of 3000 transcript lines are tool *results* and only 73 are assistant prose. Free-text search over a transcript is dominated by command output. Grouping or filtering matches was made an explicit non-goal for the first version, knowingly; it is the first thing to revisit if the feature proves hard to use.

## The design tension that prompted the spike

In-place highlighting only works for text the browser holds. Any history search therefore wants a results list. Binding one keystroke to both behaviors — highlight when the text is local, results list when it isn't — is the obvious temptation and the likely source of a confused feature. Whether ⌘F is the right entry point for both is itself a brainstorm question, not a given.

## Not verified

- `internal/agent/copilot.go` was not read line-by-line; its session-file read depth is inferred from the Codex analogue.
- Whether `cmd/hived/e2e_test.go` / `custom_agent_e2e_test.go` scrape session text for assertions.
- What `internal/agent/pi/hive.ts` reports — tool events only, or also turn text.
- Whether any agent actually scrolls within the alt screen rather than repainting it (route B's viability).


## What shipped

Spec [431](../product-specs/431-search-an-agent-session-s-transcript-history.md) absorbed [430](../product-specs/430-find-text-in-a-session-with-cmd-f.md) and shipped both behind **one ⌘F**, with the source chosen from the terminal's buffer type: normal buffer → `@xterm/addon-search` over the terminal, alternate screen → the transcript route documented above.

That merge also designed out 430's worst defect rather than mitigating it. Its PoC found that **any** search run while the alternate buffer is active permanently poisons `@xterm/addon-search` for the normal buffer — untreated, "search a Claude session, Claude exits, search again, silently find nothing, forever". Under the buffer-type rule the addon is never invoked on the alt buffer at all, so the path is unreachable.

Route A (the daemon's byte ring) remains the candidate for plain shell sessions whose scrollback has aged out of xterm's 5000-line buffer — the one case this feature still does not cover. Route B remains unmeasured.
