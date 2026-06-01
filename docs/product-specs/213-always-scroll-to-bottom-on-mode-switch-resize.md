# GUI: always scroll to bottom on mode switch; never auto-scroll up

- **Issue:** #213
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/213-always-scroll-to-bottom-on-mode-switch-resize.md](../exec-plans/active/213-always-scroll-to-bottom-on-mode-switch-resize.md)

## Problem

Switching display modes (focused / grid / grid-project) leaves the terminal viewport wherever the xterm buffer happened to be, often mid-history — `setView()` never calls `scrollToBottom`. Independently, the scrollback-replay-done handler unconditionally snaps to the bottom: when a window resize triggers a replay while the user is reading deliberate scrollback, the viewport jumps away from their position.

## Desired behavior

- Switching modes (focused → grid, grid → grid-project, etc.) snaps every visible tile to the bottom.
- Window or sidebar resize snaps to the bottom only when the user was already at/near it (existing 2-line "sticky bottom" tolerance from #163).
- The viewport never moves automatically away from where the user has placed it (no auto-scroll up from a scrolled-up position; no auto-snap to bottom from a scrolled-up position).

## Success criteria

- After producing > viewport-height output and scrolling up, switching single → grid → grid-project → single lands every visible tile at the bottom.
- After scrolling up ≥ 10 lines and resizing the window enough to trigger a scrollback replay, the viewport stays in scrollback (does not jump to bottom).
- Resize within the 2-line sticky-bottom tolerance still snaps to bottom (no regression of #163).
- Unit + Playwright tests pin all three behaviors so future refactors catch breakage.

## Non-goals

- Changing the existing 2-line sticky-bottom tolerance on resize (#163 behavior is retained).
- Auto-scrolling on new output append (the existing xterm/daemon behavior is unchanged).
- Cross-platform scroll-wheel or trackpad tuning.

## Notes

Related prior work: #163 (resize sticky-bottom), #200 (scrollback replay on resize), #208/#209 (grid scrollback baseline + sidebar focus).
