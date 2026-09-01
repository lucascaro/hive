// ---------- project editor (new + edit) ----------
//
// Built here rather than declared in index.html so the dialog and field
// primitives own the markup; focus callbacks injected via init.

import {
  CreateProject,
  LaunchDir,
  PickDirectory,
  UpdateProject,
} from '../../bridge.js';
import { button } from '../../ui/button.js';
import { dialog } from '../../ui/dialog.js';
import { colorInput, field, textInput } from '../../ui/field.js';
import { reportFailure } from '../dom.js';
import { pageEl } from '../el.js';
import { icon } from '../../ui/icon.js';
import { releaseFocus } from './focus-trap.js';
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

const DEFAULT_PROJECT_COLOR = '#f59e0b';

const editorName = textInput({
  id: 'project-editor-name',
  ariaLabel: 'Name',
});
const editorCwd = textInput({
  id: 'project-editor-cwd',
  ariaLabel: 'Working directory',
});
const browseBtn = button({
  label: 'Browse…',
  onClick: () => pickCwd(),
});
browseBtn.id = 'project-editor-browse';
const color = colorInput({
  value: DEFAULT_PROJECT_COLOR,
  ariaLabel: 'Color',
});
const editorColor = color.input;
editorColor.id = 'project-editor-color';

const cwdRow = document.createElement('div');
cwdRow.className = 'cwd-row';
cwdRow.append(editorCwd, browseBtn);

const cancelBtn = button({
  label: 'Cancel',
  onClick: () => closeProjectEditor(),
});
cancelBtn.id = 'project-editor-cancel';
const saveBtn = button({
  label: 'Save',
  kind: 'primary',
  onClick: () => saveProjectEditor(),
});
saveBtn.id = 'project-editor-save';

const dlg = dialog({
  id: 'project-editor',
  title: 'New project',
  size: 'sm',
  body: [
    field('Name', editorName),
    field('Working directory', cwdRow),
    field('Color', color.el),
  ],
  actions: [cancelBtn, saveBtn],
  onClose: () => closeProjectEditor(),
});
// keyboard.ts and the focus pipeline key off this element.
export const editorEl = dlg.el;

// null = create; else the project being edited
const editorState: { editing: ProjectInfo | null } = { editing: null };

function pickCwd() {
  PickDirectory(editorCwd.value || '')
    .then((picked) => {
      if (picked) editorCwd.value = picked;
    })
    .catch(() => {
      // Silently ignore (user cancelled, or platform refused).
    });
}

export function openProjectEditor(project: ProjectInfo | null) {
  editorState.editing = project || null;
  dlg.setTitle(project ? 'Edit project' : 'New project');
  editorName.value = project?.name ?? '';
  editorColor.value = project?.color || DEFAULT_PROJECT_COLOR;
  color.el.style.setProperty('--swatch', editorColor.value);
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
  dlg.show();
  // Drop the active tile's visual focus — modal owns the keyboard.
  deps.setFocusedTile(null);
  // Focus synchronously. The field is already visible by now (.hidden
  // came off above), so there is nothing to wait for — and deferring
  // opened a race: ⌘N immediately followed by Escape closed the modal
  // first, then the timer put focus on a field inside a display:none
  // dialog, sending every keystroke somewhere the user cannot see.
  editorName.focus();
}

export function closeProjectEditor() {
  // Before hide(): the primitive does not do this, deliberately — only
  // this module knows where focus is going next.
  releaseFocus(editorEl);
  dlg.hide();
  editorState.editing = null;
  deps.refocusActiveTerm();
}

function saveProjectEditor() {
  const name = editorName.value.trim();
  const cwd = editorCwd.value.trim();
  const colorValue = editorColor.value;
  if (!name) return;
  if (editorState.editing) {
    UpdateProject(editorState.editing.id, name, colorValue, cwd, -1).catch(
      reportFailure('save project'),
    );
  } else {
    CreateProject(name, colorValue, cwd).catch(reportFailure('create project'));
  }
  closeProjectEditor();
}

export function initProjectEditor(injected: ProjectEditorDeps) {
  deps = injected;
  document.getElementById('app')?.append(editorEl);
  // Enter saves. Escape and the backdrop are the dialog primitive's.
  editorEl.addEventListener('keydown', (e) => {
    if (
      e.key === 'Enter' &&
      (e.target === editorName || e.target === editorCwd)
    ) {
      e.preventDefault();
      saveProjectEditor();
    }
  });
  const newProjectBtn = pageEl('new-project-btn');
  newProjectBtn.replaceChildren(icon('plus'));
  newProjectBtn.addEventListener('click', () => openProjectEditor(null));
}
