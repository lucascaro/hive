// ---------- archived sessions (spec 518): the non-React half ----------
//
// The list renders from components/modals/ArchivedSessions.tsx. What
// stays here is the open/close/toggle trio the command layer imports and
// the refresh that fills the list on open — the daemon's archived:list
// answer is what the sheet renders, so opening asks for a fresh one.

import { flushSync } from 'react-dom';
import {
  anyModalOpen,
  closeModal,
  isModalOpen,
  openModal,
} from '../../store/store.js';
import { pageEl } from '../el.js';
import { releaseFocus } from '../../lib/focus-trap.js';
import { refreshArchived } from '../archive.js';

export interface ArchivedSessionsDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
}

let deps: ArchivedSessionsDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

export function openArchivedSessions(): void {
  openModal({ id: 'archived-sessions' });
  deps.setFocusedTile(null);
  refreshArchived();
}

export function closeArchivedSessions(): void {
  if (!isModalOpen('archived-sessions')) return;
  releaseFocus(pageEl('archived-sessions'));
  flushSync(() => closeModal('archived-sessions'));
  // Only when this was the last modal: handing focus to the terminal
  // underneath a still-open dialog sends keystrokes to the PTY behind it.
  if (anyModalOpen()) return;
  deps.refocusActiveTerm();
}

export function toggleArchivedSessions(): void {
  if (isModalOpen('archived-sessions')) closeArchivedSessions();
  else openArchivedSessions();
}

export function initArchivedSessions(injected: ArchivedSessionsDeps): void {
  deps = injected;
}
