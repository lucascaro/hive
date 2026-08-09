// ---------- app state ----------
//
// The single shared mutable state object. Every app module imports it
// from here; main.js stays the composition root that wires behavior.

import { DEFAULT_FONT_SIZE, clampFont } from '../lib/font.js';
import { normalizeView, VIEW_STORAGE_KEY } from '../lib/view.js';
import {
  loadCollapsed,
  serializeCollapsed,
  COLLAPSED_STORAGE_KEY,
} from '../lib/collapsed.js';
import { createNavHistory, type NavHistory } from '../lib/nav-history.js';
import type { ViewMode } from '../lib/view.js';
import type { ReplayFlags, ReplayXterm } from '../lib/scrollback.js';

// Wire payloads from the daemon (internal/wire/control.go) are
// snake_case; some paths also carry camelCase, so both spellings are
// optional and call sites read `x_y ?? xY` (see selectors.ts). These
// are NOT the generated wailsjs models — sessions/projects arrive over
// EventsOn as raw JSON, so there is nothing generated to import.
export interface SessionInfo {
  id: string;
  name?: string;
  agent?: string;
  color?: string;
  order?: number;
  alive?: boolean;
  project_id?: string;
  projectId?: string;
  worktree_path?: string;
  worktreePath?: string;
  worktree_branch?: string;
  worktreeBranch?: string;
  // Why the dead-session overlay reads: events.js:131 and
  // session-term.js:1173 both fall back off it.
  last_error?: string;
  lastError?: string;
}

export interface ProjectInfo {
  id: string;
  name?: string;
  cwd?: string;
  color?: string;
  order?: number;
}

// A structural view of SessionTerm (app/session-term.js), which is still
// JS until wave 6. Wave 3 typed the registry `unknown` so its files
// couldn't pretend to know the class; wave 5b reverses that, because
// view.ts and focus.ts touch these members at ~30 sites and the
// alternative is 12 unchecked `as` casts at the `state.terms.get()`
// calls. This lists only what app modules actually reach for — wave 6
// replaces it with SessionTerm's own type, which must then satisfy this
// shape or widen it deliberately.
//
// `term` stays optional and structural rather than `import { Terminal }`
// so a TermTile is still assignable to SnapTarget (lib/view-scroll.ts) —
// which view.ts relies on at snapVisibleTermsToBottom(). The shared
// `term` key is also what keeps that assignment out of TS's weak-type
// check, since every SnapTarget member is optional.
//
// Extends ReplayFlags and types `term` as ReplayXterm so a TermTile is
// accepted where lib/scrollback.ts wants a ReplayTerm — events.ts hands
// tiles straight to handleScrollbackEvent/abandonReplays, and the
// alternative was a cast at each of those five call sites.
export interface TermTile extends ReplayFlags {
  host: HTMLElement;
  termTitle?: string;
  // Required, not optional: session-term.js:426,432 always initializes
  // both and every reader branches on the value, never on absence
  // (scrollback.ts:28 states the rule).
  attached: boolean;
  needsReattach: boolean;
  // Timestamp of the last replay event, used by the scroll-jump
  // detector to label a following up-move (session-term.js:594,728).
  _lastReplayTs?: number;
  // `options` is here for applyFontSize (session-term.ts), the one app-side
  // writer of xterm's live config. Optional like the rest of the
  // intersection: the DOM-test stubs omit `term` entirely.
  term?:
    | (ReplayXterm & {
        focus?: () => void;
        options?: { fontSize?: number };
      })
    | null;
  // Dead-session overlay. Required for the same reason as `attached`:
  // session-term.js:525 initializes it and setDead writes it on every
  // transition, so readers branch on the value, never on absence.
  // keyboard.ts routes Enter/Escape to the two handlers when it's shown.
  deadOverlayShown: boolean;
  _closeDead(): void;
  _dismissDead(): void;
  show(): void;
  hide(): void;
  ensureAttached(): void;
  rebaselineReplayCols(reason: string): void;
  // The single resize entry point. applyFontSize (session-term.ts) calls it
  // explicitly because a font-size change doesn't resize the body box, so
  // the ResizeObserver never fires on its own.
  _onBodyResize(): void;
  setInfo(info: SessionInfo): void;
  // Both params are optional because the implementation defaults them
  // (`name || ''`, `color || '#888'`) and ProjectInfo's fields are optional.
  setProject(name?: string, color?: string): void;
  setDead(isDead: boolean, reason?: string): void;
  writeData(b64: string): void;
  destroy(): void;
}

