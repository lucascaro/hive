// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// ---------- session finder (spec 544): the non-React half ----------
//
// The list renders from components/modals/SessionFinder.tsx and the
// matching lives in lib/session-finder.ts. What stays here is the
// open/close pair the command layer runs, and the jump Enter performs.

import { flushSync } from 'react-dom';
import { releaseFocus } from '../../lib/focus-trap.js';
import {
  anyModalOpen,
  closeModal,
  isModalOpen,
  openModal,
} from '../../store/store.js';
import { pageEl } from '../el.js';

export interface SessionFinderDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
  goToSession: (id: string) => void;
}

let deps: SessionFinderDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
  goToSession: () => {},
};

export function openSessionFinder(): void {
  if (isModalOpen('session-finder')) return;
  openModal({ id: 'session-finder' });
  deps.setFocusedTile(null);
}

export function closeSessionFinder(): void {
  if (!isModalOpen('session-finder')) return;
  releaseFocus(pageEl('session-finder'));
  flushSync(() => closeModal('session-finder'));
  // Only when this was the last modal: handing focus to the terminal
  // underneath a still-open dialog sends keystrokes to the PTY behind it.
  if (anyModalOpen()) return;
  deps.refocusActiveTerm();
}

// Enter on a row: close first (focus goes home), then jump. Deferred so
// the close finishes before the jump moves focus to the target terminal —
// the same order the command palette uses to run a command.
export function activateFinderSession(id: string): void {
  closeSessionFinder();
  setTimeout(() => deps.goToSession(id), 0);
}

export function initSessionFinder(injected: SessionFinderDeps): void {
  deps = injected;
}
