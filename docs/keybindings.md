# Hive Keyboard Shortcuts

## Sidebar Navigation

| Key | Action |
|-----|--------|
| `↑` / `↓` | Move cursor up / down |
| `J` | Jump to next project |
| `K` | Jump to previous project |
| `1`–`9` | Jump to project by index |
| `Space` | Toggle collapse/expand project or team |
| `←` / `h` | Collapse current project or team; if on a session, collapse its parent |
| `→` / `l` | Expand current project or team |
| `Shift+↑` | Move selected item (session, team, or project) up |
| `Shift+↓` | Move selected item (session, team, or project) down |

> **Vim-style aliases:** `↑`/`↓` are the default navigation keys. If your config sets `"nav_up": "k"` / `"nav_down": "j"` (the default before v0.8), those keys still work — arrow keys are always active as permanent aliases. `h` and `l` are built-in aliases for collapse (`←`) and expand (`→`) respectively.

## Session Management

| Key | Action |
|-----|--------|
| `n` | New project |
| `t` | New session (opens agent picker) |
| `T` | New agent team (opens wizard) |
| `a` / `Enter` | Attach to selected session |
| `r` | Rename selected session or team |
| `x` | Kill selected session (with confirmation) |
| `D` | Kill entire team (with confirmation) |
| `c` / `C` | Cycle project color forward / backward |
| `v` / `V` | Cycle session color forward / backward |

## Pane Focus

| Key | Action |
|-----|--------|
| `s` | Switch to sidebar view (focus sidebar; exits grid if open) |
| `Tab` | Toggle between sidebar and preview focus |

## Grid View

| Key | Action |
|-----|--------|
| `g` | Open grid view for the current project |
| `G` | Open grid view for all projects |
| `←` / `→` / `↑` / `↓` | Navigate grid cells (arrow keys only) |
| `Enter` / `a` | Attach to selected session |
| `t` | New session in the selected session's project |
| `W` | New worktree session in the selected session's project |
| `x` | Kill selected session (with confirmation) |
| `r` | Rename selected session |
| `c` / `C` | Cycle project color forward / backward |
| `v` / `V` | Cycle session color forward / backward |
| `g` | Switch to project-scoped view (while all-grid is open) |
| `G` | Switch to all-projects view (while project grid is open) |
| `Shift+←` / `Shift+↑` | Move selected session left (earlier) in its group |
| `Shift+→` / `Shift+↓` | Move selected session right (later) in its group |
| `?` | Open help overlay |
| `S` | Open settings |
| `H` | Open tmux shortcuts reference |
| `q` | Quit app |
| `Esc` | Exit grid view |

## Status Indicator

Hive shows session state with a colored dot in the sidebar, team rows, and grid tiles:

- `○` gray: idle
- `●` green: working
- `◉` amber: waiting
- `✕` red: dead

The legend is visible in the main status bar and in the grid footer.

## Search & Navigation

| Key | Action |
|-----|--------|
| `/` | Start filter mode (type to filter sessions) |
| `Ctrl+P` | Command palette (fuzzy session search) |
| `Esc` | Cancel current input / close filter |

## Application

| Key | Action |
|-----|--------|
| `?` | Toggle help overlay |
| `H` | Toggle tmux keybinding reference |
| `q` | Quit (tmux sessions persist) |
| `Q` | Quit and kill all managed sessions |
| `Ctrl+C` | Force quit |

## Mouse

| Interaction | Action |
|-------------|--------|
| Left-click sidebar session | Select session (updates preview) |
| Left-click sidebar project or team | Toggle collapse / expand |
| Left-click preview pane | Focus preview and activate the displayed session |
| Left-click grid cell | Move grid cursor to that cell |
| Scroll wheel in sidebar | Move cursor up / down |
| Scroll wheel in preview | Scroll preview content up / down |
| Scroll wheel in grid | Move grid cursor up / down |

## Confirmation Dialogs

| Key | Action |
|-----|--------|
| `y` / `Enter` | Confirm |
| `n` / `Esc` | Cancel |

## Detach from an attached session

When you attach to a session (`a` / `Enter`), Hive's TUI suspends and you interact with the agent directly. To return to Hive, press the **detach key**:

| Key | Action |
|-----|--------|
| `Ctrl+Q` | Detach and return to Hive (default; configurable) |
| `Ctrl+B D` | tmux's standard detach sequence — still works as a fallback when using the tmux backend |

The detach key is set via the top-level `detach_key` field in `~/.config/hive/config.json` (not the `keybindings` block — see note below):

```json
{
  "detach_key": "ctrl+q"
}
```

**Accepted syntax:** `ctrl+<lowercase-letter>` only (e.g. `ctrl+q`, `ctrl+d`, `ctrl+x`). Alt-modifier and function keys are not supported. An invalid value falls back to `ctrl+q` with a warning printed to stderr at startup.

**Why a top-level field, not under `keybindings`?** The detach key is enforced by the multiplexer backend (the tmux backend installs a `bind-key -n` on the server before each attach; the native PTY backend intercepts the matching control byte on stdin). The `keybindings` block is processed by the in-TUI Bubble Tea key handler and never sees keystrokes during attach.

**Lifetime of the tmux binding.** On the tmux backend, Hive installs the binding via `bind-key -n` and intentionally leaves it in place across attach/detach cycles — the binding persists for the lifetime of the tmux server (or until you change `detach_key` and restart, or someone overwrites it). This is faster (no per-detach restore round-trip) and avoids a class of trap-restore fragility, at the cost of clobbering any user-defined `bind -n <key> ...` you may have in `~/.tmux.conf`. If you have such a binding you want to keep, set `detach_key` to a different `ctrl+<letter>`.

> ⚠️ **`ctrl+q` collides with terminal flow control (XOFF) on systems that have `ixon` enabled.** Most modern terminals disable it by default; if yours doesn't, set `detach_key` to a different `ctrl+<letter>` combination.

## Customization

All key bindings can be overridden in `~/.config/hive/config.json` under the `keybindings` key:

```json
{
  "keybindings": {
    "new_project": "n",
    "new_session": "t",
    "new_team": "T",
    "kill_session": "x",
    "kill_team": "D",
    "rename": "r",
    "attach": "a",
    "nav_up": "up",
    "nav_down": "down",
    "nav_project_up": "K",
    "nav_project_down": "J",
    "filter": "/",
    "sidebar_view": "s",
    "grid_overview": "g",
    "palette": "ctrl+p",
    "help": "?",
    "tmux_help": "H",
    "quit": "q",
    "quit_kill": "Q",
    "move_up": "shift+up",
    "move_down": "shift+down",
    "move_left": "shift+left",
    "move_right": "shift+right"
  }
}
```

> **Note:** Users who prefer vim-style navigation (`j`/`k` for up/down) can set `"nav_up": "k"` and `"nav_down": "j"` in their config. Arrow keys (`↑`/`↓`) are permanently available as aliases regardless of the configured key, so switching to vim-style config does not break arrow key navigation.

> **Reset to defaults:** Open Settings (`S`) and press `R` to reset all keybindings to their defaults. The change takes effect after you save (`s` → confirm).

