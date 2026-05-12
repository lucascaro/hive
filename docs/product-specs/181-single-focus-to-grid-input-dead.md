# Single-focus → grid leaves session looking focused but keyboard input is dead

- **Issue:** #181
- **Type:** bug
- **Complexity:** M
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/181-single-focus-to-grid-input-dead.md](../exec-plans/active/181-single-focus-to-grid-input-dead.md)

## Problem

Going from single-focus mode back to grid mode leaves a session in a state where it *looks* focused (focus visuals applied) but typing does not reach the terminal — input is dead until the user clicks the session again. This is the inverse direction of #159 (grid → focused → grid). The same class of bug keeps recurring because focus visuals and input routing are managed by separate codepaths that can drift out of sync.

## Desired behavior

Going from single-focus mode to grid mode never leaves a session that looks focused but cannot receive keyboard input. Focus visuals and keyboard input target move as one atomic unit across all mode transitions.

## Success criteria

- Single-focus → grid: session is never both visually focused and input-dead.
- Focus visual state and active keyboard input target are driven from one source of truth — it is not possible to set one without the other.
- Existing behavior from #159 (grid → focused → grid) continues to work.

## Non-goals

- Redesigning the broader window/session management model beyond what's needed to unify focus + input.

## Notes

- Related: #159 (grid → focused → grid input-focus regression), #176/#178 (xterm canvas resize on session switch), #177 (grid mode revert on Windows).
- User report framing: "focus visuals and ability to type should be an atomic thing that is always tied together, remove spaghetti."
