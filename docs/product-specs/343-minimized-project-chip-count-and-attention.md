---
issue: 343
pr: 346
title: "Show session count and attention state on minimized project chips"
type: enhancement
complexity: S
priority: P2
stage: GATE
---

# Show session count and attention state on minimized project chips

- **Issue:** #343
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/343-minimized-project-chip-count-and-attention.md](../exec-plans/active/343-minimized-project-chip-count-and-attention.md)

## Problem

A minimized project collapses to a single chip in the sidebar tray, which today shows only the project colour dot and its name. A *collapsed* (not minimized) project card is richer — its count reads `3 sessions · 1 needs you` — so minimizing a project loses information the same project shows one interaction earlier.

Two gaps: the chip carries no session count at all, and its attention signal is a coarse boolean (`projectHasAttention()` in `Sidebar.tsx`, derived from `readNeedsAttention`) that neither counts the ringing sessions nor uses the session state model from spec 336, so `waiting-permission` and `error` are indistinguishable from plain `waiting_input`.

## Desired behavior

The minimized project chip carries the same information as the collapsed project card header — session count and an accurate, count-aware needs-attention indicator — within the space a chip has.

## Success criteria

- A minimized project chip shows the number of sessions in that project, and the number changes when a session is created, killed or moved.
- When one or more sessions in a minimized project want the user, the chip shows how many, next to a state icon drawn from the same `StateIcon` set the sidebar rows and grid tiles use.
- That icon distinguishes "waiting for you" from "waiting for permission"; the chip no longer collapses both into one undifferentiated bell.
- The counts on a minimized chip and on the same project's collapsed card agree, because both read one shared helper.
- The chip's accessible name carries the same counts as words, not only as glyphs.
- Clicking anywhere on the chip still restores the project, and the restore `+` still hugs the right edge (spec 255's behaviour is unchanged).

## Non-goals

- Bubbling `exited` / `error` to the chip. Those are not `needs_attention`, and adding them would change the attention contract in `docs/design-docs/ui/patterns.md` for every surface, not just this one.
- Any change to the minimized *session* tray chips, which already carry a per-session `StateIcon`.
- Any wire, daemon or persistence change — `state` and `needs_attention` are already on `SessionInfo`.
