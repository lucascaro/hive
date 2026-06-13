// ---------- project editor (new + edit) ----------
//
// Moved verbatim from main.js; focus callbacks injected via init.

import {
  CreateProject, UpdateProject, LaunchDir, PickDirectory,
} from '../../bridge.js';
import { reportFailure } from '../dom.js';
import { registerModal } from './registry.js';

let deps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

export const editorEl = document.getElementById('project-editor');
const editorTitle = document.getElementById('project-editor-title');
const editorName = document.getElementById('project-editor-name');
const editorCwd = document.getElementById('project-editor-cwd');
const editorColor = document.getElementById('project-editor-color');
const editorState = { editing: null }; // null = create; else project object

export function openProjectEditor(project) {
  editorState.editing = project || null;
  editorTitle.textContent = project ? 'Edit project' : 'New project';
  editorName.value = project?.name ?? '';
  editorColor.value = project?.color || '#f59e0b';
  if (project) {
    editorCwd.value = project.cwd ?? '';
  } else {
    // Intentionally silent: cosmetic default for an empty field;
    // Browse… still works if this fails.
    LaunchDir().then((d) => { editorCwd.value = d || ''; }).catch(() => {});
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
    UpdateProject(editorState.editing.id, name, color, cwd, -1).catch(reportFailure('save project'));
  } else {
    CreateProject(name, color, cwd).catch(reportFailure('create project'));
  }
  closeProjectEditor();
}

export function initProjectEditor(injected) {
  deps = injected;
  registerModal(editorEl);
  document.getElementById('project-editor-cancel').addEventListener('click', closeProjectEditor);
  document.getElementById('project-editor-save').addEventListener('click', saveProjectEditor);
  document.getElementById('project-editor-browse').addEventListener('click', async () => {
    try {
      const picked = await PickDirectory(editorCwd.value || '');
      if (picked) editorCwd.value = picked;
    } catch (err) {
      // Silently ignore (user cancelled, or platform refused).
    }
  });
  editorEl.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.target === editorName || e.target === editorCwd)) {
      e.preventDefault();
      saveProjectEditor();
    } else if (e.key === 'Escape') {
      closeProjectEditor();
    }
  });
  document.getElementById('new-project-btn').addEventListener('click', () => openProjectEditor(null));
}
