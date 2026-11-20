---
issue: 566
pr: 568
shipped: 2026-11-19
title: Pin important agent responses to the session's inspector panel
type: enhancement
complexity: L
priority: P2
stage: DONE
---

# Pin important agent responses to the session's inspector panel

- **Issue:** [#566](https://github.com/lucascaro/hive/issues/566)
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/566-pin-important-agent-responses-to-the-session-s-ins.md](../exec-plans/completed/566-pin-important-agent-responses-to-the-session-s-ins.md)

## Problem

Long agent sessions produce a handful of responses worth returning to — a design decision, a command recipe, a root-cause explanation, a summary — and today they scroll away in the terminal. Getting one back means scrolling or searching scrollback across many sessions. The ⌘J inspector already shows what the agent is *doing* (plan, tools); it has nothing for what the agent *concluded*.

## Desired behavior

- **Agents pin.** Claude and Pi sessions started by Hive get a way to pin a response with a **title** and a **body** the agent writes. Hive instructs them to pin important responses proactively — **on by default**, with a Settings toggle to disable, never overriding the user's own config. The user can also just ask the agent to "pin that".
- **Inspector section.** The ⌘J inspector gets a "Pinned" section for that session's pins, newest first. The section collapses to a header with the pin count; expanded, each pin is one line (title + age) and clicking one expands its body. The section's height is capped and scrolls internally, so plan and timeline stay usable.
- **Actions.** Copy a pin's body; unpin one; "Unpin all" through the confirm overlay. Key hints shown where a binding exists.
- **Elsewhere.** The sidebar row shows a pin-count badge (no row growth). Activity-grid tiles (⇧⌘J) show the session's pins.
- **Durable.** Pins survive daemon restart and app relaunch; they go away when the session is closed for good.
- Non-Claude/Pi agents show no pin affordance and no badge.

## Success criteria

- In a Hive-started Claude session and a Pi session, an agent pin call produces a pin in that session's inspector with the agent's title and body — and nothing in any other session.
- With the Settings toggle at default, a new Claude/Pi session receives the proactive-pin instruction; with it off, the session gets neither the instruction nor the pin capability; a user-set value is never overwritten.
- Pins list newest first. Collapsed section shows the correct count; a collapsed pin is exactly one line; expanding shows the full body.
- Playwright (Wails mock, `CI=1`): with enough pins to overflow, the Pinned section stops at its cap and scrolls internally, the plan/timeline section keeps non-zero visible height, and the terminal keeps keyboard focus.
- Unpin removes exactly that pin. "Unpin all" requires confirm (showing confirm/cancel keys); cancel changes nothing.
- Copy puts the pin body on the clipboard.
- Pins are present after daemon restart and after app relaunch; gone after the session is permanently closed.
- Sidebar badge count matches the inspector count and changes no row height at any density (Playwright).
- Activity-grid tiles render pins without taking focus or sending keystrokes to a terminal.
- A session cannot read or modify another session's pins.
- README / help overlay / keybinding docs updated for any new binding; changeset + `site/features.json` entry added.

## Non-goals

- **Manual pinning by the user** (selecting terminal/transcript text to pin) — deferred pending a reliability evaluation.
- Pinning the agent's *verbatim* response by capturing terminal output — the agent authors the pin content.
- Agents other than Claude and Pi (Codex, Gemini, Aider, shell, custom).
- Cross-session or project-wide pin views; pins outliving their session.
- Editing, reordering, or searching pins.
- A product limit on what users pin. Only trust-boundary safety bounds exist: per pin (title 256 B, body 64 KiB) and, by operator decision during review (2026-11-19), 1000 pins per session — the next is refused, never an old one evicted; the user prunes with unpin / unpin all.
- Notifications on new pins.

## Notes

Open questions carried from brainstorm:

- **Manual pinning:** operator wants its reliability evaluated before committing — can user-selected text from PTY sessions (and ACP transcripts) be captured cleanly? Follow-up evaluation.
- **Size bound:** "no caps" is the product decision, but the daemon still needs a trust-boundary safety bound on pin size; planning sets it. Not a user-facing limit.
- **Archived sessions:** does an archived session (spec 518) keep its pins? Assumed yes.
- **Plan-stage decisions (2026-11-19):**
  - ACP Claude sessions are in scope; ACP Pi is out.
  - The Claude channel is a Hive MCP tool.
  - Each pin is rejected above title 256 B or body 64 KiB. These are trust-boundary limits, not user caps.
  - Archived and closed sessions keep their pins until the record is discarded.
- **Prior art:** specs [416](416-agent-activity-view.md) (inspector, Pi `hive_todo` tool pattern, task-tools opt-in toggle), [428](428-activity-grid-tasks-first.md) (grid tile layout).
