---
issue: 449
title: Cmd-click file paths in a session to open them
type: enhancement
complexity: L
priority: P2
pr: 450
stage: REVIEW
---

# Cmd-click file paths in a session to open them

- **Issue:** #449
- **Exec plan:** [docs/exec-plans/active/449-cmd-click-file-paths-in-a-session-to-open-them.md](../exec-plans/active/449-cmd-click-file-paths-in-a-session-to-open-them.md) (or completed/)

## Problem

Agents and shells constantly print file paths (`src/app/foo.ts:42`, `/tmp/out.log`, `~/notes.md`). Today only URLs are clickable. To open a path you have to copy it and switch to an editor or Finder by hand. Hive also refuses every `file://` link, including OSC 8 ones. That was a deliberate security choice, because terminal output is attacker-influenced, but nothing replaced it with a safe way to open files.

## Desired behavior

- Absolute, `~/`, and relative file paths in session output are detected. A path is underlined on hover only if it exists on disk. Relative paths resolve against the session's launch directory. A `:line[:col]` suffix is recognized.
- **Cmd-click** (Ctrl-click on Linux/Windows) opens the file with the OS default app. Executables, app bundles, and script or launcher types the OS would run are never opened. They are revealed in the file manager instead. A directory is revealed in the file manager.
- **Cmd+shift-click** always opens the file in the user's configured editor, at the line and column when the path has them. A directory opens as a workspace.
- The editor is set in app settings. You pick from a preset list (VS Code, Cursor, Zed, Sublime, …), each knowing its own line-jump syntax, or add a custom command or app. If no editor is set, cmd+shift-click falls back to the OS-default behavior.
- OSC 8 `file://` hyperlinks go through the same path and the same executable guard, instead of being refused.
- Mouse-reporting TUIs follow the same modifier convention URL links use today.
- Works on macOS, Linux and Windows.

## Success criteria

1. In a session launched in a repo, output of `src/foo.ts:12` (existing file) is underlined on hover. Cmd-click opens it with the OS default app. Output naming a nonexistent file is not underlined.
2. `/abs/path/file.md` and `~/file.md` behave the same as (1).
3. With VS Code (or any preset) configured, cmd+shift-click on `src/foo.ts:12:5` opens that file at line 12, column 5.
4. A custom editor command added in settings is used by cmd+shift-click, and it receives the file and the line.
5. With no editor configured, cmd+shift-click does what plain cmd-click does.
6. Cmd-click on a `.app` bundle, a `.command` file, a `+x` script (mac/Linux), or an `.exe`/`.bat`/`.lnk` file (Windows) does not launch it. The file is revealed in the file manager. A unit test covers the guard on each platform.
7. An OSC 8 link whose URI is `file:///…` opens under the same rules as (1) and (6). A `file://` link to an executable is still not launched.
8. Cmd-click on a directory path reveals it in the file manager.
9. Existing URL click behavior (http/https/mailto) is unchanged. The OpenURL scheme allowlist still rejects other schemes.
10. If an open fails (the editor is missing or the command errors), the user sees an error in the status bar (the existing `reportFailure` flash). Nothing fails silently.

## Non-goals

- Tracking the shell's live cwd (OSC 7). Relative paths resolve against the launch dir only, so paths printed after a `cd` may not resolve.
- Running, executing, or "open with" pickers for arbitrary apps beyond the configured editor.
- Detecting paths inside images, or making paths clickable in the sidebar, launcher, or other non-terminal UI.
- Using `$EDITOR`/`$VISUAL` as the editor source.
- Previewing files inside Hive.

## Notes

- Open question: the exact per-platform executable/launcher denylist (e.g. `.terminal`, `.workflow`, `.desktop`, `.ps1`) is to be enumerated at plan stage.
- `OpenURL` in `cmd/hivegui/app_calls.go` deliberately refuses `file://` because terminal content is attacker-influenced; this feature must preserve that security intent.
- Prior art in-repo: `WebLinksAddon` and the OSC 8 `linkHandler` in `cmd/hivegui/frontend/src/app/session-term.ts`; `OpenTerminalAt` in `cmd/hivegui/os_terminal.go` shows the per-OS `open`/launch pattern.

## Follow-ups

- **Resolve relative paths against the shell's live cwd.** A path printed after a `cd` does not resolve today, so it is simply not underlined. The shape: OSC 7 as the primary source (macOS `/etc/zshrc` already emits it), the pty child's cwd from the OS as the fallback (`/proc/<pid>/cwd`, `proc_pidinfo`), today's worktree path last. The real design question is *when* cwd is sampled — a click can land long after the line was printed, so correctness likely means remembering cwd per line rather than per session. Needs its own spec; deliberately out of scope here.
- **`ls somedir` output will still not work**, and should not be attempted: those lines print bare names (`foo.txt`), not `somedir/foo.txt`, so the visible text does not name a resolvable path. Only the command above it does, and guessing from scrollback would open the wrong file silently. `ls -R` and `ls somedir/*` print usable paths and work once live cwd lands.
