// ---------- quick idea capture (⌘I): the non-React half ----------
//
// The sheet renders from components/modals/QuickIdea.tsx. What stays
// here is the open/close pair its callers import (keyboard.ts,
// main.tsx, the project card) and the one daemon call it makes.
//
// Same shape as project-editor.ts, deliberately: every modal in this
// app is a state module plus a component, and the two halves are how
// keyboard.ts closes a dialog it does not render.

import { flushSync } from 'react-dom';
import { AddIdea } from '../../bridge.js';
import { flashStatus, reportFailure } from '../dom.js';
import {
  anyModalOpen,
  closeModal,
  isModalOpen,
  openModal,
} from '../../store/store.js';
import { pageEl } from '../el.js';
import { releaseFocus } from '../../lib/focus-trap.js';
import { activeProjectId } from '../selectors.js';
import { ideaTextTooLong, MAX_IDEA_TEXT } from '../../lib/ideas.js';

// Narrow on purpose, matching project-editor.ts.
export interface QuickIdeaDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
}

let deps: QuickIdeaDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

// The kinds the daemon accepts (internal/wire/control.go IdeaKinds).
// Order is the segmented control's order; the first is the default.
export const IDEA_KINDS = ['idea', 'bug', 'feedback'] as const;
export type IdeaKind = (typeof IDEA_KINDS)[number];

export function openQuickIdea(projectId?: string): void {
  // activeProjectId() is the same resolution every other
  // project-scoped action uses, and it already ends at the first
  // project — which is the default project — when nothing is focused.
  // So capture works with no session open, which is most of the point.
  openModal({ id: 'quick-idea', projectId: projectId || activeProjectId() });
  deps.setFocusedTile(null);
}

export function closeQuickIdea(): void {
  if (!isModalOpen('quick-idea')) return;
  // Before the unmount, or focus is left on a removed element and the
  // browser resolves it to <body> — stranding the keyboard.
  releaseFocus(pageEl('quick-idea'));
  // flushSync: reached from plain listeners (keyboard.ts's window
  // handler, the sheet's own Escape), and refocusActiveTerm() must not
  // run while the sheet is still on screen.
  flushSync(() => closeModal('quick-idea'));
  // Only when this was the last modal. The sheet can be opened over
  // another one, and handing focus to the terminal underneath a still-
  // open dialog sends the user's next keystrokes to the PTY behind it.
  if (anyModalOpen()) return;
  deps.refocusActiveTerm();
}

// submitIdea files the note and closes. The daemon answers with an
// IDEA_EVENT(added) fanned out to every window, so nothing is patched
// locally — the sheet does not wait for it either, because a capture
// that blocks on a round trip is a capture that interrupts.
export function submitIdea(
  projectId: string,
  kind: IdeaKind,
  text: string,
  sessionId: string,
): void {
  const trimmed = text.trim();
  if (!trimmed) return;
  // The daemon rejects an oversize note (idea_too_long) rather than
  // truncating it, and this does not wait for the answer before
  // closing — so filing one anyway would lose what the user typed.
  // Refuse here instead and leave the sheet up with the text in it.
  if (ideaTextTooLong(trimmed)) {
    flashStatus(`idea is too long (max ${MAX_IDEA_TEXT / 1024} KiB)`, true);
    return;
  }
  AddIdea(sessionId, projectId, kind, trimmed).catch(reportFailure('add idea'));
  closeQuickIdea();
}

export function initQuickIdea(injected: QuickIdeaDeps): void {
  deps = injected;
}
