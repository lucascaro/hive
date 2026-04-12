# Code Refactor: Remove Bloat — Research

## Topic & Scope

Identify unnecessary complexity, dead code, duplication, and oversized files in the Hive codebase
(~13,100 lines of non-test Go code). Goal: actionable inventory of what to remove or simplify
without changing behavior.

---

## Summary

The codebase is well-structured overall — clean package boundaries, no unused dependencies, no
TODO/FIXME debt, and comprehensive tests. The bloat concentrates in **six large TUI files** (3,726
lines combined) and a handful of **repeated patterns** that have accumulated as features were added
incrementally. There are also **3 dead exported functions**.

---

## Dead Code

### Confirmed Unused Exports

| Function | File | Lines | Notes |
|----------|------|-------|-------|
| `GetPaneActivity()` | `internal/tmux/capture.go` | 86-99 | Extracts pane activity timestamp; never called. Likely vestigial from activity-based detection. |
| `SendKeys()` | `internal/tmux/window.go` | 56-59 | Sends command string + Enter to tmux target; never called. |
| `SessionLabel()` | `internal/state/store.go` | 539-545 | Returns display string like `"★ Title [Agent]"`. Only referenced in `store_test.go`, not in production code. |

### Verification

```
grep -rn "GetPaneActivity" --include="*.go"  → only capture.go definition
grep -rn "SendKeys" --include="*.go"          → only window.go definition
grep -rn "SessionLabel" --include="*.go"      → store.go definition + store_test.go
```

---

## Large Files (>500 lines)

| File | Lines | Primary Concern |
|------|-------|-----------------|
| `internal/tui/handle_keys.go` | 824 | `handleGlobalKey()` alone is 331 lines with 26 case branches |
| `internal/tui/components/settings.go` | 719 | `buildSettingEntries()` is 184 lines of repetitive closures |
| `internal/tui/app.go` | 639 | Model struct has 70+ fields; `View()` is 124 lines |
| `internal/tui/operations.go` | 547 | 16+ CRUD methods, each ending with identical commit+rebuild |
| `internal/tui/components/gridview.go` | 539 | `renderCell()` is 159 lines doing borders+header+subtitle+content |
| `internal/tui/components/sidebar.go` | 458 | Rebuild (89 lines) and rendering (89 lines) entangled |

---

## Duplication Patterns

### 1. Grid View Sync (4 occurrences)

**Location:** `internal/tui/handle_keys.go` lines 86-90, 99-102, 118-120, 137-139

Every grid state change repeats:
```go
m.gridView.Show(...)
m.gridView.SetProjectNames(...)
m.gridView.SetProjectColors(...)
m.gridView.SetSessionColors(...)
m.gridView.SyncCursor(...)
```

**Fix:** Extract `m.syncGridView()` helper. Saves ~20 lines and prevents forgetting a call.

### 2. State Commit + Rebuild (9+ occurrences)

**Location:** `internal/tui/operations.go` lines 24, 49, 73, 174, 229, 277, 393, 421, 546

Pattern:
```go
m.commitState()
m.sidebar.Rebuild(&m.appState)
```

**Fix:** Extract `m.saveAndRebuild()`. Every mutation site becomes one call.

### 3. Sidebar Navigation (4 cases)

**Location:** `internal/tui/handle_keys.go` lines 494-548

NavUp, NavDown, NavProjectUp, NavProjectDown all follow identical pattern:
```go
prev := m.sidebar.Cursor()
m.sidebar.MoveXxx()
if m.sidebar.Cursor() != prev {
    m.syncGridCursor()
    return m, m.schedulePreviewRefresh()
}
```

**Fix:** Extract `m.navigateSidebar(moveFn)` parameterized by the movement function.

### 4. Color Cycling (2 functions)

**Location:** `internal/tui/operations.go` lines 35-51 (`cycleProjectColor`) and 55-75 (`cycleSessionColor`)

Both collect used colors, call `styles.CycleColor()`, call the corresponding `state.Set*Color()`,
then commit + rebuild.

**Fix:** Parameterize into single function accepting a color-getter and color-setter.

### 5. Move Up/Down Functions (6 functions)

**Location:** `internal/state/store.go` lines 312-408

`MoveSessionUp`, `MoveSessionDown`, `MoveTeamUp`, `MoveTeamDown`, `MoveProjectUp`, `MoveProjectDown`
all follow the same swap-adjacent-element pattern.

**Fix:** Extract generic `moveItem(slice, index, direction)` helper. ~140 lines → ~40.

### 6. Picker Components (2 files)

**Location:** `internal/tui/components/orphanpicker.go` (136 lines) and `recoverypicker.go` (242 lines)

Both implement identical keyboard navigation (up/k, down/j, space toggle, 'a' select-all, enter/esc).
Only `recoverypicker` adds agent-type cycling.

**Fix:** Extract shared `selectionList` base component.

### 7. Worktree Session Setup (2 occurrences)

**Location:** `internal/tui/handle_keys.go` lines 188-207 (grid view) and 314-341 (global keys)

Both verify git repo, set `pendingProjectID`/`pendingWorktree`, show agent picker.

**Fix:** Extract `m.initWorktreeSession(projectID)`.

---

## Complexity Hotspots

