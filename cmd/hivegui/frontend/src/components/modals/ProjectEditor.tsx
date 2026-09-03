// ---------- project editor (new + edit) ----------
//
// React port of src/app/modals/project-editor.ts. The ids the keyboard
// pipeline and the e2e specs key off are set explicitly here and are
// part of this component's contract — see field.ts, whose markup this
// reproduces by hand rather than by calling it (that primitive builds
// plain DOM nodes, not JSX).
//
// Per-open state (the three fields) is local to the body component and
// resets by remounting on `key={entry.seq}` — a reopen starts from the
// project just asked for, not the last draft.

import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {
  CreateProject,
  LaunchDir,
  PickDirectory,
  UpdateProject,
} from '../../bridge.js';
import { reportFailure } from '../../app/dom.js';
import {
  closeProjectEditor,
  DEFAULT_PROJECT_COLOR,
} from '../../app/modals/project-editor.js';
import type { ProjectInfo } from '../../app/state.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { ModalShell } from './ModalShell.js';

export function ProjectEditor({
  root,
}: {
  root: HTMLElement | null;
}): ReactNode {
  const entry = useAppStore((s) =>
    s.modals.find((m) => m.id === 'project-editor'),
  );

  // #project-editor sits outside React's tree, so its open/closed class
  // is applied here — see Settings.tsx's identical layout effect.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  return (
    <ProjectEditorDialog key={entry.seq} root={root} editing={entry.editing} />
  );
}

function ProjectEditorDialog({
  root,
  editing,
}: {
  root: HTMLElement;
  editing: ProjectInfo | null;
}): ReactNode {
  const [name, setName] = useState(editing?.name ?? '');
  const [cwd, setCwd] = useState(editing?.cwd ?? '');
  const [color, setColor] = useState(editing?.color || DEFAULT_PROJECT_COLOR);
  const nameRef = useRef<HTMLInputElement>(null);
  const cwdRef = useRef<HTMLInputElement>(null);

  // Focus in a passive effect, which is the earliest point the field is
  // actually focusable: the root's `hidden` class comes off in the
  // parent modal's layout effect, and layout effects run child first,
  // so a layout effect here would call focus() on a field still inside a
  // display:none dialog and the browser would drop it on the floor.
  // This is still the same commit — nothing like the setTimeout the
  // imperative version used, which lost the ⌘N-then-Escape race and put
  // the caret in a dialog the user had already closed.
  useEffect(() => {
    nameRef.current?.focus();
  }, []);

  // Cosmetic default for a new project's empty cwd field; Browse… still
  // works if this fails, so the rejection is silently ignored. Guarded
  // against both an unmount (re-open, close) and a keystroke that beat
  // the daemon to the field — either means the field is no longer
  // "still empty because nothing has touched it yet".
  // biome-ignore lint/correctness/useExhaustiveDependencies: see below
  useEffect(() => {
    if (editing) return;
    let live = true;
    LaunchDir()
      .then((d) => {
        if (live) setCwd((cur) => (cur === '' ? d || '' : cur));
      })
      .catch(() => {});
    return () => {
      live = false;
    };
    // `editing` is read once, at open: whether this is a create or an
    // edit never changes for the life of this mount (which is remounted
    // wholesale, key={entry.seq}, on every reopen).
  }, []);

  function pickCwd() {
    PickDirectory(cwd)
      .then((picked) => {
        if (picked) setCwd(picked);
      })
      .catch(() => {
        // Silently ignore (user cancelled, or platform refused).
      });
  }

  // The listener above is attached once per open; this keeps it calling
  // the save that closes over the current field values.
  const saveRef = useRef(() => {});
  saveRef.current = save;

  function save() {
    const trimmedName = name.trim();
    const trimmedCwd = cwd.trim();
    if (!trimmedName) return;
    if (editing) {
      UpdateProject(editing.id, trimmedName, color, trimmedCwd, -1).catch(
        reportFailure('save project'),
      );
    } else {
      CreateProject(trimmedName, color, trimmedCwd).catch(
        reportFailure('create project'),
      );
    }
    closeProjectEditor();
  }

  // Enter saves, but only from the two text fields — the colour input
  // and the buttons treat Enter as their own activation. On the dialog
  // root, where initProjectEditor() attached it, so the body's markup
  // stays exactly the three field rows the CSS expects.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== 'Enter') return;
      if (e.target !== nameRef.current && e.target !== cwdRef.current) return;
      e.preventDefault();
      saveRef.current();
    }
    root.addEventListener('keydown', onKeyDown);
    return () => root.removeEventListener('keydown', onKeyDown);
  }, [root]);

  return (
    <ModalShell
      id="project-editor"
      root={root}
      size="sm"
      title={editing ? 'Edit project' : 'New project'}
      onClose={closeProjectEditor}
      actions={
        <>
          <Button
            id="project-editor-cancel"
            label="Cancel"
            onClick={closeProjectEditor}
          />
          <Button
            id="project-editor-save"
            label="Save"
            kind="primary"
            onClick={save}
          />
        </>
      }
    >
      <>
        <label className="hv-field">
          <span className="hv-field__label">Name</span>
          <input
            ref={nameRef}
            id="project-editor-name"
            className="hv-input"
            type="text"
            autoComplete="off"
            aria-label="Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label className="hv-field">
          <span className="hv-field__label">Working directory</span>
          <div className="cwd-row">
            <input
              ref={cwdRef}
              id="project-editor-cwd"
              className="hv-input"
              type="text"
              autoComplete="off"
              aria-label="Working directory"
              value={cwd}
              onChange={(e) => setCwd(e.target.value)}
            />
            <Button
              id="project-editor-browse"
              label="Browse…"
              onClick={pickCwd}
            />
          </div>
        </label>
        <label className="hv-field">
          <span className="hv-field__label">Color</span>
          <span className="hv-swatch" style={{ ['--swatch' as string]: color }}>
            <input
              id="project-editor-color"
              type="color"
              aria-label="Color"
              value={color}
              onChange={(e) => setColor(e.target.value)}
            />
          </span>
        </label>
      </>
    </ModalShell>
  );
}
