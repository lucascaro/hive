// ---------- quick idea capture (⌘I) ----------
//
// A small sheet: what you noticed, what kind of thing it is, and which
// project it belongs to. Enter files it, Escape drops it, and either
// way focus goes straight back to the terminal — the point of the
// feature is that filing a note does not cost you the session you were
// in.
//
// Per-open state (the three fields) is local to the body component and
// resets by remounting on `key={entry.seq}`, the same way the project
// editor's does.

import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import {
  closeQuickIdea,
  IDEA_KINDS,
  submitIdea,
  type IdeaKind,
} from '../../app/modals/quick-idea.js';
import {
  ideaTextBytes,
  ideaTextTooLong,
  MAX_IDEA_TEXT,
} from '../../lib/ideas.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { ModalShell } from './ModalShell.js';

export function QuickIdea({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'quick-idea'));

  // #quick-idea sits outside React's tree, so its open/closed class is
  // applied here — see ProjectEditor.tsx's identical layout effect.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  return (
    <QuickIdeaSheet key={entry.seq} root={root} projectId={entry.projectId} />
  );
}

function QuickIdeaSheet({
  root,
  projectId: initialProjectId,
}: {
  root: HTMLElement;
  projectId: string;
}): ReactNode {
  const projects = useAppStore((s) => s.projects);
  const activeId = useAppStore((s) => s.activeId);
  const [text, setText] = useState('');
  const [kind, setKind] = useState<IdeaKind>('idea');
  const [projectId, setProjectId] = useState(initialProjectId);
  const textRef = useRef<HTMLTextAreaElement>(null);
  // Measured in UTF-8 bytes, which is what the daemon bounds. Shown
  // only once the note is close to the limit — a byte counter on a
  // one-line note is noise.
  const bytes = ideaTextBytes(text.trim());
  const tooLong = ideaTextTooLong(text);
  const nearLimit = bytes > MAX_IDEA_TEXT * 0.9;

  // Focus in a passive effect: the root's `hidden` class comes off in
  // the parent's layout effect, and layout effects run child-first, so
  // a layout effect here would focus a field still inside a
  // display:none sheet. Same reasoning as ProjectEditor.tsx.
  useEffect(() => {
    textRef.current?.focus();
  }, []);

  function save() {
    // The filing session is a provenance breadcrumb, not a parent: the
    // idea belongs to the project and outlives whatever was focused.
    submitIdea(projectId, kind, text, activeId ?? '');
  }

  // Enter files, ⇧Enter inserts a newline — the convention spec 217
  // settled on for every multi-line input in this app. Not on the
  // dialog root like the project editor's: this field is the only
  // place Enter means "file it", and the segmented control's buttons
  // treat Enter as their own activation.
  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key !== 'Enter' || e.shiftKey) return;
    e.preventDefault();
    save();
  }

  return (
    <ModalShell
      id="quick-idea"
      root={root}
      size="sm"
      title="Capture idea"
      onClose={closeQuickIdea}
      // patterns.md › Keyboard hints: `[…]` for symbols, `(…)` for
      // letters, lowercase — same as every other modal.
      hints={[
        { keys: '[enter]', label: 'save' },
        { keys: '[⇧enter]', label: 'newline' },
        { keys: '[esc]', label: 'cancel' },
      ]}
      actions={
        <>
          <Button
            id="quick-idea-cancel"
            label="Cancel"
            onClick={closeQuickIdea}
          />
          <Button
            id="quick-idea-save"
            label="Save"
            kind="primary"
            disabled={text.trim() === '' || tooLong}
            onClick={save}
          />
        </>
      }
    >
      <>
        <label className="hv-field">
          <span className="hv-field__label">Idea</span>
          <textarea
            ref={textRef}
            id="quick-idea-text"
            className="hv-input hv-idea-text"
            rows={3}
            autoComplete="off"
            aria-label="Idea"
            placeholder="What did you notice?"
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={onKeyDown}
          />
          {nearLimit ? (
            // The daemon rejects rather than truncates, and this sheet
            // does not wait for the answer, so the limit has to be
            // visible BEFORE Save — not reported after the text is
            // gone.
            <span
              className="hv-idea-count"
              id="quick-idea-count"
              data-over={tooLong ? '' : undefined}
            >
              {bytes} / {MAX_IDEA_TEXT} bytes
            </span>
          ) : null}
        </label>
        <div className="hv-field">
          <span className="hv-field__label">Kind</span>
          {/* Real radios, visually hidden, with the label as the
              control. A row of <button role="radio"> looked the same
              and would have had to re-implement arrow-key navigation
              and the group's roving tabindex by hand; the native
              element brings both. */}
          <div className="hv-idea-kinds" id="quick-idea-kind">
            {IDEA_KINDS.map((k) => (
              <label
                key={k}
                className="hv-idea-kind"
                data-kind={k}
                data-selected={k === kind ? '' : undefined}
              >
                <input
                  type="radio"
                  name="quick-idea-kind"
                  value={k}
                  checked={k === kind}
                  onChange={() => setKind(k)}
                />
                {k}
              </label>
            ))}
          </div>
        </div>
        <label className="hv-field">
          <span className="hv-field__label">Project</span>
          <select
            id="quick-idea-project"
            className="hv-input"
            aria-label="Project"
            value={projectId}
            onChange={(e) => setProjectId(e.target.value)}
          >
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name ?? p.id}
              </option>
            ))}
          </select>
        </label>
      </>
    </ModalShell>
  );
}