### `handleGlobalKey()` — 331 lines, 26 cases

This is the largest single function. It's a switch on key bindings with each case doing
inline logic. Converting to a keybinding dispatch map would:
- Reduce the function to ~30 lines of dispatch
- Make each handler independently testable
- Allow keybinding introspection for help screens

### `buildSettingEntries()` — 184 lines of closures

Each setting entry repeats the same closure pattern for `get` and `set`. A builder function
taking field name + pointer would eliminate the boilerplate:
```go
func stringField(label, desc string, ptr func(*Config) *string) settingEntry
```

### `renderCell()` — 159 lines

Does borders, header, subtitle, content, and background colors all inline. Splitting into
`renderHeader()`, `renderSubtitle()`, `renderContent()` would keep each under 40 lines.

### Model struct — 70+ fields

`app.go` Model mixes UI components, state caches, transient overlays, and formatting state.
Grouping related fields into sub-structs would improve readability:
```go
type overlayState struct { showAttachHint bool; pendingAttach string; ... }
type previewState struct { contentSnapshots map[string]string; stableCounts map[string]int; ... }
```

---

## AI Slop Patterns

### Section Separator Comments (50+ occurrences)

`// --- Section name ---` comments scattered throughout production and test code. These are a
hallmark of AI-generated code. Go organizes by package/file structure, not inline markers.

**Production files:**
- `internal/tui/messages.go` — 4 separators (lines 10, 64, 76, 100)
- `internal/tui/handle_preview.go` — line 75
- `internal/tui/components/settings.go` — lines 446, 515
- `internal/state/store.go` — line 410
- `internal/git/git.go` — line 119
- `internal/mux/interface.go` — lines 105, 132
- `internal/mux/native/manager.go` — lines 29, 97, 202

**Test files:** 30+ more in `store_test.go`, `flow_reorder_test.go`, `app_test.go`, etc.

### Unnecessary `else` After Early Return (52 occurrences across 22 files)

The worst offenders:
- `internal/tui/handle_keys.go` — 8 instances
- `internal/tui/app.go` — 8 instances
- `internal/tui/components/settings.go` — 4 instances
- `internal/tui/components/gridview.go` — 4 instances
- `internal/tui/viewstack.go` — 4 instances

### Thin Getter/Setter Methods (15+ methods)

Methods that just assign to or return a private field with no validation, transformation, or
side effects. In Go, these should be exported fields or direct access within the package:

- `gridview.go:78-100` — 5 setters: `SetContents`, `SetProjectNames`, `SetProjectColors`,
  `SetSessionColors`, `SetPaneTitles` — each is `gv.field = val`
- `sidebar.go:74` — `SetBellPending` — just `s.bellPending = bells`
- `preview.go:282` — `SetContent` — wraps field assignment
- `settings.go:110` — `GetConfig` — returns `sv.cfg`

**Note:** The GridView setters are cross-package (called from `tui/` into `tui/components/`), so
they can't be replaced with direct field access. But they could be consolidated into a single
`SyncState(contents, names, colors, sessionColors, titles)` method — which also fixes the
"grid view sync" duplication (4 repetitions of 5 setter calls).

### Verbose Comments on Obvious Functions

Comments that restate the function name without adding insight:
- `internal/tmux/session.go:7` — `CreateSession` documented as "creates a new tmux session"
- `internal/tmux/session.go:21` — `SessionExists` documented as "reports whether a session exists"
- `internal/tmux/session.go:27` — `KillSession` documented as "removes a tmux session"
- Many similar in `internal/tmux/window.go`, `internal/git/git.go`

### Boolean Toggle Anti-Pattern

`internal/tui/components/settings.go:177-184`:
```go
if cur == "true" {
    _ = f.set(&sv.cfg, "false")
} else {
    _ = f.set(&sv.cfg, "true")
}
```
Could be: `_ = f.set(&sv.cfg, strconv.FormatBool(cur != "true"))`

---

## What's NOT Bloat

- **No unused dependencies** in go.mod — all are actively imported
- **No TODO/FIXME/HACK comments** — codebase is clean
- **No generated or vendored files**
- **Type aliases** (`AgentType`, `SessionStatus`, etc.) — while they're just `string`, they
  provide documentation value and are used pervasively. Not worth removing.
- **`KeyHandler` interface** — has single implementation now, but the abstraction is lightweight
  and could be extended. Low priority to remove.

---

## Estimated Impact

| Category | Current Lines | After Refactor (est.) | Reduction |
|----------|--------------|----------------------|-----------|
| Dead code removal | 30 | 0 | -30 |
| Duplication consolidation | ~300 | ~100 | -200 |
| AI slop cleanup | ~120 | 0 | -120 |
| Function splitting | 0 net change | 0 | 0 (reorganized) |
| **Total** | | | **~350 lines** |

The main value is **reduced cognitive load** rather than raw line count reduction. The largest
wins come from making `handle_keys.go` and `operations.go` easier to navigate, removing
AI-generated noise (separator comments, redundant doc comments, unnecessary else blocks),
and consolidating the GridView setter calls.

---

## Readiness Assessment

Research is sufficient to write an implementation plan. All bloat locations are identified with
specific file:line references. No external dependencies or blocking constraints.
