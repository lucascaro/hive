// @vitest-environment jsdom
//
// Covers the project editor (new + edit)
// (src/components/modals/ProjectEditor.tsx, opened through the
// openProjectEditor/closeProjectEditor pair in
// src/app/modals/project-editor.ts): the new-vs-edit seed, the two
// silently-ignored bridge failures (LaunchDir, PickDirectory), the
// trim-and-save contract, and the one thing that regressed easily in
// the port — LaunchDir's cosmetic cwd default raced a keystroke in the
// legacy version (it unconditionally overwrote whatever the user had
// already typed); this pins the fixed "only when still empty" guard
// down so a careless edit can't bring the race back.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { resetStore } from '../../src/store/store.js';
import type { ProjectInfo } from '../../src/app/state.js';

const createProject = vi.fn(
  (_name: string, _color: string, _cwd: string): Promise<string> =>
    Promise.resolve('p2'),
);
const updateProject = vi.fn(
  (
    _id: string,
    _name: string,
    _color: string,
    _cwd: string,
    _order: number,
  ): Promise<void> => Promise.resolve(),
);
let launchDirImpl: () => Promise<string> = () => Promise.resolve('/launch/dir');
const launchDir = vi.fn((): Promise<string> => launchDirImpl());
const pickDirectory = vi.fn(
  (_cwd: string): Promise<string> => Promise.resolve(''),
);

vi.mock('../../src/bridge.js', () => ({
  CreateProject: (...a: Parameters<typeof createProject>) =>
    createProject(...a),
  UpdateProject: (...a: Parameters<typeof updateProject>) =>
    updateProject(...a),
  LaunchDir: (...a: Parameters<typeof launchDir>) => launchDir(...a),
  PickDirectory: (...a: Parameters<typeof pickDirectory>) =>
    pickDirectory(...a),
}));

// #terms / #projects / #status are the app singletons app/dom.ts
// resolves with mustEl at import time — ProjectEditor.tsx pulls dom.ts
// in for reportFailure, and initProjectEditor needs #new-project-btn.
// #project-editor is nested one level under #app for the same reason
// every other modal fixture nests its root: RTL's cleanup() removes a
// render() container whose parentNode IS document.body.
const MARKUP = `
  <main id="terms"></main>
  <ul id="projects"></ul>
  <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
  <div id="app">
    <button id="new-project-btn"></button>
    <div id="project-editor" class="hv-dialog hidden" role="dialog" aria-modal="true"
      aria-labelledby="project-editor-title"></div>
  </div>`;

type ProjectEditorModule =
  typeof import('../../src/app/modals/project-editor.js');
let openProjectEditor: ProjectEditorModule['openProjectEditor'];
let closeProjectEditor: ProjectEditorModule['closeProjectEditor'];
let initProjectEditor: ProjectEditorModule['initProjectEditor'];
let ProjectEditor: typeof import('../../src/components/modals/ProjectEditor.js')['ProjectEditor'];

const setFocusedTile = vi.fn();
const refocusActiveTerm = vi.fn();

function el<T extends HTMLElement = HTMLElement>(id: string): T {
  return document.getElementById(id) as T;
}
function root() {
  return el('project-editor');
}
function nameInput() {
  return el<HTMLInputElement>('project-editor-name');
}
function cwdInput() {
  return el<HTMLInputElement>('project-editor-cwd');
}
function colorInput() {
  return el<HTMLInputElement>('project-editor-color');
}
function type(input: HTMLInputElement, value: string) {
  fireEvent.change(input, { target: { value } });
}
const open = (project: ProjectInfo | null = null) =>
  act(() => openProjectEditor(project));
const click = (t: HTMLElement) => act(() => fireEvent.click(t));
// Lets a bridge promise's .then/.catch settle AND React re-render.
const flush = () =>
  act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ ProjectEditor } = await import(
    '../../src/components/modals/ProjectEditor.js'
  ));
  ({ openProjectEditor, closeProjectEditor, initProjectEditor } = await import(
    '../../src/app/modals/project-editor.js'
  ));
  initProjectEditor({ setFocusedTile, refocusActiveTerm });
});

