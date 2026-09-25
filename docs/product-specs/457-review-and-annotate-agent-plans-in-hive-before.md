---
issue: 457
title: Review and annotate agent plans in Hive before implementation
type: enhancement
complexity: L
priority: P2
stage: IMPLEMENT
---

# Review and annotate agent plans in Hive before implementation

## Problem

When a Claude session finishes planning (`ExitPlanMode`), the user can only approve or reject it in the terminal. There's no way to comment on specific parts of the plan, and a multi-step plan is hard to read in a scrolling TTY. Pi has no plan checkpoint at all, so its plans run without review. Tools like plannotator fill this gap in a separate browser tab. That tab is disconnected from the Hive session that's waiting on it, and with several sessions running it's unclear which review belongs to which session.

## Desired behavior

- A new opt-in Hive setting turns on in-Hive plan review.
- When it's on and a Claude session calls `ExitPlanMode`, the agent blocks. The session shows as needing attention, and Hive shows the plan as rendered, readable markdown tied to that session.
- The user can attach comments to specific passages, then approve, or deny with feedback. Approval lets the agent go ahead. Denial sends the comments back to the agent, each one with the passage it refers to, and the agent revises.
- Pi sessions get a Hive-provided tool for submitting a plan for review, and it goes through the same review flow.
- If an external `ExitPlanMode` reviewer such as plannotator is detected, it wins by default. A setting lets the user make Hive the reviewer instead. Only one reviewer ever prompts.
- The terminal approval path still works. If no GUI is attached, the agent falls back to its normal approval prompt instead of waiting.

## Success criteria

1. With the setting off (the default), Claude and Pi plan behavior is unchanged from before this feature.
2. With it on, `ExitPlanMode` in a Claude session puts the session in needs-attention and opens a plan review in Hive for that session. The agent doesn't continue until the user approves or denies.
3. Approving lets the agent go ahead. Denying with comments delivers every comment to the agent together with the text it refers to, and the agent's next plan responds to them.
4. In a Pi session with review on, asking the agent to plan before implementing produces a Hive review through the Hive-provided tool, and the same approve/deny behavior applies.
5. With plannotator's hook installed and the default setting, only plannotator prompts. With Hive selected, only Hive prompts in newly started Claude sessions when the external reviewer is a Claude plugin (such as plannotator). A reviewer hook written directly in a Claude settings.json cannot be suppressed; with Hive selected, Settings shows a warning naming that file. The reviewer choice is fixed per session when it starts, so switching it never double-prompts a running session.
6. With no GUI attached, a plan request doesn't block the agent indefinitely. The agent falls back to its normal terminal approval.
7. The plan renders as formatted markdown (headings, lists, code blocks) that follows the active Hive theme, not as raw text.

## Non-goals

- Code diff, PR or MR review.
- Share links and exports (Obsidian, Bear, URLs).
- Ask AI, HTML artifact review, and plan history or revision diffs. Each gets its own brainstorm later.
- Agents other than Claude and Pi.
- Bundling, installing or modifying plannotator.

## Notes

- Open question: what counts as a no-GUI fallback — GUI never attached, versus the GUI closing while a review is pending.
- Reference UX: https://github.com/backnotprop/plannotator. Its Claude integration is a blocking `PermissionRequest` hook with matcher `ExitPlanMode`. Hive already wires `PermissionRequest` observe-only via `--settings` (`internal/agent/claude.go`), and `--settings` hooks concatenate with the user's settings.json hooks.
- Failure signals the operator named: agent hangs; feedback lost or garbled; Pi model ignores the submit-plan tool; plan hard to read or unpolished.
