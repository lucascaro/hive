// Single re-export point for the Wails bridge surface.
//
// MUST stay a sibling of main.js: the vite plugin in vite.config.js
// substitutes the test harnesses (wails-mock.js / wails-bridge.js) by
// matching the EXACT literal specifiers '../wailsjs/go/main/App' and
// '../wailsjs/runtime/runtime' — moving this file changes the relative
// specifier and silently breaks both Playwright harnesses.
//
// Explicit named re-exports (no `export *`): under substitution both
// specifiers resolve to ONE module, and star-re-exporting the same
// names from two paths would make them ambiguous (ES modules silently
// drop ambiguous star exports).

export {
  ConnectControl, OpenSession, CloseAttach,
  WriteStdin, ResizeSession, RequestScrollbackReplay,
  CreateSession, DuplicateSession, KillSession, RestartSession, UpdateSession, ListAgents,
  CreateProject, KillProject, UpdateProject,
  LaunchDir, PickDirectory, OpenNewWindow, CloseWindow,
  IsGitRepo, OpenURL, OpenTerminalAt, Notify, Confirm,
  RestartDaemon, CheckForUpdate, SetClipboardText,
} from '../wailsjs/go/main/App';
export { EventsOn, WindowSetTitle, ClipboardGetText } from '../wailsjs/runtime/runtime';
