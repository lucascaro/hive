---
issue: 392
title: "Sidebar: flatten worktree groups and reclaim side space"
type: enhancement
complexity: S
priority: P2
pr: 393
stage: REVIEW
---

# Sidebar: flatten worktree groups and reclaim side space

- **Issue:** #392
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/392-flatten-worktree-groups-and-reclaim-side-space.md](../exec-plans/active/392-flatten-worktree-groups-and-reclaim-side-space.md)

## Problem

Follow-up to the sidebar redesign (#390). That change turned the project card into a flat, full-bleed label with a hairline rule, but left the worktree group as a card: `sidebar.css` `.hv-worktree-group` still carries a 1px border, `--radius-md` rounded corners and `margin: var(--space-1) var(--space-2)`. Next to a square, edge-to-edge project header the rounding reads as a leftover from the treatment that was just removed.

It also costs horizontal space the sidebar cannot spare. A row inside a group is inset a further 8px margin + 1px border on the left and loses the same again on the right, on top of the row's own 12px padding and the list's 8px right gutter — so grouped rows have measurably less room for the window title than ungrouped ones, in the one place the redesign was trying to widen.

## Desired behavior

A worktree group reads as part of the same flat tree as the project label. Its header is a sticky sub-label under the project header rather than the lid of a card, and its rows start at the same horizontal position as ungrouped rows (or close enough that the difference is a deliberate indent, not an accident of borders and margins). Containment is still unmistakable — a reader can still tell at a glance which sessions share a worktree — but it is carried by a flat cue rather than a rounded, bordered box.

## Success criteria

- `.hv-worktree-group` no longer uses `border-radius`, and the sidebar's rounded-box vocabulary is limited to controls (buttons, inputs, the drag placeholder).
- A grouped session row's content starts within 4px of an ungrouped row's content, horizontally, at the same density.
- A grouped row's right edge gives up no more width than an ungrouped row's, apart from the reserved colour-bar gutter.
- The worktree grouping is still visually unambiguous with the box gone — verified in a real browser against the mock/design docs, not by reasoning.
- Existing group behaviour is unchanged: sticky header offsets, collapse animation, the shared colour bar, the collapsed-state attention count, and drag-to-reorder.

## Non-goals

- Row height, density presets, or the `--sidebar-density` setting.
- The session row's internal layout (name/title/meta columns), untouched by #390's follow-up.
- The project header itself, which already has the intended flat treatment.
- Any change to grouping logic in `lib/worktree-groups.ts` or `internal/registry`.

## Notes

- Prior art: [385-sidebar-redesign](385-sidebar-redesign.md), PR #390.
- Design docs: `docs/design-docs/ui/components.md` › worktreeGroup, `docs/design-docs/ui/patterns.md`.
- The existing CSS comments record why the group is *not* `overflow: hidden` (sticky headers) — any restructuring must preserve that.
