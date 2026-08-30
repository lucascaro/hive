# Strip ANSI escape sequences from update build progress lines

- **Issue:** —
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Stage:** IMPLEMENT
- **Exec plan:** [docs/exec-plans/active/253-strip-ansi-escapes-from-update-progress.md](../exec-plans/active/253-strip-ansi-escapes-from-update-progress.md)

## Problem

On the `latest` update channel, clicking Update runs `./build.sh` in the source
checkout and streams its output into the green update banner. `build.sh` drives
`npm` / `vite` / `wails`, which emit ANSI color and cursor-control sequences and
redraw progress with carriage returns. Those bytes reach the GUI verbatim and
the banner renders them as text, so the user sees literal `ESC[32m…` garbage
instead of a readable build status.

## Desired behavior

The update banner shows a plain, readable line of build progress. Terminal
control bytes never appear; a carriage-return redraw shows only the final
segment, the way a terminal would render it.

## Success criteria

- No `\x1b` byte can reach the update progress message or the `build.sh failed:`
  error text.
- `50%\r80%\r100%` renders as `100%`.
- A line that is nothing but control sequences is skipped, not shown blank.
- UTF-8 content (e.g. `✓ built`) survives unchanged.

## Non-goals

- Rendering colors in the banner.
- A general-purpose ANSI parser; `internal/session` already owns VT parsing for
  the terminal.
- Any frontend change — the banner correctly renders whatever text it is given.

## Notes

Root cause: `runBuildScript` in `cmd/hivegui/update_apply_darwin.go` forwards
each scanned line straight to `progress(line)`.
