# Hive Keyboard Shortcuts

## Sidebar Navigation

| Key | Action |
|-----|--------|
| `j` / `↓` | Move cursor down |
| `k` / `↑` | Move cursor up |
| `J` | Jump to next project |
| `K` | Jump to previous project |
| `1`–`9` | Jump to project by index |
| `Space` | Toggle collapse/expand project or team |
| `←` | Collapse current project or team; if on a session, collapse its parent |
| `→` | Expand current project or team |

## Session Management

| Key | Action |
|-----|--------|
| `n` | New project |
| `t` | New session (opens agent picker) |
| `T` | New agent team (opens wizard) |
| `a` / `Enter` | Attach to selected session |
| `r` | Rename selected session or team |
| `x` / `d` | Kill selected session (with confirmation) |
| `D` | Kill entire team (with confirmation) |

## Pane Focus

| Key | Action |
|-----|--------|
| `Tab` | Toggle between sidebar and preview focus |

## Grid View

| Key | Action |
|-----|--------|
| `g` | Open grid view for the current project |
| `G` | Open grid view for all projects |
| `←`/`→`/`↑`/`↓` or `h`/`l`/`k`/`j` | Navigate grid cells |
| `Enter` / `a` | Attach to selected session |
| `x` | Kill selected session (with confirmation) |
| `r` | Rename selected session |
| `G` | Switch to all-projects view (while grid is open) |
| `g` / `Esc` / `q` | Exit grid view |

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

## Confirmation Dialogs

| Key | Action |
|-----|--------|
| `y` / `Enter` | Confirm |
| `n` / `Esc` | Cancel |

## Customization

All key bindings (except `G` and grid-internal keys) can be overridden in `~/.config/hive/config.json` under the `keybindings` key:

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
    "nav_up": "k",
    "nav_down": "j",
    "nav_project_up": "K",
    "nav_project_down": "J",
    "filter": "/",
    "grid_overview": "g",
    "palette": "ctrl+p",
    "help": "?",
    "tmux_help": "H",
    "quit": "q",
    "quit_kill": "Q"
  }
}
```
