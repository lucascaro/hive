---
issue: 563
title: Keep the active session's sidebar row visible on every switch
type: enhancement
complexity: M
priority: P2
stage: IMPLEMENT
---

# Keep the active session's sidebar row visible on every switch

## Problem

When the active session changes (⌘B, ⌘↑/↓, the palette, nav history, a grid tile click, a notification, the menu bar, or a session that a plugin or agent spawns), the sidebar doesn't follow. The active row can end up scrolled out of view, or hidden inside a collapsed project card or worktree group. The user then can't tell where they are without hunting for it, which breaks the rule that the active item is always clearly marked. Minimized sessions and projects already stay visible when they're active (#407, `restoreSession`). Scrolling and collapsed groups don't.

## Desired behavior

Whenever the sidebar is shown and the active session changes, for any reason, its row is fully visible:
- **Off-screen row:** the sidebar scrolls only as far as needed to bring the row fully into view. If the row is already fully visible, nothing moves.
- **Collapsed project or worktree group:** the card or group stays collapsed but shows the active row inside it, the same way an active minimized session keeps its row. When that session stops being active, the group goes back to fully collapsed.

## Success criteria

1. Use a sidebar long enough to scroll, with the active row out of view. Switch to that session with ⌘B, ⌘↑/↓, the palette, nav history, or a grid tile click. Each time, the row ends up fully inside the sidebar's visible area.
2. Clicking a fully visible sidebar row doesn't change the sidebar's scroll position.
3. If the target row is already fully visible, a switch from anywhere leaves the scroll position unchanged.
4. Switch to a session inside a collapsed project. The card stays collapsed (chevron shows collapsed) and shows the active row. Its other sessions stay hidden.
5. The same holds for a session inside a collapsed worktree group.
6. When the active session moves elsewhere, the collapsed card or group goes back to showing no rows. Its collapsed state hasn't changed and isn't persisted any differently.
7. A session that becomes active without user input, for example spawned by an agent or plugin, also meets criterion 1.
8. Existing behavior is unchanged: an active minimized session still shows its row, and ⌘B still restores a minimized project.

## Non-goals

- Auto-expanding a collapsed project or worktree group.
- Showing the sidebar when it's hidden (⌘S). The reveal applies only while the sidebar is shown.
- Centering the row, or scrolling more than the minimum needed.
- Moving the sidebar when the active session hasn't changed (attention bells, status changes, new sessions that don't become active).
- Any change to how minimizing works.

## Notes

Prior art: #407 keeps an active minimized session's row visible (`ProjectItem` in `components/Sidebar.tsx`); `restoreSession` in `app/view.ts` un-minimizes the holding project.
