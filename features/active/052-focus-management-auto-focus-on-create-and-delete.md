# Feature: Focus management: auto-focus on session create and smart fallback on delete

- **GitHub Issue:** #52
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Branch:** feat/52-focus-management

## Description

When creating a new session, focus doesn't move to it automatically — the user has to navigate to it manually. When deleting a session, focus stays on a stale position instead of moving to a logical neighbor.

### On session create
The newly created session should be focused automatically.

### On session delete
Focus should fall back in this priority order:
1. Next session in the same group (team or standalone list)
2. Previous session in the same project
3. Next session overall
4. Previous session overall
5. Nothing (no sessions remain)

## Research

### Relevant Code
- `internal/tui/handle_session.go:12-23` — `handleSessionCreated()`: sets `ActiveSessionID` and calls `sidebar.SyncActiveSession()` — auto-focus on create already partially works
- `internal/tui/handle_session.go:25-33` — `handleSessionKilled()`: rebuilds sidebar but does NOT pick a fallback session — **this is the main gap**
- `internal/tui/handle_session.go:92-102` — `handleSessionWindowGone()`: correctly calls `syncActiveFromSidebar()` — reference pattern for the fix
- `internal/tui/operations.go:283-313` — `killSession()`: removes session, returns `SessionKilledMsg`
- `internal/state/store.go:49-60` — `CreateSession()`: appends session, sets `ActiveSessionID`
- `internal/state/store.go:109-120` — `RemoveSession()`: removes session, clears `ActiveSessionID` if it matched
- `internal/tui/components/sidebar.go:72-159` — `Rebuild()`: flattens project/team/session tree, clamps cursor if out of bounds
- `internal/tui/components/sidebar.go:245-252` — `SyncActiveSession()`: moves cursor to matching session ID
- `internal/tui/components/gridview.go:59-69` — `Show()`: updates grid sessions, clamps cursor
- `internal/tui/components/gridview.go:99-109` — `SyncCursor()`: moves grid cursor to matching session ID
- `internal/tui/helpers.go:64` — `syncActiveFromSidebar()`: sets `ActiveSessionID` from sidebar's current cursor

### Findings
- **On create:** Focus is already set to the new session via `handleSessionCreated` → `SyncActiveSession()`. Basic wiring works.
- **On delete:** `handleSessionKilled()` clears `ActiveSessionID` but performs no fallback selection. The sidebar rebuild clamps cursor if it exceeds bounds, but doesn't intelligently pick a neighbor. Only `handleSessionWindowGone()` follows the right pattern by calling `syncActiveFromSidebar()`.
- **Two views track focus independently:** Sidebar (cursor in flattened tree) and GridView (cursor in sessions array). Both need updates.
- Sessions are stored in ordered slices (append-on-create). Sidebar flattens project→team→session hierarchy.

### Constraints / Dependencies
- Both list view and grid view must implement the fallback logic
- The fallback priority (next in group → previous in project → next overall → previous overall → nothing) requires knowing which group/project the deleted session belonged to, which is lost after `RemoveSession()` — need to capture context before deletion
- No existing helper computes "next logical session" — one needs to be added

## Plan

### Approach

**Core principle:** A single `focusSession(sessionID)` method on Model that sets `ActiveSessionID`, syncs sidebar cursor, syncs grid cursor, and updates the preview. All focus changes (create, delete, navigation) go through this one path — no view-specific one-offs.

**On create:** Sidebar focus works via `SyncActiveSession`, but **grid view does not auto-focus** the new session because `refreshGrid()` preserves the previous cursor. Fix: call `focusSession` after rebuild/refresh.

**On delete:** Add a `NextSessionAfterRemoval` helper in `state/store.go` that computes the fallback session ID *before* the session is removed. Then call `focusSession(fallbackID)` after removal.

The fallback priority order:
1. Next session in the same group (same team's Sessions slice, or same project's standalone Sessions slice)
2. Previous session in the same group
3. Next session overall (any project, any team)
4. Previous session overall
5. Empty string (no sessions remain)

### Files to Change

1. `internal/tui/helpers.go` — Add `focusSession(sessionID string)` method on `*Model` that:
   - Sets `m.appState.ActiveSessionID = sessionID`
   - Calls `m.sidebar.SyncActiveSession(sessionID)`
   - Calls `m.gridView.SyncCursor(sessionID)`
   - Updates preview from `contentSnapshots` (or clears it)
   This becomes the single source of truth for focus changes.

2. `internal/state/store.go` — Add `NextSessionAfterRemoval(state, sessionID) string` function that walks the project/team/session tree to find the best fallback session ID according to the priority order. Must be called *before* `RemoveSession`.

3. `internal/tui/handle_session.go` — Two changes:
   - In `handleSessionCreated()`: replace the manual `ActiveSessionID` + `SyncActiveSession` calls with `m.focusSession(msg.Session.ID)` after rebuild/refresh. This fixes the grid view bug.
   - In `handleSessionKilled()`: call `NextSessionAfterRemoval` before `RemoveSession`, then after removal call `m.focusSession(fallbackID)` and start a new poll chain. Mirror the pattern from `handleSessionWindowGone`.

4. `internal/state/store_test.go` — Add unit tests for `NextSessionAfterRemoval` covering: standalone sessions (next/prev), team sessions (next/prev), cross-project fallback, single session (returns empty), and no sessions.

5. `internal/tui/flow_session_test.go` — Extend `TestFlow_KillSession_Confirm_Removed` to verify that after killing a session, `ActiveSessionID` is set to the expected neighbor rather than empty.

### Test Strategy
- Unit tests for `NextSessionAfterRemoval` with various topologies (standalone, team, multi-project)
- Flow test verifying focus moves to neighbor after kill
- Manual test: create 3 sessions, kill middle one, verify focus moves to next

### Risks
- The fallback must be computed before `RemoveSession` since group membership info is lost after removal — ordering of calls in `handleSessionKilled` is critical
- Existing callers of `syncActiveFromSidebar` (e.g., `handleSessionWindowGone`) could be refactored to use `focusSession` too, but limit scope to avoid churn — only change the create/delete paths for now

## Implementation Notes

- Added `focusSession(sessionID)` on `*Model` as single path for all focus changes (sidebar + grid + preview)
- Added `NextSessionAfterRemoval(state, sessionID)` in `state/store.go` to compute fallback before removal
- Moved `state.RemoveSession` from `killSession()` to `handleSessionKilled()` to avoid shared-pointer mutation on discarded Model copies
- `SessionKilledMsg` now carries `TmuxSession` for cleanup in the handler
- `handleSessionCreated` uses `focusSession` instead of ad-hoc `ActiveSessionID` + `SyncActiveSession`

- **PR:** —
