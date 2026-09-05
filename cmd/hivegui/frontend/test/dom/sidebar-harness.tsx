// Shared scaffold for the RTL suites that drive the sidebar island.
//
// The sidebar's module graph reaches app/dom.ts, which resolves #terms /
// #status at import time and throws when they are missing — so the
// markup has to exist BEFORE the component is imported, which is why
// loadSidebar() does the import itself rather than the suite importing
// it at the top of the file.
import { act, render } from '@testing-library/react';
import type { ReactNode } from 'react';
import type { SidebarProps } from '../../src/components/Sidebar';
import { resetStore, type AppData } from '../../src/store/store';

export const noop = () => {};

// The ids the sidebar island and its module graph need. #minimized-tray
// and #empty-state belong to view.ts, which nothing here calls — they are
// present so an accidental call has somewhere to land instead of
// throwing.
export const SIDEBAR_HTML = `
  <div id="app">
    <ul id="projects"></ul>
    <div id="minimized-projects" class="hidden" role="toolbar"></div>
    <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    <div id="terms"></div><div id="minimized-tray"></div>
    <div id="empty-state"></div>
  </div>`;

type SidebarComponent = (p: SidebarProps) => ReactNode;

export async function loadSidebar(): Promise<SidebarComponent> {
  document.body.innerHTML = SIDEBAR_HTML;
  const mod = await import('../../src/components/Sidebar.js');
  return mod.Sidebar;
}

export function projectsUL(): HTMLElement {
  const el = document.getElementById('projects');
  if (!el) throw new Error('#projects missing — call loadSidebar() first');
  return el;
}

export function trayEl(): HTMLElement | null {
  return document.getElementById('minimized-projects');
}

export function mountSidebar(
  Sidebar: SidebarComponent,
  over: Partial<SidebarProps> = {},
) {
  const props: SidebarProps = {
    switchTo: noop,
    switchToProject: noop,
    minimizeProject: noop,
    restoreProject: noop,
    minimizeSession: noop,
    restoreSession: noop,
    confirmAndDeleteProject: noop,
    refocusActiveTerm: noop,
    trayEl: trayEl(),
    ...over,
  };
  return render(<Sidebar {...props} />, { container: projectsUL() });
}

// Seed the store the way a daemon snapshot would, then let React render
// from it. Wrapped in act() because a store write outside act() leaves
// React's work queue unflushed and the assertions race the render.
export function seed(data: Partial<AppData>) {
  act(() => {
    // projectSetsHydrated mirrors the post-boot steady state: by the time
    // a daemon snapshot lands, main.tsx has hydrated the persisted
    // project sets. Without it every harness test would take
    // applyProjectList's un-hydrated early return and never exercise the
    // real prune (#340).
    resetStore({ projectSetsHydrated: true, ...data });
  });
}

// Any store write made after the first render. Same act() reason.
export function update(fn: () => void) {
  act(() => {
    fn();
  });
}

export function row(id: string): HTMLElement {
  const el = document.querySelector<HTMLElement>(
    `.hv-session-row[data-sid="${id}"]`,
  );
  if (!el) throw new Error(`no sidebar row for ${id}`);
  return el;
}

export function card(pid: string): HTMLElement | null {
  return document.querySelector<HTMLElement>(
    `.hv-project-card[data-pid="${pid}"]`,
  );
}
