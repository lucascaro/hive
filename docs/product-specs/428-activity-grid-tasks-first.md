---
issue: 428
pr: null
shipped: null
title: "Activity grid: show the plan's tasks, not just the tool feed"
type: enhancement
complexity: S
priority: P2
stage: IMPLEMENT
---

# Activity grid: show the plan's tasks, not just the tool feed

- **Issue:** [#428](https://github.com/lucascaro/hive/issues/428)
- **Refines:** [416](416-agent-activity-view.md)

## Problem

In the activity grid (⇧⌘J, or ⌘J from a grid view), each tile renders the
session's plan as a row of bare pips and gives the whole tile body to the
tool-call feed. The pips carry the task text only in a hover `title`, so the
most useful information — what the agent set out to do and which step it is
on — is invisible at a glance, while a list of tool calls that is rarely
actionable takes all the space.

## Desired behavior

The tile makes the plan's tasks the primary content: readable step text with
per-step status, current step emphasized. The tool feed stays as secondary
content when the tile has room for it, and degrades gracefully on small tiles.
Sessions with no plan keep today's behavior. The inspector panel (⌘J in single
view) is unchanged.

## Success criteria

- An activity-grid tile renders each plan item's **text**, with the current
  item visually distinguished from pending and done ones — not only as pips.
- A plan taller than the tile leaves the tool feed **zero** height, and the
  task list scrolls; the pip strip stays capped at two rows so it cannot
  crowd out the list it summarizes.
- A plan shorter than the tile still shows the tool feed, with its calls, in
  the leftover space.
- The current step is scrolled into view when the agent moves to it, and a
  manual scroll is not yanked back while the step is unchanged.
- Scrolling the task list moves only that list: the tile grid and the tile's
  own host keep `scrollTop`/`scrollLeft` at 0.
- No keystroke reaches a terminal from the activity grid and nothing in the
  tile takes focus, including over the newly scrollable task list.
- A session with no plan renders exactly as today: the feed fills the tile.
  A tier that reports nothing still shows the "no activity data" empty state,
  and a stale session still desaturates and shows its age.
- Playwright (Wails mock, `CI=1`) proves the height claims; vitest is
  CSS-blind and cannot.

## Non-goals

- Changing the inspector panel or the sidebar plan pie.
- Any new wire field, daemon change or agent-tier work — this is a rendering
  change over the data spec 416 already delivers.