export interface AppState {
  projects: ProjectInfo[];
  sessions: SessionInfo[];
  collapsed: Set<string>;
  attention: Set<string>;
  attentionReturnId: string | null;
  attentionRestored: Set<string>;
  nav: NavHistory;
  minimized: Set<string>;
  aliveById: Map<string, boolean>;
  dismissedDead: Set<string>;
  terms: Map<string, TermTile>;
  activeId: string | null;
  currentProjectId: string | null;
  view: ViewMode;
  gridProjectId: string | null;
  fontSize: number;
}

export const state: AppState = {
  projects: [], // ProjectInfo[] in display order
  sessions: [], // SessionInfo[] in display order
  collapsed: loadSavedCollapsed(), // project ids that are collapsed — persisted
  attention: new Set(), // session ids that have unread bells
  attentionReturnId: null, // session to jump back to (⇧⌘B): the one you
  //   were in before the FIRST ⌘B. Written only
  //   when empty, so a round of bells that walks
  //   you through several flagged sessions keeps
  //   the original anchor; cleared on use.
  attentionRestored: new Set(), // sessions ⌘B pulled out of the minimized
  //   tray this round; ⇧⌘B puts them back.
  nav: createNavHistory(), // back/forward stacks of visited session ids
  //   (Ctrl+- / Ctrl+Shift+-). Deliberately NOT
  //   persisted, unlike `collapsed`: the terminals
  //   are gone after a restart anyway. Written from
  //   setActive (app/focus.js), the sole writer of
  //   activeId, so every switch path is recorded.
  minimized: new Set(), // session ids hidden from grid views; restored via tray
  aliveById: new Map(), // session id -> last-seen Alive bool (for transition detection)
  dismissedDead: new Set(), // session ids whose dead overlay user dismissed
  terms: new Map(), // session id -> SessionTerm
  activeId: null,
  currentProjectId: null, // "the project I'm working in"; can be set
  //   without a focused session (so empty
  //   projects are reachable / launchable)
  view: loadSavedView(), // 'single' | 'grid-project' | 'grid-all' — persisted across launches
  gridProjectId: null, // project shown in grid-project mode
  fontSize: clampFont(
    parseInt(localStorage.getItem('hive.fontSize') ?? '', 10) ||
      DEFAULT_FONT_SIZE,
  ),
};

// E2E test affordance: expose the term registry under a dunder name
// so Playwright specs can read xterm buffer contents via
// state.terms.get(id).term.buffer.active. Gated on the Vite mock/real
// env vars so production builds drop this — the gates are inlined to
// string literals by Vite at build time, so the whole block is dead
// code in a normal wails build.
if (
  typeof window !== 'undefined' &&
  (import.meta.env.VITE_WAILS_MOCK === '1' ||
    import.meta.env.VITE_WAILS_REAL === '1')
) {
  window.__hive_state = state;
}

export function loadSavedView(): ViewMode {
  try {
    return normalizeView(localStorage.getItem(VIEW_STORAGE_KEY));
  } catch {
    return normalizeView(null);
  }
}

export function loadSavedCollapsed(): Set<string> {
  try {
    return loadCollapsed(localStorage.getItem(COLLAPSED_STORAGE_KEY));
  } catch {
    return new Set();
  }
}

export function saveCollapsed(): void {
  try {
    localStorage.setItem(
      COLLAPSED_STORAGE_KEY,
      serializeCollapsed(state.collapsed),
    );
  } catch {
    /* private mode etc. — collapse state just won't persist */
  }
}