beforeEach(() => {
  createProject.mockClear().mockResolvedValue('p2');
  updateProject.mockClear().mockResolvedValue(undefined);
  launchDirImpl = () => Promise.resolve('/launch/dir');
  launchDir.mockClear();
  pickDirectory.mockClear().mockResolvedValue('');
  setFocusedTile.mockClear();
  refocusActiveTerm.mockClear();
  resetStore();
  render(<ProjectEditor root={root()} />, { container: root() });
});

describe('new vs edit', () => {
  it('titles "New project" and starts every field blank', async () => {
    open(null);
    await flush();
    expect(el('project-editor-title').textContent).toBe('New project');
    expect(nameInput().value).toBe('');
  });

  it('titles "Edit project" and seeds name, cwd and color from the project', async () => {
    open({ id: 'p1', name: 'Hive', cwd: '/repo/hive', color: '#123456' });
    await flush();
    expect(el('project-editor-title').textContent).toBe('Edit project');
    expect(nameInput().value).toBe('Hive');
    expect(cwdInput().value).toBe('/repo/hive');
    expect(colorInput().value).toBe('#123456');
  });

  it('focuses the name field as soon as it opens', () => {
    open(null);
    expect(document.activeElement).toBe(nameInput());
  });
});

describe('new-project cwd default', () => {
  it('seeds the cwd from LaunchDir()', async () => {
    open(null);
    await flush();
    expect(cwdInput().value).toBe('/launch/dir');
  });

  // The bug the legacy version had: LaunchDir's .then() unconditionally
  // overwrote editorCwd.value, so a value typed in the window before the
  // promise resolved was silently clobbered. This is what proves the
  // port's `cur === '' ? d : cur` guard actually guards.
  it('does not overwrite a cwd the user already typed', async () => {
    let resolveLaunchDir: (d: string) => void = () => {};
    launchDirImpl = () => new Promise((r) => (resolveLaunchDir = r));
    open(null);
    type(cwdInput(), '/typed/by/user');
    await act(async () => {
      resolveLaunchDir('/launch/dir');
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(cwdInput().value).toBe('/typed/by/user');
  });

  it('ignores a rejected LaunchDir silently', async () => {
    launchDirImpl = () => Promise.reject(new Error('boom'));
    expect(() => open(null)).not.toThrow();
    await flush();
    expect(cwdInput().value).toBe('');
  });

  it('does not fetch LaunchDir when editing an existing project', async () => {
    open({ id: 'p1', name: 'Hive', cwd: '/repo/hive', color: '#123456' });
    await flush();
    expect(launchDir).not.toHaveBeenCalled();
  });
});

describe('Browse…', () => {
  it('calls PickDirectory with the current cwd and writes back a non-empty result', async () => {
    open({ id: 'p1', name: 'Hive', cwd: '/repo/hive', color: '#123456' });
    await flush();
    pickDirectory.mockResolvedValueOnce('/picked/dir');
    click(el('project-editor-browse'));
    await flush();
    expect(pickDirectory).toHaveBeenCalledWith('/repo/hive');
    expect(cwdInput().value).toBe('/picked/dir');
  });

  it('leaves the cwd alone on an empty result', async () => {
    open({ id: 'p1', name: 'Hive', cwd: '/repo/hive', color: '#123456' });
    await flush();
    pickDirectory.mockResolvedValueOnce('');
    click(el('project-editor-browse'));
    await flush();
    expect(cwdInput().value).toBe('/repo/hive');
  });

  it('leaves the cwd alone on a rejection', async () => {
    open({ id: 'p1', name: 'Hive', cwd: '/repo/hive', color: '#123456' });
    await flush();
    pickDirectory.mockRejectedValueOnce(new Error('cancelled'));
    expect(() => click(el('project-editor-browse'))).not.toThrow();
    await flush();
    expect(cwdInput().value).toBe('/repo/hive');
  });
});

describe('save', () => {
  it('trims both text fields and calls UpdateProject(id, name, color, cwd, -1) when editing', async () => {
    open({ id: 'p1', name: 'Hive', cwd: '/repo/hive', color: '#123456' });
    await flush();
    type(nameInput(), '  Renamed  ');
    type(cwdInput(), '  /new/cwd  ');
    click(el('project-editor-save'));
    expect(updateProject).toHaveBeenCalledWith(
      'p1',
      'Renamed',
      '#123456',
      '/new/cwd',
      -1,
    );
    expect(root().classList.contains('hidden')).toBe(true);
  });

  it('calls CreateProject(name, color, cwd) for a new project', async () => {
    open(null);
    await flush();
    type(nameInput(), 'Fresh');
    type(cwdInput(), '/fresh/cwd');
    click(el('project-editor-save'));
    expect(createProject).toHaveBeenCalledWith(
      'Fresh',
      expect.any(String),
      '/fresh/cwd',
    );
    expect(root().classList.contains('hidden')).toBe(true);
  });

  it('saves nothing and stays open for a blank name', () => {
    open(null);
    type(nameInput(), '   ');
    click(el('project-editor-save'));
    expect(createProject).not.toHaveBeenCalled();
    expect(updateProject).not.toHaveBeenCalled();
    expect(root().classList.contains('hidden')).toBe(false);
  });

  it('saves nothing for a fully empty name', () => {
    open(null);
    click(el('project-editor-save'));
    expect(createProject).not.toHaveBeenCalled();
    expect(root().classList.contains('hidden')).toBe(false);
  });
});

describe('Enter confirms', () => {
  it('saves from the name field', () => {
    open(null);
    type(nameInput(), 'Enter Name');
    type(cwdInput(), '/enter/cwd');
    fireEvent.keyDown(nameInput(), { key: 'Enter' });
    expect(createProject).toHaveBeenCalledWith(
      'Enter Name',
      expect.any(String),
      '/enter/cwd',
    );
  });

  it('saves from the cwd field', () => {
    open(null);
    type(nameInput(), 'Enter Cwd');
    type(cwdInput(), '/enter/cwd2');
    fireEvent.keyDown(cwdInput(), { key: 'Enter' });
    expect(createProject).toHaveBeenCalledWith(
      'Enter Cwd',
      expect.any(String),
      '/enter/cwd2',
    );
  });

  it('does nothing from the color input', () => {
    open(null);
    type(nameInput(), 'Should Not Save');
    fireEvent.keyDown(colorInput(), { key: 'Enter' });
    expect(createProject).not.toHaveBeenCalled();
    expect(root().classList.contains('hidden')).toBe(false);
  });
});

describe('cancel', () => {
  it('closes without calling either bridge function', () => {
    open(null);
    type(nameInput(), 'Discard Me');
    click(el('project-editor-cancel'));
    expect(createProject).not.toHaveBeenCalled();
    expect(updateProject).not.toHaveBeenCalled();
    expect(root().classList.contains('hidden')).toBe(true);
  });

  it('closeProjectEditor() is a no-op when nothing is open', () => {
    expect(() => act(() => closeProjectEditor())).not.toThrow();
    expect(root().classList.contains('hidden')).toBe(true);
  });
});

// Field anatomy. The imperative field primitive carried these assertions
// until the React rewrite's last phase deleted it; the markup it produced
// is now hand-written per modal, so the contract needs asserting where it
// is actually rendered. `.hv-field` + `.hv-field__label` is the CSS
// contract (src/theme/components/*.css) and the label wrapper is the a11y
// one: an <input> inside its <label> is named by it even without a `for`.
describe('field anatomy', () => {
  it('wraps every control in a labelled .hv-field', () => {
    open();
    const fields = [
      ...document.querySelectorAll<HTMLElement>('#project-editor .hv-field'),
    ];
    expect(fields.length).toBe(3);

    for (const field of fields) {
      expect(field.tagName).toBe('LABEL');
      const label = field.querySelector('.hv-field__label');
      expect(label, 'every field carries a visible label').not.toBeNull();
      expect(label?.textContent?.trim()).toBeTruthy();
      // The control is INSIDE the label, which is what names it — none of
      // these fields uses a `for`/`id` pairing.
      expect(field.querySelector('input')).not.toBeNull();
    }
  });

  it('gives every control an accessible name matching its visible label', () => {
    open();
    for (const id of [
      'project-editor-name',
      'project-editor-cwd',
      'project-editor-color',
    ]) {
      const input = document.getElementById(id) as HTMLInputElement | null;
      expect(input, `#${id} is rendered`).not.toBeNull();
      const name = input?.getAttribute('aria-label');
      expect(name, `#${id} has an accessible name`).toBeTruthy();
      const visible = input
        ?.closest('.hv-field')
        ?.querySelector('.hv-field__label')?.textContent;
      expect(name).toBe(visible?.trim());
    }
  });
});
