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
| `q` | Quit (tmux sessions persist) |
| `Q` | Quit and kill all managed sessions |
| `Ctrl+C` | Force quit |

## Confirmation Dialogs

| Key | Action |
|-----|--------|
| `y` / `Enter` | Confirm |
| `n` / `Esc` | Cancel |

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
    "nav_up": "k",
    "nav_down": "j",
    "nav_project_up": "K",
    "nav_project_down": "J",
    "filter": "/",
    "palette": "ctrl+p",
    "help": "?",
    "quit": "q",
    "quit_kill": "Q"
  }
}
```
