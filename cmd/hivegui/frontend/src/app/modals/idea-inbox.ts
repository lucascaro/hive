// ---------- idea inbox (⇧⌘I): the non-React half ----------
//
// Per-project list of the ideas captured for it. The panel renders from
// components/modals/IdeaInbox.tsx; what stays here is the open/close
// pair its callers import (keyboard.ts, main.tsx, the project card's
// badge), the boot fetch, and the row mutations.
//
// Nothing here patches the store: every mutation is answered by an
// IDEA_EVENT fanned out to every window, and app/events.ts applies it.
// A local patch would be a second writer to keep in agreement.

import { flushSync } from 'react-dom';
import { ListIdeas, RemoveIdea, UpdateIdea } from '../../bridge.js';
import { flashStatus, reportFailure } from '../dom.js';
import { ideaTextTooLong, MAX_IDEA_TEXT } from '../../lib/ideas.js';
import { openChoiceDialog, dismissChoiceDialog } from './choice-dialog.js';
import {
  anyModalOpen,
  closeModal,
  isModalOpen,
  modalEntry,
  openModal,
} from '../../store/store.js';
import { pageEl } from '../el.js';
import { releaseFocus } from '../../lib/focus-trap.js';
import type { IdeaInfo, ProjectInfo } from '../state.js';

export interface IdeaInboxDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
}

let deps: IdeaInboxDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

// refreshIdeas asks for every project's ideas. Called once per control
// connection (boot and each reconnect): the daemon's initial snapshot
// carries projects and sessions but not ideas, and after this one
// request the IDEA_EVENT fan-out keeps the store current.
export function refreshIdeas(): void {
  ListIdeas('').catch(() => {
    // A daemon too old to know the frame logs "unexpected control
    // frame" and never answers. That degrades to an empty inbox, which
    // app_control.go already surfaces as the daemon:stale banner —
    // there is nothing useful to say here that that banner does not.
  });
}

// ideaInboxProjectId is the project the open panel is showing, or ''.
// The destructive flow re-reads it after its dialog resolves.
export function ideaInboxProjectId(): string {
  return modalEntry('idea-inbox')?.projectId ?? '';
}

export function openIdeaInbox(project: ProjectInfo | null): void {
  if (!project) {
    flashStatus('no project selected', true);
    return;
  }
  openModal({
    id: 'idea-inbox',
    projectId: project.id,
    projectName: project.name ?? '',
  });
  deps.setFocusedTile(null);
}

export function closeIdeaInbox(): void {
  if (!isModalOpen('idea-inbox')) return;
  // A question about a row in this panel outlives the panel otherwise.
  dismissChoiceDialog();
  releaseFocus(pageEl('idea-inbox'));
  flushSync(() => closeModal('idea-inbox'));
  // Only when this was the last modal — ⇧⌘I from the capture sheet
  // closes the sheet and opens this, and ⌘I from here closes this and
  // opens the sheet. Handing focus to the terminal under a dialog that
  // is still up sends the next keystrokes to the PTY behind it.
  if (anyModalOpen()) return;
  deps.refocusActiveTerm();
}

// editIdeaText commits an inline edit. The editor is kept open on a
// refused value by `validate` (see IdeaInbox.tsx), not by this return —
// what stays here is the module-boundary guard, for callers that reach
// it without one. The boolean reports whether anything was sent.
export function editIdeaText(id: string, text: string): boolean {
  const trimmed = text.trim();
  // Empty is not a delete: Delete is its own row action, behind a
  // confirm, and a blur on an emptied field must not destroy the note.
  if (!trimmed) return false;
  // The same 4 KiB cap the capture sheet enforces. The daemon applies
  // it to the update path too (registry/ideas.go), rejecting rather
  // than truncating, and this does not await the answer — so without
  // the check the editor tears down, the row reverts to the stale
  // text, and the edit is gone.
  if (ideaTextTooLong(trimmed)) {
    flashStatus(`idea is too long (max ${MAX_IDEA_TEXT / 1024} KiB)`, true);
    return false;
  }
  UpdateIdea(id, trimmed, '', '').catch(reportFailure('edit idea'));
  return true;
}

export function markIdeaDone(idea: IdeaInfo): void {
  UpdateIdea(idea.id, '', 'done', '').catch(reportFailure('mark idea done'));
}

// confirmAndDeleteIdea removes one idea outright. Confirmed because the
// text is the whole record — there is no undo and nothing else holds a
// copy of it.
export async function confirmAndDeleteIdea(idea: IdeaInfo): Promise<void> {
  const answer = await openChoiceDialog({
    title: 'Delete this idea?',
    detail: idea.text,
    note: 'Deleting discards the note. This cannot be undone — “Done” keeps it and takes it out of the inbox.',
    choices: [
      { label: 'Cancel', value: 'cancel' },
      { label: 'Delete', value: 'delete', danger: true },
    ],
  });
  if (answer !== 'delete') return;
  RemoveIdea(idea.id).catch(reportFailure('delete idea'));
}

export function initIdeaInbox(injected: IdeaInboxDeps): void {
  deps = injected;
}
