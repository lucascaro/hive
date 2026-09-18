---
issue: 434
title: "Show total session count next to the Hive title in the sidebar"
type: enhancement
complexity: S
priority: P3
pr: 435
stage: GATE
---

# Show total session count next to the Hive title in the sidebar

- **Issue:** [#434](https://github.com/lucascaro/hive/issues/434)
- **Exec plan:** [docs/exec-plans/active/434-show-total-session-count-next-to-the-hive-title-i.md](../exec-plans/active/434-show-total-session-count-next-to-the-hive-title-i.md)

## Problem

The sidebar header shows only the "Hive" brand, so there is no at-a-glance answer to "how many sessions do I have running?" — the per-project counts have to be summed by eye, and sessions in minimized projects are not visible at all.

## Desired behavior

The sidebar header shows the total number of sessions next to the Hive title, kept live as sessions are created and closed.

## Success criteria

- The sidebar header shows a muted number right after "Hive" equal to the total number of sessions, with a "N sessions" (singular "1 session") tooltip.
- The total counts every session, including minimized sessions and sessions in minimized projects.
- The number updates live when sessions are created or closed, without a reload.
- With zero sessions the number is not shown; the header reads "Hive" alone.
- The header's action buttons keep their position and layout.

## Non-goals

- Alive/exited or attention breakdowns of the total.
- Per-agent or per-project totals in the header.
- Making the count clickable.
