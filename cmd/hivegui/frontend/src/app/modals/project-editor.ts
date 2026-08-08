// ---------- project editor (new + edit) ----------
//
// Moved verbatim from main.js; focus callbacks injected via init.

import {
  CreateProject,
  UpdateProject,
  LaunchDir,
  PickDirectory,
} from '../../bridge.js';
import { reportFailure } from '../dom.js';
import { registerModal } from './registry.js';
import { pageEl } from '../el.js';
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

export const editorEl = pageEl('project-editor');
const editorTitle = pageEl('project-editor-title');
const editorName = pageEl<HTMLInputElement>('project-editor-name');
const editorCwd = pageEl<HTMLInputElement>('project-editor-cwd');
const editorColor = pageEl<HTMLInputElement>('project-editor-color');
// null = create; else the project being edited
const editorState: { editing: ProjectInfo | null } = { editing: null };

export function openProjectEditor(project: ProjectInfo | null) {
  editorState.editing = project || null;
  editorTitle.textContent = project ? 'Edit project' : 'New project';
  editorName.value = project?.name ?? '';
  editorColor.value = project?.color || '#f59e0b';
  if (project) {
    editorCwd.value = project.cwd ?? '';
  } else {
    // Intentionally silent: cosmetic default for an empty field;
    // Browse… still works if this fails.
    LaunchDir()
      .then((d) => {
        editorCwd.value = d || '';
      })
      .catch(() => {});
    editorCwd.value = '';
  }
  editorEl.classList.remove('hidden');
  // Drop the active tile's visual focus — modal owns the keyboard.
  deps.setFocusedTile(null);
  setTimeout(() => editorName.focus(), 0);
}

export function closeProjectEditor() {
  editorEl.classList.add('hidden');
  editorState.editing = null;
  deps.refocusActiveTerm();
}

function saveProjectEditor() {
  const name = editorName.value.trim();
  const cwd = editorCwd.value.trim();
  const color = editorColor.value;
  if (!name) return;
  if (editorState.editing) {
    UpdateProject(editorState.editing.id, name, color, cwd, -1).catch(
      reportFailure('save project'),
    );
  } else {
    CreateProject(name, color, cwd).catch(reportFailure('create project'));
  }
  closeProjectEditor();
}

export function initProjectEditor(injected: ProjectEditorDeps) {
  deps = injected;
  registerModal(editorEl);
  pageEl('project-editor-cancel').addEventListener('click', closeProjectEditor);
  pageEl('project-editor-save').addEventListener('click', saveProjectEditor);
  pageEl('project-editor-browse').addEventListener('click', async () => {
    try {
      const picked = await PickDirectory(editorCwd.value || '');
      if (picked) editorCwd.value = picked;
    } catch (_err) {
      // Silently ignore (user cancelled, or platform refused).
    }
  });
  editorEl.addEventListener('keydown', (e) => {
    if (
      e.key === 'Enter' &&
      (e.target === editorName || e.target === editorCwd)
    ) {
      e.preventDefault();
      saveProjectEditor();
    } else if (e.key === 'Escape') {
      closeProjectEditor();
    }
  });
  pageEl('new-project-btn').addEventListener('click', () =>
    openProjectEditor(null),
  );
}
