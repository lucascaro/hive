// ---------- app state ----------
//
// The single shared mutable state object. Every app module imports it
// from here; main.js stays the composition root that wires behavior.

import { DEFAULT_FONT_SIZE, clampFont } from '../lib/font.js';
import { normalizeView, VIEW_STORAGE_KEY } from '../lib/view.js';
import { loadCollapsed, serializeCollapsed, COLLAPSED_STORAGE_KEY } from '../lib/collapsed.js';

export const state = {
  projects: [],             // ProjectInfo[] in display order
  sessions: [],             // SessionInfo[] in display order
  collapsed: loadSavedCollapsed(), // project ids that are collapsed — persisted
  attention: new Set(),     // session ids that have unread bells
  minimized: new Set(),     // session ids hidden from grid views; restored via tray
  aliveById: new Map(),     // session id -> last-seen Alive bool (for transition detection)
  dismissedDead: new Set(), // session ids whose dead overlay user dismissed
  terms: new Map(),         // session id -> SessionTerm
  activeId: null,
  currentProjectId: null,   // "the project I'm working in"; can be set
                            //   without a focused session (so empty
                            //   projects are reachable / launchable)
  view: loadSavedView(),    // 'single' | 'grid-project' | 'grid-all' — persisted across launches
  gridProjectId: null,      // project shown in grid-project mode
  fontSize: clampFont(parseInt(localStorage.getItem('hive.fontSize') ?? '', 10) || DEFAULT_FONT_SIZE),
};

// E2E test affordance: expose the term registry under a dunder name
// so Playwright specs can read xterm buffer contents via
// state.terms.get(id).term.buffer.active. Gated on the Vite mock/real
// env vars so production builds drop this — the gates are inlined to
// string literals by Vite at build time, so the whole block is dead
// code in a normal wails build.
if (typeof window !== 'undefined'
    && (import.meta.env.VITE_WAILS_MOCK === '1' || import.meta.env.VITE_WAILS_REAL === '1')) {
  window.__hive_state = state;
}

export function loadSavedView() {
  try { return normalizeView(localStorage.getItem(VIEW_STORAGE_KEY)); }
  catch { return normalizeView(null); }
}

export function loadSavedCollapsed() {
  try { return loadCollapsed(localStorage.getItem(COLLAPSED_STORAGE_KEY)); }
  catch { return new Set(); }
}

export function saveCollapsed() {
  try { localStorage.setItem(COLLAPSED_STORAGE_KEY, serializeCollapsed(state.collapsed)); }
  catch { /* private mode etc. — collapse state just won't persist */ }
}
