// ---------- project editor (new + edit): the non-React half ----------
//
// The dialog renders from components/modals/ProjectEditor.tsx (Phase 4).
// What stays here is the open/close pair every caller already imports
// from this path (keyboard.ts, main.tsx, the sidebar, the empty state and
// the command palette), the default colour both halves need, and the
// click wiring for the new-project button, which lives in index.html's
// header rather than in the dialog.

import { flushSync } from 'react-dom';
import { closeModal, isModalOpen, openModal } from '../../store/store.js';
import { pageEl } from '../el.js';
import { releaseFocus } from '../../lib/focus-trap.js';
import type { ProjectInfo } from '../state.js';

// Narrow on purpose: this modal needs exactly two callbacks off the
// focus pipeline, so it names those two rather than the whole module.
export interface ProjectEditorDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
}

let deps: ProjectEditorDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

export const DEFAULT_PROJECT_COLOR = '#f59e0b';

export function openProjectEditor(project: ProjectInfo | null) {
  // Re-opening replaces the entry, which remounts the body on the new
  // `seq` — the fields have to show the project just asked for, not the
  // draft from the last opening.
  openModal({ id: 'project-editor', editing: project || null });
  // Drop the active tile's visual focus — the dialog owns the keyboard,
  // and the component focuses the name field on mount.
  deps.setFocusedTile(null);
}

export function closeProjectEditor() {
  if (!isModalOpen('project-editor')) return;
  // Before the unmount: the focus pipeline bails when activeElement is
  // an INPUT, and unmounting does not synchronously move focus out of
  // the dialog in every engine.
  releaseFocus(pageEl('project-editor'));
  // flushSync because this is reached from plain listeners (keyboard.ts's
  // window handler, the shell's Escape); a microtask-later write would
  // leave the dialog on screen when refocusActiveTerm() runs.
  flushSync(() => closeModal('project-editor'));
  deps.refocusActiveTerm();
}

export function initProjectEditor(injected: ProjectEditorDeps) {
  deps = injected;
  // The button's icon is rendered by components/Sidebar.tsx's
  // SidebarHeaderControls; what stays here is the click.
  pageEl('new-project-btn').addEventListener('click', () =>
    openProjectEditor(null),
  );
}
