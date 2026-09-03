// ---------- choice dialog ----------
//
// React port of src/app/modals/choice-dialog.ts. That module still owns
// the promise every caller awaits (openChoiceDialog); this renders the
// question and reports the answer back through resolveChoiceDialog —
// the only path that may settle it, so Escape and the backdrop resolve
// through ModalShell's onClose rather than hiding the dialog themselves.

import { useEffect, useLayoutEffect, useRef, type ReactNode } from 'react';
import {
  resolveChoiceDialog,
  type ChoiceSpec,
} from '../../app/modals/choice-dialog.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { ModalShell } from './ModalShell.js';

export function ChoiceDialog({
  root,
}: {
  root: HTMLElement | null;
}): ReactNode {
  const entry = useAppStore((s) => s.choiceDialog);

  // #choice-dialog sits outside React's tree, so its open/closed classes
  // are applied here — see Settings.tsx's identical layout effect.
  //
  // Two classes, not one. `.choice-dialog` is the z-index deviation that
  // puts this dialog over the modal that asked the question, and it is
  // also what the e2e specs count to assert the question is gone: before
  // Phase 4 the element itself was built per question and removed on the
  // answer, so "no .choice-dialog in the DOM" meant "nothing is being
  // asked". The root is static now; the class carries that meaning.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
    root?.classList.toggle('choice-dialog', !!entry);
  }, [root, entry]);

  if (!entry || !root) return null;
  // Remounted per question, which is what resets focus-restore capture
  // for a re-ask.
  return <ChoiceDialogBody key={entry.seq} root={root} spec={entry.spec} />;
}

function ChoiceDialogBody({
  root,
  spec,
}: {
  root: HTMLElement;
  spec: ChoiceSpec;
}): ReactNode {
  const safe = spec.choices[0];

  // Whatever had focus when the question was asked. Restored on
  // unmount, but only if it is still on the page: the button that
  // raised the question is often the one the answer removes (deleting a
  // worktree takes its row with it), and focusing a detached node
  // strands the keyboard.
  const openerRef = useRef<HTMLElement | null>(null);
  useLayoutEffect(() => {
    openerRef.current = document.activeElement as HTMLElement | null;
    return () => {
      if (openerRef.current?.isConnected) openerRef.current.focus();
    };
  }, []);

  // The safe option holds focus, so a stray Enter can never destroy
  // anything. Button is a plain function component (no ref forwarding),
  // so the first choice is found the same way the shell finds anything
  // outside React's tree: by the id/data contract, off the root.
  //
  // A PASSIVE effect on purpose. The root's `hidden` class is dropped by
  // the parent island's layout effect, and layout effects run child
  // first — so focusing from one here would run while the dialog was
  // still display:none, which the browser ignores outright, leaving
  // focus (and Tab) on the modal underneath.
  useEffect(() => {
    root
      .querySelector<HTMLButtonElement>(
        '.hv-dialog__actions button[data-choice]',
      )
      ?.focus();
  }, [root]);

  return (
    <ModalShell
      id="choice-dialog"
      root={root}
      size="sm"
      title={spec.title}
      showCloseButton={false}
      // The FIRST choice is the safe one: Escape and a backdrop click
      // resolve to it, so a stray key can never destroy anything.
      onClose={() => resolveChoiceDialog(safe.value)}
      actions={spec.choices.map((c) => (
        <Button
          key={c.value}
          label={c.label}
          kind={c.danger ? 'danger' : 'default'}
          className={c.danger ? 'danger' : undefined}
          extra={{ 'data-choice': c.value }}
          onClick={() => resolveChoiceDialog(c.value)}
        />
      ))}
    >
      {spec.detail ? (
        <p className="choice-dialog-detail">{spec.detail}</p>
      ) : null}
      {spec.bullets?.length ? (
        <ul className="choice-dialog-bullets">
          {spec.bullets.map((b) => (
            <li key={b}>{b}</li>
          ))}
        </ul>
      ) : null}
      {spec.note ? <p className="choice-dialog-note">{spec.note}</p> : null}
    </ModalShell>
  );
}
