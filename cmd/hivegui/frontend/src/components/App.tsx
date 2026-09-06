// The single React root's tree. Phase 6 of the React rewrite replaced
// the fifteen island roots the composition root used to mount with
// this one component.
//
// Every region is a portal into the element index.html already owns,
// rather than markup this tree emits, and that is deliberate:
//
//   1. #terms must not be React's. Its children are SessionTerm hosts —
//      each holding an xterm, a WebGL slot (8 process-wide) and a live
//      PTY attachment — and unmount/remount of a mounted terminal is the
//      bug the whole migration exists to avoid (master plan, Invariant
//      5). A tree that emitted #app's children would have to emit #terms
//      among them.
//   2. #boot-state's card is painted from index.html before any module
//      script runs, because a cold daemon can take seconds and a black
//      pane reads as a broken app. A tree that owned #app would blank
//      and rebuild that card at mount, one frame of flash.
//   3. Every id, grid-row placement and aria attribute in index.html
//      survives untouched, which is what keeps the 30 Playwright specs
//      passing unmodified (Invariant 1).
//
// What the single root buys over the islands it replaces: one
// reconciler and one commit, so every region lands in the same frame
// and in this file's order, instead of fifteen commits whose relative
// timing had to be reasoned about (the version footer's subscription
// used to have to be mounted ahead of the modals to win a race with the
// control handshake — with one commit there is no gap to lose).
//
// The containers' own classes (.hidden, .error, .mismatch) are still
// applied by each component's layout effect: they sit on the portal
// target, outside this tree.
import { createPortal } from 'react-dom';
import type { ReactNode } from 'react';
import { mustEl } from '../app/el.js';
import { confirmAndDeleteProject } from '../app/keyboard.js';
import { refocusActiveTerm, setFocusedTile } from '../app/focus.js';
import {
  minimizeProject,
  minimizeSession,
  restoreProject,
  restoreSession,
  switchTo,
  switchToProject,
} from '../app/view.js';
import { Banners } from './Banners.js';
import { BootState } from './BootState.js';
import { EmptyState } from './EmptyState.js';
import { GridView } from './GridView.js';
import { TileChromeHost } from './TileChrome.js';
import { MinimizedTray } from './MinimizedTray.js';
import {
  Sidebar,
  SidebarHeaderControls,
  type SidebarProps,
} from './Sidebar.js';
import { StatusBar } from './StatusBar.js';
import { VersionFooter } from './VersionFooter.js';
import { ChoiceDialog } from './modals/ChoiceDialog.js';
import { CommandPalette } from './modals/CommandPalette.js';
import { HelpOverlay } from './modals/HelpOverlay.js';
import { Launcher } from './modals/Launcher.js';
import { ProjectEditor } from './modals/ProjectEditor.js';
import { Settings } from './modals/Settings.js';
import { Worktrees } from './modals/Worktrees.js';
import { WhatsNew } from './modals/WhatsNew.js';

// Built once, at module scope, not per render: SessionItem memoises on
// this object's identity, so a fresh bag each render would defeat the
// memo that is where the phase's performance goal is collected.
const sidebarCallbacks: Omit<SidebarProps, 'trayEl'> = {
  switchTo,
  switchToProject,
  minimizeProject,
  restoreProject,
  minimizeSession,
  restoreSession,
  confirmAndDeleteProject,
  refocusActiveTerm,
};

export function App(): ReactNode {
  // getElementById per render rather than a module-scope lookup: a
  // module-scope handle would pin whichever document was loaded when this
  // module was first imported, which the dom tests (each of which builds
  // its own markup) would get wrong. The lookups are cheap and return the
  // same node, so the `root` props stay referentially stable.
  //
  // mustEl, not pageEl: pageEl is a cast that yields null for a missing id,
  // and createPortal(node, null) throws — which, with one root, takes the
  // whole tree down instead of the one island that used to be skipped.
  // Failing here names the id that went missing.
  const projects = mustEl('projects');
  const status = mustEl('status');
  const bootState = mustEl('boot-state');
  const emptyState = mustEl('empty-state');
  const minimizedTray = mustEl('minimized-tray');
  const sidebarHints = mustEl('sidebar-hints');
  const launcher = mustEl('launcher');
  const settings = mustEl('settings');
  const worktrees = mustEl('worktrees');
  const projectEditor = mustEl('project-editor');
  const helpOverlay = mustEl('help-overlay');
  const choiceDialog = mustEl('choice-dialog');
  const commandPalette = mustEl('command-palette');
  const whatsNew = mustEl('whats-new');

  return (
    <>
      {createPortal(
        <Sidebar {...sidebarCallbacks} trayEl={mustEl('minimized-projects')} />,
        projects,
      )}
      {/* The sidebar header's two icon controls. Portals of its own
          rather than props of the tree above: they land in index.html's
          <header>, a sibling of #projects. Renders null when there is no
          header, which is how the dom-test scaffolds get away with
          omitting it. */}
      <SidebarHeaderControls />
      {/* Renders nothing. Its whole job is a layout effect against
          app/grid-layout.ts when the view, active tile or grid scope
          moves — which is why it needs no portal at all. */}
      <GridView />
      {/* One portal per live terminal tile, into hosts app/session-term.ts
          owns. Renders no layout of its own — see TileChrome.tsx. */}
      <TileChromeHost />
      {/* #banners is `display: contents` (layout.css), so the three
          banners stay direct children of the #app grid and keep their
          row placement. */}
      {createPortal(<Banners />, mustEl('banners'))}
      {createPortal(<StatusBar root={status} />, status)}
      {createPortal(<BootState root={bootState} />, bootState)}
      {createPortal(<EmptyState root={emptyState} />, emptyState)}
      {createPortal(
        <MinimizedTray root={minimizedTray} restoreSession={restoreSession} />,
        minimizedTray,
      )}
      {/* Sidebar footer: hive/hived version + build. Takes its own
          "daemon:stale" subscription, live from this commit. */}
      {createPortal(<VersionFooter root={sidebarHints} />, sidebarHints)}
      {/* The modals. Each stays mounted on the root its region owns; the
          store decides whether anything renders inside, and the
          component toggles the root's `hidden` class. */}
      {createPortal(
        <Launcher root={launcher} setFocusedTile={setFocusedTile} />,
        launcher,
      )}
      {createPortal(<Settings root={settings} />, settings)}
      {createPortal(<Worktrees root={worktrees} />, worktrees)}
      {createPortal(<ProjectEditor root={projectEditor} />, projectEditor)}
      {createPortal(<HelpOverlay root={helpOverlay} />, helpOverlay)}
      {createPortal(<ChoiceDialog root={choiceDialog} />, choiceDialog)}
      {createPortal(<CommandPalette root={commandPalette} />, commandPalette)}
      {createPortal(<WhatsNew root={whatsNew} />, whatsNew)}
    </>
  );
}
