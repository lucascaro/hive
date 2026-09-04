// ---------- app state: the shared type surface ----------
//
// This file used to own the app's data as one mutable object, and then
// (Phases 0-5) a `state` facade that read and wrote through to the
// store. Phase 6 deleted the facade: the data lives in store/store.ts
// (zustand) and store/terms.ts (the terminal registry), and every module
// reads it there directly.
//
// What is left is the type surface. It stays in app/ rather than moving
// into store/ because SessionInfo/ProjectInfo are wire shapes and
// TermTile describes a SessionTerm, none of which the store owns; and
// because ~30 files import these types, so a move is a diff through all
// of them for no gain.
//
// Nothing here imports the store, which is what broke the old
// app/state <-> store/store import cycle.

import type { NavHistory } from '../lib/nav-history.js';
import type { ViewMode } from '../lib/view.js';
import type { ReplayFlags, ReplayXterm } from '../lib/scrollback.js';
import type { xtermTheme } from '../theme/theme.js';

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
  // Why the dead-session overlay reads: events.ts:174 and
  // session-term.ts:1269 both fall back off it.
  last_error?: string;
  lastError?: string;
  // Lifecycle phase (internal/wire/control.go Phase*). Absent/empty
  // means ready — the daemon omits it in the steady state.
  phase?: string;
  // OSC 0/2 window title the running program most recently set, read off
  // the daemon's VT mirror (internal/wire/control.go SessionInfo.Title).
  // Daemon-owned and in-memory only, so it is absent for a session with
  // no live process and after a daemon restart. Single-spelled: the
  // daemon emits `title` and there is no camelCase variant to fall back
  // to.
  title?: string;
  // True while the program on this session has rung the terminal bell
  // and nobody has looked since (internal/wire SessionInfo.NeedsAttention).
  // Daemon-owned, so every window and the menu bar agree; omitted when
  // false. Single-spelled, like `title`.
  needs_attention?: boolean;
  // What the daemon believes this session is doing right now
  // (internal/wire/control.go State*). Absent means idle — that is the
  // omitempty case AND what a daemon too old to send it looks like, on
  // purpose, so no client needs an "unknown" branch.
  //
  // Single-spelled snake_case like `title` and `needs_attention`: these
  // come straight off the wire as JSON, where the daemon's struct tags
  // are the only spelling that exists.
  state?: string;
  // Which tier produced `state` (internal/wire/control.go StateSource*).
  // Absent means the heuristic tier — derived from PTY bytes alone, and
  // so a guess. The UI marks the difference rather than presenting a
  // guess as a fact.
  state_source?: string;
  // The first thing this session was asked to do, and what the agent
  // said as it finished its last turn. Both absent on the heuristic
  // tier, which cannot know either.
  last_prompt?: string;
  last_summary?: string;
}

/** Reads the daemon's attention flag off a session, defaulting to false
 * for the omitempty case and for a daemon too old to send it. */
export function readNeedsAttention(s: SessionInfo): boolean {
  return s.needs_attention === true;
}

export interface ProjectInfo {
  id: string;
  name?: string;
  cwd?: string;
  color?: string;
  order?: number;
}

// A structural view of SessionTerm (app/session-term.ts). Wave 3 typed
// the registry `unknown` so its files couldn't pretend to know the class;
// wave 5b reversed that, because view.ts and focus.ts touch these members
// at ~30 sites and the alternative is 12 unchecked `as` casts at the
// `state.terms.get()` calls.
//
// Wave 6 was expected to replace this with SessionTerm's own type once
// that file converted. It deliberately did NOT: `Map<string, SessionTerm>`
// would force every DOM-test stub to spell out 53 fields instead of 16.
// The interface stays, listing only what app modules actually reach for,
// and SessionTerm satisfies it structurally — which is the check
// `state.terms.set()` already performs at every insertion site.
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
  // The chrome mount points. components/TileChrome.tsx portals the
  // header's children into `header` and the overlays into `overlays`;
  // both elements are created and destroyed by SessionTerm, never by
  // React. Optional because the DOM-test stubs render no chrome.
  header?: HTMLElement;
  overlays?: HTMLElement;
  // Required, not optional: session-term.ts:514,520 always initializes
  // both and every reader branches on the value, never on absence
  // (scrollback.ts:28 states the rule).
  attached: boolean;
  needsReattach: boolean;
  // Timestamp of the last replay event, used by the scroll-jump
  // detector to label a following up-move (session-term.ts:682,816).
  _lastReplayTs?: number;
  // `options` is here for applyFontSize and applyXtermTheme
  // (session-term.ts), the two app-side writers of xterm's live config.
  // Optional like the rest of the intersection: the DOM-test stubs omit
  // `term` entirely.
  term?:
    | (ReplayXterm & {
        focus?: () => void;
        options?: {
          fontSize?: number;
          fontFamily?: string;
          theme?: Partial<ReturnType<typeof xtermTheme>>;
        };
      })
    | null;
  // Dead-session overlay. Required for the same reason as `attached`:
  // session-term.ts:613 initializes it and setDead writes it on every
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
  // Lifecycle phase (lib/phase-steps.ts). `phase` is required for the
  // same reason as `attached`: SessionTerm initializes it and every
  // reader branches on the value.
  phase: string;
  setPhase(phase: string): void;
  // Called on scrollback_replay_done to drop the loading panel once
  // the terminal has painted.
  revealAfterReplay(): void;
  writeData(b64: string): void;
  destroy(): void;
}

export interface AppState {
  projects: ProjectInfo[];
  sessions: SessionInfo[];
  collapsed: Set<string>;
  minimizedProjects: Set<string>;
  attention: Set<string>;
  attentionReturnId: string | null;
  attentionRestored: Set<string>;
  attentionRestoredProjects: Set<string>;
  nav: NavHistory;
  minimized: Set<string>;
  aliveById: Map<string, boolean>;
  phaseById: Map<string, string>;
  dismissedDead: Set<string>;
  terms: Map<string, TermTile>;
  activeId: string | null;
  currentProjectId: string | null;
  view: ViewMode;
  gridProjectId: string | null;
  fontSize: number;
}
