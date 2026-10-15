---
issue: 517
pr: 520
title: Read Claude's terminal status (OSC 7501) to fix stuck "working" after Esc
type: enhancement
complexity: M
priority: P2
stage: REVIEW
---

# Read Claude's terminal status (OSC 7501) to fix stuck "working" after Esc

## Problem

A Claude Code session that the user interrupts with Esc stays "working" in Hive until the output-cadence heuristic expires, because Esc fires no hook. Hooks are also the only exact state source, so when one breaks, a waiting session isn't flagged until the 30 s fallback. Claude 2.1.295+ now reports its state in-band through the OSC 7501 status protocol. It does so only after the terminal answers a startup query within 2 s, and nothing in Hive answers, so Claude emits nothing today.

## Desired behavior

For agent sessions, `hived` answers Claude's 7501 capability query once, at spawn. It then reads Claude's `state` and `kind` from the PTY output as a new low-trust `osc` source. That source fills gaps only. It never overrides a fresh hook, extension or ACP report. Esc moves the session to idle at once, and a Claude question is flagged as needing attention even with hooks disabled. Message and title fields are ignored. The parser is generic 7501, so Codex 0.163+ benefits when upgraded, but only Claude is probed and verified here.

## Success criteria

- In a Claude session, pressing Esc during a turn sets the session idle within about 1 s. A test captures the 7501 `idle` output and asserts the transition.
- With hooks disabled, a Claude `AskUserQuestion` (7501 `blocked`, `kind=question`) sets needs-attention.
- While a hook or extension report is fresh, an `osc` report changes no session state (precedence test), with one exception: an osc `idle` that follows an osc `working` in the same turn ends a hook-reported `working` (the Esc interrupt, which fires no hook), keeps the existing source label, and is not undone by hook events from the interrupted tool.
- The query reply is written once per spawn for agent sessions only. Replaying scrollback on attach never writes a reply into the PTY (test).
- A recorded Claude 2.1.3xx 7501 byte stream (startup idle, working, done, question blocked, Esc idle, clear) is a fixture for the parser.
- `DaemonContract` is bumped, with a changeset and a `site/features.json` entry.

## Non-goals

- No `OSC 9;4`, `OSC 777`, `OSC 99` or `OSC 133` parsing, and no `TERM_PROGRAM` spoofing or new default (separate small spec).
- No Codex `-c tui.notification_*` workaround, no Codex hooks tier, no Gemini title parsing (each is its own spec).
- No generic socket or CLI report-state frame, and no done/error UI state beyond what the existing state model shows.
- No defence against forged 7501 beyond the gap-filling rule. A hostile `cat` can set state on the `osc` tier, accepted like BEL today. Residual exposure: a forged working→idle sequence can end a hook-fresh `working`, and a forged `idle` while hooks are stale could hide an osc-set wait; osc never lowers a wait set by a hook or extension.
- No use of `msg` or `title` fields, and no GUI-side OSC handlers.
- `blocked:kind=permission` was unverified when this spec was written; the implementation's live probe has since confirmed it (see Notes).
- Resize-owner policy is parked, and a done/error state is dropped.

## Notes

- Resolved by the Go probe during implementation: a Claude permission dialog reports `blocked:kind=permission`, and Esc on it reports `idle` (see the research note's probe results).
- Research: [agent-osc-signals.md](../references/agent-osc-signals.md) (includes the credentialed probe, 2026-10-15) and [herdr-iterm-agent-integration.md](../references/herdr-iterm-agent-integration.md). Adjacent: spec 423 (Pi seq/heartbeat), `docs/design-docs/control-plane.md`.
